package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/token"
)

// writeMemory 는 store/ 에 기억 파일 하나를 손으로 놓는다. inbox 승격을 안
// 거치므로 색인 쪽만 좁혀서 볼 수 있다.
func writeMemory(t *testing.T, dir string, memory *model.Memory) {
	t.Helper()
	folder := filepath.Join(dir, "store", "2026", "08")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	text := model.Encode(memory)
	if err := os.WriteFile(filepath.Join(folder, memory.ID+".md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sampleMemory 는 v0.2 규격을 다 채운 기억 하나다.
func sampleMemory(id string) *model.Memory {
	return &model.Memory{
		ID: id, Type: model.TypeDecision, Date: "2026-08-23", Spec: model.SpecV2,
		Title:   "색인 스키마를 판 2 로 올린다",
		Summary: "옛 판 index.db 를 그대로 읽으면 조용히 틀린 답을 내므로 판 번호를 올리고 통째로 다시 만든다",
		Tags:    []string{"index", "search"}, Scope: "aimemorytool",
		Author:     "human:mirusona",
		Sources:    []string{"file:internal/index/schema.go", "commit:42c18dd"},
		StaleAfter: "2027-01-01",
		Body:       "본문 한 줄. 5,000건 규모에서 재 봤다.",
	}
}

// 스키마 v2 — 새 표와 새 열이 실제로 만들어지는지.
func TestSchemaV2HasNewTables(t *testing.T) {
	opened := newStore(t)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, name := range []string{"memories", "tagmap", "sources", "links", "vocab_ko", "fts_ko", "fts_en"} {
		if !database.hasTable(name) {
			t.Fatalf("표가 없다 : %s", name)
		}
	}
	for _, column := range []string{"author", "todo_status", "stale_after", "simhash",
		"n_title", "n_meta", "n_sum", "n_body", "spec"} {
		if _, err := database.SQL().Exec("SELECT " + column + " FROM memories LIMIT 1"); err != nil {
			t.Fatalf("열이 없다 : %s (%v)", column, err)
		}
	}
	version := 0
	if err := database.SQL().QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion {
		t.Fatalf("판 번호가 틀리다 : %d", version)
	}
}

// v0.1 이 만든 판(1)은 옮길 게 없으니 통째로 다시 만든다 (설계 6-4).
func TestSchemaV1IsRebuilt(t *testing.T) {
	opened := newStore(t)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL().Exec("PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
	}
	database.Close()
	again, err := Open(opened.Dir)
	if err != nil {
		t.Fatalf("v0.1 판은 다시 만들어야 한다 : %v", err)
	}
	defer again.Close()
	version := 0
	if err := again.SQL().QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion {
		t.Fatalf("다시 만든 뒤에도 판이 1 이다 : %d", version)
	}
}

// author·sources·tagmap 이 실제로 채워지는지.
func TestAuthorAndSourcesAreIndexed(t *testing.T) {
	opened := newStore(t)
	writeMemory(t, opened.Dir, sampleMemory("20260823-5c8e1a02"))
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	author, stale, simhash := "", int64(0), int64(0)
	err = database.SQL().QueryRow(`SELECT author, COALESCE(stale_after, 0), simhash
		FROM memories WHERE id = ?`, "20260823-5c8e1a02").Scan(&author, &stale, &simhash)
	if err != nil {
		t.Fatal(err)
	}
	if author != "human:mirusona" {
		t.Fatalf("author 가 안 들어갔다 : %q", author)
	}
	if stale == 0 {
		t.Fatal("stale_after 가 안 들어갔다")
	}
	if simhash == 0 {
		t.Fatal("simhash 가 안 들어갔다")
	}
	kinds := map[string]string{}
	rows, err := database.SQL().Query("SELECT kind, value FROM sources WHERE mem_id = ?", "20260823-5c8e1a02")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		kind, value := "", ""
		if err := rows.Scan(&kind, &value); err != nil {
			t.Fatal(err)
		}
		kinds[kind] = value
	}
	rows.Close()
	if kinds["file"] != "internal/index/schema.go" || kinds["commit"] != "42c18dd" {
		t.Fatalf("sources 가 접두별로 안 쪼개졌다 : %v", kinds)
	}
	tags := 0
	if err := database.SQL().QueryRow("SELECT COUNT(*) FROM tagmap WHERE mem_id = ?",
		"20260823-5c8e1a02").Scan(&tags); err != nil {
		t.Fatal(err)
	}
	if tags != 2 {
		t.Fatalf("tagmap 이 2건이라야 한다 : %d", tags)
	}
}

// 같은 파일을 두 번 색인해도 딸린 표가 겹쳐 쌓이면 안 된다.
func TestSideTablesStayIdempotent(t *testing.T) {
	opened := newStore(t)
	writeMemory(t, opened.Dir, sampleMemory("20260823-5c8e1a02"))
	runIndex(t, opened)
	result, err := Run(Options{Store: opened, GC: settings().GC, Secret: settings().Secret,
		Quiet: true, Full: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Indexed != 1 {
		t.Fatalf("한 건이라야 한다 : %+v", result)
	}
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	count := 0
	if err := database.SQL().QueryRow("SELECT COUNT(*) FROM sources").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("근거가 겹쳐 쌓였다 : %d", count)
	}
}

// 열 가중이 실제로 순위를 바꾸는지 — 제목에 맞은 것이 본문에 맞은 것보다 앞이다.
func TestFieldWeightsRankHeadFirst(t *testing.T) {
	opened := newStore(t)
	head := sampleMemory("20260823-aa1b2c3d")
	head.Title = "잔해 락 회수 규칙"
	body := sampleMemory("20260823-bb1b2c3d")
	body.Title = "전혀 다른 제목 하나를 적어 둔다"
	body.Body = strings.Repeat("잔해 회수 이야기를 본문에만 적는다. ", 20)
	writeMemory(t, opened.Dir, head)
	writeMemory(t, opened.Dir, body)
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ranked, err := database.MatchKO(`"잔해"`, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked) != 2 {
		t.Fatalf("두 건이 걸려야 한다 : %d", len(ranked))
	}
	if ranked[0].Score == 0 {
		t.Fatal("bm25 가 0 이다 — 열 가중이 죽었다")
	}
	rows, err := database.FetchRows([]int64{ranked[0].Docid}, Filter{All: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "20260823-aa1b2c3d" {
		t.Fatalf("제목에 맞은 것이 1위라야 한다 : %+v", rows)
	}
}

// 재순위 재료 — 필드별 낱말 수·평균·문서 빈도가 다 나오는지.
func TestRerankMaterial(t *testing.T) {
	opened := newStore(t)
	writeMemory(t, opened.Dir, sampleMemory("20260823-5c8e1a02"))
	// idf 는 "몇 건에만 나오나" 라서 안 걸리는 기억이 하나는 있어야 값이 산다.
	other := sampleMemory("20260823-dd1b2c3d")
	other.Title = "훅 예산을 다섯 갈래로 나눈다"
	other.Summary = "세션 시작 훅이 쓰는 시간을 갈래마다 따로 잡아 두면 한 갈래가 늦어도 나머지가 산다"
	other.Body = "훅 이야기만 적는다."
	writeMemory(t, opened.Dir, other)
	third := sampleMemory("20260823-ee1b2c3d")
	third.Title = other.Title + " 두 번째"
	third.Summary = other.Summary
	third.Body = other.Body
	writeMemory(t, opened.Dir, third)
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	stats, err := database.CorpusStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Docs != 3 {
		t.Fatalf("세 건이라야 한다 : %d", stats.Docs)
	}
	for at, avg := range stats.AvgLen {
		if avg <= 0 {
			t.Fatalf("평균 길이가 0 이다 : 열 %d", at)
		}
	}
	docid := int64(0)
	if err := database.SQL().QueryRow("SELECT docid FROM memories WHERE id = ?",
		"20260823-5c8e1a02").Scan(&docid); err != nil {
		t.Fatal(err)
	}
	rows, err := database.RerankRows([]int64{docid})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("한 건이라야 한다 : %d", len(rows))
	}
	row := rows[0]
	if row.Lens[0] == 0 || row.Lens[1] == 0 || row.Lens[2] == 0 {
		t.Fatalf("필드별 낱말 수가 비었다 : %v", row.Lens)
	}
	if len(strings.Fields(row.TitleKO)) != row.Lens[0] {
		t.Fatalf("담아 둔 낱말 수와 다시 만든 조각이 안 맞는다 : %d ≠ %d",
			len(strings.Fields(row.TitleKO)), row.Lens[0])
	}
	freq, err := database.DocFreq([]string{"색인", "없는낱말"})
	if err != nil {
		t.Fatal(err)
	}
	if freq["색인"] != 1 {
		t.Fatalf("문서 빈도를 못 셌다 : %v", freq)
	}
	if _, has := freq["없는낱말"]; has {
		t.Fatal("색인에 없는 낱말이 들어 있다")
	}
	score := BM25(row, []string{"색인"}, freq, stats, config.DefaultFieldWeights,
		config.DefaultK1, config.DefaultB)
	if score <= 0 {
		t.Fatalf("우리 bm25 가 0 이하다 : %f", score)
	}
}

// 숫자 구분기호 — `5,000` 으로 색인된 글을 `5000` 으로 찾을 수 있어야 한다.
func TestNumberSeparatorIsIndexed(t *testing.T) {
	opened := newStore(t)
	memory := sampleMemory("20260823-cc1b2c3d")
	memory.Body = "가데이터 5,000건을 넣고 재 봤다."
	writeMemory(t, opened.Dir, memory)
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, word := range []string{"5000", "5,000"} {
		if !database.KnownWord(word) {
			t.Fatalf("%q 로 못 찾는다", word)
		}
	}
}

// 판 표시 — 토크나이저 이름은 config 한 곳에서만 오고, 조각 판은 token 이 정한다.
func TestMetaVersionsAreWritten(t *testing.T) {
	opened := newStore(t)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	name, err := database.Meta("tokenizer")
	if err != nil {
		t.Fatal(err)
	}
	if name != config.Tokenizer {
		t.Fatalf("토크나이저 이름이 config 와 다르다 : %q", name)
	}
	stems, err := database.Meta("stems")
	if err != nil {
		t.Fatal(err)
	}
	if stems != token.Version {
		t.Fatalf("조각 판이 token 과 다르다 : %q", stems)
	}
	changed, err := database.SchemeChanged()
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("방금 만든 색인이 다른 방식이라고 나온다")
	}
}

// 본문은 DB 에 없다 — 그래도 FTS 는 contentless 라 검색이 그대로여야 한다.
func TestBodyLivesOnlyInMarkdown(t *testing.T) {
	opened := newStore(t)
	memory := sampleMemory("20260823-5c8e1a02")
	memory.Body = "본문에만 있는 낱말 하나 : 잔해회수 이야기.\n두 번째 줄."
	writeMemory(t, opened.Dir, memory)
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// DB 에 본문 열이 아예 없다.
	if _, err := database.SQL().Exec("SELECT body FROM memories LIMIT 1"); err == nil {
		t.Fatal("memories.body 열이 아직 있다")
	}
	// 본문에만 있는 낱말이 여전히 걸린다 (fts_ko 의 body_s 는 그대로다).
	ranked, err := database.MatchKO(`"잔해"`, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked) != 1 {
		t.Fatalf("본문 낱말이 안 걸린다 : %d 건", len(ranked))
	}
	// 구절 가산도 본문에서 찾는다.
	hit, err := database.PhraseDocids([]int64{ranked[0].Docid}, "잔해회수 이야기")
	if err != nil {
		t.Fatal(err)
	}
	if !hit[ranked[0].Docid] {
		t.Fatal("본문 구절을 못 찾았다")
	}
	// 재순위 재료의 본문·조각·낱말 수가 md 와 맞는다.
	rows, err := database.RerankRows([]int64{ranked[0].Docid})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Body != memory.Body {
		t.Fatalf("md 에서 읽은 본문이 다르다 : %q", rows[0].Body)
	}
	if rows[0].BodyKO != token.ForIndex(memory.Body) {
		t.Fatal("본문 조각이 색인 때와 다르다")
	}
	if rows[0].Lens[3] != len(strings.Fields(token.ForIndex(memory.Body))) {
		t.Fatalf("n_body 가 본문과 안 맞는다 : %d", rows[0].Lens[3])
	}
}

// 파일이 없어졌으면 빈 본문이다 — 검색이 멈추면 안 된다.
func TestMissingFileGivesEmptyBody(t *testing.T) {
	opened := newStore(t)
	writeMemory(t, opened.Dir, sampleMemory("20260823-5c8e1a02"))
	runIndex(t, opened)
	if err := os.Remove(filepath.Join(opened.Dir, "store", "2026", "08",
		"20260823-5c8e1a02.md")); err != nil {
		t.Fatal(err)
	}
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if body := database.BodyOf("store/2026/08/20260823-5c8e1a02.md"); body != "" {
		t.Fatalf("없는 파일인데 본문이 나왔다 : %q", body)
	}
	// 경로 감옥 — 저장소 밖은 못 읽는다.
	if body := database.BodyOf("../../../windows/win.ini"); body != "" {
		t.Fatal("저장소 밖 파일을 읽었다")
	}
}
