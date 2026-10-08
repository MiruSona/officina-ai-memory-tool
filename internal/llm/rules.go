package llm

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// 규칙 층(판정 사다리 ① 단)이다. 반대(B)·무관(C)만 내고 지지는 안 낸다 — 지지는 규칙으로
// 못 잡았다. 조건·문턱·낱말표의 원본은 자체판정프로그램설계(2026-10-08) 2절.

// Label 은 판정 글자다 (LetterSupport · LetterContradict · LetterUnrelated 와 같은 값).
type Label = string

const (
	// RulesVersion 은 규칙 판 이름이다 (Verdict.Prompt 칸).
	RulesVersion = "rules-v1"
	// RulesProfile 은 규칙 판정의 Profile 칸이다.
	RulesProfile = "rules"

	unrelatedJaccardBelow  = 0.05
	unrelatedCoverageBelow = 0.10
	// numberGapOver 는 같은 자리 두 숫자를 「다르다」로 치는 상대 차이다.
	numberGapOver = 0.05
	// placeWords 는 「같은 자리」를 볼 때 앞에서 보는 낱말 수다.
	placeWords = 2
	// numberDigitsMax 는 값으로 읽는 숫자 자릿수 상한이다. 더 길면 id·해시 조각으로 본다.
	numberDigitsMax = 12
)

// negation 은 부정 짝 하나다. Plain 이 긍정 꼴, Flipped 가 뒤집은 꼴이다. Plain 이 Flipped
// 안에 들어 있으면(한다 ⊂ 안 한다) 긍정 꼴은 뒤집은 꼴 밖에서만 센다.
type negation struct {
	Plain   []string
	Flipped string
}

// negations 는 설계 2절 부정 짝 표다. 긴 꼴을 먼저 본다.
var negations = []negation{
	{Plain: []string{"한다"}, Flipped: "안 한다"},
	{Plain: []string{"된다"}, Flipped: "안 된다"},
	{Plain: []string{"기본 켬"}, Flipped: "기본 끔"},
	{Plain: []string{"있다"}, Flipped: "없다"},
	{Plain: []string{"넣는다"}, Flipped: "뺀다"},
	{Plain: []string{"켠다"}, Flipped: "끈다"},
	{Plain: []string{"늘린다"}, Flipped: "줄인다"},
	{Plain: []string{"늘었다"}, Flipped: "줄었다"},
	{Plain: []string{"늘어"}, Flipped: "줄어"},
	{Plain: []string{"통과", "합격"}, Flipped: "미달"},
	{Plain: []string{"올린다"}, Flipped: "내린다"},
}

// unit 은 단위 표 한 줄이다. 같은 Group 끼리만 견주고, Scale 을 곱해 맞춘다.
type unit struct {
	Name  string
	Group string
	Scale float64
}

// units 는 설계 2절 단위 표에 「종」을 더한 것이다 (조사 3절 · 10-08 실측). 긴 이름을 먼저 본다.
var units = []unit{
	{"시간", "time", 3600000}, {"ms", "time", 1}, {"초", "time", 1000}, {"분", "time", 60000}, {"s", "time", 1000},
	{"gb", "size", 1000000}, {"mb", "size", 1000}, {"kb", "size", 1},
	{"%", "%", 1}, {"종", "종", 1}, {"배", "배", 1}, {"건", "건", 1}, {"쌍", "쌍", 1}, {"개", "개", 1}, {"줄", "줄", 1},
	{"판", "판", 1}, {"번", "번", 1}, {"회", "회", 1}, {"자", "자", 1}, {"원", "원", 1},
}

// gluedNegation 은 띄어 쓰지 않은 부정 꼴(안한다·안된다)이다. 「안」이 낱말 머리일 때만 —
// 「제안한다」는 안 건드린다. 붙여 쓴 꼴을 긍정 꼴로 읽어 지지 쌍을 반대로 잡던 오발을 막는다.
var gluedNegation = regexp.MustCompile(`(^|[^\p{L}\p{N}])안(한다|된다)`)

// spacedNegation 은 붙여 쓴 부정 꼴을 짝 표의 띄어 쓴 꼴로 맞춘다.
func spacedNegation(text string) string {
	return gluedNegation.ReplaceAllString(text, "${1}안 ${2}")
}

// digitsPattern 은 숫자 하나다. 세 자리 쉼표 묶음(11,172)은 한 숫자로 읽는다.
var digitsPattern = regexp.MustCompile(`\d{1,3}(?:,\d{3})+|\d+`)

// RuleJudge 는 규칙 셋을 R-부정 → R-숫자 → R-무관 차례로 보고 첫 걸림에서 끝낸다.
// ok 가 거짓이면 「안 걸림」이다.
func RuleJudge(evidence, claim string) (label Label, reason string, ok bool) {
	if why, hit := negationHit(evidence, claim); hit {
		return LetterContradict, why, true
	}
	evidenceNumbers, claimNumbers := numbersOf(evidence), numbersOf(claim)
	if why, hit := numberHit(evidenceNumbers, claimNumbers); hit {
		return LetterContradict, why, true
	}
	if why, hit := unrelatedHit(evidence, claim, evidenceNumbers, claimNumbers); hit {
		return LetterUnrelated, why, true
	}
	return "", "", false
}

// negationHit 은 R-부정이다. 근거의 긍정 꼴이 주장에서 같은 자리의 뒤집은 꼴로 바뀌었거나 그 반대다.
func negationHit(evidence, claim string) (string, bool) {
	evidence, claim = spacedNegation(evidence), spacedNegation(claim)
	for _, pair := range negations {
		for _, plain := range pair.Plain {
			if samePlaceFlip(evidence, claim, plain, pair.Flipped, pair) {
				return "neg:" + plain + "→" + pair.Flipped, true
			}
			if samePlaceFlip(evidence, claim, pair.Flipped, plain, pair) {
				return "neg:" + pair.Flipped + "→" + plain, true
			}
		}
	}
	return "", false
}

// samePlaceFlip 은 근거에 from 이 있고 주장에 to 가 있으며, 근거에 to 가 없고 주장에 from 이
// 없고, 두 꼴의 앞 낱말이 하나 이상 같은지다.
func samePlaceFlip(evidence, claim, from, to string, pair negation) bool {
	fromOffsets := formOffsets(evidence, from, pair)
	toOffsets := formOffsets(claim, to, pair)
	if len(fromOffsets) == 0 || len(toOffsets) == 0 {
		return false
	}
	if len(formOffsets(claim, from, pair)) > 0 || len(formOffsets(evidence, to, pair)) > 0 {
		return false
	}
	return offsetsSharePlace(evidence, fromOffsets, claim, toOffsets)
}

// formOffsets 는 꼴 하나가 나오는 바이트 자리들이다. 긍정 꼴이 뒤집은 꼴 안에 든 자리는 뺀다.
func formOffsets(text, form string, pair negation) []int {
	found := allOffsets(text, form)
	if form == pair.Flipped || !strings.Contains(pair.Flipped, form) {
		return found
	}
	inside := map[int]bool{}
	lead := strings.Index(pair.Flipped, form)
	for _, at := range allOffsets(text, pair.Flipped) {
		inside[at+lead] = true
	}
	kept := []int{}
	for _, at := range found {
		if !inside[at] {
			kept = append(kept, at)
		}
	}
	return kept
}

func allOffsets(text, form string) []int {
	offsets := []int{}
	for start := 0; ; {
		at := strings.Index(text[start:], form)
		if at < 0 {
			return offsets
		}
		offsets = append(offsets, start+at)
		start += at + len(form)
	}
}

// offsetsSharePlace 는 두 글의 자리 묶음 중 앞 낱말이 하나라도 같은 짝이 있는지다.
func offsetsSharePlace(left string, leftOffsets []int, right string, rightOffsets []int) bool {
	for _, a := range leftOffsets {
		for _, b := range rightOffsets {
			if sharePlace(wordsBefore(left, a), wordsBefore(right, b)) {
				return true
			}
		}
	}
	return false
}

// wordsBefore 는 바이트 자리 at 바로 앞 낱말 placeWords 개다 (낱말은 ruleWords 와 같은 규칙).
// 앞뒤 창은 실물 num 오발 5.5% 라 접음(10-08).
func wordsBefore(text string, at int) []string {
	list := wordList(text[:at])
	if len(list) > placeWords {
		list = list[len(list)-placeWords:]
	}
	return list
}

func sharePlace(left, right []string) bool {
	for _, a := range left {
		for _, b := range right {
			if a == b {
				return true
			}
		}
	}
	return false
}

// number 는 글 속 「값」으로 쓰인 숫자 하나다. Value 는 단위를 맞춘 값이다.
type number struct {
	Text  string
	Unit  unit
	Value float64
	Place []string
}

// numbersOf 는 단위가 붙은 「값」 숫자만 모은다. 단위 없는 숫자는 R-숫자에서 뺀다.
func numbersOf(text string) []number {
	found := []number{}
	for _, loc := range digitsPattern.FindAllStringIndex(text, -1) {
		if _, plain := plainNumber(text, loc); !plain || len(strings.ReplaceAll(text[loc[0]:loc[1]], ",", "")) > numberDigitsMax {
			continue
		}
		which, ok := unitAfter(text[loc[1]:])
		if !ok {
			continue
		}
		raw, err := strconv.ParseFloat(strings.ReplaceAll(text[loc[0]:loc[1]], ",", ""), 64)
		if err != nil {
			continue
		}
		found = append(found, number{Text: text[loc[0]:loc[1]] + text[loc[1]:loc[1]+len(which.Name)],
			Unit: which, Value: raw * which.Scale, Place: wordsBefore(text, loc[0])})
	}
	return found
}

// unitAfter 는 숫자 바로 뒤(띄움 없이)에 붙은 단위다. 라틴 단위는 대소문자를 안 가린다.
// 라틴 단위 뒤에 라틴 글자가 더 이어지면(5steps) 단위로 안 친다.
func unitAfter(rest string) (unit, bool) {
	lowered := strings.ToLower(rest)
	for _, candidate := range units {
		if !strings.HasPrefix(lowered, candidate.Name) {
			continue
		}
		next := []rune(lowered[len(candidate.Name):])
		if candidate.Name[0] < unicode.MaxASCII && len(next) > 0 && next[0] < unicode.MaxASCII && unicode.IsLetter(next[0]) {
			continue
		}
		return candidate, true
	}
	return unit{}, false
}

// numberHit 은 R-숫자다. 주장의 숫자마다 근거에 같은 단위 · 같은 자리 짝을 찾는다. 맞는 짝이
// 하나라도 있으면 그 숫자는 넘긴다. 짝이 없는 숫자(주장이 더한 숫자)는 보지 않는다.
func numberHit(evidence, claim []number) (string, bool) {
	for _, b := range claim {
		conflict := ""
		matched := false
		for _, a := range evidence {
			if a.Unit.Group != b.Unit.Group || !sharePlace(a.Place, b.Place) {
				continue
			}
			if sameNumber(a.Value, b.Value) {
				matched = true
				break
			}
			if conflict == "" {
				conflict = "num:" + a.Text + "≠" + b.Text
			}
		}
		if !matched && conflict != "" {
			return conflict, true
		}
	}
	return "", false
}

// sameNumber 는 두 값이 같은 숫자로 칠 만한지다 — 5% 안이거나, 한쪽을 다른 쪽의 끝 유효숫자
// 자리로 반올림하면 같다 (1500ms ↔ 2초 · 26.6 ↔ 27). 자릿수(10의 몇 제곱)가 다르면 반올림을 안 쳐 준다.
func sameNumber(a, b float64) bool {
	high := math.Max(a, b)
	if high == 0 || math.Abs(a-b)/high <= numberGapOver {
		return true
	}
	if a <= 0 || b <= 0 || magnitudeOf(a) != magnitudeOf(b) {
		return false
	}
	return roundTo(b, precisionOf(a)) == a || roundTo(a, precisionOf(b)) == b
}

// magnitudeOf 는 맨 앞 자리의 10 제곱수다 (50 → 1 · 100 → 2 · 1.46 → 0).
func magnitudeOf(value float64) int {
	return int(math.Floor(math.Log10(value)))
}

// precisionOf 는 값의 끝 유효숫자 자리다 (1200 → 100 · 7 → 1 · 1.5 → 0.1). 0 이면 1 이다.
func precisionOf(value float64) float64 {
	text := strconv.FormatFloat(value, 'f', -1, 64)
	if _, fraction, ok := strings.Cut(text, "."); ok {
		return math.Pow(10, -float64(len(fraction)))
	}
	step := 1.0
	for value != 0 && math.Mod(value, step*10) == 0 {
		step *= 10
	}
	return step
}

// roundTo 는 value 를 step 자리로 반올림한다. 소수 자리는 곱해서 나눠야 1.5 가 1.5000000000000002 가 안 된다.
func roundTo(value, step float64) float64 {
	if step < 1 {
		scale := math.Round(1 / step)
		return math.Round(value*scale) / scale
	}
	return math.Round(value/step) * step
}

// unrelatedHit 은 R-무관이다. 낱말·바이그램이 거의 안 겹치고 같은 숫자도 부정 짝도 없다.
func unrelatedHit(evidence, claim string, evidenceNumbers, claimNumbers []number) (string, bool) {
	jaccard := wordJaccard(evidence, claim)
	coverage, has := bigramCoverage(evidence, claim)
	if !has || jaccard >= unrelatedJaccardBelow || coverage >= unrelatedCoverageBelow {
		return "", false
	}
	if shareUnit(evidenceNumbers, claimNumbers) || anyNegationPair(evidence, claim) {
		return "", false
	}
	return fmt.Sprintf("unrelated:j%.2f/b%.2f", jaccard, coverage), true
}

// shareUnit 은 같은 단위 무리의 숫자가 양쪽에 다 있는지다. 값이 달라도 같은 것을 재는 글일 수
// 있어 무관으로 치지 않는다 (조사 3절 「같은 단위 숫자 없음」).
func shareUnit(evidence, claim []number) bool {
	for _, a := range evidence {
		for _, b := range claim {
			if a.Unit.Group == b.Unit.Group {
				return true
			}
		}
	}
	return false
}

// anyNegationPair 는 자리를 안 보고 부정 짝이 양쪽에 갈려 있는지다.
func anyNegationPair(evidence, claim string) bool {
	evidence, claim = spacedNegation(evidence), spacedNegation(claim)
	for _, pair := range negations {
		for _, plain := range pair.Plain {
			if strings.Contains(evidence, plain) && strings.Contains(claim, pair.Flipped) {
				return true
			}
			if strings.Contains(evidence, pair.Flipped) && strings.Contains(claim, plain) {
				return true
			}
		}
	}
	return false
}

// wordJaccard 는 낱말 집합 자카드다.
func wordJaccard(a, b string) float64 {
	setA, setB := ruleWords(a), ruleWords(b)
	both := 0
	for word := range setA {
		if setB[word] {
			both++
		}
	}
	union := len(setA) + len(setB) - both
	if union == 0 {
		return 0
	}
	return float64(both) / float64(union)
}

// bigramCoverage 는 주장의 글자 바이그램(공백 뺌) 중 근거에도 있는 몫이다. 조사 차이를
// 흡수한다 (「규칙은」·「규칙을」은 「규칙」이 같다). 어느 쪽이든 바이그램이 없으면 has 거짓이다 —
// 빈 근거를 「무관」으로 잡지 않는다.
func bigramCoverage(evidence, claim string) (float64, bool) {
	claimGrams, evidenceGrams := bigrams(claim), bigrams(evidence)
	if len(claimGrams) == 0 || len(evidenceGrams) == 0 {
		return 0, false
	}
	found := 0
	for gram := range claimGrams {
		if evidenceGrams[gram] {
			found++
		}
	}
	return float64(found) / float64(len(claimGrams)), true
}

func bigrams(text string) map[string]bool {
	runes := []rune(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, strings.ToLower(text)))
	set := map[string]bool{}
	for at := 0; at+1 < len(runes); at++ {
		set[string(runes[at:at+2])] = true
	}
	return set
}

// ruleWords 는 testdata/kpairs 의 words() 와 같다 — 소문자, 글자·숫자 아닌 것으로 자르고 두 글자 이상만.
func ruleWords(text string) map[string]bool {
	set := map[string]bool{}
	for _, word := range wordList(text) {
		set[word] = true
	}
	return set
}

// wordList 는 ruleWords 의 차례 있는 꼴이다 (「같은 자리」를 볼 때 쓴다).
func wordList(text string) []string {
	list := []string{}
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len([]rune(word)) >= 2 {
			list = append(list, word)
		}
	}
	return list
}

// plainNumber 는 testdata/kpairs 의 같은 함수를 옮겨 쉼표 검사를 더한 것이다 — 숫자가 「값」으로 쓰였는지 보고
// 바로 뒤 글자를 돌려준다. 낱말에 붙은 숫자와 날짜·판·범위(10-06·1.2·4~5·12:30)는 빼낸다.
func plainNumber(text string, loc []int) (rune, bool) {
	const joiners = "-.~:/_"
	before, after := rune(0), rune(0)
	if loc[0] > 0 {
		before = []rune(text[:loc[0]])[len([]rune(text[:loc[0]]))-1]
	}
	if loc[1] < len(text) {
		after = []rune(text[loc[1]:])[0]
	}
	if before < unicode.MaxASCII && (unicode.IsLetter(before) || strings.ContainsRune(joiners, before)) && before != 0 {
		return 0, false
	}
	if strings.ContainsRune(joiners, after) && after != 0 {
		return 0, false
	}
	if commaDigit(text[:loc[0]], true) || commaDigit(text[loc[1]:], false) {
		return 0, false
	}
	return after, true
}

// commaDigit 은 숫자 바로 옆이 「쉼표 + 숫자」인지다 (3,5 같은 나열 · 묶음에서 잘린 조각).
// kpairs 원본에는 없다 — 11,172자 를 172자 로 읽던 버그를 여기서만 고쳤다 (10-08).
func commaDigit(side string, before bool) bool {
	runes := []rune(side)
	if len(runes) < 2 {
		return false
	}
	if before {
		return runes[len(runes)-1] == ',' && unicode.IsDigit(runes[len(runes)-2])
	}
	return runes[0] == ',' && unicode.IsDigit(runes[1])
}
