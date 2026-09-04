package eval

import "testing"

// 오배제 자는 **설계 1절의 2%** 다. 도구 자가 0.05 면 이전 뒤 손 30건 0.042 를
// 「합격」으로 찍어 자동화가 못 알아챈다 (재측정 1 R3).
func TestMisexclusionLimitIsTwoPercent(t *testing.T) {
	if maxMisexclusion != 0.02 {
		t.Fatalf("오배제 자가 0.02 가 아니다 : %v", maxMisexclusion)
	}
	counts := tally{}
	counts.add(CaseResult{Kind: KindExtract, Rank: 1})
	counts.add(CaseResult{Kind: KindExtract, Rank: 0, PushedOut: true})
	for _, item := range counts.metrics(100, false, 0, false) {
		if item.Key == MetricMisexclusion && item.Threshold != 0.02 {
			t.Fatalf("찍히는 자가 0.02 가 아니다 : %v", item.Threshold)
		}
	}
}

// p95 자는 모드를 가린다 — 의미 검색이면 900ms(설계 G4), 낱말이면 100/300ms.
func TestEngineP95LimitByMode(t *testing.T) {
	cases := []struct {
		scale    int
		semantic bool
		want     float64
	}{
		{100, false, maxEngineP95Small},
		{20000, false, maxEngineP95Large},
		{100, true, maxHybridP95},
		{20000, true, maxHybridP95},
	}
	for _, one := range cases {
		if got := engineP95Limit(one.scale, one.semantic); got != one.want {
			t.Fatalf("scale %d semantic %v : %v 여야 하는데 %v", one.scale, one.semantic, one.want, got)
		}
	}
}

// semanticOn 은 재정렬기가 꽂혔는지만 본다. `[embed] enabled` 는 낱말 임베딩
// 표 스위치라 의미 모델을 깔아도 꺼져 있다 (재측정 1 R4).
func TestSemanticOnIgnoresWordEmbedSwitch(t *testing.T) {
	if semanticOn(Options{}) {
		t.Fatal("재정렬기가 없는데 의미 모드로 봤다")
	}
	options := Options{Vectors: stubVectors{}}
	if !semanticOn(options) {
		t.Fatal("재정렬기가 꽂혔는데 낱말 모드로 봤다")
	}
	options.Embed.Enabled = true
	if !semanticOn(options) {
		t.Fatal("[embed] enabled 를 켜도 의미 모드여야 한다")
	}
}

type stubVectors struct{}

func (stubVectors) Query(string) []float32           { return nil }
func (stubVectors) Docs([]int64) map[int64][]float32 { return nil }
