package quality

import (
	"fmt"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 중복 판정 자를 고른 근거 표를 찍는 시험들이다. **자를 만드는 시험이라
// 실패시키지 않는다** — 표만 남기고 `-v` 로 본다.
//
// 여기서 바꾸는 값은 전부 패키지 변수라 시험이 끝나면 **반드시 원래대로**
// 돌려놔야 한다. 안 돌려놓으면 뒤에 도는 시험이 딴 자로 채점된다.

// knobs 는 중복 판정을 가르는 값 전부다.
type knobs struct {
	idf      bool
	alignIDF bool
	wide     bool
	onScore  bool
	cut      float64
	soft     float64
	minPairs int
	needFact bool
	reject   float64
	warn     float64
}

// nowKnobs 는 지금 쓰는 값이다.
func nowKnobs() knobs {
	return knobs{idf: useIDF, alignIDF: useAlignIDF, wide: wideCandidates, onScore: factsOnScore,
		cut: AlignCut, soft: softAlignFloor, minPairs: alignMinPairs, needFact: alignNeedFact}
}

func (k knobs) apply() {
	useIDF, useAlignIDF, wideCandidates, factsOnScore = k.idf, k.alignIDF, k.wide, k.onScore
	AlignCut, softAlignFloor = k.cut, k.soft
	alignMinPairs, alignNeedFact = k.minPairs, k.needFact
}

// run 은 이 값으로 골든셋을 다시 채점한다. 끝나면 원래 값으로 되돌린다.
func (k knobs) run(t *testing.T, set *GoldenSet, memories []*model.Memory) GoldenReport {
	t.Helper()
	before := nowKnobs()
	k.apply()
	defer before.apply()
	settings := config.Default("quality-golden")
	if k.reject > 0 {
		settings.Quality.DupReject, settings.Quality.DupWarn = k.reject, k.warn
	}
	return ScoreGolden(set, memories, RepoOptions{Options: Options{Config: settings,
		Vocab: testdataVocab(t), Now: time.Date(2026, 8, 23, 0, 0, 0, 0, time.Local)}})
}

// row 는 표 한 줄이다.
func row(report GoldenReport, head string) string {
	return fmt.Sprintf("%s | %.3f | %.3f | %d | %.3f |\n", head,
		recallOf(report, TypeDup), machineRecall(report),
		len(report.FalseAlarms), lowestPrecision(report))
}

// TestDupAblation 은 무엇이 값을 냈는지 하나씩 꺼 보며 가른다.
func TestDupAblation(t *testing.T) {
	set, memories := loadForTune(t)
	out := "| 후보 넓히기 | 문장 정렬 | DUP | 기계 | 거짓 경보 | 정밀도 |\n| --- | --- | --- | --- | --- | --- |\n"
	for _, wide := range []bool{false, true} {
		for _, align := range []bool{false, true} {
			one := nowKnobs()
			one.wide = wide
			if !align {
				one.cut, one.soft = 2.0, 0 // 아무것도 안 넘는 문턱 = 정렬 끄기
			}
			out += row(one.run(t, set, memories), fmt.Sprintf("| %v | %v", wide, align))
		}
	}
	t.Log("\n" + out)
}

// TestSweepFine 은 **고른 문턱의 근거 표**다. 정렬 문턱 · 짝 하한 · 같은 사실
// 요구 · 띠 하한을 훑고 합격 칸(거짓 경보 0 · 정밀도 ≥0.80)을 따로 모은다.
func TestSweepFine(t *testing.T) {
	set, memories := loadForTune(t)
	out := "| 띠 하한 | 정렬 문턱 | 짝 하한 | 같은 사실 | DUP | 기계 | 거짓 경보 | 정밀도 |\n| --- | --- | --- | --- | --- | --- | --- | --- |\n"
	best := ""
	for _, soft := range []float64{0, 0.15} {
		for _, cut := range []float64{0.15, 0.18, 0.20, 0.25} {
			for _, pairs := range []int{1, 2, 3} {
				for _, fact := range []bool{false, true} {
					one := nowKnobs()
					one.soft, one.cut, one.minPairs, one.needFact = soft, cut, pairs, fact
					report := one.run(t, set, memories)
					line := row(report, fmt.Sprintf("| %.2f | %.2f | %d | %v", soft, cut, pairs, fact))
					out += line
					if len(report.FalseAlarms) == 0 && lowestPrecision(report) >= 0.80 {
						best += line
					}
				}
			}
		}
	}
	t.Log("\n== 합격 칸 ==\n" + best + "\n== 전체 ==\n" + out)
}

// TestSweepIDF 는 IDF 가중(설계 결정 34)이 값을 내는지 잰다. **통째 점수 쪽**은
// 문턱까지 같이 내려 공평하게 견주고, **문장 정렬 쪽**은 그 자리에서 켜 본다.
// 둘 다 지는 것이 이 표의 결론이고 그래서 기본이 꺼져 있다.
func TestSweepIDF(t *testing.T) {
	set, memories := loadForTune(t)
	out := "| 어디에 | 거절선 | 경고선 | DUP | 기계 | 거짓 경보 | 정밀도 |\n| --- | --- | --- | --- | --- | --- | --- |\n"
	for _, warn := range []float64{0.05, 0.06} {
		for _, where := range []string{"안 씀", "통째 점수", "문장 정렬"} {
			one := nowKnobs()
			one.idf, one.alignIDF = where == "통째 점수", where == "문장 정렬"
			one.reject, one.warn = warn+0.02, warn
			out += row(one.run(t, set, memories), fmt.Sprintf("| %s | %.2f | %.2f", where, warn+0.02, warn))
		}
	}
	t.Log("\n" + out)
}

// TestSweepFactsOnScore 는 통째 점수로 걸린 쌍에도 사실 어긋남 검사를 걸 때를 잰다.
func TestSweepFactsOnScore(t *testing.T) {
	set, memories := loadForTune(t)
	out := "| 점수쪽 사실 검사 | DUP | 기계 | 거짓 경보 | 정밀도 |\n| --- | --- | --- | --- | --- |\n"
	for _, onScore := range []bool{false, true} {
		one := nowKnobs()
		one.onScore = onScore
		out += row(one.run(t, set, memories), fmt.Sprintf("| %v", onScore))
	}
	t.Log("\n" + out)
}

func recallOf(report GoldenReport, kind string) float64 {
	for _, one := range report.Types {
		if one.Type == kind {
			return one.Recall
		}
	}
	return -1
}

func machineRecall(report GoldenReport) float64 {
	total, caught := 0, 0
	for _, one := range report.Types {
		if !one.Machine {
			continue
		}
		total += one.Total
		caught += one.Caught
	}
	if total == 0 {
		return -1
	}
	return float64(caught) / float64(total)
}

func lowestPrecision(report GoldenReport) float64 {
	lowest := 1.0
	for _, one := range report.Rules {
		if one.Precision >= 0 && one.Precision < lowest {
			lowest = one.Precision
		}
	}
	return lowest
}
