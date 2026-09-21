package i18n

// 한글이 한 글자도 없는 짜임새용 서식은 여기 두지 않는다 — 쓰는 쪽 파일에
// 상수로 둔다 (cmd_lint.go 의 lintTableRow 와 같은 규칙).
//
// v0.2 가 새로 만든 명령(review · tags · migrate)과 늘어난 status 화면이
// 쓰는 문장이다. 한글 리터럴은 이 패키지 밖에 두지 않는다 (설계 12-1).

// add·set 관문.
const (
	GateNextHead   Key = "gate-next-head"
	GateFixed      Key = "gate-fixed"
	GateCheckClean Key = "gate-check-clean"
	GateCheckOnly  Key = "gate-check-only"
	GateWarnStored Key = "gate-warn-stored"
	SetSuperseded  Key = "set-superseded"
	SetByNewNext   Key = "set-by-new-next"
	// 무효화 전파 — 덮인 기억을 근거로 삼은 기억을 알린다.
	SetUsedByFound   Key = "set-used-by-found"
	SetUsedByNext    Key = "set-used-by-next"
	SetUsedByUnknown Key = "set-used-by-unknown"
	SetUsedByRebuilt Key = "set-used-by-rebuilt"
	SetUsedByNoIndex Key = "set-used-by-no-index"
	SetUsedByAllDead Key = "set-used-by-all-dead"
	SetSelfSupersede Key = "set-self-supersede"
	// 목록을 자를 때 붙이는 꼬리다. review 의 「같이 볼 것」 줄과 같은 자를 쓴다.
	ListMore Key = "list-more"
)

// show 의 「나를 근거로 삼은 기억」 절.
const (
	ShowUsedByHead   Key = "show-used-by-head"
	ShowUsedBySource Key = "show-used-by-source"
	ShowUsedByLink   Key = "show-used-by-link"
	ShowUsedByDead   Key = "show-used-by-dead"
)

// tags.
const (
	TagsNothing     Key = "tags-nothing"
	TagsAdded       Key = "tags-added"
	TagsAliased     Key = "tags-aliased"
	TagsBadPair     Key = "tags-bad-pair"
	TagsBadTag      Key = "tags-bad-tag"
	TagsCheckHead   Key = "tags-check-head"
	TagsCheckClean  Key = "tags-check-clean"
	TagsOffList     Key = "tags-off-list"
	TagsDenied      Key = "tags-denied"
	TagsAliasUsed   Key = "tags-alias-used"
	TagsRenameDone  Key = "tags-rename-done"
	TagsRenameNone  Key = "tags-rename-none"
	TagsDryRun      Key = "tags-dry-run"
	TagsSaved       Key = "tags-saved"
	TagsScopeAdded  Key = "tags-scope-added"
	TagsScopeAlias  Key = "tags-scope-alias"
	TagsBadScope    Key = "tags-bad-scope"
	TagsScopeOff    Key = "tags-scope-off"
	TagsTypeOff     Key = "tags-type-off"
	TagsLearning    Key = "tags-learning"
	TagsConfirmed   Key = "tags-confirmed"
	TagsListHead    Key = "tags-list-head"
	TagsListScopes  Key = "tags-list-scopes"
	TagsListNoScope Key = "tags-list-no-scope"
	// 태그 표준은 scope 와 따로 선다 (리뷰 D7).
	TagsTagLearning  Key = "tags-tag-learning"
	TagsTagConfirmed Key = "tags-tag-confirmed"
)

// migrate.
const (
	MigrateHead      Key = "migrate-head"
	MigrateHandHead  Key = "migrate-hand-head"
	MigrateNothing   Key = "migrate-nothing"
	MigrateDryRun    Key = "migrate-dry-run"
	MigrateApplied   Key = "migrate-applied"
	MigrateBackedUp  Key = "migrate-backed-up"
	MigrateRestored  Key = "migrate-restored"
	MigrateNoBackup  Key = "migrate-no-backup"
	MigrateLocked    Key = "migrate-locked"
	MigrateNoteOnly  Key = "migrate-note-only"
	MigrateReasonTag Key = "migrate-reason-tag"
	MigrateReasonScp Key = "migrate-reason-scope"
	MigrateReasonBad Key = "migrate-reason-bad"
)

// review.
const (
	ReviewNotYet Key = "review-not-yet"
)

// status 하위 화면.
const (
	StatusQualityHead Key = "status-quality-head"
	StatusQualityRow  Key = "status-quality-row"
	StatusDBHead      Key = "status-db-head"
	StatusDBNone      Key = "status-db-none"
	StatusDoctorHead  Key = "status-doctor-head"
	StatusLogEmpty    Key = "status-log-empty"
	StatusBadSince    Key = "status-bad-since"
)

var commandV2Messages = map[Key]string{
	GateNextHead:   "다음에 할 것 :",
	GateFixed:      "고쳐 넣었다 : %s `%s` → `%s`",
	GateCheckClean: "관문을 다 지났다. 넣어도 된다.",
	GateCheckOnly:  "--check 는 미리보기라 넣지 않았다. 진짜로 넣으려면 --check 를 빼고 다시 친다.",
	GateWarnStored: "위 %d가지는 경고다. 막지 않았고 기억은 큐에 들어갔다 — 고치려면 mem set 을 쓴다.",
	SetSuperseded:  "덮음 표시를 달았다 : %s 를 %s 가 덮는다.",
	SetByNewNext:   "새 기억을 넣을 때 `mem add … --by %s` 를 주면 덮음 표시가 채워진다.",
	SetUsedByFound: "이 기억을 근거로 삼은 기억이 %d건 있다 : %s",
	SetUsedByNext:  "→ mem review --kind basis   (사람이 하나씩 판정한다)",
	SetUsedByUnknown: "이 기억을 근거로 삼은 기억은 못 셌다 " +
		"(색인이 없다 — mem index 뒤에 mem review --kind basis)",
	SetUsedByRebuilt: "이 기억을 근거로 삼은 기억은 못 셌다 " +
		"(색인을 통째로 다시 만들어 아직 비어 있다 — mem index 뒤에 mem review --kind basis)",
	SetUsedByNoIndex: "`--no-index` 라 이 기억을 근거로 삼은 기억은 안 셌다 " +
		"(mem review --kind basis 로 본다)",
	SetUsedByAllDead: "이 기억을 근거로 삼은 기억 %d건은 다 무효이거나 다시 볼 날을 미뤄 둔 것이라 검토 큐에 안 오른다.",
	SetSelfSupersede: "자기 자신을 덮을 수는 없다 : %s (덮은 새 기억의 id 를 준다)",
	ListMore:         " … 외 %d건",

	ShowUsedByHead:   "## 나를 근거로 삼은 기억 (%d건)",
	ShowUsedBySource: "근거",
	ShowUsedByLink:   "링크",
	ShowUsedByDead:   "(이미 무효)",

	TagsNothing:     "할 일을 안 줬다. --list · --add · --add-scope · --alias · --alias-scope · --rename · --check 중 하나를 준다.",
	TagsListHead:    "표준 태그 — 상위 %d가지, 통틀어 %d가지",
	TagsListScopes:  "표준 scope %d개 : %s",
	TagsListNoScope: "표준 scope 가 아직 없다. mem tags --add-scope <이름> 으로 첫 이름을 정한다.",
	TagsAdded:       "표준 태그에 넣었다 : %s",
	TagsAliased:     "별칭을 넣었다 : %s → %s",
	TagsBadPair:     "`%s` 는 `왼쪽=오른쪽` 꼴이어야 한다.",
	TagsBadTag:      "태그는 영어 소문자·숫자·하이픈이어야 한다 : %s",
	TagsCheckHead:   "태그 검사 — 기억 %d건, 표준 밖 %d가지",
	TagsCheckClean:  "표준 밖 태그가 없다.",
	TagsOffList:     "  표준 밖 : %s (%d건)",
	TagsDenied:      "  못 쓰는 태그 : %s (%d건) — 주제가 아니라 출처다",
	TagsAliasUsed:   "  별칭 : %s → %s (%d건)",
	TagsRenameDone:  "%d건의 태그 바꾸기를 큐에 넣었다 (`mem index` 가 반영한다).",
	TagsRenameNone:  "그 태그를 쓰는 기억이 없다 : %s",
	TagsDryRun:      "위는 미리보기다. 아직 아무것도 안 고쳤다 — 기억을 고치는 일이라 미리보기가 기본이다.\n  진짜로 바꾸려면 같은 명령에 --apply 를 붙인다.",
	TagsSaved:       "vocab.toml 을 고쳤다 : %s",
	TagsScopeAdded:  "표준 scope 에 넣었다 : %s",
	TagsScopeAlias:  "scope 별칭을 넣었다 : %s → %s",
	TagsBadScope:    "scope 는 영어 소문자·숫자·하이픈이어야 한다 : %s",
	TagsScopeOff:    "  표준 밖 scope : %s (%d건)",
	TagsTypeOff:     "  표 밖 종류 : %s (%d건) — vocab.toml 의 [type.%s] 로 늘린다",
	TagsLearning: "표준 scope 가 하나도 없다. 지금은 목록 밖 scope 를 경고로만 알린다.\n" +
		"  이 저장소 이름을 정하면 그때부터 거절한다 : mem tags --add-scope <이름>",
	TagsConfirmed: "표준 scope %d가지가 정해져 있다. 목록 밖 scope 는 거절이다.",
	TagsTagLearning: "표준 태그는 아직 씨앗 목록 그대로다. 목록 밖 태그는 경고로만 알린다.\n" +
		"  이 저장소 낱말을 한 번이라도 넣으면 그때부터 거절한다 : mem tags --add <태그>",
	TagsTagConfirmed: "표준 태그 %d가지가 정해져 있다. 목록 밖 태그는 거절이다.",

	MigrateHead:      "이전 대상 %d건 — 자동 %d · 사람 손 %d",
	MigrateHandHead:  "사람 손이 필요한 것 :",
	MigrateNothing:   "옛 규격 기억이 없다. 이전할 것이 없다.",
	MigrateDryRun:    "--dry-run 이라 아무것도 안 고쳤다. 진짜로 하려면 --apply 를 준다.",
	MigrateApplied:   "%d건을 이전 큐에 넣었다 (`mem index` 가 반영한다).",
	MigrateBackedUp:  "원본을 남겼다 : %s",
	MigrateRestored:  "%d건을 되돌렸다 : %s",
	MigrateNoBackup:  "되돌릴 아카이브가 없다. `mem migrate --apply` 를 먼저 돌린 적이 없다.",
	MigrateLocked:    "다른 데서 저장소를 고치는 중이다. 끝난 뒤에 다시 돌려라.",
	MigrateNoteOnly:  "note:이전됨 — v0.1 규격이라 근거가 없다",
	MigrateReasonTag: "태그가 %d개뿐이다 (출처 태그를 떼면 모자란다)",
	MigrateReasonScp: "scope `%s` 가 표준 목록에 없다",
	MigrateReasonBad: "%s",

	ReviewNotYet: "검토 큐는 아직 안 붙었다 (파도 G 의 internal/review 를 기다린다).",

	StatusQualityHead: "품질 지표 (설계 3-6)",
	StatusQualityRow:  "  %s  %-20s %8s  목표 %s  %s",
	StatusDBHead:      "색인 표별 크기 (%.2fMB)",
	StatusDBNone:      "색인이 없다. `mem index` 를 먼저 돌려라.",
	StatusDoctorHead:  "환경 점검",
	StatusLogEmpty:    "기록이 없다 (Memory/log.md).",
	StatusBadSince:    "`--since` 는 YYYY-MM-DD 꼴이어야 한다 : %s",
}

func init() {
	for key, text := range commandV2Messages {
		messages[key] = text
	}
}
