package quality

import (
	"fmt"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// B-05 · B08 재상향 (2026-10-07 설계) 시험.

// gapMemory 는 요약 이름씨가 본문에 하나도 없는 결정이다 (받친 비율 0).
func gapMemory(pinned bool) *model.Memory {
	return &model.Memory{ID: "m1", Type: model.TypeDecision, Pinned: pinned,
		Summary: "사과 포도 수박 참외 딸기",
		Body:    "전혀 다른 이야기를 적은 본문이다.\n두 번째 줄도 다른 이야기다."}
}

func b08Options(on bool) Options {
	opt := Options{Config: config.Default("")}
	opt.Config.Quality.SummaryBodyReject = on
	return opt
}

func TestB08OffKeepsWarn(t *testing.T) {
	opt := b08Options(false)
	opt.addStage = true
	one, ok := summaryBodyFinding(gapMemory(false), opt)
	old, _ := summaryBodyGap(gapMemory(false), false, nil)
	if !ok || one.Level != GradeWarn || one.Reason != old {
		t.Fatalf("꺼짐인데 기존과 다르다 : %+v", one)
	}
}

func TestB08OnRejectsOnlyDecisionAtAdd(t *testing.T) {
	opt := b08Options(true)
	if one, _ := summaryBodyFinding(gapMemory(false), opt); one.Level != GradeWarn {
		t.Errorf("lint(Check) 는 켜도 경고여야 한다 : %q", one.Level)
	}
	opt.addStage = true
	if one, _ := summaryBodyFinding(gapMemory(false), opt); one.Level != GradeReject {
		t.Errorf("결정 · add · 비율 0 이면 거절이어야 한다 : %q", one.Level)
	}
	if one, _ := summaryBodyFinding(gapMemory(true), opt); one.Level != GradeWarn {
		t.Errorf("pin 이면 경고여야 한다 : %q", one.Level)
	}
	history := gapMemory(false)
	history.Type = model.TypeHistory
	if one, _ := summaryBodyFinding(history, opt); one.Level == GradeReject {
		t.Errorf("결정 아닌 종류는 거절하면 안 된다")
	}
	// 받친 비율 0.4(2/5) — 경고 자에는 걸리고 거절 문턱(0.25) 위다.
	half := gapMemory(false)
	half.Body = "사과 포도 만 본문에 있다.\n나머지는 다른 이야기다."
	if one, ok := summaryBodyFinding(half, opt); ok && one.Level == GradeReject {
		t.Errorf("받친 비율 0.25 이상은 거절하면 안 된다")
	}
}

func TestB08NilCanonSafe(t *testing.T) {
	if _, share := summaryBodyGap(gapMemory(false), true, nil); share != 0 {
		t.Errorf("canon 이 nil 이어도 돌아야 한다 : share %v", share)
	}
	// norm 은 조사를 떼서 「색인을」 이 「색인」 을 받친다.
	m := &model.Memory{Type: model.TypeDecision, Summary: "색인 원본 기억 파생물",
		Body: "색인을 다시 만든다.\n원본을 고친다.\n기억을 지킨다.\n파생물을 버린다."}
	if reason, _ := summaryBodyGap(m, true, nil); reason != "" {
		t.Errorf("norm 대조가 조사를 못 뗐다 : %s", reason)
	}
}

func TestUnjudgedCountsUnmarked(t *testing.T) {
	set := &GoldenSet{
		Flag:  []GoldenCase{{ID: "f1", Rules: []string{RuleSummaryBodyMatch}}},
		Clean: []GoldenCase{{ID: "c1"}},
	}
	fired := map[string]map[string]bool{
		"f1": {RuleSummaryBodyMatch: true}, "c1": {RuleSummaryBodyMatch: true},
		"x1": {RuleSummaryBodyMatch: true}, "x2": {RuleSummaryBodyMatch: true}, "x3": {RuleSummaryBodyMatch: true},
	}
	for _, one := range ruleScores(set, fired, config.QualityConfig{}) {
		if one.Rule != RuleSummaryBodyMatch {
			continue
		}
		if one.Unjudged != 3 || !one.FewJudged() {
			t.Fatalf("안 셈은 표시 없는 3건이어야 한다 : %+v", one)
		}
		return
	}
	t.Fatal("B08 점수가 없다")
}

func sampleMemories(count int) []*model.Memory {
	out := []*model.Memory{}
	for at := 0; at < count; at++ {
		m := gapMemory(false)
		m.ID = fmt.Sprintf("m%02d", at)
		out = append(out, m)
	}
	return out
}

func TestSampleRuleSeedAndLimits(t *testing.T) {
	opt := b08Options(false)
	a := SampleRule(sampleMemories(45), RuleSummaryBodyMatch, opt, 30, 7)
	b := SampleRule(sampleMemories(45), RuleSummaryBodyMatch, opt, 30, 7)
	if a.Total != 45 || len(a.Items) != 30 || a.TooFew {
		t.Fatalf("상한 30 · 전체 45 여야 한다 : total %d · items %d", a.Total, len(a.Items))
	}
	for at := range a.Items {
		if a.Items[at].ID != b.Items[at].ID {
			t.Fatalf("씨앗이 같은데 목록이 다르다")
		}
	}
	if few := SampleRule(sampleMemories(9), RuleSummaryBodyMatch, opt, 30, 7); !few.TooFew || len(few.Items) != 9 {
		t.Errorf("10건 미만은 못 잼이어야 한다 : %+v", few)
	}
}

func TestScoreSamplePassLine(t *testing.T) {
	items := func(trues, falses int, pinnedFalse bool) []SampleItem {
		out := []SampleItem{}
		for at := 0; at < trues; at++ {
			out = append(out, SampleItem{ID: fmt.Sprint("t", at), Label: LabelTrue})
		}
		for at := 0; at < falses; at++ {
			out = append(out, SampleItem{ID: fmt.Sprint("f", at), Label: LabelFalse, Pinned: pinnedFalse})
		}
		return out
	}
	if got := ScoreSample(items(20, 1, false), nil); !got.Pass { // 0.952
		t.Errorf("0.952 · pinned 오탐 0 은 통과다 : %+v", got)
	}
	if got := ScoreSample(items(10, 1, false), nil); got.Pass { // 0.909
		t.Errorf("0.909 는 미달이다 : %+v", got)
	}
	if got := ScoreSample(items(30, 1, true), nil); got.Pass || got.PinnedFalse != 1 {
		t.Errorf("pinned 오탐이 있으면 미달이다 : %+v", got)
	}
	if got := ScoreSample(items(9, 0, false), nil); !got.TooFew || got.Pass {
		t.Errorf("9건은 못 잼이다 : %+v", got)
	}
	current := map[string]*model.Memory{"t0": {ID: "t0", Body: "바뀐 본문"}}
	if got := ScoreSample([]SampleItem{{ID: "t0", Hash: BodyHash("옛 본문"), Label: LabelTrue}}, current); got.Rehash != 1 {
		t.Errorf("해시가 바뀌면 다시 판정이다 : %+v", got)
	}
}
