package i18n

// `add --hold` 와 `search --include-held` 가 쓰는 말이다 (결정 6). 승격 쪽
// 말은 wave3b.go 에 있다.

const AddHeldNote Key = "add-held-note"

var holdMessages = map[Key]string{
	AddHeldNote: "보류로 넣었다 : %s — 검색·훅에 안 뜬다. 사람이 `mem review --promote <id>` 로 승격한다.",
}

func init() {
	for key, text := range holdMessages {
		messages[key] = text
	}
}
