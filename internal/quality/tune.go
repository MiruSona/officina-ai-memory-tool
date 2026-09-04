package quality

import (
	"fmt"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// TunePoint 는 중복 문턱 하나에서의 성적이다.
type TunePoint struct {
	Cut float64 `json:"cut"`
	// Caught 는 골든셋 DUP 건 중 이 문턱을 넘은 수다.
	Caught int     `json:"caught"`
	Recall float64 `json:"recall"`
	// FalseAlarm 은 대조군 중 이 문턱을 넘은 수다. 0 이 목표다.
	FalseAlarm int `json:"false_alarm"`
}

// 훑는 구간. 설계 3-2 는 0.40~0.90 을 0.02 씩 훑으라고 했지만, 실측에서 닮음
// 점수 S 의 최대가 0.25 라 그 구간에는 아무것도 없다. 0.05 부터 0.01 씩 훑는다.
const (
	TuneFrom = 0.05
	TuneTo   = 0.90
	TuneStep = 0.01
)

// GoldenInStore 는 골든셋에 적힌 기억 중 이 저장소에 실제로 있는 건수다.
// 0 이면 곡선이 전부 0 으로 나온다 — 왜 그런지 사람에게 말해 줘야 한다
// (스트레스 V6 : 146초를 쓰고 얻은 것이 0 이었다).
func GoldenInStore(set *GoldenSet, memories []*model.Memory) (int, int) {
	dup, clean := dupAndClean(set)
	here := 0
	for _, m := range memories {
		if dup[m.ID] || clean[m.ID] {
			here++
		}
	}
	return here, len(dup) + len(clean)
}

// TuneDuplicate 는 중복 문턱 곡선을 그린다 (`mem eval --tune-dup`).
// 기억마다 「가장 닮은 것의 점수」를 구해 두고 문턱을 훑는다.
func TuneDuplicate(set *GoldenSet, memories []*model.Memory, opt RepoOptions) []TunePoint {
	opt.Options = opt.Options.normalized()
	table := newSimilarFor(opt.Options)
	docs := make([]*Doc, 0, len(memories))
	for _, m := range memories {
		doc := NewDoc(m)
		docs = append(docs, doc)
		table.Add(doc)
	}
	// **판정기와 같은 길로 잰다.** 그냥 후보를 다 훑어 Score 를 재면 2단(띠 하한 ·
	// 넓힌 후보는 거절선부터)이 빠져서, 실제로는 안 걸리는 쌍이 곡선에 들어와
	// 권장 문턱을 엉뚱하게 밀어 올린다.
	dup, clean := dupAndClean(set)
	// **골든셋에 든 기억만 잰다** (리뷰 B · V6). 곡선은 dup·clean 에 든 id 만
	// 세므로 나머지 기억의 점수는 어디에도 안 쓰인다. 20k 저장소에서 112건짜리
	// 골든셋을 재느라 2만 건을 다 훑던 자리다 (19분 넘게 안 끝났다).
	// **표에는 저장소 전부가 들어 있다** — 후보는 저장소 전체에서 나와야 한다.
	best := map[string]float64{}
	session := table.Session()
	for _, doc := range docs {
		if !dup[doc.ID] && !clean[doc.ID] {
			continue
		}
		for _, match := range session.Nearest(doc, TuneFrom, 0) {
			if match.Score > best[doc.ID] {
				best[doc.ID] = match.Score
			}
		}
	}
	points := []TunePoint{}
	for cut := TuneFrom; cut <= TuneTo+TuneStep/2; cut += TuneStep {
		point := TunePoint{Cut: cut}
		for id, score := range best {
			if score < cut {
				continue
			}
			if dup[id] {
				point.Caught++
			} else if clean[id] {
				point.FalseAlarm++
			}
		}
		if len(dup) > 0 {
			point.Recall = float64(point.Caught) / float64(len(dup))
		}
		points = append(points, point)
		if point.Caught == 0 && point.FalseAlarm == 0 {
			break
		}
	}
	return points
}

// dupAndClean 은 골든셋에서 DUP 로 판정된 id 와 대조군 id 다.
func dupAndClean(set *GoldenSet) (map[string]bool, map[string]bool) {
	dup, clean := map[string]bool{}, map[string]bool{}
	for _, one := range set.Flag {
		if hasString(one.Defects, TypeDup) {
			dup[one.ID] = true
		}
	}
	for _, one := range set.Clean {
		if !dup[one.ID] {
			clean[one.ID] = true
		}
	}
	return dup, clean
}

// Recommend 는 곡선에서 문턱 둘을 고른다.
//   - 경고선 : **대조군을 하나도 안 잡으면서** 무언가는 잡는 가장 낮은 문턱.
//   - 거절선 : 그 자리에서 잡히는 것의 절반 이하만 남는 가장 낮은 문턱.
//     「확실한 절반만 막는다」는 뜻이다.
//
// 설계 3-2 는 「정밀도 0.90 을 지키는 가장 낮은 값」이라고 적었다. 대조군이
// 20건뿐이라 정밀도를 소수점 둘째 자리까지 못 재므로 「대조군을 몇 건까지
// 잡느냐」로 갈음한다.
//
// **(리뷰 B) 경고선에도 대조군 0 을 건다.** 전에는 경고선이 대조군 셋까지
// 봐줬는데, G6 ② 는 경고도 거짓 경보로 세기 때문에 그 셋이 그대로 불합격이 됐다.
// **(리뷰 D4) 재현율이 0 인 문턱은 절대 안 권한다.** 「절반 규칙」이 곡선 끝의
// 빈 자리를 골라 DUP 를 하나도 못 잡는 값을 권한 적이 있다. 두 선 다 「거짓
// 경보 0 이면서 **무언가는 잡는** 가장 낮은 문턱」에서만 고른다.
func Recommend(points []TunePoint) (reject float64, warn float64) {
	warn = lowestCut(points, 0)
	if warn < 0 {
		warn = lowestCut(points, 1)
	}
	reject = halfCut(points, warn)
	if RecallAt(points, reject) <= 0 {
		reject = warn
	}
	return reject, warn
}

// RecallAt 은 그 문턱에서의 DUP 재현율이다. 곡선에 없는 값이면 가장 가까운
// 아래 칸을 쓴다. 권장 줄이 「이 값이면 얼마나 잡히나」를 같이 말하는 데 쓴다.
func RecallAt(points []TunePoint, cut float64) float64 {
	best := 0.0
	found := false
	for _, point := range points {
		if point.Cut <= cut+TuneStep/2 {
			best, found = point.Recall, true
			continue
		}
		break
	}
	if !found {
		return 0
	}
	return best
}

// halfCut 은 경고선에서 잡히던 것의 절반 이하만 남는 가장 낮은 문턱이다.
// 그런 자리가 없으면 경고선을 그대로 쓴다 — 경고선보다 낮은 거절선은 없다.
func halfCut(points []TunePoint, warn float64) float64 {
	caught := 0
	for _, point := range points {
		if point.Cut+TuneStep/2 >= warn {
			caught = point.Caught
			break
		}
	}
	for _, point := range points {
		if point.Cut+TuneStep/2 >= warn && point.Caught*2 <= caught && point.Caught > 0 {
			return point.Cut
		}
	}
	return warn
}

// lowestCut 은 오탐이 maxFalse 이하이면서 무언가는 잡는 가장 낮은 문턱이다.
func lowestCut(points []TunePoint, maxFalse int) float64 {
	for _, point := range points {
		if point.FalseAlarm <= maxFalse && point.Caught > 0 {
			return point.Cut
		}
	}
	return -1
}

// TuneTable 은 사람이 읽는 곡선 표다. 권장값 줄은 안 찍는다 — 그건
// `eval.TuneMarkdown` 한 곳의 몫이다(N3 : 두 곳에서 찍혀 중복이던 것을 합쳤다).
func TuneTable(points []TunePoint) string {
	out := strings.Builder{}
	out.WriteString("| 문턱 | 잡은 DUP | 재현율 | 대조군 잡음 |\n| --- | --- | --- | --- |\n")
	for _, point := range points {
		fmt.Fprintf(&out, "| %.2f | %d | %.3f | %d |\n", point.Cut, point.Caught, point.Recall, point.FalseAlarm)
	}
	return out.String()
}

// AlignPoint 는 문장 정렬 문턱 하나에서의 성적이다.
type AlignPoint struct {
	Cut float64 `json:"cut"`
	// Caught 는 골든셋 DUP 건 중 이 문턱에서 잡힌 수다 (2단까지 통과한 것).
	Caught int     `json:"caught"`
	Recall float64 `json:"recall"`
	// FalseAlarm 은 대조군 중 잡힌 수다. 0 이 목표다.
	FalseAlarm int `json:"false_alarm"`
}

// 문장 정렬 문턱을 훑는 구간.
const (
	AlignFrom = 0.10
	AlignTo   = 0.50
	AlignStep = 0.02
)

// TuneAlign 은 문장 최대 정렬 문턱 곡선을 그린다 (`mem eval --tune-dup`).
// 통째 점수(S)로 이미 잡히는 것은 빼고 **정렬이 새로 무엇을 잡는지**만 센다.
func TuneAlign(set *GoldenSet, memories []*model.Memory, opt RepoOptions) []AlignPoint {
	opt.Options = opt.Options.normalized()
	table := newSimilarFor(opt.Options)
	docs := make([]*Doc, 0, len(memories))
	for _, m := range memories {
		doc := NewDoc(m)
		docs = append(docs, doc)
		table.Add(doc)
	}
	dup, clean := dupAndClean(set)
	points := []AlignPoint{}
	save := AlignCut
	defer func() { AlignCut = save }()
	for cut := AlignFrom; cut <= AlignTo+AlignStep/2; cut += AlignStep {
		AlignCut = cut
		point := AlignPoint{Cut: cut}
		session := table.Session()
		for _, doc := range docs {
			// 골든셋에 든 기억만 잰다 (리뷰 B · V6). 문턱 칸마다 저장소를 통째로
			// 훑던 자리다.
			if !dup[doc.ID] && !clean[doc.ID] {
				continue
			}
			hit := false
			for _, match := range session.Nearest(doc, opt.Config.Quality.DupWarn, dupListMax) {
				if match.Partial {
					hit = true
				}
			}
			if !hit {
				continue
			}
			if dup[doc.ID] {
				point.Caught++
			} else if clean[doc.ID] {
				point.FalseAlarm++
			}
		}
		if len(dup) > 0 {
			point.Recall = float64(point.Caught) / float64(len(dup))
		}
		points = append(points, point)
	}
	return points
}

// AlignTable 은 사람이 읽는 정렬 곡선 표다.
func AlignTable(points []AlignPoint) string {
	out := strings.Builder{}
	out.WriteString("| 정렬 문턱 | 정렬이 새로 잡은 DUP | 재현율 | 대조군 잡음 |\n| --- | --- | --- | --- |\n")
	for _, point := range points {
		fmt.Fprintf(&out, "| %.2f | %d | %.3f | %d |\n", point.Cut, point.Caught, point.Recall, point.FalseAlarm)
	}
	return out.String()
}
