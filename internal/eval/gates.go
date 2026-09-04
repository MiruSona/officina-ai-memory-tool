package eval

import (
	"fmt"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
)

// Gate 는 합격선 하나다 (설계 1절 G1~G6). **하나라도 미달이면 미달이라고
// 적는다.** 안 잰 것은 「안 잼」이라고 적고 합격으로 안 친다.
type Gate struct {
	Key  string `json:"key"`  // G1 … G6
	What string `json:"what"` // 한 줄 이름
	// Value 와 Want 는 사람이 읽는 글이다. 지표마다 단위가 달라 글로 적는다.
	Value string `json:"value"`
	Want  string `json:"want"`
	Pass  bool   `json:"pass"`
	// Measured 가 거짓이면 이번에 안 잰 것이다.
	Measured bool `json:"measured"`
}

// SearchGates 는 검색 쪽 합격선이다 — G1(찾으면 나온다) · G2(없으면 없다고 한다).
func SearchGates(report *Report) []Gate {
	recall5 := findMetric(report.Metrics, MetricRecall5)
	legacy5 := findMetric(report.Metrics, MetricRecall5V01)
	recall10 := findMetric(report.Metrics, MetricRecall10)
	mrr := findMetric(report.Metrics, MetricMRR)
	gap := findMetric(report.Metrics, MetricLangGap)
	abstain := findMetric(report.Metrics, MetricAbstain)
	miss := findMetric(report.Metrics, MetricMisexclusion)

	one := Gate{Key: "G1", What: "찾으면 나온다",
		Value: fmt.Sprintf("strict(v0.2 자) %s / strict(v0.1 자) %s / wide %s · recall@10 %s · MRR %s · ko-en %s",
			three(recall5.Value), three(legacy5.Value), three(report.WideRecall5),
			three(recall10.Value), three(mrr.Value), three(gap.Value)),
		Want:     "recall@5 ≥ 0.85 · recall@10 ≥ 0.90 · MRR ≥ 0.70 · ko/en 격차 ≤ 0.15",
		Measured: recall5.Measured,
		Pass:     recall5.Pass && legacy5.Pass && recall10.Pass && mrr.Pass && gap.Pass,
	}
	two := Gate{Key: "G2", What: "없으면 없다고 한다",
		Value:    fmt.Sprintf("abstain %s · 오배제 %s", three(abstain.Value), three(miss.Value)),
		Want:     "abstain 1.000 · 오배제 ≤ 0.02",
		Measured: abstain.Measured,
		Pass:     abstain.Value >= 1.0 && miss.Pass,
	}
	return []Gate{one, two}
}

// QualityGates 는 문서 품질 합격선이다 — G6 ①②③ (설계 1절).
// ④「새로 들어온 기억의 결함률」은 저장소 통계라 `mem status --quality` 가 잰다.
func QualityGates(report *QualityReport) []Gate {
	score := report.Score
	// 정밀도가 음수인 규칙은 「잴 것이 없었다」는 뜻이라 가장 낮은 값으로
	// 안 친다 — 안 잰 것을 최악으로 찍으면 표가 거짓말을 한다.
	worst, worstName := 1.0, ""
	for _, rule := range score.Rules {
		if rule.Fired > 0 && rule.Precision >= 0 && rule.Precision < worst {
			worst, worstName = rule.Precision, rule.Rule
		}
	}
	recall, missed := machineRecall(score)
	value := fmt.Sprintf("기계 규칙군 재현율 %s · 대조군 거짓 경보 %d건 · 가장 낮은 정밀도 %s",
		three(recall), len(score.FalseAlarms), three(worst))
	if worstName != "" {
		value += " (" + worstName + ")"
	}
	if len(missed) > 0 {
		value += " · 놓친 유형 " + strings.Join(missed, "·")
	}
	return []Gate{{Key: "G6", What: "문서 품질", Value: value,
		Want: fmt.Sprintf("재현율 ≥ %.2f · 거짓 경보 0 · 정밀도 ≥ %.2f",
			quality.MachineRecallFloor, quality.PrecisionFloor),
		Measured: report.Flag > 0, Pass: report.Pass}}
}

// machineRecall 은 기계 규칙군(G6 ①)의 재현율과 못 채운 유형이다.
func machineRecall(score quality.GoldenReport) (float64, []string) {
	total, caught, missed := 0, 0, []string{}
	for _, one := range score.Types {
		if !one.Machine {
			continue
		}
		total += one.Total
		caught += one.Caught
		if one.Recall < quality.MachineRecallFloor {
			missed = append(missed, fmt.Sprintf("%s %s", one.Type, three(one.Recall)))
		}
	}
	if total == 0 {
		return 0, missed
	}
	return float64(caught) / float64(total), missed
}

// Unmeasured 는 이번 명령이 안 잰 합격선이다. G3·G4·G5 는 훅·색인·설치가
// 재는 것이라 `mem eval` 이 못 찍는다 — 빈칸으로 두지 않고 안 쟀다고 적는다.
func Unmeasured() []Gate {
	return []Gate{
		{Key: "G3", What: "세션을 안 막고 싸다", Want: "훅 ≤ 1초 · ≤ 1,000 토큰 · ≤ 8,000 바이트", Value: "`mem hook --json` 이 잰다"},
		{Key: "G4", What: "규모를 견딘다", Want: "20k 색인 ≤ 15초 · index.db ≤ 60MB", Value: "`mem status --db` 가 잰다"},
		{Key: "G5", What: "혼자 완결이다", Want: "승인 0회 · 툴 폴더 밖 경로 0", Value: "`mem status --doctor` 가 잰다"},
	}
}

// AllGates 는 G1~G6 을 한 표에 모은다. 안 잰 것은 안 잰 자리에 그대로 둔다.
func AllGates(searched *Report, scored *QualityReport) []Gate {
	gates := []Gate{}
	if searched != nil {
		gates = append(gates, SearchGates(searched)...)
	} else {
		gates = append(gates, Gate{Key: "G1", What: "찾으면 나온다", Value: "`mem eval --search` 가 잰다"},
			Gate{Key: "G2", What: "없으면 없다고 한다", Value: "`mem eval --search` 가 잰다"})
	}
	gates = append(gates, Unmeasured()...)
	if scored != nil {
		gates = append(gates, QualityGates(scored)...)
	} else {
		gates = append(gates, Gate{Key: "G6", What: "문서 품질", Value: "`mem eval --quality` 가 잰다"})
	}
	return gates
}

// GateTable 은 합격선 한 표다.
func GateTable(gates []Gate) string { return gateTable(gates) }

func gateTable(gates []Gate) string {
	out := strings.Builder{}
	out.WriteString("| 합격선 | 잰 값 | 넘어야 할 선 | 판정 |\n| --- | --- | --- | --- |\n")
	for _, gate := range gates {
		out.WriteString(fmt.Sprintf("| **%s** %s | %s | %s | %s |\n",
			gate.Key, gate.What, gate.Value, gate.Want, gateVerdict(gate)))
	}
	return out.String()
}

func gateVerdict(gate Gate) string {
	if !gate.Measured {
		return "안 잼"
	}
	if gate.Pass {
		return "합격"
	}
	return "불합격"
}

func findMetric(metrics []Metric, key string) Metric {
	for _, one := range metrics {
		if one.Key == key {
			return one
		}
	}
	return Metric{Key: key}
}
