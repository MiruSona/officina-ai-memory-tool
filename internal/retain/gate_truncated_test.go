package retain

import "testing"

// truncScriptJudge 는 늘 잘렸다고 답하는 scriptJudge 다.
type truncScriptJudge struct{ scriptJudge }

func (truncScriptJudge) Truncated() bool { return true }

// R3 물음 중 하나라도 서버가 잘랐다고 하면 Result.Truncated 가 선다. 판정(통과)은 그대로다.
func TestStrictMarksTruncated(t *testing.T) {
	fixture := newGateFixture(t)
	answer := func(evidence, claim string) verdictOf {
		if claim == fixture.memory.Title {
			return verdictOf{ok: true, probs: probsTitleOK}
		}
		return verdictOf{supported: true, ok: true, probs: probsSupport}
	}
	context := fixture.context
	context.Judge = truncScriptJudge{newScriptJudge(answer)}
	result := Check(strictCandidate(fixture, bundleQuote1), context)
	if result.Rejected() || !result.Truncated {
		t.Fatalf("통과 + 잘림 표시여야 한다 : %+v", result)
	}
	context.Judge = newScriptJudge(answer)
	if result := Check(strictCandidate(fixture, bundleQuote1), context); result.Truncated {
		t.Fatal("잘림을 말하지 않는 판정자면 표시가 없다")
	}
}
