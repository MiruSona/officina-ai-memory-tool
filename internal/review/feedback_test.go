package review

import (
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
)

// 점검·정리 피드백(Docs/Todo/2026-10-10-기억점검정리피드백.md) 의 review 몫 시험이다.

// TestConflictKeepsC08OverC05 는 한 기억에 C05·C08 이 같이 걸리면 conflict 줄이
// 하나만 남는지 본다. 둘 다 「같은 자리에 산 결정이 둘」이라 같은 짝을 두 번 센다.
func TestConflictKeepsC08OverC05(t *testing.T) {
	byID := map[string][]quality.Finding{
		"20260105-aaaa0001": {
			{Rule: quality.RuleDecisionConflictLive, ID: "20260105-aaaa0001", Related: []string{"20260105-aaaa0002"}},
			{Rule: quality.RuleSupersedeMissing, ID: "20260105-aaaa0001", Related: []string{"20260105-aaaa0002"}},
		},
		// C08 이 없는 기억의 C05 는 그대로 둔다.
		"20260105-aaaa0003": {
			{Rule: quality.RuleDecisionConflictLive, ID: "20260105-aaaa0003", Related: []string{"20260105-aaaa0004"}},
		},
	}
	dropShadowed(byID)
	both := byID["20260105-aaaa0001"]
	if len(both) != 1 || both[0].Rule != quality.RuleSupersedeMissing {
		t.Fatalf("C05·C08 이 같이 걸린 기억은 C08 한 줄이어야 한다 : %+v", both)
	}
	if len(byID["20260105-aaaa0003"]) != 1 {
		t.Fatalf("C08 이 없는 기억의 C05 를 지웠다 : %+v", byID["20260105-aaaa0003"])
	}
}

// TestMergeNextOffersLink 는 merge 큐가 「합치기」 말고 「잇기만」 길도 주는지 본다.
// 실사용에서 merge 3짝은 주제가 달라 links 로 잇는 게 맞았다.
func TestMergeNextOffersLink(t *testing.T) {
	found := quality.Finding{Rule: quality.RuleMergeCandidate, ID: "20260105-aaaa0001",
		Related: []string{"20260105-aaaa0002"}}
	next := strings.Join(nextFor(KindMerge, found), "\n")
	if !strings.Contains(next, "mem set 20260105-aaaa0001 --link 20260105-aaaa0002") {
		t.Fatalf("merge 다음 명령에 --link 줄이 없다 :\n%s", next)
	}
	if !strings.Contains(next, "mem add") {
		t.Fatalf("합치기 줄이 사라졌다 :\n%s", next)
	}
}

// TestBasisNextKeepsRelinkLine 은 links 로 덮인 기억을 단 C14 가 review 큐에서도
// `mem set --links <새 id>` 줄을 주는지 본다. quality 의 relinkNext 가 채운 줄이다.
func TestBasisNextKeepsRelinkLine(t *testing.T) {
	opened := newRepo(t)
	put(t, opened, "20260105-aaaa0001", "decision",
		"superseded_by: 20260105-aaaa0002\ninvalid_at: 2026-02-01\n", "")
	put(t, opened, "20260105-aaaa0002", "decision", "", "")
	put(t, opened, "20260105-aaaa0003", "history", "links: [20260105-aaaa0001]\n", "")
	report := runOn(t, opened, Options{Kinds: []string{KindBasis}})
	for _, item := range report.Items {
		if item.ID != "20260105-aaaa0003" {
			continue
		}
		next := strings.Join(item.Next, "\n")
		if !strings.Contains(next, "mem set 20260105-aaaa0003 --links 20260105-aaaa0002") {
			t.Fatalf("basis 다음 명령에 --links 줄이 없다 :\n%s", next)
		}
		if !strings.Contains(next, "mem show 20260105-aaaa0003") {
			t.Fatalf("basis 기본 명령(BasisNext)이 사라졌다 :\n%s", next)
		}
		return
	}
	t.Fatalf("links 로 덮인 기억을 단 기억이 basis 큐에 없다 : %+v", report.Items)
}
