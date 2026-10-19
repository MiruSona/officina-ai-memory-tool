package i18n

// `set --sources` 와 add 거절 화면 다듬기가 쓰는 말이다
// (사용 피드백 2026-10-10 · 2026-10-11).

const (
	SetSourcesEmpty  Key = "set-sources-empty"
	AddAutoWarnRules Key = "add-auto-warn-rules"
)

var setAddFeedbackMessages = map[Key]string{
	SetSourcesEmpty:  "근거를 비울 수는 없다 — --sources file:<경로>,commit:<hex> 처럼 하나 이상 준다.",
	AddAutoWarnRules: "경고 : %s (막지는 않은 규칙이다)",
}

func init() {
	for key, text := range setAddFeedbackMessages {
		messages[key] = text
	}
}
