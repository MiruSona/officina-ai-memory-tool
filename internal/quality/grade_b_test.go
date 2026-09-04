package quality

import (
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// v0.4 갈래 B③ — B09·B08 등급을 올린 근거 시험이다.
//
// 올릴 조건 셋(설계 결정 12)은 골든셋으로 쟀다 : 규칙 정밀도 1.000 ·
// 좋은 10 오거절 0 · 대조군 거짓 경보 0. 여기서는 **등급이 실제로 그렇게
// 매겨지는지**와 **안 올린 것이 안 올라갔는지**를 못 박는다.

// gradeOf 는 카탈로그에서 그 규칙의 등급을 본다.
func gradeOf(t *testing.T, name, memoryType string) Grade {
	t.Helper()
	rule, found := Lookup(name)
	if !found {
		t.Fatalf("카탈로그에 %s 가 없다", name)
	}
	return rule.GradeFor(model.DefaultTypes().Spec(memoryType))
}

// TestRelativeDateRejects 는 B09 가 종류를 안 가리고 거절인지다.
func TestRelativeDateRejects(t *testing.T) {
	for _, kind := range []string{model.TypeHistory, model.TypeDecision, model.TypeHowto} {
		if got := gradeOf(t, RuleRelativeDate, kind); got != GradeReject {
			t.Errorf("%s 에서 relative-date 등급이 %q 다 (거절이어야 한다)", kind, got)
		}
	}
}

// TestSummaryBodyStaysWarn 은 B08 이 **어느 종류에서도 경고**인지다.
// 한때 결정에서만 거절로 올렸다가 되돌렸다 (리뷰 B · 메인 판단) — 실기억
// 206건에서 결정 거절 5건 중 둘이 오탐이었고 둘 다 사람이 확정한 pinned
// 결정이었다. 이름씨 대조가 같은 말의 다른 표기를 못 본다.
func TestSummaryBodyStaysWarn(t *testing.T) {
	for _, kind := range []string{model.TypeDecision, model.TypeHistory, model.TypeHowto, model.TypeIssue} {
		if got := gradeOf(t, RuleSummaryBodyMatch, kind); got != GradeWarn {
			t.Errorf("%s 에서 summary-body-match 등급이 %q 다 (경고여야 한다)", kind, got)
		}
	}
}

// TestNoValueStaysWarn 은 B12 를 **안 올렸는지**다. 골든셋 130건에서 한 번도
// 안 울어 정밀도를 못 재고, 좋은 10 중 다섯(G01·G03·G05·G06·G10)에 운다.
func TestNoValueStaysWarn(t *testing.T) {
	for _, kind := range []string{model.TypeHistory, model.TypeDecision, model.TypeHowto} {
		if got := gradeOf(t, RuleNoValue, kind); got != GradeWarn {
			t.Errorf("%s 에서 no-value 등급이 %q 다 (경고여야 한다)", kind, got)
		}
	}
}

// TestM02F06AlreadyStrict 는 설계가 「이미 세다」고 적은 둘을 확인만 한다.
func TestM02F06AlreadyStrict(t *testing.T) {
	if got := gradeOf(t, RuleMultiSummary, model.TypeDecision); got != GradeReject {
		t.Errorf("결정에서 multi-summary 등급이 %q 다", got)
	}
	if got := gradeOf(t, RuleTitleShape, model.TypeHistory); got != GradeReject {
		t.Errorf("title-shape 등급이 %q 다", got)
	}
}

// TestB20B22Grades 는 관문 골든셋의 B20·B22 가 어떻게 되는지다.
// **B20 은 막고(B09 거절) B22 는 경고로 남는다**(B08 을 되돌렸다).
func TestB20B22Grades(t *testing.T) {
	cases := []struct {
		name   string
		memory *model.Memory
	}{
		{"B20", &model.Memory{ID: "20260823-b2000000", Type: model.TypeHistory,
			Title: "어제와 지난주로 적은 기록", Author: "claude-code/opus-5", Date: "2026-08-23",
			Summary: "날짜를 어제 지난주처럼 적어서 나중에 읽으면 언제인지 알 수 없는 경우다",
			Tags:    []string{"hook", "security"}, Scope: "aimemorytool",
			Body: "어제 고쳤다. 지난주에 났던 문제다.\n왜 : 그때는 급했다.\n주의 : 다음 주에 다시 본다."}},
		{"B22", &model.Memory{ID: "20260823-b2200000", Type: model.TypeDecision,
			Title: "요약과 본문이 딴 이야기", Author: "claude-code/opus-5", Date: "2026-08-23",
			Summary: "훅은 세션이 열릴 때만 돌고 저장소 전체를 훑지 않기로 못 박아 정한 것이다",
			Tags:    []string{"hook", "security"}, Scope: "aimemorytool",
			Sources: []string{"file:internal/hook/hook.go"},
			Body: "결론 : 태그 표를 늘렸다.\n왜 : 남의 프로젝트가 첫 기억부터 막혔다.\n" +
				"주의 : 씨앗 목록에는 우리 낱말을 안 넣는다."}},
	}
	want := map[string]bool{"B20": true, "B22": false}
	for _, one := range cases {
		blocked, warned := false, false
		for _, found := range Check(one.memory, testOptions()) {
			if found.Level == GradeReject {
				blocked = true
			}
			if found.Rule == RuleSummaryBodyMatch && found.Level == GradeWarn {
				warned = true
			}
		}
		if blocked != want[one.name] {
			t.Errorf("%s 막힘이 %v 다 (%v 여야 한다)", one.name, blocked, want[one.name])
		}
		if one.name == "B22" && !warned {
			t.Error("B22 가 summary-body-match 경고로도 안 뜬다")
		}
	}
}
