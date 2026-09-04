package quality

import (
	"fmt"
	"strings"
	"testing"
)

// 2단(사실 대조·짝 조각 하한)을 다듬는 근거 표다 (v0.4 갈래 B② 차례 3).
// 자를 만드는 시험이라 실패시키지 않고 표만 남긴다 — `-v` 로 본다.

// stage2Knobs 는 2단 값을 갈아 끼우고 되돌린다.
type stage2Knobs struct {
	loose    bool
	minPairs int
	needFact bool
}

func (s stage2Knobs) with(t *testing.T, run func()) {
	t.Helper()
	loose, pairs, need := stageTwoLoose, alignMinPairs, alignNeedFact
	stageTwoLoose, alignMinPairs, alignNeedFact = s.loose, s.minPairs, s.needFact
	defer func() { stageTwoLoose, alignMinPairs, alignNeedFact = loose, pairs, need }()
	run()
}

// TestSweepStageTwo 는 어긋남 자 · 짝 조각 하한 · 같은 사실 요구를 훑는다.
// T11 오거절(뜻은 가깝고 사실은 다른 쌍 20개)을 같은 표에 같이 적는다 —
// 재현율만 보고 고르면 그 자리가 무방비가 된다.
func TestSweepStageTwo(t *testing.T) {
	set, memories := loadForTune(t)
	pairs := loadPairs(t)
	out := "| 느슨한 자 | 짝 하한 | 같은 사실 | DUP | 기계 | 거짓 경보 | 정밀도 | T11 오거절 |\n"
	out += "| --- | --- | --- | --- | --- | --- | --- | --- |\n"
	for _, loose := range []bool{false, true} {
		for _, minPairs := range []int{1, 2} {
			for _, need := range []bool{true, false} {
				knobs := stage2Knobs{loose: loose, minPairs: minPairs, needFact: need}
				line := ""
				knobs.with(t, func() {
					report := nowKnobs().run(t, set, memories)
					line = fmt.Sprintf("| %v | %d | %v | %.3f | %.3f | %d | %.3f | %d/20 |\n",
						loose, minPairs, need, recallOf(report, TypeDup), machineRecall(report),
						len(report.FalseAlarms), lowestPrecision(report), t11Wrong(pairs))
				})
				out += line
			}
		}
	}
	t.Log("\n" + out)
}

// t11Wrong 은 T11 20쌍 중 중복이라고 잘못 잡은 수다.
func t11Wrong(pairs [][2]string) int {
	wrong := 0
	for at, pair := range pairs {
		if flagged(t11Memory(at*2, pair[0], 0), t11Memory(at*2+1, pair[1], 1)) {
			wrong++
		}
	}
	return wrong
}

// TestClashDetail 은 놓친 DUP 짝의 정렬 조각과 거기서 뽑힌 사실을 그대로 찍는다.
// 「같은 뜻 다른 표기를 어긋남으로 세는가」를 눈으로 보려는 표다.
func TestClashDetail(t *testing.T) {
	set, memories := loadForTune(t)
	ids := map[string]bool{}
	docs := map[string]*Doc{}
	table := newSimilarFor(testOptions())
	for _, m := range memories {
		doc := NewDoc(m)
		docs[doc.ID] = doc
		ids[doc.ID] = true
		table.Add(doc)
	}
	table.Session()
	out := strings.Builder{}
	for _, pair := range dupPairsOf(set, ids) {
		left, right := docs[pair[0]], docs[pair[1]]
		if left == nil || right == nil {
			continue
		}
		info := alignOf(left, right, AlignCut, 0)
		if info.Best < AlignCut || !info.Clash {
			continue
		}
		one, two := factsOf(info.Left), factsOf(info.Right)
		fmt.Fprintf(&out, "\n%s ↔ %s  정렬 %.3f 짝 %d\n  왼 : %s\n  오 : %s\n  숫자 %v / %v\n  이름 %v / %v\n",
			short(pair[0]), short(pair[1]), info.Best, info.Pairs, info.Left, info.Right,
			one.numbers, two.numbers, one.names, two.names)
	}
	t.Log(out.String())
}

// TestSweepCutWithClash 는 어긋남 자를 바꾼 뒤 정렬 문턱·띠 하한을 다시 훑는다.
// 문턱은 마지막에 정한다 (설계 2절 ② 차례 4).
func TestSweepCutWithClash(t *testing.T) {
	set, memories := loadForTune(t)
	pairs := loadPairs(t)
	out := "| 느슨한 자 | 정렬 문턱 | 띠 하한 | DUP | 기계 | 거짓 경보 | 정밀도 | T11 오거절 |\n"
	out += "| --- | --- | --- | --- | --- | --- | --- | --- |\n"
	for _, loose := range []bool{false, true} {
		for _, cut := range []float64{0.14, 0.15, 0.16, 0.18, 0.20} {
			for _, soft := range []float64{0.12, 0.15, 0.18} {
				line := ""
				stage2Knobs{loose: loose, minPairs: alignMinPairs, needFact: alignNeedFact}.with(t, func() {
					one := nowKnobs()
					one.cut, one.soft = cut, soft
					report := one.run(t, set, memories)
					saveCut, saveSoft := AlignCut, softAlignFloor
					AlignCut, softAlignFloor = cut, soft
					wrong := t11Wrong(pairs)
					AlignCut, softAlignFloor = saveCut, saveSoft
					line = fmt.Sprintf("| %v | %.2f | %.2f | %.3f | %.3f | %d | %.3f | %d/20 |\n",
						loose, cut, soft, recallOf(report, TypeDup), machineRecall(report),
						len(report.FalseAlarms), lowestPrecision(report), wrong)
				})
				out += line
			}
		}
	}
	t.Log("\n" + out)
}

// TestSweepThresholdWithClash 는 어긋남 자를 바꾼 뒤 거절선·경고선을 다시 훑는다.
func TestSweepThresholdWithClash(t *testing.T) {
	set, memories := loadForTune(t)
	out := "| 느슨한 자 | 거절선 | 경고선 | DUP | 기계 | 거짓 경보 | 정밀도 |\n| --- | --- | --- | --- | --- | --- | --- |\n"
	for _, loose := range []bool{false, true} {
		for _, warn := range []float64{0.04, 0.05, 0.06, 0.07} {
			for _, reject := range []float64{0.07, 0.08, 0.10} {
				if reject < warn {
					continue
				}
				line := ""
				stage2Knobs{loose: loose, minPairs: alignMinPairs, needFact: alignNeedFact}.with(t, func() {
					one := nowKnobs()
					one.reject, one.warn = reject, warn
					report := one.run(t, set, memories)
					line = fmt.Sprintf("| %v | %.2f | %.2f | %.3f | %.3f | %d | %.3f |\n",
						loose, reject, warn, recallOf(report, TypeDup), machineRecall(report),
						len(report.FalseAlarms), lowestPrecision(report))
				})
				out += line
			}
		}
	}
	t.Log("\n" + out)
}
