// Package secret checks text against the patterns in mem.toml before anything
// is stored. store/ is committed to git, so one leak lives in history forever.
package secret

import (
	"regexp"
	"strings"
)

// nameCut keeps an unnamed rule short enough to print.
const nameCut = 40

// names label the built in patterns of mem.toml. A pattern somebody added by
// hand is reported by its own text.
var names = map[string]string{
	`sk-ant-[A-Za-z0-9_-]{20,}`:                                     "anthropic-key",
	`sk-(proj|live|test)-[A-Za-z0-9_-]{20,}`:                        "openai-key",
	`sk-[A-Za-z0-9]{20,}`:                                           "api-key",
	`ghp_[A-Za-z0-9]{20,}`:                                          "github-token",
	`github_pat_[A-Za-z0-9_]{20,}`:                                  "github-fine-grained",
	`glpat-[A-Za-z0-9_-]{20,}`:                                      "gitlab-token",
	`xox[baprs]-[A-Za-z0-9-]{10,}`:                                  "slack-token",
	`AKIA[0-9A-Z]{16}`:                                              "aws-key",
	`-----BEGIN [A-Z ]*PRIVATE KEY-----`:                            "private-key",
	`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`: "jwt",
	`\b\d{6}-[1-4]\d{6}\b`:                                          "krn-id",
	`\b(?:4\d{3}|5[1-5]\d{2}|3[47]\d{2}|6011)[ -]?\d{4}[ -]?\d{4}[ -]?\d{2,4}\b`: "card-number",
	`(?i)\b(password|passwd|pw|token|secret|api[_-]?key)\b\s*[:=]\s*\S{4,}`:      "password-value",
	`(?i)\b(password|passwd|pw|token|secret|api[_-]?key)\b\s*[:=]\s*\S+`:         "password-word",
	`[A-Za-z0-9+/]{40,}={1,2}`:                       "long-blob",
	`[A-Za-z0-9+]{40,}={0,2}`:                        "long-blob",
	`[A-Za-z0-9+/]{32,}={0,2}`:                       "long-blob",
	`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`: "email",
	`\b01[016-9]-?\d{3,4}-?\d{4}\b`:                  "phone-kr",
}

// Finding says where a rule matched. The matched value is never carried, so an
// error message cannot leak the secret a second time.
type Finding struct {
	Line int
	Rule string
}

// rule is one compiled pattern with the name we report it under.
type rule struct {
	name    string
	pattern *regexp.Regexp
	// filter 는 정규식을 돌리기 전의 값싼 물음이다. nil 이면 늘 돌린다.
	filter func(string) bool
}

// Scanner holds the compiled patterns of one repository. skipped keeps the
// patterns that would not compile, so lint can say a rule is silently off.
type Scanner struct {
	rules   []rule
	skipped []string
}

// Skipped lists the patterns that did not compile, in mem.toml order.
func (s *Scanner) Skipped() []string {
	return s.skipped
}

// New compiles the patterns; a broken pattern is skipped so a typo in mem.toml
// cannot stop every add.
func New(patterns []string) *Scanner {
	scanner := Scanner{}
	for _, pattern := range patterns {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			scanner.skipped = append(scanner.skipped, pattern)
			continue
		}
		name := nameFor(pattern)
		scanner.rules = append(scanner.rules, rule{name: name, pattern: compiled,
			filter: filterFor(name, compiled)})
	}
	return &scanner
}

// ScanText reports the first line that trips a rule, counting from one.
func (s *Scanner) ScanText(text string) *Finding {
	for number, line := range strings.Split(text, "\n") {
		if found := s.ScanLine(line); found != nil {
			found.Line = number + 1
			return found
		}
	}
	return nil
}

// MemoryText 는 기억 하나에서 훑을 글을 한 벌로 모은다. add 관문 · 승격 ·
// 색인 · lint 가 **같은 칸**을 보게 하는 한 곳이다 — 자리마다 보는 칸이 다르면
// 한쪽이 통과시킨 것을 다른 쪽이 버린다 (보안시험 H-1).
func MemoryText(title, summary string, sources []string, body string) string {
	return strings.Join([]string{title, summary, strings.Join(sources, "\n"), body}, "\n")
}

// ScanLine reports a rule tripped by a single line; Line stays zero.
func (s *Scanner) ScanLine(line string) *Finding {
	for _, item := range s.rules {
		if item.filter != nil && !item.filter(line) {
			continue
		}
		if tripped(item, line) {
			return &Finding{Rule: item.name}
		}
	}
	return nil
}

// tripped asks the pattern first and the rule's guard second. Every match on
// the line is offered, so a name that fools the pattern cannot hide a real key
// sitting later on the same line.
func tripped(item rule, line string) bool {
	guard := guards[item.name]
	if guard == nil {
		return item.pattern.MatchString(line)
	}
	for _, hit := range item.pattern.FindAllString(line, -1) {
		if guard(hit) {
			return true
		}
	}
	return false
}

// guards narrow a rule the pattern alone cannot. A regular expression here has
// no lookahead, so "letters and digits mixed" has to be counted in code.
var guards = map[string]func(string) bool{"long-blob": blobLike, "password-value": secretLike}

// The value after "token =" has to be at least this long to be a secret at all,
// and letters-only values have to be this long before we call them one.
const (
	minSecretValue = 6
	longWordValue  = 20
	mixedWordValue = 12
)

// secretLike says the right hand side of "token = …" really looks like a key.
// Without this every "token = 1200" and "secret: patterns" was refused, and a
// plain note could not be stored (review #6, field test).
func secretLike(hit string) bool {
	value := valueOf(hit)
	if len([]rune(value)) < minSecretValue || !asciiText(value) {
		return false
	}
	upper, lower, digit, other := shapeOf(value)
	if other {
		return true
	}
	if digit && (upper || lower) {
		return true
	}
	if digit {
		return false
	}
	return len(value) >= longWordValue || (upper && lower && len(value) >= mixedWordValue)
}

// valueOf takes what stands after the first : or = of the match.
func valueOf(text string) string {
	cut := strings.IndexAny(text, ":=")
	if cut < 0 {
		return ""
	}
	return strings.TrimSpace(text[cut+1:])
}

// asciiText is false as soon as the value carries Hangul or any other letter
// outside ASCII: a secret is typed on an English keyboard.
func asciiText(text string) bool {
	for _, letter := range text {
		if letter > 127 {
			return false
		}
	}
	return true
}

// shapeOf reports which kinds of character the value is made of.
func shapeOf(text string) (upper, lower, digit, other bool) {
	for _, letter := range text {
		switch {
		case letter >= 'A' && letter <= 'Z':
			upper = true
		case letter >= 'a' && letter <= 'z':
			lower = true
		case letter >= '0' && letter <= '9':
			digit = true
		default:
			other = true
		}
	}
	return upper, lower, digit, other
}

// blobLike says a long run of base64 letters really looks like encoded bytes:
// upper case, lower case and digits all show up. A forty letter CamelCase name
// such as BattleFlowControllerFactoryProviderRegistry does not (field test B1).
func blobLike(text string) bool {
	upper, lower, digit := false, false, false
	for _, letter := range text {
		switch {
		case letter >= 'A' && letter <= 'Z':
			upper = true
		case letter >= 'a' && letter <= 'z':
			lower = true
		case letter >= '0' && letter <= '9':
			digit = true
		}
	}
	return upper && lower && digit
}

func nameFor(pattern string) string {
	if name, ok := names[pattern]; ok {
		return name
	}
	if len(pattern) > nameCut {
		return pattern[:nameCut] + "…"
	}
	return pattern
}

// prefilters 는 규칙마다의 값싼 앞거르기다. 여기서 아니라고 하면 정규식을 아예
// 안 돌린다. 한글 줄에는 열쇠가 없는데도 규칙 스물이 줄마다 돌던 자리다.
// 틀리려면 "될 수도 있다" 쪽으로만 틀려야 한다.
var prefilters = map[string]func(string) bool{
	"email":          func(line string) bool { return strings.IndexByte(line, '@') >= 0 },
	"krn-id":         hasDigit,
	"card-number":    hasDigit,
	"phone-kr":       hasDigit,
	"password-value": hasAssign,
	"password-word":  hasAssign,
	"long-blob":      hasBlobRun,
}

// blobRunMin 은 long-blob 규칙 셋 중 가장 짧은 것이 요구하는 길이다.
const blobRunMin = 32

// filterFor 는 규칙 하나의 앞거르기를 고른다. 이름을 아는 규칙은 손으로 적은
// 것을 쓰고, 모르는 규칙은 정규식이 반드시 시작하는 글월을 쓴다.
func filterFor(name string, compiled *regexp.Regexp) func(string) bool {
	if made, ok := prefilters[name]; ok {
		return made
	}
	prefix, _ := compiled.LiteralPrefix()
	if prefix == "" {
		return nil
	}
	return func(line string) bool { return strings.Contains(line, prefix) }
}

func hasDigit(line string) bool {
	for at := 0; at < len(line); at++ {
		if line[at] >= '0' && line[at] <= '9' {
			return true
		}
	}
	return false
}

func hasAssign(line string) bool {
	return strings.IndexByte(line, ':') >= 0 || strings.IndexByte(line, '=') >= 0
}

// hasBlobRun 은 base64 글자가 잇달아 32자 넘게 있는지다.
func hasBlobRun(line string) bool {
	run := 0
	for at := 0; at < len(line); at++ {
		letter := line[at]
		if letter >= 'A' && letter <= 'Z' || letter >= 'a' && letter <= 'z' ||
			letter >= '0' && letter <= '9' || letter == '+' || letter == '/' {
			run++
			if run >= blobRunMin {
				return true
			}
			continue
		}
		run = 0
	}
	return false
}
