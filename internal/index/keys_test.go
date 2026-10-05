package index

import (
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// keys 칸(C1)과 A1 뒷정리 둘(보류 셈 · 덮인 고정)의 색인 쪽 시험이다.

func keysRows(t *testing.T, database *DB) int {
	t.Helper()
	count := 0
	if err := database.SQL().QueryRow("SELECT COUNT(*) FROM keys_ko").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func openIndex(t *testing.T, opened *store.Store) *DB {
	t.Helper()
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// keys 는 머리말에 남고 keys 표에 한 줄 선다. set 으로 갈면 옛 줄이 안 남고,
// 비우면 줄이 사라진다.
func TestKeysIndexedAndReplaced(t *testing.T) {
	opened := newStore(t)
	request := addRequest(longSummary, "본문 한 줄")
	request.Keys = []string{"reindex", "재빌드절차"}
	name, err := opened.WriteAdd(request)
	if err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	id := model.QueueID(name, "본문 한 줄", today())
	file, err := opened.ReadMemory(model.StorePath(id))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Memory.Keys) != 2 || file.Memory.Keys[0] != "reindex" {
		t.Fatalf("머리말에 keys 가 안 남았다 : %v", file.Memory.Keys)
	}
	database := openIndex(t, opened)
	if keysRows(t, database) != 1 || !database.HasKeys() {
		t.Fatal("keys 표에 줄이 안 섰다")
	}
	if found, _ := database.MatchKeys("keys_en", `"reindex"`, 5); len(found) != 1 {
		t.Fatalf("keys_en 에서 영문 key 를 못 찾는다 : %v", found)
	}
	if found, _ := database.MatchKO(`"reindex"`, 5); len(found) != 0 {
		t.Fatal("keys 가 큰 표(fts_ko)에 샜다 — keys 가 빈 기억 점수가 바뀐다")
	}
	database.Close()

	if _, err := opened.WritePatch(id, map[string]any{"keys": []any{"재색인"}}); err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	database = openIndex(t, opened)
	if keysRows(t, database) != 1 {
		t.Fatal("keys 를 갈았는데 줄이 겹쳐 쌓였다")
	}
	if found, _ := database.MatchKeys("keys_en", `"reindex"`, 5); len(found) != 0 {
		t.Fatal("옛 key 가 keys 표에 남았다")
	}
	database.Close()

	if _, err := opened.WritePatch(id, map[string]any{"keys": []any{}}); err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	database = openIndex(t, opened)
	if keysRows(t, database) != 0 || database.HasKeys() {
		t.Fatal("keys 를 비웠는데 줄이 남았다")
	}
}

// keys 꼴이 틀리면 승격이 bad 로 보낸다 (7개 · 한 글자 · 쉼표).
func TestKeysShapeRejected(t *testing.T) {
	for _, keys := range [][]string{{"a1", "b2", "c3", "d4", "e5", "f6", "g7"}, {"한"}, {"가,나"}} {
		opened := newStore(t)
		request := addRequest(longSummary, "본문 한 줄")
		request.Keys = keys
		if _, err := opened.WriteAdd(request); err != nil {
			t.Fatal(err)
		}
		if result := runIndex(t, opened); result.Bad != 1 {
			t.Fatalf("keys %v 가 bad 로 안 갔다 : %+v", keys, result)
		}
	}
}

// 합치기는 keys 를 뒤에 붙이되 같은 말은 한 번, 6개에서 자른다.
func TestMergeKeys(t *testing.T) {
	got := mergeKeys([]string{"가나", "다라"}, []string{"다라", "a1", "b2", "c3", "d4", "e5"})
	if len(got) != model.KeyMax || got[1] != "다라" || got[2] != "a1" {
		t.Fatalf("합친 keys 가 틀렸다 : %v", got)
	}
	if mergeKeys(nil, nil) != nil {
		t.Fatal("둘 다 비었으면 칸도 비어야 한다")
	}
}

// A1 뒷정리 — 보류 기억은 「낱말이 N건 있다」 셈과 닮은 낱말 후보에서 빠진다.
func TestLiveWordCountSkipsHeld(t *testing.T) {
	opened := newStore(t)
	request := addRequest(longSummary, "보류된 기억에만 있는 낱말 젓가락질")
	request.Review = true
	if _, err := opened.WriteAdd(request); err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	database := openIndex(t, opened)
	expr := `"젓가" "가락" "락질"`
	if database.WordCount("fts_ko", expr) != 1 {
		t.Fatal("시험 자료가 틀렸다 — 보류 기억이 색인에 없다")
	}
	if got := database.LiveWordCount("fts_ko", expr); got != 0 {
		t.Fatalf("보류 기억을 셌다 : %d", got)
	}
	words, err := database.HeadWordsLike("젓가", 3)
	if err != nil || len(words) != 0 {
		t.Fatalf("보류 기억의 낱말을 후보로 냈다 : %v %v", words, err)
	}
}

// A1 뒷정리 — 덮인 기억은 pinned 가 남아 있어도 「고정 N건」 에 안 든다.
func TestPinnedCountSkipsSuperseded(t *testing.T) {
	opened := newStore(t)
	request := addRequest(longSummary, "고정했다가 덮인 결정")
	request.Pinned = true
	name, err := opened.WriteAdd(request)
	if err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	database := openIndex(t, opened)
	if count, _ := database.PinnedCount(); count != 1 {
		t.Fatalf("고정 셈이 틀렸다 : %d", count)
	}
	database.Close()
	id := model.QueueID(name, "고정했다가 덮인 결정", today())
	if _, err := opened.WritePatch(id, map[string]any{"superseded_by": "20260901-aa11bb22",
		"invalid_at": today()}); err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	database = openIndex(t, opened)
	if count, _ := database.PinnedCount(); count != 0 {
		t.Fatalf("덮인 기억을 고정으로 셌다 : %d", count)
	}
}
