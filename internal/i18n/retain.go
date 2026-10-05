package i18n

// 자동 쌓기(A1)가 쓰는 말이다 — origin 칸, Stop 알림, 자동 관문 R1·R2·R4~R8,
// `mem auto`, `review --reject` (자동쌓기설계 2절).

const (
	BadOrigin Key = "bad-origin"

	NudgeHead    Key = "nudge-head"
	NudgeTypes   Key = "nudge-types"
	NudgeSkip    Key = "nudge-skip"
	NudgeHow     Key = "nudge-how"
	NudgeNothing Key = "nudge-nothing"
	NudgeScopes  Key = "nudge-scopes"

	AutoNoRecord          Key = "auto-no-record"
	AutoNoRecordWarn      Key = "auto-no-record-warn"
	AutoDuplicate         Key = "auto-duplicate"
	AutoNoBy              Key = "auto-no-by"
	AutoNoDecision        Key = "auto-no-decision"
	AutoQuoteNeeded       Key = "auto-quote-needed"
	AutoQuoteTooMany      Key = "auto-quote-too-many"
	AutoQuoteShort        Key = "auto-quote-short"
	AutoQuoteMissing      Key = "auto-quote-missing"
	AutoFragmentMissing   Key = "auto-fragment-missing"
	AutoFragmentUnchecked Key = "auto-fragment-unchecked"
	AutoJudgeSkipped      Key = "auto-judge-skipped"
	AutoNotSupported      Key = "auto-not-supported"
	AutoCapSession        Key = "auto-cap-session"
	AutoCapDay            Key = "auto-cap-day"
	AutoSourcesNeeded     Key = "auto-sources-needed"
	AutoSourceDead        Key = "auto-source-dead"
	AutoScopeUnknown      Key = "auto-scope-unknown"

	AddOriginOnly     Key = "add-origin-only"
	AddOriginJSONL    Key = "add-origin-jsonl"
	AddAutoRejected   Key = "add-auto-rejected"
	AddAutoWarnLine   Key = "add-auto-warn-line"
	AddAutoPassed     Key = "add-auto-passed"
	AddSessionUnknown Key = "add-session-unknown"
	SessionTooShort   Key = "session-too-short"
	AddSessionNeeded  Key = "add-session-needed"
	AutoRedoCold      Key = "auto-redo-cold"

	AutoUsage        Key = "auto-usage"
	AutoNoFilter     Key = "auto-no-filter"
	AutoBadDate      Key = "auto-bad-date"
	AutoListEmpty    Key = "auto-list-empty"
	AutoListTotal    Key = "auto-list-total"
	AutoUndoPlan     Key = "auto-undo-plan"
	AutoUndoLinked   Key = "auto-undo-linked"
	AutoUndoDryRun   Key = "auto-undo-dry-run"
	AutoUndoDone     Key = "auto-undo-done"
	AutoUndoNothing  Key = "auto-undo-nothing"
	AutoRedoNothing  Key = "auto-redo-nothing"
	AutoRedoPlan     Key = "auto-redo-plan"
	AutoRedoDryRun   Key = "auto-redo-dry-run"
	AutoRedoDone     Key = "auto-redo-done"
	AutoLogUndo      Key = "auto-log-undo"
	AutoLogRedo      Key = "auto-log-redo"
	AutoLogRejected  Key = "auto-log-rejected"
	ReviewRejectNot  Key = "review-reject-not-held"
	ReviewRejectDone Key = "review-reject-done"
	ReviewRejectBoth Key = "review-reject-both"
	ReviewLogReject  Key = "review-log-reject"

	InitRetainAdded Key = "init-retain-added"
)

var retainMessages = map[Key]string{
	BadOrigin: "origin 값이 틀렸다 : %q — stop · card · retain:<모델> · consolidate:<모델> 중 하나다.",

	NudgeHead:    "[mem] 이 세션에서 고친 것이 쌓였다. 남길 만한 것이 있으면 `mem add` 로 0~3건 넣는다.",
	NudgeTypes:   "> 남길 것 : 정한 것(decision) · 주의(caution) · 끝낸 일(history) · 방법(howto) · 남은 일(todo).",
	NudgeSkip:    "> 남기지 않을 것 : 코드·git 으로 알 수 있는 것, 이미 mem 에 있는 것, 이번 판에만 쓰는 임시값.",
	NudgeHow:     "> 넣을 때 `--origin stop --session %s` 를 붙인다. 이 세션 안에서 바로 찾을 수 있는 근거만 적는다.",
	NudgeNothing: "> 남길 것이 없으면 「남길 것 없음」 한 줄로 끝낸다.",
	NudgeScopes:  "> 쓸 수 있는 scope : %s",

	AutoNoRecord:          "세션 대화 기록을 못 찾아 근거를 대조할 수 없다.",
	AutoNoRecordWarn:      "세션 대화 기록을 못 찾았다. 경로·id 만 저장소에서 대조했다.",
	AutoDuplicate:         "비슷한 기억이 이미 있다 (%s). 자동 기억은 합치지 않는다 — 사람이 넣거나 그 기억을 고친다.",
	AutoNoBy:              "자동 기억은 다른 기억을 덮지(--by) 못한다. 사람이 `mem set --by-new` 로 한다.",
	AutoNoDecision:        "로컬 모델 길은 decision 을 못 넣는다.",
	AutoQuoteNeeded:       "근거 문장(--quotes)이 1개 이상 있어야 한다.",
	AutoQuoteTooMany:      "근거 문장이 %d개다. %d개까지만 받는다.",
	AutoQuoteShort:        "근거 문장이 너무 짧다 : 「%s」 — %d자 이상이어야 한다.",
	AutoQuoteMissing:      "근거 문장이 대화에 없다 : 「%s」",
	AutoFragmentMissing:   "본문의 값이 대화에도 저장소에도 없다 : %s",
	AutoFragmentUnchecked: "대화 기록이 없어 수·날짜·주소 %d개를 대조하지 못했다.",
	AutoJudgeSkipped:      "근거 지지 판정(R3)을 못 받아 건너뛰었다.",
	AutoNotSupported:      "근거 문장이 요약을 뒷받침하지 않는다 : 「%s」",
	AutoCapSession:        "이 세션에서 자동 기억 상한 %d건을 넘는다.",
	AutoCapDay:            "오늘 자동 기억 상한 %d건을 넘는다.",
	AutoSourcesNeeded:     "근거(--sources)가 하나 이상 있어야 한다.",
	AutoSourceDead:        "근거 파일이 저장소에 없다 : %s",
	AutoScopeUnknown:      "scope %q 는 이 저장소 vocab 목록에 없다.",

	AddOriginOnly:     "--quotes · --session 은 --origin 과 같이 쓴다.",
	AddOriginJSONL:    "--origin 은 --jsonl 과 같이 못 쓴다. 한 건씩 넣는다.",
	AddAutoRejected:   "자동 관문에 걸려 저장하지 않았다 (%s).",
	AddAutoWarnLine:   "  알림 : %s",
	AddAutoPassed:     "자동 관문 통과 (%s · 세션 %s).",
	AddSessionUnknown: "세션 %q 을 이 기계 상태 파일에서 못 찾았다. 대화 기록 없이 대조한다.",
	SessionTooShort:   "--session 값 %q 이 짧다. 세션 id 앞 8자 이상을 준다.",
	AddSessionNeeded:  "최근 %d분 안에 돈 세션이 %d개다. 어느 세션 기록과 대조할지 `--session <앞 8자>` 로 준다.",
	AutoRedoCold:      "  %s 는 버린 기억(cold)이라 건너뛴다. 되살리려면 `mem gc --restore %s` 뒤 다시 본다.",

	AutoUsage:        "쓰는 법 : mem auto list|undo|redo [옵션]. `mem help auto` 를 본다.",
	AutoNoFilter:     "undo 는 거름(--origin · --since · --until · --session) 하나 이상이 있어야 한다.",
	AutoBadDate:      "날짜를 못 읽었다 : %q — 2026-10-05 꼴이다. 기억 날짜(date)로 거른다.",
	AutoListEmpty:    "자동 기억이 없다.",
	AutoListTotal:    "자동 기억 %d건.",
	AutoUndoPlan:     "되돌릴 자동 기억 %d건 (보류로 돌린다 · 파일은 안 지운다).",
	AutoUndoLinked:   "  %s 를 가리키는 기억 %d건 : %s",
	AutoUndoDryRun:   "미리보기다. 실제로 하려면 --apply 를 붙인다.",
	AutoUndoDone:     "%d건을 보류로 돌렸다. 되살리려면 `mem auto redo --apply` (기록 %s).",
	AutoUndoNothing:  "거름에 맞는 자동 기억이 없다.",
	AutoRedoNothing:  "되살릴 undo 기록이 없다.",
	AutoRedoPlan:     "되살릴 기억 %d건 (undo 기록 %d개).",
	AutoRedoDryRun:   "미리보기다. 실제로 하려면 --apply 를 붙인다.",
	AutoRedoDone:     "%d건을 되살렸다.",
	AutoLogUndo:      "%d건 · 거름 %s · 기록 %s",
	AutoLogRedo:      "%d건 · 기록 %s",
	AutoLogRejected:  "자동 · %s · %s · %s",
	ReviewRejectNot:  "%s 는 보류 기억이 아니다. --reject 는 보류 기억만 접는다.",
	ReviewRejectDone: "%s 를 버렸다 (cold 로 접음 · 본문은 archive 에 남는다).",
	ReviewRejectBoth: "--promote 와 --reject 는 같이 못 쓴다.",
	ReviewLogReject:  "보류 버림 %s",

	InitRetainAdded: "mem.toml 에 [retain] 칸을 붙였다.",
}

func init() {
	for key, text := range retainMessages {
		messages[key] = text
	}
}
