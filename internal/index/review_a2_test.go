package index

import (
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 리뷰 A(2회차) H-1 — store/ 의 md 를 손으로 고쳐 넣은 비밀정보는 색인을
// 통과하면 안 된다. 승격 자리(add·set)만 막고 있어서 훅·search·git 으로 샜다.
func TestHandEditedSecretIsNotIndexed(t *testing.T) {
	opened := newStore(t)
	name, err := opened.WriteAdd(addRequest(longSummary, "본문 한 줄이다"))
	if err != nil {
		t.Fatal(err)
	}
	if result := runIndex(t, opened); result.Indexed != 1 {
		t.Fatalf("먼저 한 건이 들어가야 한다 : %+v", result)
	}
	id := model.QueueID(name, "본문 한 줄이다", today())
	file, err := opened.ReadMemory(model.StorePath(id))
	if err != nil {
		t.Fatal(err)
	}
	// 사람이 파일을 손으로 고친 것과 같은 자리다.
	file.Memory.Body = "본문 한 줄이다\n배포에는 ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123 을 쓴다"
	if err := opened.WriteMemory(file.Memory); err != nil {
		t.Fatal(err)
	}
	result := runIndex(t, opened)
	if result.Bad != 1 || len(result.Unindexed) != 1 {
		t.Fatalf("비밀정보가 든 md 를 색인에서 빼야 한다 : %+v", result)
	}
	// 파일은 안 고치고 안 옮긴다 (불변조건 I7).
	if _, err := opened.ReadMemory(model.StorePath(id)); err != nil {
		t.Fatalf("도구가 남의 파일을 건드렸다 : %v", err)
	}
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	count, err := database.Count()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("전에 들어간 행까지 빠져야 한다 : %d", count)
	}
}

// 요약 칸에 넣은 것도 같이 막힌다 — 훅 블록에 실리는 것이 바로 요약이다.
func TestHandEditedSecretInSummaryIsNotIndexed(t *testing.T) {
	opened := newStore(t)
	name, err := opened.WriteAdd(addRequest(longSummary, "본문 한 줄이다"))
	if err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	id := model.QueueID(name, "본문 한 줄이다", today())
	file, err := opened.ReadMemory(model.StorePath(id))
	if err != nil {
		t.Fatal(err)
	}
	file.Memory.Summary = "배포 열쇠는 AKIAIOSFODNN7EXAMPLE 이고 이 줄은 색인에 들어가면 안 되는 요약이다"
	if err := opened.WriteMemory(file.Memory); err != nil {
		t.Fatal(err)
	}
	if result := runIndex(t, opened); result.Bad != 1 {
		t.Fatalf("요약의 비밀정보를 막아야 한다 : %+v", result)
	}
}

// 리뷰 A(2회차) — 고치기 검사는 칸을 안 가린다. 예전에는 summary·title·scope·
// superseded_by·links 만 봐서 migrate 가 채우는 sources·author 가 그냥 지났다.
func TestPatchScansEveryField(t *testing.T) {
	for field, value := range map[string]any{
		"sources": []any{"url:https://x.example/?token=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123"},
		"author":  "human:AKIAIOSFODNN7EXAMPLE",
		"tags":    []any{"fts5", "AKIAIOSFODNN7EXAMPLE"},
	} {
		opened := newStore(t)
		name, err := opened.WriteAdd(addRequest(longSummary, "본문 한 줄이다"))
		if err != nil {
			t.Fatal(err)
		}
		runIndex(t, opened)
		id := model.QueueID(name, "본문 한 줄이다", today())
		if _, err := opened.WritePatch(id, map[string]any{field: value}); err != nil {
			t.Fatal(err)
		}
		if result := runIndex(t, opened); result.Bad != 1 || result.Patched != 0 {
			t.Fatalf("%s 칸의 비밀정보를 막아야 한다 : %+v", field, result)
		}
	}
}
