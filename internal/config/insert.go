package config

import (
	"sort"
	"strings"
)

// mem init 이 이미 있는 mem.toml · vocab.toml 에 빠진 것을 채울 때 쓰는 줄 끼우기다.
// 파일을 다시 쓰면 사람이 단 주석이 사라진다 (mem issue 20261005-ebe02fad) —
// 그래서 **기존 줄은 한 글자도(줄 끝 CRLF/LF 포함) 안 바꾸고** 새 줄만 끼운다.

// tomlLine 은 줄 하나를 읽은 결과다. key 가 비면 키 줄이 아니다.
type tomlLine struct {
	section string
	header  bool
	key     string
	// end 는 이 키 값이 끝나는 줄 번호다. 여러 줄 배열·문자열이면 닫는 줄이다.
	end int
}

// scanTOMLLines 는 줄마다 어느 절의 무슨 키인지 읽는다. 여러 줄 배열과
// 따옴표 셋으로 여는 여러 줄 문자열 안쪽은 키 줄로 안 본다.
func scanTOMLLines(lines []string) []tomlLine {
	out := make([]tomlLine, len(lines))
	section := ""
	depth, closer := 0, ""
	owner := -1
	for at, raw := range lines {
		line := strings.TrimSpace(raw)
		out[at] = tomlLine{section: section, end: at}
		if closer != "" {
			out[owner].end = at
			if strings.Contains(line, closer) {
				closer = ""
			}
			continue
		}
		if depth > 0 {
			out[owner].end = at
			depth += bracketDepth(line)
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 머리 판별은 파서와 같은 함수다. 따로 두면 `[search] # 주석` 을 서로 달리
		// 읽어, 있는 절을 빠졌다고 보고 기본값 절을 하나 더 붙인다 (코드리뷰 10-05).
		if name, ok := SectionHeader(line); ok {
			section = name
			out[at] = tomlLine{section: section, header: true, end: at}
			continue
		}
		if headerShaped(strings.TrimSpace(stripComment(line))) {
			continue // 파서가 경고하고 건너뛰는 망가진 머리다.
		}
		name, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		out[at].key = keyName(name)
		owner = at
		value = strings.TrimSpace(stripComment(value))
		closer = openMultiString(value)
		if closer == "" && strings.HasPrefix(value, "[") {
			depth = bracketDepth(value)
		}
	}
	return out
}

// keyName 은 따옴표를 벗긴 키 이름이다. `색인 = …` 과 `"색인" = …` 은 같은 키다.
func keyName(raw string) string {
	name := strings.TrimSpace(raw)
	if unquoted, ok := unquote(name); ok {
		return unquoted
	}
	return name
}

// openMultiString 은 값이 여러 줄 문자열을 열고 안 닫았으면 닫는 표시를 준다.
func openMultiString(value string) string {
	for _, mark := range []string{`"""`, `'''`} {
		if strings.HasPrefix(value, mark) && strings.Count(value, mark) == 1 {
			return mark
		}
	}
	return ""
}

func bracketDepth(text string) int {
	depth := 0
	for _, letter := range scanOutsideQuotes(stripComment(text)) {
		if letter == '[' {
			depth++
		}
		if letter == ']' {
			depth--
		}
	}
	return depth
}

// keyLine 은 Encode 한 글에서 찾은 키 줄 하나와 그 절이다. at 은 그 글 안
// 줄 번호라, 끼우는 차례를 Encode 차례에 맞출 때 쓴다. comment 는 키 바로 위에
// 붙은 설명 주석 줄들이다 — 키를 끼울 때 같이 따라간다.
type keyLine struct {
	section string
	comment []string
	text    string
	at      int
}

// keyLines 는 「절.키」 → 그 키를 적은 줄이다. 키에 점이 들 수 있어
// (`"node.js"`) 절은 이름을 잘라 얻지 않고 여기 같이 적어 둔다.
func keyLines(text string) map[string]keyLine {
	lines := strings.Split(text, "\n")
	table := map[string]keyLine{}
	for at, one := range scanTOMLLines(lines) {
		if one.key == "" {
			continue
		}
		table[one.section+"."+one.key] = keyLine{section: one.section, comment: commentAbove(lines, at),
			text: strings.Join(lines[at:one.end+1], "\n"), at: at}
	}
	return table
}

// commentAbove 는 at 줄 바로 위에 빈 줄 없이 붙은 # 줄들이다.
func commentAbove(lines []string, at int) []string {
	from := at
	for from > 0 && strings.HasPrefix(strings.TrimSpace(lines[from-1]), "#") {
		from--
	}
	return lines[from:at]
}

// withComment 는 끼울 줄에 설명 주석을 앞세운다. 그 주석이 이미 파일에 다 있으면
// (키만 지운 경우) 두 벌이 되지 않게 빼고 키 줄만 준다.
func withComment(wanted keyLine, have map[string]bool) string {
	if len(wanted.comment) == 0 {
		return wanted.text
	}
	for _, line := range wanted.comment {
		if !have[strings.TrimSpace(line)] {
			return strings.Join(wanted.comment, "\n") + "\n" + wanted.text
		}
	}
	return wanted.text
}

// trimmedSet 은 글의 줄을 앞뒤 공백을 걷어 모은 것이다.
func trimmedSet(lines []string) map[string]bool {
	have := map[string]bool{}
	for _, line := range lines {
		have[strings.TrimSpace(line)] = true
	}
	return have
}

// FillMissing 은 mem.toml 글에 빠진 키를 끼운 글을 준다. 끼울 줄은 이 파일을
// Parse 한 값에서 가져온다 — 옛 이름 `[repo] pin_max` 를 `[pin] max` 로 옮겨 읽는
// 규칙까지 Parse 와 같아진다. 끼운 뒤 원래 값이 하나라도 바뀌면 거절한다.
func FillMissing(text string) (string, error) {
	loaded, err := Parse(text)
	if err != nil {
		return "", err
	}
	filled := InsertMissing(text, MissingKeys(text), loaded)
	if err := CheckConfigKept(text, filled); err != nil {
		return "", err
	}
	return filled, nil
}

// InsertMissing 은 빠진 「절.키」 마다 from 을 Encode 한 줄을 그 절의 마지막 키 줄
// 바로 뒤에 끼운다. 절이 아예 없으면 파일 끝에 절째로 붙인다. from 에 없는 키는
// 건너뛴다.
func InsertMissing(text string, missing []string, from Config) string {
	defaults := keyLines(string(Encode(from)))
	lines := strings.Split(text, "\n")
	lastOf, headerOf := sectionAnchors(scanTOMLLines(lines))
	have := trimmedSet(lines)
	after := map[int][]string{}
	newSections := []string{}
	newKeys := map[string][]string{}
	// MissingKeys 는 이름 차례다. 사람이 읽기 좋게 기본 파일에 적힌 차례로 끼운다.
	ordered := append([]string{}, missing...)
	sort.SliceStable(ordered, func(left, right int) bool {
		return defaults[ordered[left]].at < defaults[ordered[right]].at
	})
	for _, name := range ordered {
		wanted, known := defaults[name]
		if !known {
			continue
		}
		section, line := wanted.section, withComment(wanted, have)
		at, anchored := lastOf[section]
		if !anchored {
			at, anchored = headerOf[section]
		}
		if anchored {
			after[at] = append(after[at], line)
			continue
		}
		if section == "" {
			after[-1] = append(after[-1], line)
			continue
		}
		if _, seen := newKeys[section]; !seen {
			newSections = append(newSections, section)
		}
		newKeys[section] = append(newKeys[section], line)
	}
	return joinInserted(lines, after, sectionLines(newSections, newKeys), mostlyCRLF(text))
}

// AppendSections 는 빠진 절을 파일 끝에 붙인다. vocab.toml 처럼 절 단위로만
// 빠짐을 보는 파일이 쓴다. bodies 는 절 이름 → 그 절에 넣을 줄이다.
func AppendSections(text string, sections []string, bodies map[string][]string) string {
	return joinInserted(strings.Split(text, "\n"), map[int][]string{}, sectionLines(sections, bodies),
		mostlyCRLF(text))
}

// sectionLines 는 끝에 붙일 절들이다. 절 앞마다 빈 줄 하나를 둔다.
func sectionLines(sections []string, bodies map[string][]string) []string {
	tail := []string{}
	for _, section := range sections {
		tail = append(tail, "", "["+section+"]")
		tail = append(tail, bodies[section]...)
	}
	return tail
}

// sectionAnchors 는 절마다 마지막 키 값이 끝나는 줄과 마지막 머리 줄이다.
// 머리 줄 없이 맨 위에 적힌 키는 절 "" 이다.
func sectionAnchors(scanned []tomlLine) (map[string]int, map[string]int) {
	lastOf := map[string]int{}
	headerOf := map[string]int{}
	for at, one := range scanned {
		if one.header {
			headerOf[one.section] = at
			continue
		}
		if one.key != "" {
			lastOf[one.section] = one.end
		}
	}
	return lastOf, headerOf
}

// mostlyCRLF 는 줄 끝의 절반 넘게 CRLF 인지다.
func mostlyCRLF(text string) bool {
	return strings.Count(text, "\r\n")*2 > strings.Count(text, "\n")
}

// joinInserted 는 원래 줄 사이에 새 줄을 끼워 다시 잇는다. 원래 줄은 Split 한
// 그대로라 \r 까지 남는다. 새 줄에는 파일 줄 끝(CRLF 면 \r)을 붙인다.
func joinInserted(lines []string, after map[int][]string, tail []string, crlf bool) string {
	end := ""
	if crlf {
		end = "\r"
	}
	last := len(lines) - 1
	closed := last > 0 && lines[last] == ""
	if !closed && (len(tail) > 0 || len(after[last]) > 0) {
		// 마지막 줄에 줄바꿈이 없었다. 뒤에 줄을 붙이려면 그 줄을 닫아야 한다.
		lines = append(append([]string{}, lines...), "")
		lines[last] += end
		last++
	}
	out := make([]string, 0, len(lines)+len(tail)+len(after)*2)
	for _, extra := range after[-1] {
		out = append(out, withEnd(extra, end))
	}
	for at, line := range lines {
		if at == last && len(tail) > 0 {
			for _, extra := range tail {
				out = append(out, withEnd(extra, end))
			}
		}
		out = append(out, line)
		for _, extra := range after[at] {
			out = append(out, withEnd(extra, end))
		}
	}
	return strings.Join(out, "\n")
}

// withEnd 는 새 줄(여러 줄일 수 있다)의 줄마다 줄 끝 표시를 붙인다.
func withEnd(text, end string) string {
	if end == "" {
		return text
	}
	return strings.ReplaceAll(text, "\n", end+"\n") + end
}
