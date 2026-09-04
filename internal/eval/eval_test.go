package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 지표 계산 — 작은 고정 결과로 recall@5·MRR·abstain 을 손으로 대 본다 (설계 9-3).
func TestMetricsMath(t *testing.T) {
	counts := tally{}
	counts.add(CaseResult{Kind: KindExtract, Rank: 1})
	counts.add(CaseResult{Kind: KindExtract, Rank: 4})
	counts.add(CaseResult{Kind: KindExtract, Rank: 8})
	counts.add(CaseResult{Kind: KindExtract, Rank: 0, PushedOut: true})
	counts.add(CaseResult{Kind: KindAbstain, Got: nil})
	counts.add(CaseResult{Kind: KindAbstain, Got: []string{"x"}, All: []string{"x"}})
	got := map[string]float64{}
	for _, item := range counts.metrics(100, false, 0, false) {
		got[item.Key] = item.Value
	}
	want := map[string]float64{
		MetricRecall5:      2.0 / 4.0,
		MetricRecall10:     3.0 / 4.0,
		MetricMRR:          (1 + 0.25 + 0.125) / 4.0,
		MetricAbstain:      0.5,
		MetricMisexclusion: 0.25,
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("%s 가 %.4f 여야 하는데 %.4f", key, value, got[key])
		}
	}
}

// 못 닿는 질의는 합격선 계산에서 빠지고 따로 센다 (설계 9-3).
func TestUnreachableIsReportedApart(t *testing.T) {
	report := Report{Cases: 3, Unreachable: 1}
	report.Scored = report.Cases - report.Unreachable
	if report.Scored != 2 {
		t.Fatalf("분모에서 안 뺐다 : %d", report.Scored)
	}
}

// 골든셋은 version 2 를 읽고 잘못된 것은 한국어 한 문장으로 거절한다.
func TestLoadGoldenSet(t *testing.T) {
	path := writeGolden(t, `version: 2
cases:
  - q: 훅 예산
    kind: extract
    hard: true
    lang: en
    expect: [20260822-47bf15db]
  - q: 훅 왜
    kind: extract
    unreachable: true
    expect: [20260822-38f16960]
  - q: 셰이더
    kind: abstain
    expect: []
    not_expect: [20260822-01efa3e3]
`)
	set, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if set.Version != 2 || len(set.Cases) != 3 {
		t.Fatalf("골든셋을 잘못 읽었다 : %+v", set)
	}
	if !set.Cases[1].Unreachable || set.Cases[0].LangOf() != "en" || set.Cases[2].NotExpect == nil {
		t.Fatalf("새 칸을 못 읽었다 : %+v", set.Cases)
	}
	if set.Cases[1].LangOf() != "ko" {
		t.Fatal("lang 을 안 적으면 한국어여야 한다")
	}
}

func TestBadGoldenSet(t *testing.T) {
	bad := map[string]string{
		"abstain 에 정답": "version: 2\ncases:\n  - q: a\n    kind: abstain\n    expect: [20260822-47bf15db]\n",
		"모르는 kind":     "version: 2\ncases:\n  - q: a\n    kind: nope\n    expect: [x]\n",
		"더 새 판":        "version: 9\ncases:\n  - q: a\n    kind: extract\n    expect: [x]\n",
	}
	for name, text := range bad {
		if _, err := Load(writeGolden(t, text)); err == nil {
			t.Fatalf("%s : 거절해야 한다", name)
		}
	}
	if _, err := Load(filepath.Join(t.TempDir(), "없다.yaml")); err == nil {
		t.Fatal("없는 파일은 거절해야 한다")
	}
}

// 보고서에는 못 닿는 질의 줄과 완화 거절 줄이 반드시 있다.
func TestMarkdownLines(t *testing.T) {
	report := Report{Cases: 3, Unreachable: 1, Hard: 2, HardHit: 1,
		Metrics: (&tally{}).metrics(10, false, 0, false)}
	text := Markdown(&report)
	for _, want := range []string{"못 닿는 질의", "완화", "recall@5"} {
		if !strings.Contains(text, want) {
			t.Fatalf("보고서에 %q 가 없다 :\n%s", want, text)
		}
	}
}

func writeGolden(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// 「완화 포함」 값과 표의 값은 다른 것을 센다. 둘이 늘 같으면 자를 못 믿는다
// (실데이터 시험 7절 #7).
func TestWideRecallIsNotTheStrictOne(t *testing.T) {
	results := []CaseResult{
		{Kind: KindExtract, Rank: 1, WideRank: 1},
		{Kind: KindExtract, Rank: 0, WideRank: 2},
		{Kind: KindExtract, Rank: 0, WideRank: 0},
		{Kind: KindAbstain, WideRank: 0},
		{Kind: KindExtract, Rank: 0, WideRank: 3, Unreachable: true},
	}
	hit, wide := wideScore(results)
	if hit != 2 || wide != 2.0/3.0 {
		t.Fatalf("완화 포함 값이 틀렸다 : %d건 %.3f", hit, wide)
	}
	counts := tally{}
	for _, item := range results {
		if item.Unreachable {
			continue
		}
		counts.add(item)
	}
	strict := 0.0
	for _, metric := range counts.metrics(10, false, 0, false) {
		if metric.Key == MetricRecall5 {
			strict = metric.Value
		}
	}
	if strict == wide {
		t.Fatalf("두 값이 같다 : strict=%.3f wide=%.3f", strict, wide)
	}
	if strict != 1.0/3.0 {
		t.Fatalf("표의 recall@5 가 틀렸다 : %.3f", strict)
	}
}
