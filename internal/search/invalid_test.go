package search

import (
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// invalidRepo 는 살아 있는 기억 하나와 무효 기억 둘(기한 지남 · 덮임)을 담은
// 시험 저장소다. 셋 다 같은 낱말을 갖고 있어 한 질의에 다 걸린다.
func invalidRepo(t *testing.T) (*index.DB, string, string, string) {
	t.Helper()
	opened := store.Open(t.TempDir(), false)
	if err := opened.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	live := memoryAt(t, "20260801-aaaa1111", "바이그램 검색 방식을 이렇게 쓰기로 한 지금 살아 있는 결정 하나다", nil)
	expired := memoryAt(t, "20260801-bbbb2222", "바이그램 검색 방식을 예전에 이렇게 쓰기로 했다가 기한이 지난 결정", func(m *model.Memory) {
		m.InvalidAt = time.Now().AddDate(0, 0, -3).Format("2006-01-02")
	})
	covered := memoryAt(t, "20260801-cccc3333", "바이그램 검색 방식을 예전에 이렇게 쓰기로 했다가 새 결정이 덮은 것", func(m *model.Memory) {
		m.SupersededBy = live.ID
	})
	for _, item := range []*model.Memory{live, expired, covered} {
		if err := opened.WriteMemory(item); err != nil {
			t.Fatal(err)
		}
	}
	settings := config.Default("시험")
	if _, err := index.Run(index.Options{Store: opened, GC: settings.GC, Secret: settings.Secret, Quiet: true}); err != nil {
		t.Fatal(err)
	}
	database, err := index.Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database, live.ID, expired.ID, covered.ID
}

func memoryAt(t *testing.T, id, summary string, tweak func(*model.Memory)) *model.Memory {
	t.Helper()
	item := model.Memory{ID: id, Type: model.TypeDecision, Date: "2026-08-01", Summary: summary,
		Tags: []string{"search", "index"}, LegacySource: model.LegacySourceAI, Scope: "mem-search",
		Body: "바이그램 검색 방식에 대한 결정이다."}
	if tweak != nil {
		tweak(&item)
	}
	if problems := model.Validate(&item); len(problems) > 0 {
		t.Fatalf("시험 기억이 규격을 어겼다 : %v", problems[0])
	}
	return &item
}

func lookUp(t *testing.T, database *index.DB, query string, all bool) *Result {
	t.Helper()
	result, err := Search(Options{Sources: []Source{{DB: database}}, Query: query, Limit: 10,
		Stopwords: config.DefaultStopwords(), Filter: index.Filter{All: all}})
	if err != nil {
		t.Fatalf("검색이 죽었다 : %v", err)
	}
	return result
}

// 기본 검색은 무효 기억을 아예 안 보여준다 (명세 `--all`).
func TestSearchHidesInvalid(t *testing.T) {
	database, live, expired, covered := invalidRepo(t)
	result := lookUp(t, database, "바이그램", false)
	if !hasID(result, live) {
		t.Fatal("살아 있는 기억이 기본 검색에서 빠졌다")
	}
	for _, id := range []string{expired, covered} {
		if hasID(result, id) {
			t.Fatalf("무효 기억 %s 가 기본 검색에 나왔다", id)
		}
	}
}

// --all 은 셋 다 보여주고 무효에는 [무효] 꼬리표가 붙는다.
func TestSearchAllShowsInvalidWithMark(t *testing.T) {
	database, live, expired, covered := invalidRepo(t)
	result := lookUp(t, database, "바이그램", true)
	for _, id := range []string{live, expired, covered} {
		if !hasID(result, id) {
			t.Fatalf("--all 인데 %s 가 안 나왔다", id)
		}
	}
	marked := map[string]bool{}
	for _, hit := range result.Hits {
		marked[hit.ID] = hit.Invalid
	}
	if marked[live] {
		t.Fatal("살아 있는 기억에 무효 표시가 붙었다")
	}
	if !marked[expired] || !marked[covered] {
		t.Fatal("무효 기억에 무효 표시가 안 붙었다")
	}
	text := Markdown(result, 0)
	if strings.Count(text, "[무효]") != 2 {
		t.Fatalf("[무효] 꼬리표가 두 번 안 나왔다 :\n%s", text)
	}
}

// 조건 목록(낱말 없이 --tag 만)도 무효를 뺀다.
func TestListHidesInvalid(t *testing.T) {
	database, live, expired, covered := invalidRepo(t)
	result, err := Search(Options{Sources: []Source{{DB: database}}, Limit: 10,
		Filter: index.Filter{Tags: []string{"search"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !hasID(result, live) || hasID(result, expired) || hasID(result, covered) {
		t.Fatalf("조건 목록이 무효를 안 뺐다 : %v", allHitIDs(result))
	}
	if result.Total != 1 {
		t.Fatalf("총 건수가 무효까지 셌다 : %d", result.Total)
	}
}

// `#태그` 만 친 질의는 --tag 를 준 것과 같은 답이어야 한다 (최종측정 4-1 ②).
func TestTagOnlyQueryLists(t *testing.T) {
	database, live, _, _ := invalidRepo(t)
	byWord := lookUp(t, database, "#search", false)
	if !byWord.FilterOnly {
		t.Fatal("#태그 만 친 질의가 조건 목록으로 안 갔다")
	}
	if len(byWord.Hits) != 1 || byWord.Hits[0].ID != live {
		t.Fatalf("#search 가 0건이거나 딴 것을 냈다 : %v", allHitIDs(byWord))
	}
	byFlag, err := Search(Options{Sources: []Source{{DB: database}}, Limit: 10,
		Filter: index.Filter{Tags: []string{"search"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(byWord.Hits) != len(byFlag.Hits) || byWord.Total != byFlag.Total {
		t.Fatalf("#search 와 --tag search 가 다르다 : %d/%d 대 %d/%d",
			len(byWord.Hits), byWord.Total, len(byFlag.Hits), byFlag.Total)
	}
}

// scope:이름 만 친 질의도 같다.
func TestScopeOnlyQueryLists(t *testing.T) {
	database, live, _, _ := invalidRepo(t)
	result := lookUp(t, database, "scope:mem-search", false)
	if len(result.Hits) != 1 || result.Hits[0].ID != live {
		t.Fatalf("scope:mem-search 가 0건이거나 딴 것을 냈다 : %v", allHitIDs(result))
	}
}

func allHitIDs(result *Result) []string {
	out := []string{}
	for _, hit := range result.Hits {
		out = append(out, hit.ID)
	}
	return out
}
