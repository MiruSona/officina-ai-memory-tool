package review

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/llm"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// C19 (모순 후보) 시험이다. NLI 는 httptest 가짜 서버이고 진짜 서버엔 아무것도 안 보낸다.
// 닮은 짝(RepoReport.Near)은 quality 가 채우는 칸이라 여기서는 짝을 손으로 만든다.

const (
	// 규칙 층이 반대(B)로 잡는 짝 · 규칙 층이 답을 못 내는 짝.
	flipLeft   = "검색 결과 캐시는 기본 켬 으로 둔다"
	flipRight  = "검색 결과 캐시는 기본 끔 으로 둔다"
	plainLeft  = "색인은 SQLite FTS5 로 만든다"
	plainRight = "색인은 SQLite FTS5 로 만들고 점수 순으로 보여 준다"
	// nliContradict 는 반대 0.9 를 주는 가짜 답이다.
	nliContradict = `{"a":0.05,"b":0.9,"c":0.05,"model":"fake0001","ms":3.0}`
)

// fakeNLI 는 /health 와 /judge 를 받는 가짜 서버다. judge 에 온 물음 수를 센다.
func fakeNLI(t *testing.T, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	asked := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		switch {
		case r.URL.Path == "/health" && r.Method == http.MethodGet:
			io.WriteString(w, `{"ok":true}`)
		case r.URL.Path == "/judge" && r.Method == http.MethodPost:
			asked.Add(1)
			io.WriteString(w, body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server, asked
}

func nliOptions(t *testing.T, address string, pairs int) *NLIOptions {
	t.Helper()
	settings := config.LLMConfig{NLIURL: address, NLITimeoutMS: 1000, NLISure: 0.70, NLISureSupport: 0.90}
	return &NLIOptions{Judge: NLIJudge(llm.NewNLI(settings), t.TempDir(), nil),
		Health: HealthOf(address), Pairs: pairs}
}

func pairMemory(id, kind, scope string) *model.Memory {
	return &model.Memory{ID: id, Type: kind, Scope: scope, Date: id[:4] + "-" + id[4:6] + "-" + id[6:8],
		Title: "제목 " + id}
}

// pairsFixture 는 짝 n 개다. 짝 i 는 (20260101-000i0001, 20260102-000i0002) 이고 글은 left/right 다.
func pairsFixture(count int, left, right string) (quality.RepoReport, map[string]*model.Memory) {
	found := quality.RepoReport{ByID: map[string][]quality.Finding{}}
	byID := map[string]*model.Memory{}
	for at := 0; at < count; at++ {
		older := "20260101-000" + string(rune('a'+at)) + "0001"
		newer := "20260102-000" + string(rune('a'+at)) + "0002"
		byID[older] = pairMemory(older, model.TypeCaution, "aimemorytool")
		byID[newer] = pairMemory(newer, model.TypeCaution, "aimemorytool")
		found.Near = append(found.Near, quality.NearPair{Left: older, Right: newer, Align: 0.5,
			LeftText: left, RightText: right})
	}
	return found, byID
}

func contradictCount(found quality.RepoReport) int {
	count := 0
	for _, list := range found.ByID {
		for _, one := range list {
			if one.Rule == quality.RuleContradictCand {
				count++
			}
		}
	}
	return count
}

func runContradicts(found *quality.RepoReport, byID map[string]*model.Memory, nli *NLIOptions) *Report {
	report := &Report{Counts: map[string]int{}, ByScope: map[string]map[string]int{}}
	report.addContradicts(found, byID, Options{Now: now, NLI: nli})
	return report
}

func hasNote(report *Report, part string) bool {
	for _, note := range report.Notes {
		if strings.Contains(note, part) {
			return true
		}
	}
	return false
}

// 「기본 켬」↔「기본 끔」 은 서버 없이 규칙 층에서 걸린다. 작은 id 에 달고 Related 는 큰 id.
func TestContradictRuleLayerCatchesFlip(t *testing.T) {
	found, byID := pairsFixture(1, flipLeft, flipRight)
	report := runContradicts(&found, byID, nil)
	list := found.ByID["20260101-000a0001"]
	if len(list) != 1 || list[0].Rule != quality.RuleContradictCand || list[0].Related[0] != "20260102-000a0002" {
		t.Fatalf("규칙 층이 켬↔끔 을 못 잡았다 : %+v", found.ByID)
	}
	if len(report.Notes) != 0 {
		t.Fatalf("--nli 를 안 줬으면 「못 본 것」이 없어야 한다 : %v", report.Notes)
	}
}

// 규칙 층이 답을 못 내는 짝은 NLI 가 반대 0.9 를 주면 걸린다. 근거 = 옛 문장이다.
func TestContradictNLIContradictCatches(t *testing.T) {
	server, asked := fakeNLI(t, nliContradict)
	found, byID := pairsFixture(1, plainLeft, plainRight)
	report := runContradicts(&found, byID, nliOptions(t, server.URL, 0))
	if asked.Load() != 1 || contradictCount(found) != 1 {
		t.Fatalf("NLI 반대 0.9 가 큐에 안 올랐다 : 물음 %d · %+v · %v", asked.Load(), found.ByID, report.Notes)
	}
	if !strings.Contains(found.ByID["20260101-000a0001"][0].Reason, "NLI 반대 0.90") {
		t.Fatalf("까닭에 판정 단이 없다 : %+v", found.ByID)
	}
}

// NLI 가 확신선 아래로 반대를 주면 안 올린다.
func TestContradictNLIUnsureSkips(t *testing.T) {
	server, _ := fakeNLI(t, `{"a":0.3,"b":0.5,"c":0.2,"model":"fake0001","ms":3.0}`)
	found, byID := pairsFixture(1, plainLeft, plainRight)
	runContradicts(&found, byID, nliOptions(t, server.URL, 0))
	if contradictCount(found) != 0 {
		t.Fatalf("확신선 아래 반대가 큐에 올랐다 : %+v", found.ByID)
	}
}

// 서버가 없으면 규칙 층만 돌고 「못 본 것」에 한 줄 남긴다. 규칙 층의 걸림은 그대로다.
func TestContradictNoServerFallsBackToRules(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	address := closed.URL
	closed.Close()
	found, byID := pairsFixture(2, flipLeft, flipRight)
	found.Near[1].LeftText, found.Near[1].RightText = plainLeft, plainRight
	report := runContradicts(&found, byID, nliOptions(t, address, 0))
	if contradictCount(found) != 1 {
		t.Fatalf("규칙 층 걸림이 하나여야 한다 : %+v", found.ByID)
	}
	if !hasNote(report, "NLI 를 못 써서 규칙 층만 봤다") || len(report.Notes) != 1 {
		t.Fatalf("「못 본 것」 한 줄이 없다 : %v", report.Notes)
	}
	// nli_url 이 아예 없을 때도 같은 한 줄이다.
	found, byID = pairsFixture(1, plainLeft, plainRight)
	report = runContradicts(&found, byID, &NLIOptions{})
	if !hasNote(report, "NLI 를 못 써서 규칙 층만 봤다") {
		t.Fatalf("nli_url 없음을 안 알린다 : %v", report.Notes)
	}
}

// 상한이 3 이면 4번째 짝은 NLI 에 안 묻고, 몇 쌍을 못 물었는지 「못 본 것」에 적는다.
func TestContradictNLIPairCap(t *testing.T) {
	server, asked := fakeNLI(t, nliContradict)
	found, byID := pairsFixture(4, plainLeft, plainRight)
	report := runContradicts(&found, byID, nliOptions(t, server.URL, 3))
	if asked.Load() != 3 {
		t.Fatalf("상한 3 인데 %d번 물었다", asked.Load())
	}
	if contradictCount(found) != 3 || !hasNote(report, "1쌍은 규칙 층만 봤다") {
		t.Fatalf("상한 넘침을 안 알린다 : %d · %v", contradictCount(found), report.Notes)
	}
	if len(found.ByID["20260101-000d0001"]) != 0 {
		t.Fatal("4번째 짝(차례상 마지막)이 걸렸다 — 상한에 밀리는 짝이 매번 같아야 한다")
	}
}

// scope 당 상한도 따로 건다.
func TestContradictNLIPerScopeCap(t *testing.T) {
	server, asked := fakeNLI(t, nliContradict)
	found, byID := pairsFixture(3, plainLeft, plainRight)
	options := nliOptions(t, server.URL, 0)
	options.PerScope = 2
	runContradicts(&found, byID, options)
	if asked.Load() != 2 {
		t.Fatalf("scope 당 상한 2 인데 %d번 물었다", asked.Load())
	}
}

// 다른 scope · 죽은 기억 · 볼 종류가 아닌 짝은 묻지도 않는다.
func TestContradictPairFilter(t *testing.T) {
	server, asked := fakeNLI(t, nliContradict)
	found, byID := pairsFixture(3, plainLeft, plainRight)
	byID["20260102-000a0002"].Scope = "other"
	byID["20260102-000b0002"].SupersededBy = "20260103-00000001"
	byID["20260102-000c0002"].Type = model.TypeHistory
	runContradicts(&found, byID, nliOptions(t, server.URL, 0))
	if asked.Load() != 0 || contradictCount(found) != 0 {
		t.Fatalf("걸러야 할 짝을 물었다 : %d", asked.Load())
	}
}

// 한 짝은 한 큐로만 — C19·C13 에 걸린 짝의 merge(C20)·C17 줄은 지운다. 다른 짝은 둔다.
func TestDropShadowedMergeAndSibling(t *testing.T) {
	byID := map[string][]quality.Finding{
		"a": {{Rule: quality.RuleContradictCand, ID: "a", Related: []string{"b"}},
			{Rule: quality.RuleMergeCandidate, ID: "a", Related: []string{"b"}},
			{Rule: quality.RuleMergeCandidate, ID: "a", Related: []string{"z"}}},
		"b": {{Rule: quality.RuleNewerSibling, ID: "b", Related: []string{"a"}}},
		"c": {{Rule: quality.RuleStaleConflictPair, ID: "c", Related: []string{"d"}}},
		"d": {{Rule: quality.RuleMergeCandidate, ID: "d", Related: []string{"c"}}},
	}
	dropShadowed(byID)
	if len(byID["a"]) != 2 || byID["a"][1].Related[0] != "z" {
		t.Fatalf("a 의 merge(b) 만 빠져야 한다 : %+v", byID["a"])
	}
	if len(byID["b"]) != 0 || len(byID["d"]) != 0 {
		t.Fatalf("C17·C13 짝의 줄이 남았다 : %+v · %+v", byID["b"], byID["d"])
	}
}

// putCaution 은 제목·요약이 서로 다른 caution 하나를 놓는다. put 은 제목·요약이 늘 같아
// 가장 닮은 문장 짝이 요약끼리로 잡힌다 — C19 가 볼 본문 문장 짝이 안 나온다.
func putCaution(t *testing.T, opened *store.Store, id, title, summary, body string) {
	t.Helper()
	text := "---\nid: " + id + "\ntype: caution\nseverity: medium\n" +
		"title: " + title + "\nsummary: " + summary + "\n" +
		"tags: [hook, korean]\nscope: aimemorytool\ndate: 2026-01-05\nauthor: human:mirusona\n" +
		"sources: [\"file:internal/hook/hook.go\"]\n---\n\n" + body
	dir := filepath.Join(opened.StoreDir(), "2026", "01")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Run 끝까지 — quality 가 뽑은 닮은 짝이 규칙 층을 지나 contradict 큐에 서고, 같은 짝은
// merge 큐에 안 선다 (한 짝은 한 큐로만).
func TestContradictReachesQueueThroughRun(t *testing.T) {
	opened := newRepo(t)
	putCaution(t, opened, "20260105-aaaa0001", "검색 캐시 켜 두기", "첫 질의 지연을 줄이려고 캐시를 켠 채로 둔다",
		"검색 결과 캐시는 기본 켬 으로 둔다.\n첫 질의가 느린 것을 막으려고 4096 건까지만 담는다.\n")
	putCaution(t, opened, "20260105-bbbb0002", "메모리 모자람 주의", "메모리가 모자라 색인 크기를 먼저 본다",
		"검색 결과 캐시는 기본 끔 으로 둔다.\n메모리가 모자라 2026-02-01 에 바꿨고 색인 크기가 문제였다.\n")
	report := runOn(t, opened, Options{Kinds: []string{KindContradict, KindMerge}})
	if len(report.Items) != 1 || report.Items[0].Kind != KindContradict ||
		report.Items[0].ID != "20260105-aaaa0001" || report.Items[0].Related[0] != "20260105-bbbb0002" {
		t.Fatalf("C19 하나만 contradict 큐에 서야 한다 : %+v", report.Items)
	}
	if len(report.Items[0].Next) != 3 {
		t.Fatalf("다음 명령은 show 둘과 --by 하나다 : %v", report.Items[0].Next)
	}
}
