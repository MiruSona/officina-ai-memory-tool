package quality

// 사용 피드백 반영 3 (2026-09-23) — decision-gate 거절문 · 태그 거절 뒤 다음 수 차례.

import (
	"fmt"
	"strings"
	"testing"
)

func findingOf(found []Finding, rule string) *Finding {
	for at := range found {
		if found[at].Rule == rule {
			return &found[at]
		}
	}
	return nil
}

// 결정 관문 거절문은 부딪힌 상대의 제목 · 요약 닮음과 문턱 · 겹친 태그를 싣고,
// 다음 수는 `--new` 가 먼저, `--by` 덮기가 뒤다.
func TestDecisionGateReasonShowsRival(t *testing.T) {
	live := goodMemory()
	live.ID = "20260822-1fae6641"
	live.Summary = "훅은 세션 시작에만 붙이고 그 밖의 자리에는 안 붙인다. 예산은 절마다 다섯 줄이다"
	live.Title = "훅은 세션 시작에만 붙인다"
	fresh := goodMemory()
	fresh.ID = "20260823-99999999"
	fresh.Summary = "훅 예산을 절마다 다섯 줄에서 세 줄로 줄인다. 20k 저장소에서 주입이 8,000바이트를 넘겨서다"
	fresh.Title = "훅 예산을 세 줄로 줄인다"
	opt := testOptions()
	opt.Repo = MemorySlice{live}

	gate := findingOf(Gate(fresh, opt).Findings, RuleDecisionGate)
	if gate == nil {
		t.Fatal("decision-gate 가 안 걸렸다")
	}
	for _, want := range []string{live.ID, live.Title, "요약 닮음", fmt.Sprintf("문턱 %.3f", gateSummaryCut), "hook·security"} {
		if !strings.Contains(gate.Reason, want) {
			t.Errorf("거절문에 `%s` 가 없다 : %s", want, gate.Reason)
		}
	}
	if len(gate.Next) < 2 || gate.Next[0] != newTopicStep {
		t.Fatalf("첫 다음 수가 --new 가 아니다 : %v", gate.Next)
	}
	if !strings.Contains(gate.Next[len(gate.Next)-1], "--by "+live.ID) {
		t.Fatalf("--by 덮기가 뒤에 안 왔다 : %v", gate.Next)
	}
}

// 상대 제목은 남이 쓴 글이다. 줄바꿈·백틱이 거절문 모양을 못 깬다 (불변조건 I3).
func TestDecisionGateReasonNeutralizesTitle(t *testing.T) {
	live := goodMemory()
	live.ID = "20260822-1fae6641"
	live.Title = "훅 `예산`\n두 줄"
	live.Summary = "훅은 세션 시작에만 붙이고 그 밖의 자리에는 안 붙인다. 예산은 절마다 다섯 줄이다"
	fresh := goodMemory()
	fresh.ID = "20260823-99999999"
	fresh.Summary = "훅 예산을 절마다 다섯 줄에서 세 줄로 줄인다. 20k 저장소에서 주입이 8,000바이트를 넘겨서다"
	fresh.Title = "훅 예산을 세 줄로 줄인다"
	reason := rivalReason(fresh, live)
	if strings.Contains(reason, "\n") || strings.Contains(reason, "`예산`") {
		t.Fatalf("상대 제목이 중화되지 않았다 : %q", reason)
	}
}

// 태그 거절 뒤 다음 수는 `mem tags --list` 가 먼저다. --suggest(규격 밖)나
// --add(표준 밖)는 그 뒤다 (사용 피드백 2026-09-21 · 09-22).
func TestTagRejectPointsToListFirst(t *testing.T) {
	shape := goodMemory()
	shape.Tags = []string{"hook", "농장"}
	found := findingOf(Gate(shape, testOptions()).Findings, RuleTagShape)
	if found == nil || len(found.Next) == 0 {
		t.Fatalf("tag-shape 가 안 걸렸다 : %+v", found)
	}
	if !strings.HasPrefix(found.Next[0], "mem tags --list") {
		t.Fatalf("tag-shape 첫 다음 수가 --list 가 아니다 : %v", found.Next)
	}

	standard := goodMemory()
	standard.Tags = []string{"hook", "zzqqxx"}
	found = findingOf(Gate(standard, testOptions()).Findings, RuleTagStandard)
	if found == nil {
		t.Fatal("tag-standard 가 안 걸렸다")
	}
	list, add := -1, -1
	for at, next := range found.Next {
		if strings.HasPrefix(next, "mem tags --list") {
			list = at
		}
		if strings.HasPrefix(next, "mem tags --add") {
			add = at
		}
	}
	if list < 0 || add < 0 || list > add {
		t.Fatalf("tag-standard 에서 --list 가 --add 보다 앞이 아니다 : %v", found.Next)
	}
}

// C04 문턱 0.04 (2026-09-23 조사) — 조사 문서가 재현한 오탐 두 짝은 지나고,
// 사람이 덮은 정답지 짝 중 가장 낮은 값(0.0444)은 그대로 막힌다.
// 0.029 는 옛 주석의 「정말 부딪히는 짝(훅 예산 다섯 줄 ↔ 세 줄)」 값이다. 새
// 문턱에서는 **지나간다** — 오탐을 59% 줄이려고 받아들인 값이다(C05 lint 가 잡는다).
// 근거 : Docs/Research/2026-09-23-결정관문오탐조사.md
func TestGateSummaryCutKeepsBothDirections(t *testing.T) {
	for _, pass := range []float64{0.0187, 0.0265, 0.029, 0.0388} {
		if summaryBlocks(pass) {
			t.Errorf("오탐 짝(닮음 %.4f)을 막는다", pass)
		}
	}
	for _, block := range []float64{0.0444, 0.0694, 0.573} {
		if !summaryBlocks(block) {
			t.Errorf("정말 부딪히는 짝(닮음 %.4f)을 놓친다", block)
		}
	}
}

// 글 두 벌로도 두 방향을 본다. 같은 대상을 다시 정한 짝은 막히고, 같은 자리
// (scope · 태그 둘)라도 대상이 다른 결정은 지난다.
func TestDecisionGatePassesUnrelatedSameTags(t *testing.T) {
	live := goodMemory()
	live.ID = "20260822-1fae6641"
	live.Title = "훅은 세션 시작에만 붙인다"
	live.Summary = "훅은 세션 시작에만 붙이고 그 밖의 자리에는 안 붙인다. 예산은 절마다 다섯 줄이다"
	other := goodMemory()
	other.ID = "20260823-88888888"
	other.Title = "비밀정보 패턴은 기본표를 쓴다"
	other.Summary = "저장소가 패턴을 비워 두면 기본 비밀정보 패턴으로 떨어진다. 검사 없이 지나가는 길은 두지 않는다"
	opt := testOptions()
	opt.Repo = MemorySlice{live}
	if sim := summarySim(other, live); summaryBlocks(sim) {
		t.Fatalf("시험 짝이 문턱 위다 (%.4f) — 짝을 다시 고른다", sim)
	}
	if hasRule(Gate(other, opt).Findings, RuleDecisionGate) {
		t.Error("대상이 다른 결정을 막는다")
	}
}

// 리뷰 2026-09-23 #6 — 상대가 셋을 넘어 Related 를 자를 때, 거절문이 이름 댄
// 가장 닮은 상대가 빠지면 안 된다. id 차례로 맨 뒤인 상대를 가장 닮게 둔다.
func TestDecisionGateRelatedKeepsClosest(t *testing.T) {
	fresh := goodMemory()
	fresh.ID = "20260823-99999999"
	fresh.Title = "훅 예산을 세 줄로 줄인다"
	fresh.Summary = "훅 예산을 절마다 다섯 줄에서 세 줄로 줄인다. 20k 저장소에서 주입이 8,000바이트를 넘겨서다"
	repo := MemorySlice{}
	for at, id := range []string{"20260822-000000a1", "20260822-000000a2", "20260822-000000a3", "20260822-000000a4"} {
		live := goodMemory()
		live.ID = id
		live.Title = "훅 예산은 절마다 다섯 줄이다"
		live.Summary = fmt.Sprintf("훅 예산은 절마다 다섯 줄로 둔다. 넘으면 긴 절부터 깎는다 %d", at)
		repo = append(repo, live)
	}
	closest := repo[3]
	closest.Title = fresh.Title
	closest.Summary = fresh.Summary + " 그대로"
	opt := testOptions()
	opt.Repo = repo
	gate := findingOf(Gate(fresh, opt).Findings, RuleDecisionGate)
	if gate == nil {
		t.Fatal("decision-gate 가 안 걸렸다")
	}
	if len(gate.Related) != dupListMax || gate.Related[0] != closest.ID {
		t.Fatalf("가장 닮은 상대가 Related 맨 앞이 아니다 : %v", gate.Related)
	}
	if !strings.Contains(gate.Reason, closest.ID) {
		t.Fatalf("거절문이 가장 닮은 상대를 안 댄다 : %s", gate.Reason)
	}
}
