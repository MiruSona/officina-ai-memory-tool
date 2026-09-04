package eval

import (
	"math"
	"sort"
)

// 지표 이름. i18n 이 한글 이름으로 바꾼다.
const (
	MetricRecall5      = "recall5"
	MetricRecall5V01   = "recall5_v01"
	MetricLangGap      = "langgap"
	MetricRecall10     = "recall10"
	MetricMRR          = "mrr"
	MetricAbstain      = "abstain"
	MetricMisexclusion = "misexclusion"
	MetricP95          = "p95"
	MetricTokens       = "tokens"
)

// 합격선 (설계 9-3). p95 는 밀리초, tokens 는 예산 없이 만든 답 하나다.
const (
	// G1 을 올렸다 (설계 1절) : recall@5 0.80 → 0.85 · MRR 0.60 → 0.70.
	minRecall5  = 0.85
	minRecall10 = 0.90
	minMRR      = 0.70
	// minRecall5V01 은 옛 자(0·1번 칸까지)로 잰 값의 합격선이다. 자를 옮긴
	// 것이므로 옛 자 값도 나란히 재고, 그 자로도 0.80 을 넘는 것이 진짜
	// 목표다 (설계 1절 주 · 결정 29).
	minRecall5V01 = 0.80
	// maxLangGap 은 한국어와 영어 recall@5 차이다 (G1).
	maxLangGap = 0.15
	minAbstain = 0.70
	// maxMisexclusion 은 오배제 상한이다. **설계 1절이 적은 2% 다** — 도구가
	// 0.05 를 쓰던 것이 실데이터 시험 D2 다 (이전 뒤 손 30건 0.042 를 「합격」
	// 이라 찍었는데 설계 자로는 미달이었다).
	maxMisexclusion = 0.02
	// 설계 9-3 표가 적은 주입 토큰 합격선은 약 1,000 이다 (리뷰B #24).
	maxTokens = 1000.0
)

// p95 합격선은 저장소 크기를 따라간다. 한 숫자로 못 박으면 크면 늘 울리고
// 작으면 늘 통과한다 (설계 9-3).
//
// **모드도 가른다 (리뷰 D9).** 의미 검색을 켜면 벡터를 재는 값이 얹혀 328ms 가
// 나오는데, 낱말만 재던 100ms 자를 그대로 대면 켜기만 해도 늘 「미달」이다.
// 설계 G4 의 낱말+의미 예산은 900ms 라 그 자를 쓴다.
const (
	p95SmallScale     = 5000
	maxEngineP95Small = 100.0
	maxEngineP95Large = 300.0
	// maxHybridP95 는 낱말+의미 예산이다 (설계 G4).
	maxHybridP95 = 900.0
)

func engineP95Limit(scale int, semantic bool) float64 {
	if semantic {
		return maxHybridP95
	}
	if scale <= p95SmallScale {
		return maxEngineP95Small
	}
	return maxEngineP95Large
}

// Metric 은 잰 숫자 하나와 넘어야 할 선이다.
type Metric struct {
	Key       string  `json:"key"`
	Value     float64 `json:"value"`
	Threshold float64 `json:"threshold"`
	Higher    bool    `json:"higher_is_better"`
	Pass      bool    `json:"pass"`
	// Measured 는 분모가 있었는지다. abstain 질문이 하나도 없는 골든셋이
	// `abstain 1.000 합격` 을 찍으면 잰 적 없는 것이 합격으로 보인다 (리뷰B #22).
	Measured bool `json:"measured"`
}

// GroupScore 는 유형별·말별 한 줄이다.
type GroupScore struct {
	Name    string  `json:"name"`
	Cases   int     `json:"cases"`
	Recall5 float64 `json:"recall5"`
	MRR     float64 `json:"mrr"`
}

// tally 는 도는 동안 세는 것이다.
type tally struct {
	answerable  int
	abstain     int
	hit5        int
	hit5Legacy  int
	hit10       int
	reciprocal  float64
	abstained   int
	abstainWide int
	pushedOut   int
	latencies   []float64
	tokenTotal  int
	caseTotal   int
	hard        int
	hardHit     int
}

func (t *tally) add(outcome CaseResult) {
	t.caseTotal++
	if outcome.Hard {
		t.hard++
		if hitOfCase(outcome) {
			t.hardHit++
		}
	}
	t.tokenTotal += outcome.Tokens
	t.latencies = append(t.latencies, outcome.Milliseconds)
	if outcome.Kind == KindAbstain {
		t.abstain++
		// 사다리는 2번 칸부터 늘 넓히므로 완화 답은 거의 언제나 나온다.
		// 그래서 정답 기준은 0·1 칸(strict)이고, 완화 답이 보였다는 사실은
		// 따로 센다 — 숨기지 않되 합격선을 흔들지도 않는다 (리뷰B #15).
		if len(outcome.All) > 0 {
			t.abstainWide++
		}
		if len(outcome.Got) == 0 && !outcome.WrongAnswer {
			t.abstained++
		}
		return
	}
	t.answerable++
	if outcome.PushedOut {
		t.pushedOut++
	}
	if outcome.LegacyRank > 0 && outcome.LegacyRank <= 5 && outcome.Rank == 0 {
		t.hit5Legacy++
	}
	if outcome.Rank == 0 {
		return
	}
	t.reciprocal += 1 / float64(outcome.Rank)
	if outcome.Rank <= 5 {
		t.hit5++
	}
	if outcome.LegacyRank > 0 && outcome.LegacyRank <= 5 {
		t.hit5Legacy++
	}
	if outcome.Rank <= 10 {
		t.hit10++
	}
}

func (t *tally) metrics(scale int, semantic bool, langGap float64, langMeasured bool) []Metric {
	return []Metric{
		measured(higher(MetricRecall5, ratio(t.hit5, t.answerable, 1), minRecall5), t.answerable),
		measured(higher(MetricRecall5V01, ratio(t.hit5Legacy, t.answerable, 1), minRecall5V01), t.answerable),
		measured(lower(MetricLangGap, langGap, maxLangGap), boolCount(langMeasured)),
		measured(higher(MetricRecall10, ratio(t.hit10, t.answerable, 1), minRecall10), t.answerable),
		measured(higher(MetricMRR, meanOrPass(t.reciprocal, t.answerable, minMRR), minMRR), t.answerable),
		measured(higher(MetricAbstain, ratio(t.abstained, t.abstain, 1), minAbstain), t.abstain),
		measured(lower(MetricMisexclusion, ratio(t.pushedOut, t.answerable, 0), maxMisexclusion), t.answerable),
		measured(lower(MetricP95, percentile95(t.latencies), engineP95Limit(scale, semantic)), len(t.latencies)),
		measured(lower(MetricTokens, mean(float64(t.tokenTotal), t.caseTotal), maxTokens), t.caseTotal),
	}
}

// measured 는 분모가 0인 지표를 「잰 것 없음」 으로 표시한다. 합격 판정은
// 그대로 두되 화면에 숫자 대신 그 말을 찍는다 (리뷰B #22).
func measured(metric Metric, whole int) Metric {
	metric.Measured = whole > 0
	return metric
}

// hitOfCase 는 질문 하나를 맞혔다고 보는 기준이다. abstain 은 아무것도 안
// 나오는 것이, 나머지는 상위 5에 드는 것이 정답이다.
func hitOfCase(outcome CaseResult) bool {
	if outcome.Kind == KindAbstain {
		return len(outcome.Got) == 0 && !outcome.WrongAnswer
	}
	return outcome.Rank > 0 && outcome.Rank <= 5
}

// groups 는 유형별·말별 점수를 따로 센다.
type groups struct {
	kind    map[string]*tally
	lang    map[string]*tally
	variant map[string]*tally
}

func newGroups() *groups {
	return &groups{kind: map[string]*tally{}, lang: map[string]*tally{}, variant: map[string]*tally{}}
}

func (g *groups) add(outcome CaseResult) {
	pick(g.kind, outcome.Kind).add(outcome)
	pick(g.lang, outcome.Lang).add(outcome)
	pick(g.variant, outcome.Variant).add(outcome)
}

// variantRows 는 질의 꼴별 한 줄씩이다.
func (g *groups) variantRows() []GroupScore { return scoresOf(g.variant, Variants) }

func pick(table map[string]*tally, name string) *tally {
	found, ok := table[name]
	if !ok {
		found = &tally{}
		table[name] = found
	}
	return found
}

func (g *groups) rows() ([]GroupScore, []GroupScore) {
	return scoresOf(g.kind, Kinds), scoresOf(g.lang, []string{"ko", "en"})
}

func scoresOf(table map[string]*tally, order []string) []GroupScore {
	out := []GroupScore{}
	for _, name := range order {
		found, ok := table[name]
		if !ok {
			continue
		}
		out = append(out, groupScore(name, found))
	}
	for name, found := range table {
		if containsName(order, name) {
			continue
		}
		out = append(out, groupScore(name, found))
	}
	return out
}

func groupScore(name string, found *tally) GroupScore {
	whole := found.answerable
	if found.abstain > 0 && whole == 0 {
		return GroupScore{Name: name, Cases: found.caseTotal,
			Recall5: ratio(found.abstained, found.abstain, 1), MRR: ratio(found.abstained, found.abstain, 1)}
	}
	return GroupScore{Name: name, Cases: found.caseTotal,
		Recall5: ratio(found.hit5, whole, 0), MRR: meanOrPass(found.reciprocal, whole, 0)}
}

func containsName(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func higher(key string, value, threshold float64) Metric {
	return Metric{Key: key, Value: value, Threshold: threshold, Higher: true, Pass: value >= threshold}
}

func lower(key string, value, threshold float64) Metric {
	return Metric{Key: key, Value: value, Threshold: threshold, Pass: value <= threshold}
}

// ratio 는 분모가 0이면 부르는 쪽이 정한 값을 준다. abstain 질문이 하나도 없는
// 골든셋이 괜히 울리지 않게 하는 자리다.
func ratio(part, whole int, empty float64) float64 {
	if whole == 0 {
		return empty
	}
	return float64(part) / float64(whole)
}

func meanOrPass(total float64, count int, empty float64) float64 {
	if count == 0 {
		return empty
	}
	return total / float64(count)
}

func mean(total float64, count int) float64 {
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

// percentile95 는 가장 가까운 순위를 쓴다. 작은 골든셋에 맞는 방식이다.
func percentile95(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64{}, values...)
	sort.Float64s(sorted)
	place := int(math.Ceil(0.95*float64(len(sorted)))) - 1
	if place < 0 {
		place = 0
	}
	return sorted[place]
}

// boolCount 는 「잰 것이 있나」를 분모 꼴로 바꾼다.
func boolCount(measured bool) int {
	if measured {
		return 1
	}
	return 0
}

// LangGap 은 한국어와 영어 recall@5 의 차이다. 한쪽이 없으면 0 이고 「안 잼」이다.
func LangGap(rows []GroupScore) (float64, bool) {
	values := map[string]float64{}
	for _, row := range rows {
		values[row.Name] = row.Recall5
	}
	ko, hasKO := values["ko"]
	en, hasEN := values["en"]
	if !hasKO || !hasEN {
		return 0, false
	}
	if ko > en {
		return ko - en, true
	}
	return en - ko, true
}

func allPassed(metrics []Metric) bool {
	for _, item := range metrics {
		if !item.Pass {
			return false
		}
	}
	return true
}
