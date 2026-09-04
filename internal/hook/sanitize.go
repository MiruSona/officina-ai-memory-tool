package hook

import (
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/safe"
)

// 주입되는 모든 줄이 safeLine 하나를 지난다. 요약은 남이 쓴 글이라 제목·꼬리표·
// 코드펜스를 위조할 수 있으면 안 된다 (설계 6-6 불변조건 I3).
// 실제 중화는 internal/safe 한 곳에만 있다 — search·show 와 같은 함수를 쓴다.
const (
	// lineMaxRunes 는 글머리표까지 넣은 줄 전체다. 보통 줄은 145룬쯤이라 잘리는
	// 것은 사실상 공격 줄뿐이다.
	lineMaxRunes = 160
	// bullet 은 살균 뒤에 붙인다. 요약이 자기 항목을 열지 못하게 하려는 것이다.
	bullet = "- "
)

// safeLine 은 못 믿을 글에서 한 줄을 만든다 — 납작하고 무해하고 짧게.
func safeLine(text string) string {
	return safe.Summary(text, lineMaxRunes-len([]rune(bullet)))
}

// safeHead 는 우리가 만든 `[id] ` 앞머리를 납작하게만 한다. 대괄호는
// 남긴다 — 마크다운 구조를 열면 안 되는 것은 요약뿐이다.
func safeHead(text string) string {
	return safe.OneLine(text)
}

// safeSummary 는 요약만 따로 살균한다. 앞머리를 먼저 붙이면 중화가 요약이
// 여는 마크다운 기호를 못 본다.
func safeSummary(text string, room int) string {
	return safe.Summary(text, room)
}

func clip(text string, maxRunes int) string { return safe.Clip(text, maxRunes) }

// safeID 는 망가진 id 가 줄에 아무것도 못 싣게 한다.
func safeID(id string) string {
	if model.IsID(id) {
		return id
	}
	return "?"
}
