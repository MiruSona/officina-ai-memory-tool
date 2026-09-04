package quality

import (
	"math/bits"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/simhash"
)

// 문장 최대 정렬 (설계 결정 30·31 · 0실측 T0 3절).
//
// 통째 자카드는 「긴 글 안에 같은 한 줄이 박힌」 부분 중복을 못 잡는다. 실측에서
// 그런 쌍이 놓친 12건 중 7건이었고, 통째로 보면 0.03 인 쌍이 줄 단위로 보면
// 0.25~0.56 이었다.
//
// **정렬 값은 판정자가 아니라 후보 뽑기(1단)다.** 어느 문턱에서도 대조군 20건
// 중 4~20건이 걸린다 (0실측 3절 표). 확정은 2단 — 기존 S 와 factsClash 다.

// minUnitLen 은 조각으로 칠 정규형 글자 수 하한이다. 이보다 짧은 줄은 표 칸
// 이름·목록 머리처럼 어느 기억에나 있는 말이라 겹쳐도 뜻이 없다.
const minUnitLen = 12

// maxUnits 는 기억 하나에서 볼 조각 수 상한이다. 정렬은 조각 수의 곱이라
// 상한이 없으면 긴 기억 둘이 만났을 때 그대로 시간이 된다 (결정 35).
// 긴 조각부터 남긴다 — 긴 줄일수록 겹쳤을 때 증거가 세다.
const maxUnits = 48

// unit 은 견줄 조각 하나다. text 는 사실 대조(2단)에 쓸 원문이다.
type unit struct {
	text  string
	grams []uint64
	fp    uint64
	// known 은 미리 뽑아 둔 「숫자·이름」이다 (2단 검사용). 표에 든 뒤에
	// 채워진다. 안 채워져 있으면 그때 뽑는다 — 짝마다 다시 뽑으면 20k lint
	// 에서 그것만 10초다 (리뷰 B · V3).
	known *facts
	// weight·total 은 조각 차례에 맞춘 IDF 무게다. 표에 든 뒤에 채워진다.
	// 어느 기억에나 있는 말(`했다`·`기억`)이 겹쳐서 생기는 거짓 정렬을 눌러 준다.
	weight []float32
	total  float32
}

// splitUnits 는 글을 문장·표 줄·목록 줄로 자른다.
//
// 자르는 자리 : 줄바꿈 · 마침표 뒤 빈칸 · `다.` 뒤. 표는 `|` 를 빈칸으로 바꿔
// **줄 하나를 통째로** 둔다 — 칸 하나씩 자르면 대부분 길이 하한에 걸려 사라진다.
func splitUnits(text string) []string {
	out := []string{}
	for _, line := range bodyLines(text) {
		line = strings.TrimSpace(strings.ReplaceAll(line, "|", " "))
		line = strings.TrimLeft(line, "#>-*+ \t")
		for _, piece := range splitSentences(line) {
			piece = strings.TrimSpace(piece)
			if piece == "" {
				continue
			}
			out = append(out, piece)
		}
	}
	return out
}

// splitSentences 는 한 줄을 문장으로 자른다.
func splitSentences(line string) []string {
	letters := []rune(line)
	out := []string{}
	start := 0
	for at := 0; at < len(letters); at++ {
		if letters[at] != '.' {
			continue
		}
		next := at + 1
		if next < len(letters) && letters[next] != ' ' && letters[next] != '\t' {
			continue // 3.14 · v0.2 · main.go 처럼 붙어 있는 마침표는 문장 끝이 아니다
		}
		if at > start {
			out = append(out, string(letters[start:at]))
		}
		start = next
	}
	if start < len(letters) {
		out = append(out, string(letters[start:]))
	}
	return out
}

// unitsOf 는 조각을 견줄 수 있는 꼴로 간다. 짧은 조각은 버리고, 정규형이 같은
// 조각은 하나로 친다.
func unitsOf(text string) []unit {
	seen := map[string]bool{}
	found := []unit{}
	for _, piece := range splitUnits(text) {
		norm := normalizeText(piece)
		if len([]rune(norm)) < minUnitLen || seen[norm] {
			continue
		}
		seen[norm] = true
		found = append(found, unit{text: piece, grams: grams(norm), fp: simhash.Of(norm)})
	}
	if len(found) <= maxUnits {
		return found
	}
	// 긴 것부터 남긴다. 자리 차례는 안 지켜도 된다 — 최대값만 쓴다.
	for at := 1; at < len(found); at++ {
		one := found[at]
		to := at
		for to > 0 && len(found[to-1].grams) < len(one.grams) {
			found[to] = found[to-1]
			to--
		}
		found[to] = one
	}
	return found[:maxUnits]
}

// alignPrefilter 는 조각 둘을 정확히 견줄지 정하는 싼 자다. 지문 거리가 이보다
// 멀면 자카드는 어차피 낮다 — 20k 에서 조각 곱셈을 줄이는 고삐다.
const alignPrefilter = 26

// unitBound 는 두 조각의 정렬 점수가 절대 못 넘는 값이다.
//
// 무게가 실려 있으면 **무게 합의 비**다 — 겹침 무게는 작은 쪽 합을, 합집합
// 무게는 큰 쪽 합을 못 넘는다 (bodyBound 와 같은 셈). 개수 비로 재면 무게
// 실린 값의 상한이 **아니다** : 드문 낱말 하나로 된 짧은 조각이 긴 조각과 그
// 낱말을 나눠 가지면 무게 자카드는 1 에 가까운데 개수 비는 1/N 이다.
// 그 자리를 미리 잘라 버리면 부분 중복을 통째로 잃는다 (리뷰 B).
func unitBound(one, two *unit) float64 {
	if one.weight == nil || two.weight == nil {
		return sizeBound(len(one.grams), len(two.grams))
	}
	small, big := one.total, two.total
	if small > big {
		small, big = big, small
	}
	if big <= 0 {
		return 0
	}
	return float64(small / big)
}

// alignInfo 는 두 기억의 문장 정렬 결과다.
type alignInfo struct {
	// Best 는 가장 닮은 짝 조각의 자카드다.
	Best float64
	// Left·Right 는 그 짝 조각의 원문이다 (2단 사실 대조용).
	Left, Right string
	// Pairs 는 문턱을 넘은 짝 조각 수다. 둘 이상이면 「긴 글 둘이 곁말 한 줄만
	// 우연히 같다」가 아니라 정말 같은 대목을 나눠 갖고 있다는 뜻이다.
	Pairs int
	// Clash 는 문턱을 넘은 짝 조각 **어느 하나라도** 숫자·경로·이름이 어긋나는지다.
	// 「나머지 줄은 똑같은데 값 하나만 다르다」가 바로 다른 결정이다 (T11).
	Clash bool
	// ClashLoose 는 느슨한 자로 잰 같은 값이다 (facts.go). 경고 쪽에만 쓴다.
	ClashLoose bool
}

// alignMax 는 두 기억의 짝 조각 중 가장 닮은 값과 그 짝이다.
func alignMax(left, right *Doc) (float64, string, string) {
	found := alignOf(left, right, AlignCut, 0)
	return found.Best, found.Left, found.Right
}

// alignOf 는 문장 최대 정렬을 잰다. cut 을 넘은 짝 수도 같이 센다.
//
// **need 아래 값은 안 채운다** (리뷰 B · V3). 부르는 쪽이 그 아래를 안 보므로
// 짝 조각 하나하나를 끝까지 셀 이유가 없다. need 는 부르는 쪽이 정말 쓰는 가장
// 낮은 자여야 한다 — 그보다 위는 값이 한 자리도 안 바뀐다.
func alignOf(left, right *Doc, cut, need float64) alignInfo {
	found := alignInfo{}
	for at := range left.units {
		one := &left.units[at]
		for other := range right.units {
			two := &right.units[other]
			bound := unitBound(one, two)
			if bound <= found.Best && bound < cut {
				continue
			}
			// (v0.4 C2) **자카드가 절대 못 넘는 값이 need 아래면 답은 0 이다.**
			// 재 봐야 0 이 나오고 0 은 아무것도 안 바꾼다(0 > found.Best 가 거짓).
			// 옛 판은 위 줄이 `bound <= found.Best` 를 같이 요구해서, found.Best
			// 가 아직 0 인 동안(대부분이다) 이 자리를 **한 번도 못 걸렀다** —
			// 20k lint 에서 조각 곱셈이 그대로 다 돌던 까닭이다.
			if bound < need {
				continue
			}
			if bits.OnesCount64(one.fp^two.fp) > alignPrefilter {
				continue
			}
			score := gramSimAtLeast(one.grams, one.weight, one.total,
				two.grams, two.weight, two.total, need)
			if score >= cut {
				found.Pairs++
				strict, loose := factsClashUnits(one, two)
				found.Clash = found.Clash || strict
				found.ClashLoose = found.ClashLoose || loose
			}
			if score > found.Best {
				found.Best, found.Left, found.Right = score, one.text, two.text
			}
		}
	}
	return found
}

// unitSketch 는 조각 하나를 대표하는 조각 지문 몇 개다. 두 조각이 닮으면 가장
// 작은 지문 몇 개가 겹칠 확률이 높다 (minhash 얼개). 후보 좁히기용이다.
const unitSketch = 8

// sketchOf 는 조각의 가장 작은 지문 unitSketch 개다. grams 는 이미 정렬돼 있다.
func sketchOf(one *unit) []uint64 {
	if len(one.grams) <= unitSketch {
		return one.grams
	}
	return one.grams[:unitSketch]
}

// 문턱 넷은 **설계에 숫자가 없어 골든셋 130건 스윕으로 뽑았다** (결정 32).
// 근거 표는 `TestSweepFine` · `TestSweepIDFFair` 가 찍는다. 뽑은 자리와 값 :
//
//	AlignCut       0.18   1단 — 짝 조각 자카드가 이만큼 넘으면 부분 중복 후보
//	alignMinPairs  2      2단 — 그런 짝 조각이 둘 이상이어야 확정 (한 줄만 닮은 것은 곁말이다)
//	alignNeedFact  true   2단 — 짝 조각이 같은 숫자·경로·이름을 나눠 가져야 한다
//	softAlignFloor 0.15   경고선~거절선 띠에도 조금이라도 겹치는 문장을 요구한다
//
// 이 값에서 DUP 재현율 0.667 → **0.750** · 기계 규칙군 0.875 → **0.904** ·
// 대조군 거짓 경보 **0** · 가장 낮은 정밀도 **1.000** · T11 오거절 **0/20** 이다.
// 자를 더 늘리면(문턱을 내리면) 재현율은 0.86 까지 오르지만 대조군 거짓 경보가
// 3~8건 생긴다 — G6 ② 는 경고도 거짓 경보로 세므로 그 자리는 못 쓴다.
var (
	AlignCut       = 0.18
	alignMinPairs  = 2
	alignNeedFact  = true
	softAlignFloor = 0.15
)

// alignTwoStage 는 2단(사실 대조)을 켜는지다. 스윕 시험이 끄고 재려고 변수로
// 뒀다. 늘 켜져 있다.
var alignTwoStage = true

// stageTwoLoose 는 **경고 쪽(부분 중복)** 에만 느슨한 어긋남 자를 쓰는지다.
// 저장을 막는 쪽(거절)은 늘 엄한 자다 — 느슨한 자를 거절에도 쓰면 실데이터
// 40건 `duplicate-hard` 가 1 → 5건(2.5% → 12.5%)이 돼 자(5%)를 넘는다.
// 골든셋 130건 스윕(TestSweepStageTwo)이 고른 값이다 : DUP 0.750 → 0.778 ·
// 기계 규칙군 0.904 → 0.913 · 대조군 거짓 경보 0 · 가장 낮은 정밀도 1.000.
var stageTwoLoose = true

// passesStageTwo 는 정렬 후보가 2단을 넘는지다.
//
//	① 짝 조각의 숫자·경로·이름이 서로 어긋나지 않는다 (뜻은 가깝고 사실은 다른 쌍 거르기)
//	② 문턱을 넘은 짝 조각이 alignMinPairs 개 이상이다
//	③ (켜면) 짝 조각이 같은 확인 가능한 값을 나눠 갖는다
//
// warnOnly 가 참이면 ① 을 느슨한 자로 잰다 (경고까지만 하는 자리).
func passesStageTwo(found alignInfo, warnOnly bool) bool {
	if stageTwoLoose && warnOnly {
		if found.ClashLoose || factsClashLoose(found.Left, found.Right) {
			return false
		}
	} else if found.Clash || factsClash(found.Left, found.Right) {
		return false
	}
	if found.Pairs < alignMinPairs {
		return false
	}
	return !alignNeedFact || sharedFact(found.Left, found.Right)
}

// factsOnScore 는 통째 점수(S)로 걸린 쌍에도 사실 어긋남 검사를 거는지다.
// 값은 골든셋 스윕으로 정한다.
var factsOnScore = false

// alignNeed 는 중복 판정이 보는 가장 낮은 정렬 값이다. 짝 조각 값을 여기까지만
// 정확히 채우면 된다 — 1단 자(AlignCut)보다 낮은 softAlignFloor 를 통째 점수
// 쪽에서 보기 때문에 둘 중 낮은 쪽이다 (리뷰 B · V3).
func alignNeed() float64 {
	if softAlignFloor < AlignCut {
		return softAlignFloor
	}
	return AlignCut
}
