package main

// A2 코드리뷰(13 소단계) 지적 시험 — --json 거절 기록 · 자식 시간 상한 · 인용 길이 · 마지막 JSON 줄.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
	"github.com/mirusona/officina-ai-memory-tool/internal/retain"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// testChildHang 이 켜진 자식은 끝나지 않는다 (TestMain).
const testChildHang = "MEM_TEST_CHILD_HANG"

// retain 은 늘 --json 이다 — 그 길의 거절도 log.md 에 남아야 report 가 센다. 인용 조각은 안 싣는다.
func TestAutoJSONRejectLogsRuleAndTitleOnly(t *testing.T) {
	memory := autoRepo(t)
	missing := "대화에 없는 인용 문장이다 ZQXW 열다섯 자를 넘긴다"
	args := append(addArgs("본문 한 줄"), "--origin", "stop", "--session", autoSession[:8],
		"--quotes", missing, "--json")
	if _, code := capture(t, func() int { return run(args) }); code != exitCheck {
		t.Fatalf("R1 거절 · 종료 2 여야 한다 : %d", code)
	}
	raw, _ := os.ReadFile(store.LogPath(memory))
	text := string(raw)
	if !strings.Contains(text, "거절 자동 · stop · R1-quote · `trigram 이 0건을 낸다`") {
		t.Fatalf("log.md 에 규칙과 제목이 있어야 한다 :\n%s", text)
	}
	if strings.Contains(text, "ZQXW") {
		t.Fatalf("log.md 에 인용 조각이 샜다 :\n%s", text)
	}
	before := strings.Count(text, "R1-quote")
	capture(t, func() int { return run(append(args, "--check")) })
	raw, _ = os.ReadFile(store.LogPath(memory))
	if strings.Count(string(raw), "R1-quote") != before {
		t.Fatal("--check 는 미리보기라 log.md 에 안 남긴다")
	}
}

func TestChildTimeoutBounds(t *testing.T) {
	if got := childTimeout(3, 5000); got != childMinDuration {
		t.Fatalf("짧으면 하한 90초다 : %s", got)
	}
	if got := childTimeout(3, 30000); got != 210*time.Second {
		t.Fatalf("30초 × (3+2) + 60초 = 210초여야 한다 : %s", got)
	}
}

// 끝나지 않는 자식은 상한에서 죽이고 오류로 센다 — retain 판이 멈추지 않는다.
func TestRunChildKillsHungChild(t *testing.T) {
	t.Setenv(testChildHang, "1")
	started := time.Now()
	_, err := runChild([]string{"version"}, "", 500*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "child timeout") {
		t.Fatalf("시간 넘김 오류여야 한다 : %v", err)
	}
	if time.Since(started) > 30*time.Second {
		t.Fatalf("상한에서 끊지 못했다 : %s", time.Since(started))
	}
}

func TestChildQuotesCapTotalLength(t *testing.T) {
	long := strings.Repeat("가", 1500)
	got := childQuotes([]string{long, long, "짧은 인용 문장이다"})
	total := 0
	for _, quote := range got {
		total += len([]rune(quote))
	}
	if total != childQuoteRoom || len(got) != 2 || !strings.HasPrefix(long, got[1]) {
		t.Fatalf("합계 %d자에서 잘라야 한다 : %d자 %d개", childQuoteRoom, total, len(got))
	}
	short := []string{"짧은 인용 문장이다", "둘째 인용 문장이다"}
	if kept := childQuotes(short); strings.Join(kept, "|") != strings.Join(short, "|") {
		t.Fatalf("상한 안이면 그대로다 : %v", kept)
	}
}

// statusGenerate 는 /v1/models 에는 답하고 생성 요청에는 늘 status 로 답하는 가짜 Strata 다.
func statusGenerate(t *testing.T, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"data":[]}`)
			return
		}
		w.WriteHeader(status)
		io.WriteString(w, `{"error":"context length exceeded"}`)
	}))
	t.Cleanup(server.Close)
	return server
}

// 생성 400(문맥 초과 등)은 그 세션만의 문제다 — error 로 표시하고 판은 이어 간다. 5xx 는 멈춘다 (리뷰 13 #2).
func TestRetainGenerateErrorSplitsByStatus(t *testing.T) {
	memory := retainRepo(t)
	useRetainLLM(t, statusGenerate(t, http.StatusBadRequest).URL, fakeNLI(t, 0.97, 0).URL)
	out, code := captureBoth(t, func() int { return run([]string{"retain"}) })
	entry := queueEntryOf(t, memory)
	if code != exitOK || entry.Result != retain.ResultError || entry.DoneAt == "" {
		t.Fatalf("400 은 그 세션을 error 로 표시한다 : %d %+v\n%s", code, entry, out)
	}
	memory = retainRepo(t)
	useRetainLLM(t, statusGenerate(t, http.StatusBadGateway).URL, fakeNLI(t, 0.97, 0).URL)
	out, code = captureBoth(t, func() int { return run([]string{"retain"}) })
	if entry := queueEntryOf(t, memory); code != exitOK || entry.DoneAt != "" {
		t.Fatalf("502 는 표시 없이 판을 멈춘다 : %d %+v\n%s", code, entry, out)
	}
}

// 자식 stdout 에 사람용 글이 섞여도 판정 JSON 줄을 읽는다. 저장 때는 JSON 뒤에 id 줄이 붙는다.
func TestLastJSONSkipsHumanLines(t *testing.T) {
	text := "{사람용 글이 중괄호로 시작한다\n{\"id\":\"20261019-aaaaaaaa\",\"findings\":[]}\n20261019-aaaaaaaa\n"
	parsed, found := lastJSON(text)
	if !found || parsed.ID != "20261019-aaaaaaaa" {
		t.Fatalf("판정 줄을 못 읽었다 : %v %+v", found, parsed)
	}
	if _, found := lastJSON("저장됨\n"); found {
		t.Fatal("JSON 이 없는데 찾았다")
	}
}

// 자동 관문과 기존 관문이 같은 이름을 내도 거절 규칙 목록에는 한 번만 적는다 (리뷰 13 #12).
func TestAutoRejectRulesDedupAcrossLists(t *testing.T) {
	verdict := quality.Verdict{Findings: []quality.Finding{{Rule: quality.RuleBodyThin, Level: quality.GradeReject}}}
	auto := autoAdd{Result: retain.Result{Rejects: []retain.Finding{
		{Rule: retain.RuleFragment}, {Rule: quality.RuleBodyThin}, {Rule: retain.RuleFragment}}}}
	got := strings.Join(autoRejectRules(verdict, auto), ",")
	if got != retain.RuleFragment+","+quality.RuleBodyThin {
		t.Fatalf("이름이 겹쳐 적혔다 : %s", got)
	}
}
