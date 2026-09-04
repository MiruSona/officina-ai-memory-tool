package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/token"
)

// 스키마 v3 — fts_norm 이 생기고 memories 열은 안 늘었다 (설계 3절 · 결정 21·58).
func TestSchemaV3HasNormTable(t *testing.T) {
	opened := newStore(t)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if !database.hasTable("fts_norm") {
		t.Fatal("fts_norm 표가 없다")
	}
	// norm 은 열로 두지 않는다 — v0.2 가 body·stems 열을 뺀 이유를 되돌리지 않는다.
	for _, column := range []string{"norm", "norm_head", "body", "stems"} {
		if _, err := database.SQL().Exec("SELECT " + column + " FROM memories LIMIT 1"); err == nil {
			t.Fatalf("memories 에 %s 열이 생겼다", column)
		}
	}
	// v0.3 이 3 으로 올린 자리다. 뒤 판이 더 올리는 것은 맞고, 내려가면 옛
	// index.db 를 조용히 그대로 읽는다.
	if SchemaVersion < 3 {
		t.Fatalf("스키마 판이 3 아래로 내려갔다 : %d", SchemaVersion)
	}
	if !strings.HasSuffix(config.Tokenizer, "+norm") || token.Version != "6" {
		t.Fatalf("판 상수가 안 올라갔다 : %q %q", config.Tokenizer, token.Version)
	}
}

// T19 — v0.2 가 만든 index.db 를 열면 아무 명령 없이 통째로 다시 만든다.
func TestV02IndexIsRebuiltOnOpen(t *testing.T) {
	opened := newStore(t)
	writeMemory(t, opened.Dir, sampleMemory("20260823-5c8e1a02"))
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	// v0.2 를 흉내 낸다 — 판 2 · fts_norm 없음 · 옛 토크나이저 이름.
	for _, statement := range []string{"DROP TABLE fts_norm", "PRAGMA user_version=2"} {
		if _, err := database.SQL().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.SetMeta(metaTokenizer, "bigram-ko3col+word-en3col+contentless"); err != nil {
		t.Fatal(err)
	}
	database.Close()
	result := runIndex(t, opened)
	if !result.Rebuilt {
		t.Fatalf("v0.2 색인은 통째로 다시 만들어야 한다 : %+v", result)
	}
	if result.Indexed != 1 {
		t.Fatalf("다시 만들었으면 한 건을 다시 색인해야 한다 : %d", result.Indexed)
	}
	again, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if !again.hasTable("fts_norm") {
		t.Fatal("다시 만든 색인에 fts_norm 이 없다")
	}
	version := 0
	if err := again.SQL().QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion {
		t.Fatalf("판이 안 올라갔다 : %d", version)
	}
}

// 판 번호는 우리와 같은데 우리 표가 없는 DB 도 다른 스키마다 — 통째로 다시 만든다.
// (v0.0 이 쓰던 번호 3 이 v0.3 과 겹친다)
func TestSameVersionButForeignSchemaIsRebuilt(t *testing.T) {
	opened := newStore(t)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL().Exec("DROP TABLE fts_norm"); err != nil {
		t.Fatal(err)
	}
	database.Close()
	again, err := Open(opened.Dir)
	if err != nil {
		t.Fatalf("표가 빠진 색인은 다시 만들어야 한다 : %v", err)
	}
	defer again.Close()
	if !again.hasTable("fts_norm") {
		t.Fatal("다시 만든 색인에 fts_norm 이 없다")
	}
}

// 정규화기 주입 — index 는 자기 정규화 규칙을 안 갖고 밖에서 받는다.
// 1A 의 token.Normalize 가 들어올 자리다.
func TestNormalizeFuncIsUsed(t *testing.T) {
	restore := Normalize
	SetNormalize(func(text string) string {
		return strings.ReplaceAll(text, "인덱싱", "색인")
	})
	defer SetNormalize(restore)

	opened := newStore(t)
	memory := sampleMemory("20260823-11223344")
	memory.Title = "인덱싱 스키마를 판 3 으로 올린다"
	memory.Summary = "인덱싱 이야기만 적는다. 요약은 서른 자를 넘겨야 규격에 맞으니 조금 더 길게 적는다"
	memory.Body = "본문에도 인덱싱 이야기."
	writeMemory(t, opened.Dir, memory)
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// 원문에 없는 「색인」 으로 fts_norm 에서 걸려야 한다.
	ranked, err := database.MatchNorm(token.ForQuery("색인"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked) != 1 {
		t.Fatalf("대표말로 못 찾는다 : %d 건", len(ranked))
	}
	if ranked[0].Score == 0 {
		t.Fatal("bm25 가 0 이다 — 열 가중이 죽었다")
	}
	// fts_ko 는 원문 그대로라 「색인」 으로 안 걸린다.
	plain, err := database.MatchKO(token.ForQuery("색인"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != 0 {
		t.Fatalf("fts_ko 가 정규화한 글을 담고 있다 : %d 건", len(plain))
	}
}

// contentless 표는 UPDATE 가 안 된다. 같은 파일을 다시 색인해도 fts_norm 행이
// 겹쳐 쌓이거나 옛 글이 남으면 안 된다 (결정 58 · v0.1 warm 접기 사고).
func TestNormRowIsDeletedAndReinserted(t *testing.T) {
	restore := Normalize
	SetNormalize(func(text string) string { return strings.ReplaceAll(text, "제목", "이름표") })
	defer SetNormalize(restore)
	opened := newStore(t)
	memory := sampleMemory("20260823-99887766")
	memory.Title = "처음제목 하나"
	writeMemory(t, opened.Dir, memory)
	runIndex(t, opened)

	memory.Title = "고친제목 둘"
	writeMemory(t, opened.Dir, memory)
	if result := runIndex(t, opened); result.Indexed != 1 {
		t.Fatalf("고친 파일을 다시 색인해야 한다 : %+v", result)
	}
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	count := 0
	if err := database.SQL().QueryRow("SELECT COUNT(*) FROM fts_norm").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("fts_norm 행이 겹쳐 쌓였다 : %d", count)
	}
	stale, err := database.MatchNorm(token.ForQuery("처음이름표"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Fatalf("옛 글이 fts_norm 에 남아 있다 : %d 건", len(stale))
	}
	fresh, err := database.MatchNorm(token.ForQuery("고친이름표"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh) != 1 {
		t.Fatalf("고친 글이 fts_norm 에 없다 : %d 건", len(fresh))
	}
}

// 파일이 없어지면 fts_norm 행도 같이 없어진다.
func TestNormRowGoesWithTheFile(t *testing.T) {
	restore := Normalize
	SetNormalize(func(text string) string { return strings.ReplaceAll(text, "색인", "인덱스") })
	defer SetNormalize(restore)
	opened := newStore(t)
	writeMemory(t, opened.Dir, sampleMemory("20260823-44556677"))
	runIndex(t, opened)
	database0, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	before := 0
	if err := database0.SQL().QueryRow("SELECT COUNT(*) FROM fts_norm").Scan(&before); err != nil {
		t.Fatal(err)
	}
	database0.Close()
	if before != 1 {
		t.Fatalf("지우기 전에 한 행이 있어야 한다 : %d", before)
	}
	removeMemory(t, opened.Dir, "20260823-44556677")
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	count := 0
	if err := database.SQL().QueryRow("SELECT COUNT(*) FROM fts_norm").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("없어진 파일의 fts_norm 행이 남았다 : %d", count)
	}
}

// ChangedFiles 는 file_hash 가 바뀐 건만 준다. 3A 의 vectors.bin 이 이 목록으로
// 다시 계산할 것을 고른다 (결정 58).
func TestChangedFilesListsOnlyNewHashes(t *testing.T) {
	opened := newStore(t)
	memory := sampleMemory("20260823-22334455")
	writeMemory(t, opened.Dir, memory)
	first := runIndex(t, opened)
	if len(first.Changed) != 1 || first.Changed[0].ID != "20260823-22334455" {
		t.Fatalf("처음 색인은 바뀐 건으로 세야 한다 : %+v", first.Changed)
	}
	if first.Changed[0].Hash == "" {
		t.Fatal("file_hash 가 비었다")
	}
	// 안 고치고 다시 돌리면 바뀐 건이 없다.
	if again := runIndex(t, opened); len(again.Changed) != 0 {
		t.Fatalf("안 고쳤는데 바뀐 건이 있다 : %+v", again.Changed)
	}
	memory.Body = "본문을 고쳤다."
	writeMemory(t, opened.Dir, memory)
	third := runIndex(t, opened)
	if len(third.Changed) != 1 {
		t.Fatalf("고친 건이 안 잡힌다 : %+v", third.Changed)
	}
	if third.Changed[0].Hash == first.Changed[0].Hash {
		t.Fatal("해시가 그대로다")
	}
}

// removeMemory 는 store/ 에서 기억 파일 하나를 지운다.
func removeMemory(t *testing.T, dir, id string) {
	t.Helper()
	if err := os.Remove(filepath.Join(dir, "store", "2026", "08", id+".md")); err != nil {
		t.Fatal(err)
	}
}

// 정규화가 글자를 안 바꾼 문서는 fts_norm 에 안 넣는다. 질의도 같은 Normalize 를
// 지나므로 그런 문서는 fts_ko 에서 똑같이 걸린다 — 20k 에서 2.50MB 짜리 자리다 (R4).
func TestUnchangedDocSkipsNormTable(t *testing.T) {
	restore := Normalize
	SetNormalize(func(text string) string { return strings.ReplaceAll(text, "인덱싱", "색인") })
	defer SetNormalize(restore)
	opened := newStore(t)
	// 「인덱싱」 이 없는 기억이라 정규화해도 그대로다.
	writeMemory(t, opened.Dir, sampleMemory("20260823-77665544"))
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	count := 0
	if err := database.SQL().QueryRow("SELECT COUNT(*) FROM fts_norm").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("안 바뀐 문서가 fts_norm 에 들어갔다 : %d", count)
	}
	// 그래도 정규화한 질의로 찾을 수 있다 — fts_ko 가 같은 글자를 담고 있다.
	ranked, err := database.MatchKO(token.ForQuery("색인"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked) != 1 {
		t.Fatalf("fts_ko 에서 못 찾는다 : %d 건", len(ranked))
	}
}
