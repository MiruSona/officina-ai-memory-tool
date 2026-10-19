package retain

import (
	"strings"
	"unicode/utf8"
)

// 토막 글의 머리 줄. 생성 모델이 누가 한 말인지 알게 발화마다 붙인다.
const (
	HeadUser      = "[사용자]"
	HeadAssistant = "[AI]"
)

// Chunks 는 발화를 생성 서버에 보낼 토막으로 나눈다 (A2 설계 3절 토막 규칙 1~4).
// 사용자 발화부터 다음 사용자 발화 앞까지가 한 주고받기이고, 뒤에서부터 maxChars(글자 수)
// 안으로 묶는다. maxChunks 를 넘는 앞쪽 토막은 버리고 그 수를 dropped 로 준다.
// 돌려주는 토막은 시간순(앞→뒤)이다. 0 이하 상한은 끈 것으로 본다.
func Chunks(utterances []Utterance, maxChars, maxChunks int) (chunks []string, dropped int) {
	rendered := []string{}
	for _, exchange := range splitExchanges(utterances) {
		rendered = append(rendered, chunkExchange(exchange))
	}
	backward := []string{}
	current := ""
	for at := len(rendered) - 1; at >= 0; at-- {
		piece := rendered[at]
		if maxChars > 0 && utf8.RuneCountInString(piece) > maxChars {
			piece = cutRunes(piece, maxChars)
		}
		if current == "" {
			current = piece
			continue
		}
		joined := piece + "\n" + current
		if maxChars > 0 && utf8.RuneCountInString(joined) > maxChars {
			backward = append(backward, current)
			current = piece
			continue
		}
		current = joined
	}
	if current != "" {
		backward = append(backward, current)
	}
	if maxChunks > 0 && len(backward) > maxChunks {
		dropped = len(backward) - maxChunks
		backward = backward[:maxChunks]
	}
	for at := len(backward) - 1; at >= 0; at-- {
		chunks = append(chunks, backward[at])
	}
	return chunks, dropped
}

// splitExchanges 는 사용자 발화마다 새 주고받기를 연다. 첫 사용자 발화 앞의 AI 글은
// 그것만으로 한 주고받기다.
func splitExchanges(utterances []Utterance) [][]Utterance {
	groups := [][]Utterance{}
	current := []Utterance{}
	for _, one := range utterances {
		if one.Role == RoleUser && len(current) > 0 {
			groups = append(groups, current)
			current = []Utterance{}
		}
		current = append(current, one)
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}
	return groups
}

// chunkExchange 는 주고받기 하나를 머리 줄 단 글로 편다.
func chunkExchange(exchange []Utterance) string {
	lines := []string{}
	for _, one := range exchange {
		head := HeadAssistant
		if one.Role == RoleUser {
			head = HeadUser
		}
		lines = append(lines, head+"\n"+one.Text)
	}
	return strings.Join(lines, "\n")
}

// cutRunes 는 글 끝을 잘라 limit 글자 안에 넣는다. 주고받기는 사용자 말이 앞이라
// 잘리는 것은 AI 글 끝이다. 잘렸다는 표시로 「…」을 붙인다.
func cutRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}
