package retain

import (
	"regexp"
	"strings"
)

// 조각 갈래 (R2). 지어내기 쉬운 「확인 가능한 값」만 뽑는다 — 영어 낱말까지
// 다 대조하면 대화를 요약한 말투가 거절된다.
const (
	KindID     = "id"
	KindDate   = "date"
	KindURL    = "url"
	KindPath   = "path"
	KindNumber = "number"
)

// Fragment 는 기억 글에서 뽑은 조각 하나다.
type Fragment struct {
	Kind string
	Text string
}

// 뽑는 차례가 곧 이긴 차례다. 앞에서 뽑은 자리는 지우고 뒤 꼴을 찾는다 —
// 날짜 안 숫자를 따로 또 세지 않게.
var fragmentPatterns = []struct {
	kind    string
	pattern *regexp.Regexp
}{
	{KindURL, regexp.MustCompile(`https?://[^\s)\]>"'` + "`" + `]+`)},
	{KindID, regexp.MustCompile(`\b\d{8}-[0-9a-f]{8}\b`)},
	{KindDate, regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)},
	// 슬래시가 든 경로 또는 `이름.확장자`, 뒤에 `:줄` 이 붙을 수 있다.
	{KindPath, regexp.MustCompile(`[A-Za-z0-9_.\-]*[/\\][A-Za-z0-9_.\-/\\]*[A-Za-z0-9_](:\d+(-\d+)?)?`)},
	{KindPath, regexp.MustCompile(`\b[A-Za-z0-9_\-]+(\.[A-Za-z0-9_\-]+)*\.(go|md|ps1|cs|json|toml|yaml|yml|txt|py|js|ts|html|css|sh|jsonl|db|exe|asmdef|prefab|unity|asset|csv)(:\d+(-\d+)?)?\b`)},
	// 두 자리 이상인 수. 한 자리 수는 어디에나 있어 대조할 값어치가 없다.
	{KindNumber, regexp.MustCompile(`\d+(\.\d+)?`)},
}

// Fragments 는 글에서 대조할 조각을 뽑는다. 같은 조각은 한 번만 낸다.
func Fragments(text string) []Fragment {
	out := []Fragment{}
	seen := map[string]bool{}
	for _, item := range fragmentPatterns {
		taken := strings.Builder{}
		last := 0
		for _, at := range item.pattern.FindAllStringIndex(text, -1) {
			found := text[at[0]:at[1]]
			if item.kind == KindPath && !pathLike(found) {
				// 경로가 아니면 그 자리를 안 지운다 — 안의 수는 뒤 꼴이 센다.
				continue
			}
			taken.WriteString(text[last:at[0]] + " ")
			last = at[1]
			if item.kind == KindNumber && digits(found) < 2 {
				continue
			}
			key := item.kind + "\x00" + found
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Fragment{Kind: item.kind, Text: found})
		}
		taken.WriteString(text[last:])
		text = taken.String()
	}
	return out
}

// extensionPattern 은 마지막 토막 끝의 `.확장자` 다 (`:줄` 은 떼고 본다).
var extensionPattern = regexp.MustCompile(`\.[A-Za-z0-9]+$`)

// pathLineSuffix 는 경로 뒤 `:줄` · `:줄-줄` 이다.
var pathLineSuffix = regexp.MustCompile(`:\d+(-\d+)?$`)

// pathLike 는 경로 꼴 조각을 정말 경로로 칠지다. 확장자가 있거나 빗금이 둘 이상일
// 때만이다 — `and/or` · `R1/R2` · `Stop/PreCompact` 같은 낱말 사이 빗금은 경로가 아니다
// (리뷰 2026-10-05).
func pathLike(found string) bool {
	if !strings.ContainsAny(found, `/\.`) {
		return false
	}
	if !strings.ContainsAny(found, `/\`) {
		return true
	}
	bare := pathLineSuffix.ReplaceAllString(found, "")
	if strings.Count(bare, "/")+strings.Count(bare, `\`) >= 2 {
		return true
	}
	segments := strings.FieldsFunc(bare, func(r rune) bool { return r == '/' || r == '\\' })
	return len(segments) > 0 && extensionPattern.MatchString(segments[len(segments)-1])
}

func digits(text string) int {
	count := 0
	for _, letter := range text {
		if letter >= '0' && letter <= '9' {
			count++
		}
	}
	return count
}
