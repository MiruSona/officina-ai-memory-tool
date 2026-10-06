package search

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// undoneScope 는 이 시험의 기억만 모으는 scope 다 (조건만 준 검색 시험용).
const undoneScope = "mem-undone"

// heldPair 는 같은 낱말 「은빛너구리펭귄」에 걸리는 보류 기억 둘을 넣고
// 색인한 뒤, 「되돌림」 쪽 id 를 돌려준다. 둘 다 review: true 다 — 색인으로는
// 사람 보류와 자동 되돌림이 구별되지 않는 오늘 모양 그대로다.
func heldPair(t *testing.T) (*index.DB, string) {
	t.Helper()
	opened := store.Open(t.TempDir(), false)
	if err := opened.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	for _, summary := range []string{
		"사람 보류 은빛너구리펭귄 시험용 요약 문장 하나다 승격 대기 몫",
		"자동 되돌림 은빛너구리펭귄 시험용 요약 문장 하나다 undo 몫",
	} {
		request := store.AddRequest{Op: store.OpAdd, Type: model.TypeDecision, Date: "2026-10-07",
			Summary: summary, Tags: []string{"hold"}, Source: model.LegacySourceAI, Scope: undoneScope,
			Body: "은빛너구리펭귄 이라는 낱말은 이 시험 기억에만 있다.", Review: true}
		if _, err := opened.WriteAdd(request); err != nil {
			t.Fatal(err)
		}
	}
	settings := config.Default("시험")
	if _, err := index.Run(index.Options{Store: opened, GC: settings.GC, Secret: settings.Secret,
		Quiet: true}); err != nil {
		t.Fatal(err)
	}
	database, err := index.Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	all, err := Search(Options{Sources: []Source{{DB: database}}, Query: "은빛너구리펭귄", Limit: 5,
		Stopwords: config.DefaultStopwords(), Filter: index.Filter{IncludeHeld: true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range all.Hits {
		if strings.Contains(hit.Summary, "자동 되돌림") {
			return database, hit.ID
		}
	}
	t.Fatalf("되돌림 기억을 못 찾았다 : %+v", all.Hits)
	return nil, ""
}

// askUndone 은 낱말 검색을 Undone 집합과 같이 던진다.
func askUndone(t *testing.T, database *index.DB, undone map[string]bool) *Result {
	t.Helper()
	result, err := Search(Options{Sources: []Source{{DB: database}}, Query: "은빛너구리펭귄", Limit: 5,
		Stopwords: config.DefaultStopwords(), Undone: undone})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 || result.Why == nil {
		t.Fatalf("보류만 걸리니 0건이어야 한다 : %+v", result)
	}
	return result
}

// 되돌림 id 를 주면 0건 안내가 「자동 되돌림」과 「보류(승격 대기)」 두 줄로
// 갈리고, `--json` 에 undone 칸이 실린다.
func TestEmptySplitsUndoneFromHeld(t *testing.T) {
	database, id := heldPair(t)
	result := askUndone(t, database, map[string]bool{id: true})
	if result.Why.Held != 1 || result.Why.Undone != 1 {
		t.Fatalf("보류 1 · 되돌림 1 이어야 한다 : %+v", result.Why)
	}
	text := whyLines(result)
	for _, want := range []string{"자동 되돌림 1건은 뺐다", "mem auto redo --apply", "보류(승격 대기) 1건은 뺐다"} {
		if !strings.Contains(text, want) {
			t.Fatalf("%q 가 없다 :\n%s", want, text)
		}
	}
	if strings.Contains(text, "  · 보류 ") {
		t.Fatalf("나눠 말할 때는 예전 한 줄이 같이 나오면 안 된다 :\n%s", text)
	}
	data, err := json.Marshal(result.Why)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"undone":1`) || !strings.Contains(string(data), `"held":1`) {
		t.Fatalf("--json 에 held·undone 칸이 없다 : %s", data)
	}
}

// Undone 이 nil 이면 오늘 출력과 글자까지 같다 — 보류 2건 한 줄, undone 칸 없음.
func TestEmptyWithoutUndoneKeepsOldLine(t *testing.T) {
	database, _ := heldPair(t)
	result := askUndone(t, database, nil)
	if result.Why.Held != 2 || result.Why.Undone != 0 {
		t.Fatalf("보류 2 · 되돌림 0 이어야 한다 : %+v", result.Why)
	}
	text := whyLines(result)
	if !strings.Contains(text, "  · 보류 2건은 뺐다 — `--include-held` 로 본다.") {
		t.Fatalf("예전 한 줄이 아니다 :\n%s", text)
	}
	if strings.Contains(text, "자동 되돌림") || strings.Contains(text, "승격 대기") {
		t.Fatalf("nil 인데 나눠 말했다 :\n%s", text)
	}
	data, _ := json.Marshal(result.Why)
	if strings.Contains(string(data), `"undone"`) {
		t.Fatalf("nil 인데 undone 칸이 실렸다 : %s", data)
	}
}

// 보류 몫이 0 이면 되돌림 줄만 찍는다. 조건만 준 검색(heldFilterCount)도 같다.
func TestEmptyUndoneOnly(t *testing.T) {
	database, _ := heldPair(t)
	all, err := Search(Options{Sources: []Source{{DB: database}}, Query: "은빛너구리펭귄", Limit: 5,
		Stopwords: config.DefaultStopwords(), Filter: index.Filter{IncludeHeld: true}})
	if err != nil {
		t.Fatal(err)
	}
	undone := map[string]bool{}
	for _, hit := range all.Hits {
		undone[hit.ID] = true
	}
	result := askUndone(t, database, undone)
	if result.Why.Held != 0 || result.Why.Undone != 2 {
		t.Fatalf("보류 0 · 되돌림 2 여야 한다 : %+v", result.Why)
	}
	text := whyLines(result)
	if !strings.Contains(text, "자동 되돌림 2건은 뺐다") || strings.Contains(text, "보류") {
		t.Fatalf("되돌림 줄만 나와야 한다 :\n%s", text)
	}

	listed, err := Search(Options{Sources: []Source{{DB: database}}, Limit: 5,
		Filter: index.Filter{Scope: undoneScope}, Undone: undone})
	if err != nil {
		t.Fatal(err)
	}
	if listed.Why == nil || listed.Why.Held != 0 || listed.Why.Undone != 2 {
		t.Fatalf("조건만 준 검색도 되돌림 2 여야 한다 : %+v", listed.Why)
	}
}
