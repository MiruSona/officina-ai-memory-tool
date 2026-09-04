package i18n

// 3회차 물결 3 갈래 3B 가 늘린 말들이다 — `review --promote` · `tags --suggest`
// · `migrate` 부분 이전. 다른 갈래와 같은 파일을 안 건드리려고 표를 따로 둔다.

const (
	// review --promote (결정 6·60)
	ReviewPromoteNeedID  Key = "review-promote-need-id"
	ReviewPromoteNotHeld Key = "review-promote-not-held"
	ReviewPromoted       Key = "review-promoted"

	// tags --suggest (결정 40)
	TagsSuggestHead  Key = "tags-suggest-head"
	TagsSuggestRow   Key = "tags-suggest-row"
	TagsSuggestNone  Key = "tags-suggest-none"
	TagsSuggestOnly  Key = "tags-suggest-only"
	TagsSuggestSteps Key = "tags-suggest-steps"

	// migrate 부분 이전 (설계 6절 · 조사 E #6)
	MigratePartHead   Key = "migrate-part-head"
	MigratePartRow    Key = "migrate-part-row"
	MigrateSecretStop Key = "migrate-secret-stop"
)

var wave3bMessages = map[Key]string{
	ReviewPromoteNeedID: "`--promote` 는 기억 id 가 있어야 한다 : mem review --promote <id>",
	ReviewPromoteNotHeld: "그 기억은 승격을 기다리는 중이 아니다 : %s\n" +
		"  머리말에 `review: true` 가 있는 기억만 승격한다. `mem show <id>` 로 먼저 본다.",
	ReviewPromoted: "승격했다 : %s — 이제 검색·훅에 뜬다 (`mem index` 가 반영한다).",

	TagsSuggestHead:  "동의어 후보 %d가지 — **제안만 한다. 아무것도 안 고쳤다.**",
	TagsSuggestRow:   "  %s → %s   (까닭 %s)",
	TagsSuggestNone:  "동의어 후보가 없다.",
	TagsSuggestOnly:  "표에 넣는 것은 사람이 한다. 잘못 넣은 동의어는 오탐이 폭발한다 (결정 40).",
	TagsSuggestSteps: "넣으려면 :",

	MigratePartHead: "칸별로만 옮길 것 %d건 (막힌 칸은 사람 몫으로 남는다) :",
	MigratePartRow:  "  %s  남은 칸 %s — %s",
	MigrateSecretStop: "이전을 멈췄다. 기억 %s 의 `%s` 칸에 비밀정보로 보이는 것이 있다 (규칙 %s).\n" +
		"  값은 안 찍는다. 그 줄을 지우고 다시 돌려라.",
}

func init() {
	for key, text := range wave3bMessages {
		messages[key] = text
	}
}
