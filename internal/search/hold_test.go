package search

import (
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// 보류(`review: true`)만 걸리는 낱말을 검색하면 0건이되, 왜 0건인지에
// "보류라서 뺐다" 가 실려야 한다 (뒷정리-3). 안 실리면 사람은 "그런 기억이
// 없다" 로 잘못 읽는다.
func TestSearchExplainsHeldMiss(t *testing.T) {
	opened := store.Open(t.TempDir(), false)
	if err := opened.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	request := store.AddRequest{Op: store.OpAdd, Type: model.TypeDecision, Date: "2026-08-22",
		Summary: "보류 낱말 골든가젤박쥐 만 들어 있는 시험용 요약 문장 하나다 이것뿐",
		Tags:    []string{"hold"}, Source: model.LegacySourceAI, Scope: "mem-search",
		Body: "골든가젤박쥐 라는 낱말은 이 기억에만 있다.", Review: true}
	if _, err := opened.WriteAdd(request); err != nil {
		t.Fatal(err)
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
	defer database.Close()

	result := ask(t, database, "골든가젤박쥐", 5)
	if result.Total != 0 {
		t.Fatalf("보류는 기본 검색에 안 떠야 한다 : %+v", result)
	}
	if result.Why == nil || result.Why.Held != 1 {
		t.Fatalf("보류 1건이 빠졌다는 것을 알려야 한다 : %+v", result.Why)
	}
	line := whyLines(result)
	if !strings.Contains(line, "보류") {
		t.Fatalf("사람이 읽는 줄에 보류 안내가 없다 :\n%s", line)
	}

	held, err := Search(Options{Sources: []Source{{DB: database}}, Query: "골든가젤박쥐", Limit: 5,
		Stopwords: config.DefaultStopwords(), Filter: index.Filter{IncludeHeld: true}})
	if err != nil {
		t.Fatal(err)
	}
	if held.Total != 1 {
		t.Fatalf("--include-held 면 나와야 한다 : %+v", held)
	}
}
