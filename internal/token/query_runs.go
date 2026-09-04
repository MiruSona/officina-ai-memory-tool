package token

import (
	"strings"
	"unicode"
)

// 질의 조각의 글자종. 어느 표로 보낼지가 이걸로 갈린다 (설계 6-3).
const (
	RunKO  = "ko"
	RunKO1 = "ko1"
	RunEN  = "en"
)

// Run 은 낱말 하나에서 글자종 경계로 갈라 나온 조각이다.
type Run struct {
	Text string
	Kind string
}

// Runs 는 낱말을 글자종 경계에서 가른다. `unity빌드` 는 영문 하나와 한글 하나가
// 되고, 영문 한 글자는 잡음만 늘어서 버린다 (설계 6-3).
func Runs(word string) []Run {
	word = NormalizeNumber(strings.TrimSpace(word))
	out := []Run{}
	position := 0
	for _, span := range hangulRun.FindAllStringIndex(word, -1) {
		out = appendLatinRun(out, word[position:span[0]])
		out = appendHangulRun(out, word[span[0]:span[1]])
		position = span[1]
	}
	return appendLatinRun(out, word[position:])
}

func appendHangulRun(out []Run, text string) []Run {
	if len([]rune(text)) == 1 {
		return append(out, Run{Text: text, Kind: RunKO1})
	}
	return append(out, Run{Text: text, Kind: RunKO})
}

// appendLatinRun 은 기호만 남은 조각과 한 글자짜리를 버린다.
func appendLatinRun(out []Run, text string) []Run {
	text = strings.Trim(strings.TrimSpace(text), "()[]{}<>,.;:!?\"'`")
	if len([]rune(text)) < 2 || !hasLetterOrDigit(text) {
		return out
	}
	return append(out, Run{Text: text, Kind: RunEN})
}

// hasLetterOrDigit 은 글자나 숫자가 하나라도 있는지다. ASCII 만 보면 키릴·
// 일본어·라틴확장·깨진 cp949 글자가 통째로 버려져 질의가 빈 것이 된다 (리뷰B #23).
func hasLetterOrDigit(text string) bool {
	for _, letter := range text {
		if unicode.IsLetter(letter) || unicode.IsDigit(letter) {
			return true
		}
	}
	return false
}

// MatchFor 는 조각 하나를 FTS5 가 읽는 꼴로 바꾼다. 한글은 바이그램 구절,
// 한글 한 글자와 영문은 접두다 (설계 6-3). 색인 조각과 짝이 맞아야 하므로
// 여기에는 칸 제한을 안 붙인다 (설계 12-3 토큰 짝 시험).
func MatchFor(item Run) string {
	switch item.Kind {
	case RunKO:
		return quote(strings.Join(bigrams(item.Text), " "))
	case RunKO1:
		return quote(item.Text) + "*"
	}
	return quote(item.Text) + "*"
}

// headColumns 는 한글 한 글자 접두를 가둘 칸이다. `훅*` 은 `훅` 으로 시작하는
// 바이그램을 전부 물어 20k 본문 색인을 통째로 훑는다(501ms). 한 글자로 물을 때
// 사람이 바라는 것은 제목·요약이지 본문 어딘가가 아니다 (리뷰B #3).
const headColumns = "{title_s meta_s summary_s} : "

// HeadMatchFor 는 제목·요약 칸에만 묻는 꼴이다. 한글 한 글자 접두에만 쓴다.
func HeadMatchFor(item Run) string {
	if item.Kind != RunKO1 {
		return MatchFor(item)
	}
	return headColumns + MatchFor(item)
}

// PrefixFor 는 낱말을 접두로 묻는 꼴이다. 사다리 5번 칸이 쓴다 (설계 6-6).
func PrefixFor(text string) string {
	if !HasHangul(text) {
		return quote(text) + "*"
	}
	return quote(strings.Join(bigrams(text), " ")) + "*"
}

// Despace 는 띄어쓰기를 지운 꼴이다. `기억 저장소` 와 `기억저장소` 가 같은
// 글이라는 걸 사다리 5번 칸이 알아야 한다 (설계 6-1).
func Despace(text string) string {
	return strings.Join(strings.Fields(text), "")
}

// SplitPairs 는 붙여 쓴 한글 낱말을 색인 어휘로 쪼갤 수 있는 모든 자리다.
// 바이그램 어휘로는 어느 자리나 「있는 낱말」 처럼 보여서 하나만 고르면 자주
// 틀린다. 그래서 후보를 다 내고 검색이 OR 로 묶는다 (설계 6-1b).
func SplitPairs(word string, known func(string) bool) [][2]string {
	letters := []rune(word)
	if known == nil || len(letters) < 4 || hangulRun.FindString(word) != word {
		return nil
	}
	out := [][2]string{}
	for cut := len(letters) - 2; cut >= 2; cut-- {
		left, right := string(letters[:cut]), string(letters[cut:])
		if known(left) && known(right) {
			out = append(out, [2]string{left, right})
		}
	}
	return out
}

// GluedFor 는 낱말을 쪼개지 않고 통째로 묻는 꼴이다. 색인은 `5,000건` 을
// `5000` `건` 말고 합친 `5000건` 도 담으므로 그 꼴도 같이 던져야 짝이 맞는다
// (설계 6-1 ForQuery 표 · 리뷰B #25).
func GluedFor(text string) string {
	if text == "" {
		return ""
	}
	return quote(text)
}

// SplitVocab 은 붙여 쓴 한글 낱말을 색인 어휘로 끝까지 쪼갠 후보들이다.
// 두 조각으로 안 나뉘면 앞에서 아는 만큼 떼고 나머지를 같은 방법으로 다시
// 쪼갠다 (설계 6-1b 3단계 · 리뷰B #26). 조각은 최대 maxSplitParts 개다.
func SplitVocab(word string, known func(string) bool) [][]string {
	if known == nil || !allHangul(word) {
		return nil
	}
	out := [][]string{}
	for _, pair := range SplitPairs(word, known) {
		out = append(out, []string{pair[0], pair[1]})
		if len(out) >= maxSplitCandidates {
			return out
		}
	}
	for _, deeper := range deepSplits(word, known) {
		out = append(out, deeper)
		if len(out) >= maxSplitCandidates {
			return out
		}
	}
	return out
}

// maxSplitParts 는 한 낱말을 몇 조각까지 쪼개는지, maxSplitCandidates 는
// 후보를 몇 벌까지 내는지다. 안 막으면 긴 낱말 하나가 질의를 통째로 삼킨다.
const (
	maxSplitParts      = 3
	maxSplitCandidates = 6
)

// deepSplits 는 앞머리를 떼고 나머지를 다시 쪼갠 세 조각짜리 후보다.
func deepSplits(word string, known func(string) bool) [][]string {
	letters := []rune(word)
	out := [][]string{}
	for cut := 2; cut+4 <= len(letters); cut++ {
		head := string(letters[:cut])
		if !known(head) {
			continue
		}
		for _, pair := range SplitPairs(string(letters[cut:]), known) {
			out = append(out, []string{head, pair[0], pair[1]})
			if len(out) >= maxSplitCandidates {
				return out
			}
		}
	}
	return out
}

func allHangul(word string) bool {
	return word != "" && hangulRun.FindString(word) == word
}

// KnownPrefix 는 붙여 쓴 한글 낱말에서 색인이 아는 가장 긴 앞머리다.
// 뒷부분이 아는 낱말이 아니어도 앞머리만으로 찾을 수 있어야 한다 —
// `효과음개수` 의 `개수` 는 저장소에 없지만 `효과음` 은 있다 (설계 6-1b 4단계).
func KnownPrefix(word string, known func(string) bool) (string, bool) {
	letters := []rune(word)
	if known == nil || len(letters) < 4 || hangulRun.FindString(word) != word {
		return "", false
	}
	for cut := len(letters) - 1; cut >= 2; cut-- {
		head := string(letters[:cut])
		if known(head) {
			return head, true
		}
	}
	return "", false
}
