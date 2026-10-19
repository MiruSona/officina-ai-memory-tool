package secret

import "strings"

// Redacted 는 가린 자리에 넣는 글이다 (A2 retain 설계 3절 가리기).
const Redacted = "[가림]"

// Redact 는 ScanLine 과 같은 규칙(설정 패턴 · builtin · guard)에 걸린 자리를 Redacted 로 바꾸고
// 바꾼 수를 센다. 줄 단위로 본다 — 막는 검사와 가리기가 같은 자리를 보게 한다.
func (s *Scanner) Redact(text string) (string, int) {
	lines := strings.Split(text, "\n")
	count := 0
	for at, line := range lines {
		for _, item := range s.rules {
			if item.filter != nil && !item.filter(line) {
				continue
			}
			line = redactRule(item, line, &count)
		}
		lines[at] = line
	}
	return strings.Join(lines, "\n"), count
}

// redactRule 은 규칙 하나가 맞은 자리 중 guard 도 넘는 것만 바꾼다. guard 를 못 넘은
// 자리(40자 git SHA · `token = 1200`)는 그대로 둔다.
func redactRule(item rule, line string, count *int) string {
	guard := guards[item.name]
	return item.pattern.ReplaceAllStringFunc(line, func(hit string) string {
		if guard != nil && !guard(hit) {
			return hit
		}
		*count++
		return Redacted
	})
}
