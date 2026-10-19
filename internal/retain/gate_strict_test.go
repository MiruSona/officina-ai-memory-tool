package retain

import (
	"strings"
	"testing"
)

// A2 설계 5절 — retain:* 는 R3 판정을 못 받으면 거절이다. stop 은 지금처럼 경고다.
func TestGateStrictRejectsWithoutVerdict(t *testing.T) {
	cases := []struct {
		name  string
		judge Judge
		kind  string
	}{
		{"못 닿음", countingJudge{calls: new(int), ok: false, problem: "nli:unreachable"}, SupportUnreachable},
		{"시간 넘김", countingJudge{calls: new(int), ok: false, problem: "nli:timeout"}, SupportTimeout},
		{"모른다", countingJudge{calls: new(int), ok: false, problem: "unsure a=0.55"}, SupportUnsure},
		{"물을 단 없음", skippingJudge{skipped: true}, SupportSkipped},
		{"서버 5xx", countingJudge{calls: new(int), ok: false, problem: "nli:http-503"}, SupportUnreachable},
		// 아래는 그 후보만 거절하고 판은 이어 간다 (리뷰 13 #2 — 전에는 다 unreachable 로 접혀 큐가 막혔다).
		{"비밀 꼴", countingJudge{calls: new(int), ok: false, problem: "secret-shape"}, SupportRefused},
		{"이상한 답", countingJudge{calls: new(int), ok: false, problem: "nli:bad-answer"}, SupportBad},
		{"4xx", countingJudge{calls: new(int), ok: false, problem: "nli:http-422"}, SupportBad},
		{"까닭 모름", fakeJudge{ok: false}, SupportBad},
	}
	for _, one := range cases {
		fixture := newGateFixture(t)
		candidate := Candidate{Memory: fixture.memory, Origin: "retain:strata",
			Quotes: []string{"훅 지연이 길어서 상태 파일을 오프셋으로 읽게 바꾸자"}}
		context := fixture.context
		context.Judge = one.judge
		result := Check(candidate, context)
		if rulesOf(result) != RuleSupport {
			t.Errorf("%s : R3 거절 하나여야 한다 : %q %v", one.name, rulesOf(result), result.Warnings)
			continue
		}
		if !strings.HasPrefix(result.Rejects[0].Reason, one.kind) {
			t.Errorf("%s : 까닭 글이 %s 로 시작해야 한다 : %q", one.name, one.kind, result.Rejects[0].Reason)
		}
		down := one.kind == SupportUnreachable || one.kind == SupportTimeout
		if SupportDown(result.Rejects[0].Reason) != down {
			t.Errorf("%s : 서버가 끊긴 것인지 잘못 셌다", one.name)
		}
	}
}

// R4 까닭 글에 겹친 기억 id 를 싣는다 — 실측에서 어느 기억과 겹쳤는지 못 짚었다 (첫 실측 5절 3).
func TestGateR4ReasonNamesDuplicate(t *testing.T) {
	fixture := newGateFixture(t)
	candidate := Candidate{Memory: fixture.memory, Origin: "retain:strata",
		Quotes:    []string{"훅 지연이 길어서 상태 파일을 오프셋으로 읽게 바꾸자"},
		GateRules: []string{"duplicate-hard"}, DupIDs: []string{"20261017-0123b331"}}
	result := Check(candidate, fixture.context)
	if rulesOf(result) != RuleNoMerge || !strings.Contains(result.Rejects[0].Reason, "duplicate-hard → 20261017-0123b331") {
		t.Fatalf("R4 까닭 글에 겹친 id 가 있어야 한다 : %+v", result.Rejects)
	}
}

func TestGateStopKeepsWarning(t *testing.T) {
	fixture := newGateFixture(t)
	candidate := Candidate{Memory: fixture.memory, Origin: OriginStop,
		Quotes: []string{"훅 지연이 길어서 상태 파일을 오프셋으로 읽게 바꾸자"}}
	context := fixture.context
	context.Judge = countingJudge{calls: new(int), ok: false, problem: "unsure a=0.55"}
	if result := Check(candidate, context); result.Rejected() || len(result.Warnings) == 0 {
		t.Fatalf("stop 은 못 받으면 경고로 흘린다 : %+v", result)
	}
	context.Judge = skippingJudge{skipped: true}
	if result := Check(candidate, context); result.Rejected() {
		t.Fatalf("stop 은 물을 단이 없으면 조용히 넘긴다 : %+v", result)
	}
}

func TestGateStrictPassesWhenSupported(t *testing.T) {
	fixture := newGateFixture(t)
	candidate := Candidate{Memory: fixture.memory, Origin: "retain:strata",
		Quotes: []string{"훅 지연이 길어서 상태 파일을 오프셋으로 읽게 바꾸자"}}
	context := fixture.context
	context.Judge = fakeJudge{answer: true, ok: true}
	if result := Check(candidate, context); result.Rejected() {
		t.Fatalf("지지를 받으면 통과다 : %s", rulesOf(result))
	}
}
