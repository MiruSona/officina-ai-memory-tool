// Package safe 는 못 믿을 글(기억 본문·요약)을 AI 컨텍스트에 실을 수 있는 꼴로
// 중화한다. 훅 블록·`search` 표·`show` 출력이 **같은 함수 하나**를 지난다 —
// 세 자리가 따로 놀면 한 자리만 뚫려도 주입이 통한다 (설계 6-6 불변조건 I3).
package safe

import (
	"strings"
	"unicode"
)

// markup 은 블록을 닫거나 태그·코드펜스를 여는 글자를 닮은 글자로 바꾼다.
// 코드펜스가 먼저다 — Replacer 는 앞에 적은 것부터 맞춰 본다.
var markup = strings.NewReplacer("<", "‹", ">", "›", "```", "'''", "`", "'")

// LeadMarks 는 마크다운이 구조로 읽는 줄머리 글자다. 인용·꺾쇠는 markup 이
// 이미 닮은 글자로 바꿔 놨다.
const LeadMarks = " \t#-|`*+"

// 줄바꿈으로 읽힐 수 있는 공백들이다. 글자 그대로 쓰면 눈에 안 보여서 값으로 둔다.
const (
	noBreakSpace = 0x00a0
	lineSep      = 0x2028
	paraSep      = 0x2029
)

// OneLine 은 줄바꿈·탭·제어문자를 공백 하나로 접는다. 기억 하나가 두 줄이 될
// 수 없게 하는 자리다. 눈에 안 보이는 글자는 공백도 아니고 아예 뺀다.
func OneLine(text string) string {
	out := strings.Builder{}
	gap := false
	for _, letter := range text {
		if Invisible(letter) {
			continue
		}
		if isBlank(letter) {
			gap = true
			continue
		}
		if gap && out.Len() > 0 {
			out.WriteByte(' ')
		}
		gap = false
		out.WriteRune(letter)
	}
	return out.String()
}

// Invisible 은 폭이 없거나 글자 차례를 뒤집는 글자다. 제로폭 이음쇠로 낱말을
// 쪼개면 사람 눈에는 안 보이는데 규칙 검사만 빗나간다. 공백으로 바꾸지 않고
// 통째로 뺀다 — 있던 자리가 남으면 안 된다 (보안시험 M-2).
func Invisible(letter rune) bool {
	switch letter {
	case 0x200b, 0x200c, 0x200d, 0x2060, 0xfeff, 0x00ad, 0x180e:
		return true
	}
	// Cf 는 방향 뒤집기(U+202E 따위)까지 담는 서식 글자 갈래다.
	return unicode.Is(unicode.Cf, letter)
}

// isBlank 은 C0/C1 제어문자와 마크다운이 줄바꿈으로 읽을 수 있는 구분자다.
func isBlank(letter rune) bool {
	switch letter {
	case ' ', noBreakSpace, lineSep, paraSep:
		return true
	}
	return unicode.IsControl(letter) || unicode.IsSpace(letter)
}

// Neutralize 는 블록을 닫거나 태그를 열 수 있는 글자를 무디게 하고, 남은 것의
// 줄머리 마크다운 기호를 떼어 낸다.
func Neutralize(text string) string {
	return strings.TrimLeft(markup.Replace(text), LeadMarks)
}

// Clip 은 바이트가 아니라 룬으로 자르고 잘렸다는 표시를 남긴다.
func Clip(text string, maxRunes int) string {
	letters := []rune(text)
	if len(letters) <= maxRunes {
		return text
	}
	if maxRunes <= 1 {
		return "…"
	}
	return string(letters[:maxRunes-1]) + "…"
}

// Summary 는 못 믿을 한 토막을 한 줄로 납작하게 만들고 room 룬으로 자른다.
func Summary(text string, room int) string {
	return Clip(Neutralize(OneLine(text)), room)
}

// Text 는 여러 줄 글을 줄 수를 지킨 채로 중화한다. `mem show` · `mem search`
// 처럼 원문 모양이 뜻을 갖는 자리에서 쓴다 — 한 줄로 접지 않고 줄마다
// Neutralize 만 지난다.
func Text(text string) string {
	lines := strings.Split(text, "\n")
	for at, line := range lines {
		lines[at] = Neutralize(dropInvisible(strings.TrimRight(line, "\r")))
	}
	return strings.Join(lines, "\n")
}

// dropInvisible 은 폭 없는 글자만 뺀다. 보이는 글자와 띄어쓰기는 안 건드린다.
func dropInvisible(text string) string {
	if !strings.ContainsFunc(text, Invisible) {
		return text
	}
	return strings.Map(func(letter rune) rune {
		if Invisible(letter) {
			return -1
		}
		return letter
	}, text)
}
