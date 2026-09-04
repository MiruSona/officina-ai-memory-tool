package embed

// 임베딩에 넣기 전 글 다듬기 (리뷰 B · H-1).
//
// 3자 토크나이저(`sugarme/tokenizer`)가 **어떤 글자 조합에서 슬라이스 범위를
// 벗어나 죽는다** — 정규화기가 글자를 바꿔치기하면서 원본 자리표를 잘못 셈한다.
// 라이브러리는 못 고치니 ①들어가는 글에서 그런 글자를 미리 빼고 ②그래도 터지면
// 그 한 건만 건너뛴다 (model.go 의 recover).
//
// 빼는 것은 **뜻이 없는 글자**뿐이다 — 제로폭·방향 뒤집기·서식 제어·ANSI 색깔
// 코드·NUL·벨. 사람이 읽는 글자는 하나도 안 건드린다.

import "strings"

// Clean 은 임베딩에 넣을 글을 다듬는다. 빈 글이면 빈 글을 돌려준다.
func Clean(text string) string {
	if text == "" {
		return text
	}
	text = dropANSI(text)
	out := make([]rune, 0, len(text))
	for _, letter := range text {
		if dropRune(letter) {
			continue
		}
		if letter < 0x20 && letter != '\n' && letter != '\t' {
			out = append(out, ' ')
			continue
		}
		out = append(out, letter)
	}
	return strings.Join(strings.Fields(string(out)), " ")
}

// dropRune 은 통째로 빼는 글자다.
func dropRune(letter rune) bool {
	switch {
	case letter == 0xFEFF: // BOM · 제로폭 넌브레이킹
		return true
	case letter >= 0x200B && letter <= 0x200F: // 제로폭 · 방향 표시
		return true
	case letter >= 0x202A && letter <= 0x202E: // 방향 뒤집기
		return true
	case letter >= 0x2060 && letter <= 0x2064: // 단어 이음 · 보이지 않는 연산자
		return true
	case letter >= 0x2066 && letter <= 0x2069: // 방향 격리
		return true
	case letter == 0x00AD: // 소프트 하이픈
		return true
	case letter >= 0xFFF9 && letter <= 0xFFFB: // 주석 표시
		return true
	case letter == 0xFFFE || letter == 0xFFFF:
		return true
	case letter == 0x7F: // DEL
		return true
	}
	return false
}

// dropANSI 는 터미널 색깔 코드(ESC [ … m 꼴)를 뺀다.
func dropANSI(text string) string {
	if !strings.ContainsRune(text, 0x1B) {
		return text
	}
	out := make([]rune, 0, len(text))
	letters := []rune(text)
	for at := 0; at < len(letters); at++ {
		if letters[at] != 0x1B {
			out = append(out, letters[at])
			continue
		}
		// ESC 다음이 '[' 면 알파벳이 나올 때까지 버린다. 아니면 ESC 만 버린다.
		if at+1 < len(letters) && letters[at+1] == '[' {
			at += 2
			for at < len(letters) && !isFinalByte(letters[at]) {
				at++
			}
		}
	}
	return string(out)
}

func isFinalByte(letter rune) bool {
	return (letter >= 'a' && letter <= 'z') || (letter >= 'A' && letter <= 'Z')
}
