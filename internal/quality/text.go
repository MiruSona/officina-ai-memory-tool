package quality

import (
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// bodyLines 는 본문을 줄로 자른다. 끝의 빈 줄은 버린다.
func bodyLines(body string) []string {
	trimmed := strings.TrimRight(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// substantialLines 는 실질 줄 수다 — 빈 줄과 제목 줄은 안 센다 (규칙 B01).
func substantialLines(body string) int {
	count := 0
	for _, line := range bodyLines(body) {
		text := strings.TrimSpace(line)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		count++
	}
	return count
}

// headings 는 `##` 로 시작하는 절 제목이다.
func headings(body string) []string {
	out := []string{}
	for _, line := range bodyLines(body) {
		text := strings.TrimSpace(line)
		if strings.HasPrefix(text, "## ") {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(text, "##")))
		}
	}
	return out
}

// section 은 제목 하나와 그 아래 실질 줄이다.
type section struct {
	Title string
	Lines []string
}

// sections 는 `##` 절로 자른다. 첫 제목 앞의 글은 안 담는다.
func sections(body string) []section {
	out := []section{}
	for _, line := range bodyLines(body) {
		text := strings.TrimSpace(line)
		if strings.HasPrefix(text, "## ") {
			out = append(out, section{Title: strings.TrimSpace(strings.TrimPrefix(text, "##"))})
			continue
		}
		if len(out) == 0 || text == "" {
			continue
		}
		last := &out[len(out)-1]
		last.Lines = append(last.Lines, text)
	}
	return out
}

// table 은 마크다운 표 하나다. Rows 는 가름줄(`---`)을 뺀 데이터 행이다.
type table struct {
	Header []string
	Rows   [][]string
}

var dividerCell = regexp.MustCompile(`^:?-{2,}:?$`)

// tables 는 본문의 마크다운 표를 다 찾는다. 가름줄이 있어야 표로 친다 —
// 없으면 그냥 `|` 가 든 글이다.
func tables(body string) []table {
	found := []table{}
	rows := [][]string{}
	flush := func() {
		if len(rows) >= 2 && isDivider(rows[1]) {
			found = append(found, table{Header: rows[0], Rows: rows[2:]})
		}
		rows = nil
	}
	for _, line := range bodyLines(body) {
		text := strings.TrimSpace(line)
		if !strings.HasPrefix(text, "|") {
			flush()
			continue
		}
		rows = append(rows, splitRow(text))
	}
	flush()
	return found
}

func splitRow(line string) []string {
	line = strings.TrimPrefix(strings.TrimSuffix(strings.TrimSpace(line), "|"), "|")
	cells := strings.Split(line, "|")
	for at := range cells {
		cells[at] = strings.TrimSpace(cells[at])
	}
	return cells
}

func isDivider(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		if !dividerCell.MatchString(strings.TrimSpace(cell)) {
			return false
		}
	}
	return true
}

// numberGroup 은 자릿수 구분기호다. 닮음 점수를 재기 전에 5,000 과 5000 을 같은
// 글로 만든다 (품질규칙표 2절).
var numberGroup = regexp.MustCompile(`(\d),(\d{3})`)

// normalizeText 는 닮음 점수용 정규형이다. 한글·영문·숫자만 남기고 소문자로.
func normalizeText(text string) string {
	text = strings.ToLower(text)
	for numberGroup.MatchString(text) {
		text = numberGroup.ReplaceAllString(text, "$1$2")
	}
	out := strings.Builder{}
	for _, letter := range text {
		if isContent(letter) {
			out.WriteRune(letter)
		}
	}
	return out.String()
}

func isContent(letter rune) bool {
	if letter >= 0xAC00 && letter <= 0xD7A3 {
		return true
	}
	return unicode.IsLetter(letter) || unicode.IsDigit(letter)
}

// grams 는 글자 3-gram 집합이다. 3글자보다 짧으면 글 자체가 조각 하나다.
//
// 조각을 글자열 map 으로 들면 짝을 견줄 때마다 글자열 해시를 다시 뜬다.
// 20k lint 에서 그 자리가 18초였다. **64비트 지문을 정렬해 들고** 병합으로
// 겹침을 세면 답은 그대로면서 훨씬 싸다.
func grams(text string) []uint64 {
	letters := []rune(text)
	if len(letters) == 0 {
		return nil
	}
	if len(letters) <= gramSize {
		return []uint64{gramHash(letters)}
	}
	out := make([]uint64, 0, len(letters)-gramSize+1)
	for at := 0; at+gramSize <= len(letters); at++ {
		out = append(out, gramHash(letters[at:at+gramSize]))
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// gramHash 는 조각 하나의 64비트 지문이다 (FNV-1a).
func gramHash(letters []rune) uint64 {
	hash := uint64(14695981039346656037)
	for _, letter := range letters {
		hash ^= uint64(letter)
		hash *= 1099511628211
	}
	return hash
}

const gramSize = 3

// jaccard 는 두 조각 집합이 겹치는 비율이다. 둘 다 정렬돼 있어 병합하며 센다.
// jaccardAtLeast 는 자카드를 재되 **need 에 못 미치는 것이 확실해지면 거기서
// 그만둔다** (리뷰 B · V3). 남은 조각을 다 겹쳐도 못 넘으면 더 셀 이유가 없다.
// 문장 정렬은 짝의 대부분이 자 아래라 이 한 줄이 20k lint 시간의 3분의 1을 뗀다.
// 값이 need 위인 짝은 자카드 그대로고, 아래인 짝은 0 이다 — 부르는 쪽이 자로
// 가르는 자리라 답이 안 바뀐다.
func jaccardAtLeast(left, right []uint64, need float64) float64 {
	if need <= 0 {
		return jaccard(left, right)
	}
	// (v0.4 C2) 길이를 밖으로 빼고, 「어느 쪽을 한 칸 옮길까」를 **분기가 아니라
	// 셈**으로 푼다. 안쪽 고리가 20k lint 에서 CPU 의 38.8% 였고, 그 절반이
	// 어긋난 갈래 예측이었다. 답은 한 자리도 안 바뀐다 — 걷는 차례가 같다.
	nl, nr := len(left), len(right)
	total := nl + nr
	// most/(total-most) < need 이면 그만둔다. 자리를 옮겨 쓰면 정수 하나와의
	// 견줌이 된다 : most < need·total/(1+need).
	// 1e-9 는 「값이 need 와 꼭 같은 짝」을 실수 오차로 잃지 않으려는 여유다.
	floor := int(math.Ceil(need*float64(total)/(1+need) - 1e-9))
	shared, at, other := 0, 0, 0
	for at < nl && other < nr {
		one, two := left[at], right[other]
		if one == two {
			// 겹치면 「앞으로 겹칠 수 있는 최대」가 그대로다. 다시 안 잰다.
			shared++
			at++
			other++
			continue
		}
		step := 0
		if one < two {
			step = 1
		}
		at += step
		other += 1 - step
		restLeft, restRight := nl-at, nr-other
		if restLeft > restRight {
			restLeft = restRight
		}
		if shared+restLeft < floor {
			return 0
		}
	}
	union := total - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

func jaccard(left, right []uint64) float64 {
	shared := 0
	at, other := 0, 0
	for at < len(left) && other < len(right) {
		switch {
		case left[at] == right[other]:
			shared++
			at++
			other++
		case left[at] < right[other]:
			at++
		default:
			other++
		}
	}
	union := len(left) + len(right) - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

// wordSplit 은 한글 덩어리와 영문·숫자 낱말로 자른다. 요약 낱말이 본문에
// 나오는지 볼 때 쓴다 (규칙 B08).
func wordSplit(text string) []string {
	words := []string{}
	current := strings.Builder{}
	kind := 0 // 1=한글 2=영문·숫자
	flush := func() {
		if current.Len() > 0 {
			words = append(words, current.String())
			current.Reset()
		}
	}
	for _, letter := range strings.ToLower(text) {
		now := 0
		switch {
		case letter >= 0xAC00 && letter <= 0xD7A3:
			now = 1
		case unicode.IsLetter(letter) || unicode.IsDigit(letter):
			now = 2
		}
		if now == 0 || now != kind {
			flush()
		}
		kind = now
		if now != 0 {
			current.WriteRune(letter)
		}
	}
	flush()
	return words
}

// contentWords 는 낱말 중 뜻이 있는 것만 남긴다 — 한글 2자 이상, 영문 3자 이상.
func contentWords(text string) []string {
	out := []string{}
	for _, word := range wordSplit(text) {
		letters := []rune(word)
		if isHangul(letters[0]) {
			if len(letters) >= 2 {
				out = append(out, word)
			}
			continue
		}
		if len(letters) >= 3 {
			out = append(out, word)
		}
	}
	return out
}

func isHangul(letter rune) bool { return letter >= 0xAC00 && letter <= 0xD7A3 }

// particleTails 는 이름씨 뒤에 붙는 토씨다. 긴 것부터 본다.
var particleTails = []string{"에서는", "에게는", "에서", "에게", "까지", "부터", "으로", "보다", "처럼",
	"마다", "만큼", "이라", "라는", "라고", "이는", "은", "는", "이", "가", "을", "를", "의", "에", "와", "과", "도", "로", "만"}

// predicateTails 는 풀이말 꼬리다. 이렇게 끝나는 낱말은 이름씨가 아니라서
// 본문에 글자 그대로 다시 나올 까닭이 없다 — 세면 요약이 본문과 다르다고
// 잘못 말한다 (규칙 B08 은 「요약의 이름씨 낱말」만 본다).
//
// **(2D) 한 글자 꼬리를 「다」만 남기고 다 뺐다.** 「지」 때문에 `이미지`·`패키지`·
// `메시지` 가, 「자」 때문에 `숫자`·`글자`·`문자` 가, 「서」 때문에 `순서`·`문서` 가,
// 「고」 때문에 `경고`·`참고`·`보고` 가 이름씨에서 빠졌다. 이름씨를 잘못 빼면
// B08 이 「요약 낱말이 본문에 없다」를 멀쩡한 기억에도 말한다.
var predicateTails = []string{"였다", "었다", "겠다", "한다", "된다", "이다", "않다", "린다", "든다",
	"난다", "간다", "온다", "본다", "쓴다", "묻다", "다"}

// nounWords 는 요약에서 이름씨로 보이는 낱말만 뽑는다. 형태소 분석기를 안 쓰기로
// 했으므로(설계 4-2) 토씨를 떼고 풀이말 꼬리를 버리는 것으로 갈음한다.
func nounWords(text string) []string {
	out := []string{}
	for _, word := range contentWords(text) {
		letters := []rune(word)
		if !isHangul(letters[0]) {
			out = append(out, word)
			continue
		}
		// **(2D) 홀로 선 토씨는 이름씨가 아니다.** `으로`·`에서` 가 앞말과 떨어져
		// 적히면 stripTail 이 아무것도 못 떼고 그대로 이름씨로 셌다.
		if isParticle(word) || hasTail(word, predicateTails) {
			continue
		}
		out = append(out, stripTail(word))
	}
	return out
}

func isParticle(word string) bool {
	for _, tail := range particleTails {
		if word == tail {
			return true
		}
	}
	return false
}

func hasTail(word string, tails []string) bool {
	for _, tail := range tails {
		if strings.HasSuffix(word, tail) && len([]rune(word)) > len([]rune(tail)) {
			return true
		}
	}
	return false
}

// stripTail 은 토씨 하나를 뗀다. 떼고 두 글자 아래로 줄면 안 뗀다.
func stripTail(word string) string {
	for _, tail := range particleTails {
		if !strings.HasSuffix(word, tail) {
			continue
		}
		rest := []rune(strings.TrimSuffix(word, tail))
		if len(rest) >= 2 {
			return string(rest)
		}
	}
	return word
}

// containsWord 는 본문에 그 낱말이 나오는지다. 한글은 조사가 붙고 복합어로
// 갈라져서 글자 그대로는 잘 안 맞는다 — 「비밀정보」와 「비밀 패턴」이 남남이 되면
// 요약과 본문이 다르다고 잘못 말한다. 그래서 낱말의 글자 2조각이 본문에 거의 다
// 있으면 나온 것으로 본다.
func containsWord(body, word string) bool {
	if strings.Contains(body, word) {
		return true
	}
	letters := []rune(word)
	if len(letters) < 3 {
		return false
	}
	pieces := 0
	hit := 0
	for at := 0; at+2 <= len(letters); at++ {
		pieces++
		if strings.Contains(body, string(letters[at:at+2])) {
			hit++
		}
	}
	return pieces > 0 && float64(hit)/float64(pieces) >= wordPieceFloor
}

// wordPieceFloor 는 「같은 낱말로 친다」 는 조각 겹침 비율이다.
const wordPieceFloor = 0.5
