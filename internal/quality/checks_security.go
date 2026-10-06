package quality

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/secret"
)

// privatePath 는 남의 집 폴더를 가리키는 실제 경로다 (규칙 E03). 낱말 앞이어야
// 해서 주소 안의 /users/ 는 안 걸린다. 환경변수 이름(%USERPROFILE%)은 통과한다.
var privatePath = regexp.MustCompile(
	`(?m)(?:^|[\s(\[|>"'` + "`" + `])((?i:[a-z]:\\users\\[^\\\s|]+)|/home/[^/\s|]+|/Users/[^/\s|]+)`)

// injectionShapes 는 기억 본문이 지시인 척하는 꼴이다 (규칙 E05). 저장은 하되
// 훅이 주입할 때 무력화한다 — 막는 것이 아니라 중화가 맞다.
var injectionShapes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(이전|위의|앞의)\s*(지시|명령|규칙)(을|를)?\s*(무시|잊)`),
	regexp.MustCompile(`(?i)ignore\s+(all\s+)?(previous|above|prior)\s+instructions`),
	regexp.MustCompile(`(?im)^\s*(system|assistant|human)\s*:`),
	regexp.MustCompile(`(?i)</?(context|system|instructions)>`),
	regexp.MustCompile(`(?i)여기(까지)?가?\s*자료(다|이다)`),
}

// injectionNeeds 는 위 꼴마다 **반드시 들어 있어야 하는 글자**다 (차례가 같다).
// 정규식 되돌이가 20k lint CPU 의 5.7% 였다. 이 글자가 하나도 없으면 정규식도
// 절대 못 맞추니 아예 안 돌린다 — 답은 한 자리도 안 바뀐다.
var injectionNeeds = [][]string{
	{"무시", "잊"},
	{"ignore"},
	{"system", "assistant", "human"},
	{"context", "system", "instructions"},
	{"자료"},
}

// privatePathNeeds 는 E03 정규식이 맞으려면 있어야 하는 글자다.
var privatePathNeeds = []string{`\users\`, "/home/", "/users/"}

// anyContains 는 낱말 중 하나라도 글 안에 있는지다. text 는 소문자로 낮춘 것이다.
func anyContains(text string, words []string) bool {
	for _, word := range words {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

// checkSecurity 는 E 계열이다. 값은 절대 안 찍고 규칙 이름과 줄 번호만 말한다 —
// 오류 문구가 비밀을 두 번째로 흘리면 안 된다 (불변조건 I4).
func checkSecurity(m *model.Memory, opt Options) []Finding {
	// 훑는 칸 목록은 secret.MemoryText 한 곳에만 둔다. 관문·승격·색인이 다른
	// 칸을 보면 한쪽이 통과시킨 것을 다른 쪽이 버린다 (리뷰 A → C 넘김).
	text := secret.MemoryText(m.Title, m.Summary, m.Sources, m.Body)
	found := []Finding{}
	if hit := opt.Secret.ScanText(text); hit != nil {
		found = append(found, Finding{Rule: RuleSecretPattern, Level: GradeReject, ID: m.ID,
			Reason: fmt.Sprintf("%d번째 줄에 비밀정보로 보이는 것이 있다 (%s). 값은 안 찍는다", hit.Line, hit.Rule),
			Line:   hit.Line,
			Next:   []string{"그 줄을 지우고 다시 넣는다. 억지로 넣는 옵션은 없다"}})
		return found
	}
	if hit := opt.SecretWarn.ScanText(text); hit != nil {
		found = append(found, Finding{Rule: RuleSecretEntropy, Level: GradeWarn, ID: m.ID,
			Reason: fmt.Sprintf("%d번째 줄에 개인정보로 보이는 것이 있다 (%s)", hit.Line, hit.Rule), Line: hit.Line})
	} else if line, token := highEntropy(secret.MemoryText(m.Title, m.Summary, entropySources(m.Sources), m.Body)); line > 0 {
		// 엔트로피만 출처 url 경로를 뺀 글로 잰다. 거절·개인정보 검사는 원래 글 그대로다.
		found = append(found, Finding{Rule: RuleSecretEntropy, Level: GradeWarn, ID: m.ID,
			Reason: fmt.Sprintf("%d번째 줄에 뜻 없는 %d자 토막이 있다. 열쇠가 아닌지 본다", line, token), Line: line})
	}
	// 정규식 앞에 **싼 글자 검사**를 둔다. 한 번만 낮춰 두고 돌려 쓴다.
	lowered := strings.ToLower(text)
	if path := privatePath.FindStringSubmatch(text); anyContains(lowered, privatePathNeeds) && path != nil {
		found = append(found, Finding{Rule: RulePrivatePath, Level: GradeWarn, ID: m.ID,
			Reason: "남의 기계에서는 없는 개인 폴더 경로가 있다. 환경변수 이름으로 적는다"})
	}
	for at, shape := range injectionShapes {
		if !anyContains(lowered, injectionNeeds[at]) {
			continue
		}
		if shape.MatchString(text) {
			found = append(found, Finding{Rule: RuleInjectionNeutralize, Level: GradeWarn, ID: m.ID,
				Reason: "지시처럼 읽히는 글이 있다. 저장은 하되 훅이 주입할 때 무력화한다"})
			break
		}
	}
	return found
}

// entropyFloor 는 「뜻 없는 토막」의 경계다 (규칙 E02). 4.0 아래는 사람이 쓴
// 이름일 확률이 높다.
const (
	entropyFloor  = 4.0
	entropyMinLen = 20
)

// highEntropy 는 엔트로피가 높은 긴 토막이 있는 줄과 그 길이다.
func highEntropy(text string) (int, int) {
	for number, line := range strings.Split(text, "\n") {
		for _, token := range strings.FieldsFunc(line, func(letter rune) bool {
			return !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+/=_-", letter)
		}) {
			if len([]rune(token)) < entropyMinLen || !mixedToken(token) {
				continue
			}
			if shannon(token) >= entropyFloor {
				return number + 1, len(token)
			}
		}
	}
	return 0, 0
}

// entropySources 는 엔트로피 검사용 출처 목록이다. `url:` 출처의 **경로만** 뺀다 —
// 문서 주소의 경로 토막(…/AbC12xYz…)은 열쇠가 아닌데 경고가 났다. 쿼리(`?`)와
// 조각(`#`) 꼬리는 남긴다(`?token=` 처럼 진짜 열쇠가 거기 실린다). `user:pass@`
// 같은 userinfo 도 호스트 쪽이라 그대로 남는다. 줄 수는 그대로라 줄 번호가 안 어긋난다.
func entropySources(sources []string) []string {
	out := make([]string, len(sources))
	for at, source := range sources {
		out[at] = source
		if strings.HasPrefix(source, "url:") {
			out[at] = "url:" + withoutURLPath(strings.TrimPrefix(source, "url:"))
		}
	}
	return out
}

// withoutURLPath 는 `scheme://` 뒤 첫 `/` 부터 `?`·`#` 전까지(경로)를 뺀 주소다.
// `scheme://` 가 없으면 어디가 경로인지 몰라 아무것도 안 뺀다 — 덜 빼는 쪽이 안전하다.
func withoutURLPath(value string) string {
	scheme := strings.Index(value, "://")
	if scheme < 0 {
		return value
	}
	host := scheme + len("://")
	pathAt := strings.IndexAny(value[host:], "/?#")
	if pathAt < 0 || value[host+pathAt] != '/' {
		return value
	}
	pathAt += host
	tail := len(value)
	if end := strings.IndexAny(value[pathAt:], "?#"); end >= 0 {
		tail = pathAt + end
	}
	return value[:pathAt] + value[tail:]
}

// mixedToken 은 대문자·소문자·숫자가 다 섞였는지다. 하나라도 빠지면 사람이 지은
// 이름일 확률이 높아 안 센다 — 40자 CamelCase 함수 이름을 살리는 가드다.
func mixedToken(token string) bool {
	upper, lower, digit := false, false, false
	for _, letter := range token {
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

func shannon(token string) float64 {
	counts := map[rune]int{}
	for _, letter := range token {
		counts[letter]++
	}
	total := float64(len([]rune(token)))
	sum := 0.0
	for _, count := range counts {
		share := float64(count) / total
		sum -= share * math.Log2(share)
	}
	return sum
}

// SecretLine 은 본문 한 덩어리에서 비밀정보가 걸린 줄을 돌려준다. set --body ·
// --jsonl 처럼 관문 전체를 안 도는 자리가 이것만 부른다 (불변조건 I4 의 다섯 자리).
func SecretLine(text string, opt Options) *Finding {
	opt = opt.normalized()
	hit := opt.Secret.ScanText(text)
	if hit == nil {
		return nil
	}
	return &Finding{Rule: RuleSecretPattern, Level: GradeReject, Line: hit.Line,
		Reason: fmt.Sprintf("%d번째 줄에 비밀정보로 보이는 것이 있다 (%s)", hit.Line, hit.Rule)}
}
