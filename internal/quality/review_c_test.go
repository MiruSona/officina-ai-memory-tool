package quality

import (
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
)

// 7단계 리뷰 C — 낱말 표준을 아직 안 정한 저장소는 F09·F12 가 경고다.
// 갓 깐 프로젝트가 첫 add 부터 다 막히면 도구를 아무도 안 쓴다 (실데이터 시험 C5).
func TestLearningVocabWarnsInsteadOfRejecting(t *testing.T) {
	memory := goodMemory()
	memory.Tags = []string{"tilemap", "levelgen"}
	memory.Scope = "mygame-level"
	opt := testOptions()
	opt.Vocab = config.DefaultVocab() // scope 가 하나도 없다 = 배우는 중
	verdict := Gate(memory, opt)
	if verdict.Kind != KindWarn {
		t.Fatalf("배우는 중에는 경고여야 한다 : %v %v", verdict.Kind, verdict.Reasons())
	}
	if !hasRule(verdict.Findings, RuleScopeStandard) {
		t.Fatal("경고로라도 알리기는 해야 한다")
	}
	for _, line := range verdict.Reasons() {
		if strings.Contains(line, "--new-scope") {
			t.Errorf("없는 옵션을 시킨다 : %s", line)
		}
	}
}

// scope 를 하나라도 정하면 그때부터 거절이다.
func TestConfirmedVocabRejects(t *testing.T) {
	memory := goodMemory()
	memory.Scope = "mygame-level"
	opt := testOptions() // testVocab 은 scope 를 정해 둔 표다
	if verdict := Gate(memory, opt); verdict.Kind != KindQualityReject {
		t.Fatalf("표준을 정한 저장소는 거절이어야 한다 : %v", verdict.Kind)
	}
}

// 거절 문구가 시키는 명령은 실제로 있는 것이라야 한다 (실데이터 시험 A2).
func TestNextStepsUseRealCommands(t *testing.T) {
	memory := goodMemory()
	memory.Scope = "mygame-level"
	memory.Tags = []string{"tilemap", "levelgen"}
	verdict := Gate(memory, testOptions())
	want := map[string]bool{"mem tags --add-scope": false, "mem tags --add": false}
	for _, one := range verdict.Findings {
		for _, next := range one.Next {
			for key := range want {
				if strings.HasPrefix(next, key) {
					want[key] = true
				}
			}
			if strings.Contains(next, "--new-scope") || strings.Contains(next, "--id ") {
				t.Errorf("없는 옵션을 시킨다 : %s", next)
			}
		}
	}
	for key, found := range want {
		if !found {
			t.Errorf("%s 를 안 알려준다", key)
		}
	}
}

// B11 — 본문 상한을 넘으면 거절이다 (스트레스 시험 2절 : 301줄이 그냥 들어갔다).
func TestBodyMaxRejects(t *testing.T) {
	memory := goodMemory()
	memory.Body = "결론 : 아주 긴 본문은 관문이 막아야 한다.\n\n" +
		strings.Repeat("한 줄 더 쓴다.\n", 320)
	verdict := Gate(memory, testOptions())
	if !hasRule(verdict.Findings, RuleBodyMax) {
		t.Fatalf("body-max 가 안 걸렸다 : %v", verdict.Rules())
	}
	if verdict.Kind != KindQualityReject {
		t.Fatalf("상한을 넘으면 거절이다 : %v", verdict.Kind)
	}
	if hasRule(verdict.Findings, RuleBodyLong) {
		t.Error("경고와 거절이 같이 뜨면 사람이 뭘 고칠지 모른다")
	}
}

// B03 — 짧아도 문장으로 끝맺은 첫 줄은 결론이다 (실데이터 시험 C7).
func TestShortSentenceIsConclusion(t *testing.T) {
	memory := goodMemory()
	memory.Body = "결론 : 바이트로 자른다.\n\n" + memory.Body
	if hasRule(Gate(memory, testOptions()).Findings, RuleFirstLineConclusion) {
		t.Error("마침표로 끝난 한 줄 결론이 걸렸다")
	}
	memory.Body = "그래서\n\n" + memory.Body
	if !hasRule(Gate(memory, testOptions()).Findings, RuleFirstLineConclusion) {
		t.Error("토막 난 첫 줄은 걸려야 한다")
	}
}

// 닮음·결정 관문이 같은 「다른 주제다」 줄을 두 번 찍지 않는다 (실데이터 시험 A6).
func TestNewTopicStepIsOneLine(t *testing.T) {
	seen := map[string]int{}
	for _, next := range append(nextForDuplicate("20260823-aaaaaaaa"),
		newTopicStep) {
		seen[next]++
	}
	if seen[newTopicStep] != 2 {
		t.Fatalf("두 자리가 같은 문안을 안 쓴다 : %v", seen)
	}
}
