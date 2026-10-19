package retain

import (
	"strings"
	"testing"
)

// A2 첫 실측 5절 — retain:* 의 R3 꼴 보완 : 인용별로 못 넘으면 묶음으로 한 번 더 · 제목 반대 거절 ·
// 까닭 글에 세 칸 확률. 가짜 NLI(scriptJudge)로 본다.

const (
	bundleQuote1 = "훅 지연이 길어서 상태 파일을 오프셋으로 읽게 바꾸자"
	bundleQuote2 = "상태 파일을 오프셋으로 읽게 바꾸자. p95 가 180ms 였다"
	bundleQuote3 = "길어서 상태 파일을 오프셋으로 읽게"
)

// verdictOf 는 가짜 판정 하나다.
type verdictOf struct {
	supported, ok bool
	probs         map[string]float64
}

// scriptJudge 는 (근거, 주장) 마다 answer 가 정한 답을 내고 부른 차례를 적는 판정자다 (llm.Judge 꼴).
type scriptJudge struct {
	calls  *[]string
	last   *map[string]float64
	answer func(evidence, claim string) verdictOf
}

func newScriptJudge(answer func(evidence, claim string) verdictOf) scriptJudge {
	return scriptJudge{calls: &[]string{}, last: &map[string]float64{}, answer: answer}
}

func (s scriptJudge) Supports(evidence, claim string) (bool, bool) {
	*s.calls = append(*s.calls, evidence+" | "+claim)
	got := s.answer(evidence, claim)
	*s.last = got.probs
	return got.supported, got.ok
}
func (s scriptJudge) Probs() map[string]float64 { return *s.last }
func (s scriptJudge) Problem() string           { return "" }

var (
	probsUnrelated = map[string]float64{"A": 0.30, "B": 0.10, "C": 0.60}
	probsSupport   = map[string]float64{"A": 0.95, "B": 0.02, "C": 0.03}
	probsTitleOK   = map[string]float64{"A": 0.20, "B": 0.10, "C": 0.70}
)

// isBundle 는 근거가 인용 둘 이상을 이은 묶음인지다.
func isBundle(evidence string) bool {
	return strings.Contains(evidence, bundleQuote1) && strings.Contains(evidence, bundleQuote2)
}

func strictCandidate(fixture gateFixture, quotes ...string) Candidate {
	return Candidate{Memory: fixture.memory, Origin: "retain:strata", Quotes: quotes}
}

func TestStrictBundlePassesWhenEachQuoteFails(t *testing.T) {
	fixture := newGateFixture(t)
	judge := newScriptJudge(func(evidence, claim string) verdictOf {
		switch {
		case claim == fixture.memory.Title:
			return verdictOf{ok: true, probs: probsTitleOK}
		case isBundle(evidence):
			return verdictOf{supported: true, ok: true, probs: probsSupport}
		}
		return verdictOf{ok: true, probs: probsUnrelated}
	})
	context := fixture.context
	context.Judge = judge
	if result := Check(strictCandidate(fixture, bundleQuote1, bundleQuote2), context); result.Rejected() {
		t.Fatalf("묶음이 지지하면 통과다 : %+v", result.Rejects)
	}
	if len(*judge.calls) != 4 {
		t.Fatalf("인용 둘 + 묶음 + 제목 = 4번이어야 한다 : %v", *judge.calls)
	}
}

func TestStrictBundleAlsoFailsRejectsWithProbs(t *testing.T) {
	fixture := newGateFixture(t)
	judge := newScriptJudge(func(string, string) verdictOf { return verdictOf{ok: true, probs: probsUnrelated} })
	context := fixture.context
	context.Judge = judge
	result := Check(strictCandidate(fixture, bundleQuote1, bundleQuote2, bundleQuote3), context)
	if rulesOf(result) != RuleSupport {
		t.Fatalf("묶음도 못 넘으면 R3 거절 하나다 : %q", rulesOf(result))
	}
	reason := result.Rejects[0].Reason
	if !strings.Contains(reason, "a/b/c 0.30/0.10/0.60") || !strings.HasPrefix(reason, "근거 문장을 다 이어도") {
		t.Fatalf("까닭 글에 묶음 판정과 세 칸 확률이 있어야 한다 : %q", reason)
	}
	// 상한 : 인용 수 + 1. 거절이라 제목은 안 묻는다.
	if len(*judge.calls) != 4 {
		t.Fatalf("판정 호출은 인용 셋 + 묶음 = 4번이어야 한다 : %d %v", len(*judge.calls), *judge.calls)
	}
}

func TestStrictSingleQuoteDoesNotAskBundle(t *testing.T) {
	fixture := newGateFixture(t)
	judge := newScriptJudge(func(string, string) verdictOf { return verdictOf{ok: true, probs: probsUnrelated} })
	context := fixture.context
	context.Judge = judge
	if result := Check(strictCandidate(fixture, bundleQuote1), context); rulesOf(result) != RuleSupport {
		t.Fatalf("인용 하나가 못 넘으면 거절이다 : %q", rulesOf(result))
	}
	if len(*judge.calls) != 1 {
		t.Fatalf("인용이 하나면 묶음은 같은 글이라 다시 안 묻는다 : %v", *judge.calls)
	}
}

func TestStrictTitleContradictionRejects(t *testing.T) {
	cases := []struct {
		name    string
		against float64
		reject  bool
	}{
		{"반대 0.70", 0.70, true},
		{"반대 0.50 (문턱)", 0.50, true},
		{"반대 0.40", 0.40, false},
	}
	for _, one := range cases {
		fixture := newGateFixture(t)
		judge := newScriptJudge(func(evidence, claim string) verdictOf {
			if claim == fixture.memory.Title {
				return verdictOf{ok: false, probs: map[string]float64{"A": 0.1, "B": one.against, "C": 0.9 - one.against}}
			}
			return verdictOf{supported: true, ok: true, probs: probsSupport}
		})
		context := fixture.context
		context.Judge = judge
		result := Check(strictCandidate(fixture, bundleQuote1), context)
		if result.Rejected() != one.reject {
			t.Errorf("%s : 거절 %v 여야 한다 : %+v", one.name, one.reject, result.Rejects)
			continue
		}
		if one.reject && !strings.HasPrefix(result.Rejects[0].Reason, "제목이 근거와 어긋난다") {
			t.Errorf("%s : 까닭 글이 제목 반대여야 한다 : %q", one.name, result.Rejects[0].Reason)
		}
	}
}

// (가) stop 은 묶음·제목을 안 묻는다 — 근거마다 지지를 받아야 하는 옛 규칙 그대로다.
func TestLooseKeepsPerQuoteRule(t *testing.T) {
	fixture := newGateFixture(t)
	judge := newScriptJudge(func(evidence, _ string) verdictOf {
		if isBundle(evidence) {
			return verdictOf{supported: true, ok: true, probs: probsSupport}
		}
		return verdictOf{ok: true, probs: probsUnrelated}
	})
	context := fixture.context
	context.Judge = judge
	candidate := strictCandidate(fixture, bundleQuote1, bundleQuote2)
	candidate.Origin = OriginStop
	result := Check(candidate, context)
	if rulesOf(result) != RuleSupport+" "+RuleSupport || len(*judge.calls) != 2 {
		t.Fatalf("stop 은 근거마다 거절하고 묶음을 안 묻는다 : %q %v", rulesOf(result), *judge.calls)
	}
	if !strings.Contains(result.Rejects[0].Reason, "a/b/c 0.30/0.10/0.60") {
		t.Fatalf("stop 의 까닭 글에도 세 칸 확률이 있어야 한다 : %q", result.Rejects[0].Reason)
	}
}
