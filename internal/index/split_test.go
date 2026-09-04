package index

import (
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// C2 — 제목 열과 메타(태그+scope) 열이 실제로 갈렸는지.
func TestSchemaV4SplitsTitleAndMeta(t *testing.T) {
	opened := newStore(t)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// 열이 갈린 판이 4 다. 그 뒤로 판이 더 올라가도 열은 그대로여야 한다.
	if SchemaVersion < 4 {
		t.Fatalf("스키마 판이 4 아래로 내려갔다 : %d", SchemaVersion)
	}
	shapes := map[string][]string{
		"fts_ko":   {"title_s", "meta_s", "summary_s", "body_s"},
		"fts_en":   {"title_e", "meta_e", "summary_e", "body_e"},
		"fts_norm": {"title_n", "meta_n", "summary_n", "body_n"},
	}
	for table, columns := range shapes {
		for _, column := range columns {
			if _, err := database.SQL().Exec("SELECT " + column + " FROM " + table + " LIMIT 1"); err != nil {
				t.Fatalf("%s 에 %s 열이 없다 : %v", table, column, err)
			}
		}
	}
	for _, column := range []string{"n_title", "n_meta", "n_sum", "n_body"} {
		if _, err := database.SQL().Exec("SELECT " + column + " FROM memories LIMIT 1"); err != nil {
			t.Fatalf("memories 에 %s 열이 없다 : %v", column, err)
		}
	}
}

// 제목 열에는 태그·scope 가 안 섞이고, 메타 열에는 제목이 안 섞인다.
func TestTitleAndMetaFieldsAreSeparate(t *testing.T) {
	memory := model.Memory{Title: "잔해 락 회수 규칙", Tags: []string{"mem-search", "index"},
		Scope: "tool"}
	if title := TitleField(&memory); title != "잔해 락 회수 규칙" {
		t.Fatalf("제목 열에 딴 것이 섞였다 : %q", title)
	}
	meta := MetaField(&memory)
	if !strings.Contains(meta, "mem-search") || !strings.Contains(meta, "tool") {
		t.Fatalf("메타 열에 태그·scope 가 없다 : %q", meta)
	}
	if strings.Contains(meta, "잔해") {
		t.Fatalf("메타 열에 제목이 섞였다 : %q", meta)
	}
}

// 열을 갈랐으니 태그에만 있는 낱말도, 제목에만 있는 낱말도 다 찾혀야 한다.
func TestSplitKeepsTagsSearchable(t *testing.T) {
	opened := newStore(t)
	if _, err := opened.WriteAdd(addRequest(longSummary, "본문에는 fts5 라는 말이 없다")); err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	count := 0
	if err := database.SQL().QueryRow(`SELECT COUNT(*) FROM fts_en WHERE fts_en MATCH ?`,
		"meta_e : fts5").Scan(&count); err != nil {
		t.Fatalf("fts_en 질의가 죽었다 : %v", err)
	}
	if count != 1 {
		t.Fatalf("태그가 meta 열에 안 들어갔다 : %d 건", count)
	}
}

// 제목에 맞은 것이 태그에만 맞은 것보다 앞이라야 한다 — 가르기의 요점이다.
func TestTitleOutranksTagOnlyMatch(t *testing.T) {
	opened := newStore(t)
	// 태그는 영어 소문자·숫자·하이픈만 받는다 (tag-shape). 그래서 영문 낱말로
	// 잰다 — 제목에 있는 쪽이 태그에만 있는 쪽보다 앞이라야 한다.
	titled := sampleMemory("20260824-aa1b2c3d")
	titled.Title = "shadowmap 을 다시 그린다"
	titled.Tags = []string{"gc", "lock"}
	tagged := sampleMemory("20260824-bb1b2c3d")
	tagged.Title = "전혀 다른 제목 하나를 적어 둔다"
	tagged.Tags = []string{"shadowmap", "gc", "lock", "index", "search"}
	writeMemory(t, opened.Dir, titled)
	writeMemory(t, opened.Dir, tagged)
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ranked, err := database.MatchEN(`shadowmap`, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked) != 2 {
		t.Fatalf("두 건이 걸려야 한다 : %d", len(ranked))
	}
	rows, err := database.FetchRows([]int64{ranked[0].Docid}, Filter{All: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "20260824-aa1b2c3d" {
		t.Fatalf("제목에 맞은 것이 1위라야 한다 : %+v", rows)
	}
}

// 0 이하 가중은 안 받는다 — bm25 가 그 열을 통째로 버린다 (v0.2 고삐).
func TestSetFieldWeightsRejectsZero(t *testing.T) {
	before := FieldWeights()
	defer SetFieldWeights(before)
	SetFieldWeights([4]float64{8, 0, 4, 1})
	if FieldWeights() != before {
		t.Fatalf("0 이 섞인 가중을 받았다 : %v", FieldWeights())
	}
	SetFieldWeights([4]float64{8, 3, 4, 1})
	if FieldWeights() != ([4]float64{8, 3, 4, 1}) {
		t.Fatalf("멀쩡한 가중을 안 받았다 : %v", FieldWeights())
	}
}

// 재순위 재료도 네 열이라야 짝이 맞는다.
func TestRerankRowHasFourFields(t *testing.T) {
	opened := newStore(t)
	one := sampleMemory("20260824-5c8e1a02")
	one.Tags = []string{"mem-search", "index"}
	writeMemory(t, opened.Dir, one)
	other := sampleMemory("20260824-dd1b2c3d")
	other.Title = "훅 예산을 다섯 갈래로 나눈다"
	writeMemory(t, opened.Dir, other)
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	docid := int64(0)
	if err := database.SQL().QueryRow("SELECT docid FROM memories WHERE id = ?",
		"20260824-5c8e1a02").Scan(&docid); err != nil {
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
	pieces := [4]string{row.TitleKO, row.MetaKO, row.SummaryKO, row.BodyKO}
	for at, text := range pieces {
		if len(strings.Fields(text)) != row.Lens[at] {
			t.Fatalf("열 %d 의 낱말 수가 안 맞는다 : %d ≠ %d", at,
				len(strings.Fields(text)), row.Lens[at])
		}
	}
	stats, err := database.CorpusStats()
	if err != nil {
		t.Fatal(err)
	}
	for at, avg := range stats.AvgLen {
		if avg <= 0 {
			t.Fatalf("평균 길이가 0 이다 : 열 %d", at)
		}
	}
}
