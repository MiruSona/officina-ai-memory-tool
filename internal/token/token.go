// Package token 은 글을 색인이 담는 조각과 검색이 묻는 구절로 바꾼다.
// 색인 쪽과 질의 쪽이 반드시 한 곳을 같이 써야 짝이 맞는다 (설계 6-1).
package token

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	hangulRun     = regexp.MustCompile(`[가-힣]+`)
	groupedNumber = regexp.MustCompile(`\d{1,3}(?:,\d{3})+`)
	symbolCut     = regexp.MustCompile(`[^\p{L}\p{N}]+`)
)

// isHangul 은 `[가-힣]` 한 글자인지다. 색인은 낱말 수백만 개를 훑어서
// 정규식으로 같은 답을 내면 시간의 3할을 여기서 쓴다.
func isHangul(letter rune) bool {
	return letter >= '가' && letter <= '힣'
}

// hangulSpans 는 `hangulRun.FindAllStringIndex` 와 같은 답을 낸다.
func hangulSpans(text string) [][2]int {
	spans := [][2]int{}
	start := -1
	for offset, letter := range text {
		if isHangul(letter) {
			if start < 0 {
				start = offset
			}
			continue
		}
		if start >= 0 {
			spans = append(spans, [2]int{start, offset})
			start = -1
		}
	}
	if start >= 0 {
		spans = append(spans, [2]int{start, len(text)})
	}
	return spans
}

// hasHangulRune 은 `hangulRun.MatchString` 과 같은 답을 낸다.
func hasHangulRune(text string) bool {
	for _, letter := range text {
		if isHangul(letter) {
			return true
		}
	}
	return false
}

// Version 은 이 exe 가 쓰는 토큰 방식이다. 색인에 다른 값이 적혀 있으면
// 디스크의 조각이 다른 방식으로 만들어진 것이라 색인을 다시 만든다.
const Version = "6"

// ForIndex 는 색인 쪽이다. 앞부분은 읽는 순서 그대로라서 구절 검색이 되고,
// 뒤에 붙는 덧조각(원형·camelCase·기호 쪼갠 것)은 순서를 안 지킨다.
func ForIndex(text string) string {
	main := []string{}
	extra := []string{}
	for _, word := range strings.Fields(text) {
		plain := NormalizeNumber(word)
		main = append(main, pieces(plain)...)
		extra = append(extra, extrasOf(word, plain)...)
	}
	return strings.Join(dedup(main, extra), " ")
}

// LatinOnly 는 ForIndex 가 만든 줄에서 한글이 든 조각을 뺀다. fts_en 은 영문·
// 코드만 담으면 되고, 다시 계산하지 않으려고 만들어 둔 줄을 걸러 쓴다 (설계 6-2).
func LatinOnly(indexed string) string {
	kept := []string{}
	for _, piece := range strings.Fields(indexed) {
		if !hasHangulRune(piece) {
			kept = append(kept, piece)
		}
	}
	return strings.Join(kept, " ")
}

// NormalizeNumber 는 자릿수 쉼표를 없앤다. `5,000건` 과 `5000건` 이 같은
// 글이라는 걸 색인이 알아야 한다 (조사E #6).
func NormalizeNumber(word string) string {
	// 쉼표가 없으면 바꿀 것도 없다. 낱말 대부분이 여기서 끝난다.
	if !strings.ContainsRune(word, ',') {
		return word
	}
	return groupedNumber.ReplaceAllStringFunc(word, func(match string) string {
		return strings.ReplaceAll(match, ",", "")
	})
}

// extrasOf 는 한 낱말에서 나오는 덧조각이다. 원형을 잃지 않는 것이 규칙이다.
func extrasOf(word, plain string) []string {
	out := []string{}
	if plain != word {
		out = append(out, plain, word)
	}
	for _, chunk := range latinChunks(plain) {
		out = append(out, splitMore(chunk)...)
	}
	return out
}

// splitMore 는 라틴 덩어리를 camelCase 와 기호에서 쪼갠다. 원형은 이미
// 앞 줄기에 들어 있으므로 여기서는 쪼갠 것만 돌려준다.
func splitMore(chunk string) []string {
	out := []string{}
	if parts := splitCamel(chunk); len(parts) > 1 {
		out = append(out, strings.ToLower(chunk))
		out = append(out, parts...)
	}
	if hasSymbol(chunk) {
		for _, part := range symbolCut.Split(chunk, -1) {
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// hasSymbol 은 글자도 숫자도 아닌 것이 섞였는지다. 안 섞였으면 쪼갤 일이 없다.
func hasSymbol(chunk string) bool {
	for _, letter := range chunk {
		if !unicode.IsLetter(letter) && !unicode.IsNumber(letter) {
			return true
		}
	}
	return false
}

// splitCamel 은 대문자가 시작하는 자리에서 쪼갠다. `MyClassName` 이 세 낱말이다.
func splitCamel(chunk string) []string {
	parts := []string{}
	current := strings.Builder{}
	for _, letter := range chunk {
		if unicode.IsUpper(letter) && current.Len() > 0 {
			parts = append(parts, strings.ToLower(current.String()))
			current.Reset()
		}
		current.WriteRune(letter)
	}
	if current.Len() > 0 {
		parts = append(parts, strings.ToLower(current.String()))
	}
	return parts
}

// latinChunks 는 낱말에서 한글이 아닌 덩어리만 뽑는다.
func latinChunks(word string) []string {
	chunks := []string{}
	position := 0
	for _, span := range hangulSpans(word) {
		chunks = appendTrimmed(chunks, word[position:span[0]])
		position = span[1]
	}
	return appendTrimmed(chunks, word[position:])
}

// dedup 은 같은 조각을 두 번 안 쓴다. 앞 줄기는 순서가 뜻이 있어 그대로 둔다.
func dedup(main, extra []string) []string {
	// 덧조각이 없으면 map 을 만들 것도 없다. 한글만 있는 글이 대개 그렇다.
	if len(extra) == 0 {
		return main
	}
	seen := map[string]bool{}
	for _, item := range main {
		seen[item] = true
	}
	out := main
	for _, item := range extra {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

// pieces 는 한 낱말을 왼쪽에서 오른쪽으로 훑는다. 한글 덩어리는 바이그램이 되고
// 사이의 다른 글자종은 통째로 남는다. `unity빌드` 는 `unity 빌드` 가 된다.
func pieces(word string) []string {
	out := []string{}
	position := 0
	for _, span := range hangulSpans(word) {
		out = appendTrimmed(out, word[position:span[0]])
		out = append(out, bigrams(word[span[0]:span[1]])...)
		position = span[1]
	}
	return appendTrimmed(out, word[position:])
}

// ForQuery 는 질의 쪽이다. 한글 덩어리는 바이그램만 쓴다. 낱말을 통째로 물으면
// 조사가 붙은 본문을 놓친다. 바이그램은 한 구절로 묶어 엉뚱한 글을 안 끌어온다.
func ForQuery(term string) string {
	term = NormalizeNumber(term)
	if prefix, ok := oneLetterPrefix(term); ok {
		return prefix
	}
	parts := []string{}
	position := 0
	for _, span := range hangulRun.FindAllStringIndex(term, -1) {
		parts = appendQuoted(parts, term[position:span[0]])
		parts = append(parts, runQuery(term[span[0]:span[1]]))
		position = span[1]
	}
	parts = appendQuoted(parts, term[position:])
	if len(parts) == 0 {
		return `""`
	}
	return strings.Join(parts, " AND ")
}

// runQuery 는 한글 덩어리 하나를 묻는 꼴이다. 한 글자짜리 덩어리는 접두로 묻는다.
// 색인에는 `건` 이 아니라 조사가 붙은 `건을` 이 들어 있기 때문이다.
func runQuery(run string) string {
	if len([]rune(run)) == 1 {
		return quote(run) + "*"
	}
	return quote(strings.Join(bigrams(run), " "))
}

// QueryExpr turns a whole query line into one FTS5 MATCH expression.
func QueryExpr(query string) string {
	parts := []string{}
	for _, term := range strings.Fields(query) {
		parts = append(parts, ForQuery(term))
	}
	if len(parts) == 0 {
		return `""`
	}
	return strings.Join(parts, " AND ")
}

func bigrams(run string) []string {
	letters := []rune(run)
	if len(letters) == 1 {
		return []string{run}
	}
	out := make([]string, 0, len(letters)-1)
	for i := 0; i+1 < len(letters); i++ {
		out = append(out, string(letters[i:i+2]))
	}
	return out
}

func appendQuoted(parts []string, text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return parts
	}
	return append(parts, quote(text))
}

func quote(text string) string {
	return `"` + strings.ReplaceAll(text, `"`, `""`) + `"`
}

// PhraseFor turns a quoted group into one FTS phrase, so the words must sit
// next to each other. Korean runs become bigrams, everything else stays as is.
func PhraseFor(text string) string {
	if prefix, ok := oneLetterPrefix(text); ok {
		return prefix
	}
	tokens := []string{}
	for _, word := range strings.Fields(text) {
		tokens = append(tokens, pieces(NormalizeNumber(word))...)
	}
	if len(tokens) == 0 {
		return `""`
	}
	return quote(strings.Join(tokens, " "))
}

func appendTrimmed(tokens []string, text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return tokens
	}
	return append(tokens, text)
}

// oneLetterPrefix answers a query that is one Korean letter on its own. The
// index holds bigrams, so the letter itself is almost never a token; a prefix
// query finds every bigram that starts with it instead.
func oneLetterPrefix(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if len([]rune(text)) != 1 || !hangulRun.MatchString(text) {
		return "", false
	}
	return quote(text) + "*", true
}

// SplitRun 은 붙여 쓴 한글 낱말을 쪼갠다. 가운데를 무조건 자르면 `바이그램검색`
// 이 `바이그`+`램검색` 이 돼 둘 다 못 맞았다 (조사E #5). 어휘를 모르면 안 쪼갠다.
func SplitRun(word string) (string, string, bool) {
	return SplitByVocab(word, nil)
}

// SplitByVocab 은 아는 낱말 중 가장 긴 것을 앞에 놓고 쪼갠다. known 은 검색
// 단계가 색인 어휘로 채워 준다. nil 이면 쪼개지 않는다.
func SplitByVocab(word string, known func(string) bool) (string, string, bool) {
	letters := []rune(word)
	if known == nil || len(letters) < 4 || hangulRun.FindString(word) != word {
		return "", "", false
	}
	for cut := len(letters) - 2; cut >= 2; cut-- {
		left, right := string(letters[:cut]), string(letters[cut:])
		if known(left) && known(right) {
			return left, right, true
		}
	}
	return "", "", false
}

// HasHangul says whether Korean routing applies to this text.
func HasHangul(text string) bool {
	return hasHangulRune(text)
}
