package llm

import (
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
)

// 서버 응답의 truncated 는 판정을 안 바꾸고 기록(Verdict · Judge.Truncated)에만 남는다.
func TestNLITruncatedIsRecorded(t *testing.T) {
	server, _ := newNLI(t, `{"a":0.95,"b":0.03,"c":0.02,"model":"abcd1234","ms":2.0,"truncated":true}`, 0, 0)
	judge := &Judge{NLI: nliFor(server.URL, 1000), Only: []string{StageNLI}}
	supported, ok := judge.Supports(plainEvidence, plainClaim)
	if !supported || !ok || !judge.Truncated() {
		t.Fatalf("잘림 표시가 없거나 판정이 바뀌었다 : %v %v %v", supported, ok, judge.Truncated())
	}
	plain, _ := newNLI(t, `{"a":0.95,"b":0.03,"c":0.02,"model":"abcd1234","ms":2.0}`, 0, 0)
	judge = &Judge{NLI: nliFor(plain.URL, 1000), Only: []string{StageNLI}}
	if judge.Supports(plainEvidence, plainClaim); judge.Truncated() {
		t.Fatal("칸이 없으면 잘림 아님이다")
	}
}

// 지지 선은 NewNLI 에 넘긴 NLISureSupport 를 따른다 — retain 은 이 칸에 retain_sure_support 를 넣는다.
func TestNLISureSupportFromSettings(t *testing.T) {
	server, _ := newNLI(t, `{"a":0.86,"b":0.10,"c":0.04,"model":"abcd1234","ms":2.0}`, 0, 0)
	for _, one := range []struct {
		sure float64
		ok   bool
	}{{0.89, false}, {0.85, true}} {
		nli := NewNLI(config.LLMConfig{NLIURL: server.URL, NLITimeoutMS: 1000, NLISure: 0.5, NLISureSupport: one.sure})
		judge := &Judge{NLI: nli, Only: []string{StageNLI}}
		if _, ok := judge.Supports(plainEvidence, plainClaim); ok != one.ok {
			t.Fatalf("선 %.2f 에서 a .86 의 ok 는 %v 여야 한다", one.sure, one.ok)
		}
	}
}
