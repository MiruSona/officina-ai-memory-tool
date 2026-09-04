package token

import "strings"

// particles 는 질의에서 떼 볼 조사다. 20개를 넘기지 않는다 — 많이 떼면
// `가로`·`정보` 처럼 조사가 아닌 것을 잘못 뗀다 (설계 6-1c).
var particles = []string{"으로", "에서", "에게", "한테", "보다", "까지", "부터",
	"을", "를", "이", "가", "은", "는", "의", "로", "와", "과", "도", "만", "에"}

// StripParticle 은 조사를 뗀 꼴을 준다. 뗀 결과가 한 글자면 안 뗀다 —
// `도구` 가 `도` 가 되는 사고를 막는다 (설계 6-1c).
func StripParticle(word string) (string, bool) {
	if !HasHangul(word) || hangulRun.FindString(word) != word {
		return "", false
	}
	for _, tail := range particles {
		if !strings.HasSuffix(word, tail) {
			continue
		}
		stem := strings.TrimSuffix(word, tail)
		if len([]rune(stem)) < 2 {
			continue
		}
		return stem, true
	}
	return "", false
}
