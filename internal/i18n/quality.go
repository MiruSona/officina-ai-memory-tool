package i18n

// lint · gc · status 가 쓰는 문장이다. i18n.go 의 표를 건드리지 않고 여기서
// 덧붙인다 (설계 12-1 : 한글 리터럴은 이 패키지 밖에 두지 않는다).

// lint 규칙이 사람에게 하는 말.
const (
	LintFrontMatter       Key = "lint-front-matter"
	LintTagCount          Key = "lint-tag-count"
	LintTagShape          Key = "lint-tag-shape"
	LintScopeAll          Key = "lint-scope-all"
	LintBodyLongWarn      Key = "lint-body-long-warn"
	LintBodyLongError     Key = "lint-body-long-error"
	LintHeadings          Key = "lint-headings"
	LintEmptySection      Key = "lint-empty-section"
	LintIssueSections     Key = "lint-issue-sections"
	LintDuplicate         Key = "lint-duplicate"
	LintDuplicateCrowded  Key = "lint-duplicate-crowded"
	LintSameBody          Key = "lint-same-body"
	LintDeadLink          Key = "lint-dead-link"
	LintDeadPath          Key = "lint-dead-path"
	LintDecisionConflict  Key = "lint-decision-conflict"
	LintSecret            Key = "lint-secret"
	LintSecretWarn        Key = "lint-secret-warn"
	LintPrivatePath       Key = "lint-private-path"
	LintShadowExe         Key = "lint-shadow-exe"
	LintEncodingCP949     Key = "lint-encoding-cp949"
	LintByteOrderMark     Key = "lint-bom"
	LintCarriageReturn    Key = "lint-crlf"
	LintInboxBad          Key = "lint-inbox-bad"
	LintTodoStale         Key = "lint-todo-stale"
	LintSummaryImperative Key = "lint-summary-imperative"
	LintSynonymCandidate  Key = "lint-synonym-candidate"
)

// lint 보고.
const (
	LintHeader       Key = "lint-header"
	LintTableHead    Key = "lint-table-head"
	LintClean        Key = "lint-clean"
	LintTotals       Key = "lint-totals"
	LintFixDone      Key = "lint-fix-done"
	LintFixNone      Key = "lint-fix-none"
	LintFixLocked    Key = "lint-fix-locked"
	LintFixReadOnly  Key = "lint-fix-read-only"
	LintFixHeld      Key = "lint-fix-held"
	LintFixHeldNote  Key = "lint-fix-held-note"
	LintFixEncoding  Key = "lint-fix-encoding"
	LintFixBOM       Key = "lint-fix-bom"
	LintFixLineEnd   Key = "lint-fix-line-end"
	LintFixTagCase   Key = "lint-fix-tag-case"
	LintFixComment   Key = "lint-fix-comment"
	LintLevelError   Key = "lint-level-error"
	LintLevelWarn    Key = "lint-level-warn"
	LintMissLogUsed  Key = "lint-miss-log-used"
	LintPathSetLimit Key = "lint-path-set-limit"
)

// gc.
const (
	GCFoldNotice Key = "gc-fold-notice"
	GCColdNotice Key = "gc-cold-notice"
	GCCodeFolded Key = "gc-code-folded"
	GCTooSoon    Key = "gc-too-soon"
	GCLocked     Key = "gc-locked"
	GCNoIndex    Key = "gc-no-index"
	GCReadOnly   Key = "gc-read-only"
	GCNothing    Key = "gc-nothing"
	GCPlanHeader Key = "gc-plan-header"
	GCPlanRow    Key = "gc-plan-row"
	GCDone       Key = "gc-done"
	GCDryRunFoot Key = "gc-dry-run-foot"
	GCCapped     Key = "gc-capped"
	GCIndexRan   Key = "gc-index-ran"
	GCIndexSkip  Key = "gc-index-skip"
)

// status.
const (
	StatusRepoLine     Key = "status-repo-line"
	StatusCountLine    Key = "status-count-line"
	StatusStateLine    Key = "status-state-line"
	StatusIndexLine2   Key = "status-index-line2"
	StatusIndexMissing Key = "status-index-missing"
	StatusUnindexed2   Key = "status-unindexed2"
	StatusInstallLine  Key = "status-install-line"
	StatusConfigLine   Key = "status-config-line"
	StatusUsageLine    Key = "status-usage-line"
	StatusUsageNone    Key = "status-usage-none"
	StatusYes          Key = "status-yes"
	StatusNo           Key = "status-no"
	StatusNone         Key = "status-none"
	StatusBadPattern   Key = "status-bad-pattern"
	StatusHealthBad    Key = "status-health-bad"
)

// Version 은 이 exe 의 판이다. status 의 「설치」 줄이 찍는다.
const Version = "0.3.0"

// IssueSymptomHeading · IssueCureHeading 은 issue 본문이 반드시 가져야 하는
// 두 절 제목이다 (설계 4-3).
const (
	IssueSymptomHeading = "증상"
	IssueCureHeading    = "해결"
)

var qualityMessages = map[Key]string{
	LintFrontMatter:       "머리말을 못 읽었다 : %s",
	LintTagCount:          "태그가 %d개다. 1~5개여야 한다.",
	LintTagShape:          "태그에 소문자·숫자·하이픈 말고 다른 글자가 있다 : %s",
	LintScopeAll:          "`scope: all` 이 %d건으로 전체 %d건의 %d%% 다. 20%% 를 넘으면 scope 가 뜻을 잃는다.",
	LintBodyLongWarn:      "본문이 %d줄이다. %d줄을 넘으면 한 파일 한 주제가 아닐 수 있다.",
	LintBodyLongError:     "본문이 %d줄이다. %d줄을 넘었다. 쪼개라.",
	LintHeadings:          "`##` 제목이 %d개다. %d개를 넘으면 한 파일 한 주제가 아니다.",
	LintEmptySection:      "`## %s` 아래가 비었다.",
	LintIssueSections:     "issue 는 `## 증상` 과 `## 해결` 두 절이 있어야 한다.",
	LintDuplicate:         "%s 와 본문이 %d%% 같다.",
	LintDuplicateCrowded:  "닮은 기억이 너무 많다(후보 %d건). 중복 검사를 건너뛰었다.",
	LintSameBody:          "%s 와 본문이 완전히 같다.",
	LintDeadLink:          "가리키는 기억이 없다 : %s",
	LintDeadPath:          "가리키는 경로가 없다 : %s",
	LintDecisionConflict:  "같은 scope(%s) 의 새 결정 %s 가 있는데 이 결정에 `invalid_at` 이 없다.",
	LintSecret:            "%d번째 줄에 비밀정보로 보이는 것이 있다. 규칙 '%s'. 값은 안 찍는다.",
	LintSecretWarn:        "%d번째 줄에 개인정보로 보이는 것이 있다. 규칙 '%s'. 값은 안 찍는다.",
	LintPrivatePath:       "%d번째 줄에 사적 경로가 있다. 팀이 같이 보는 파일이다.",
	LintShadowExe:         "저장소 폴더에 %s 가 있다. 설치된 mem 보다 먼저 잡힌다.",
	LintEncodingCP949:     "cp949 로 읽어야 했다. UTF-8 로 다시 써라 (`mem lint --fix`).",
	LintByteOrderMark:     "BOM 이 붙어 있다 (`mem lint --fix`).",
	LintCarriageReturn:    "줄끝이 CRLF 다. LF 여야 한다 (`mem lint --fix`).",
	LintInboxBad:          "`inbox/bad/` 에 파일이 %d개 있다. 승격에 실패한 것들이다.",
	LintTodoStale:         "열린 todo 가 %d일째다. %d일을 넘었다.",
	LintSummaryImperative: "요약이 시킴말 '%s' 로 끝난다. 기억은 자료지 지시가 아니다.",
	LintSynonymCandidate:  "'%s' 로 %d번 찾았는데 0건이었다. 동의어 표에 넣을 후보다.",

	LintHeader:      "검사 : %d건 · 오류 %d · 경고 %d (%.2f초)",
	LintTableHead:   "\n| 규칙 | 등급 | 건수 |\n| --- | --- | --- |",
	LintClean:       "고칠 것이 없다.",
	LintTotals:      "\n오류 %d · 경고 %d",
	LintFixDone:     "\n고친 것 %d건 :",
	LintFixNone:     "고칠 수 있는 것이 없었다.",
	LintFixLocked:   "다른 데서 색인하는 중이라 검사만 했다. 아무것도 안 고쳤다.",
	LintFixReadOnly: "읽기 전용 저장소라 검사만 했다.",
	LintFixHeld:     "  %s — 규격에 없는 머리말 칸(%s)이 있어 건너뛰었다.",
	LintFixHeldNote: "  %s — %s 건너뛰었다.",
	LintFixEncoding: "cp949 를 UTF-8 로",
	LintFixBOM:      "BOM 제거",
	LintFixLineEnd:  "줄끝 LF",
	LintFixTagCase:  "태그 소문자",
	LintFixComment:  "머리말에 사람이 적은 주석이 있어",
	LintLevelError:  "오류",
	LintLevelWarn:   "경고",
	LintMissLogUsed: "0건 기록 %s 에서 %d 질의를 봤다.",
	LintPathSetLimit: "프로젝트 폴더가 너무 커서 경로 %d개까지만 훑었다. " +
		"`dead-path` 는 못 본 자리를 건너뛴다.",

	GCFoldNotice: "(줄인 본문이다. 원본은 아카이브에 있다 : `mem show %s --from-archive`)",
	GCColdNotice: "(본문을 접었다. 원본은 아카이브에 있다 : `mem show %s --from-archive`)",
	GCCodeFolded: "(코드 %d줄 접음)",
	GCTooSoon:    "정리한 지 하루가 안 됐다 (마지막 %s). 아무것도 안 했다.",
	GCLocked:     "다른 데서 색인하는 중이라 정리를 건너뛰었다.",
	GCNoIndex:    "색인이 없다. `mem index` 를 먼저 돌려라.",
	GCReadOnly:   "읽기 전용 저장소라 정리하지 않는다.",
	GCNothing:    "정리할 기억이 없다 (전체 %d건).",
	GCPlanHeader: "\n| 기억 | 종류 | 단계 | 지난 날 |\n| --- | --- | --- | --- |",
	GCPlanRow:    "| %s | %s | %s | %d일 |",
	GCDone:       "정리 : 따뜻함 %d · 차가움 %d (전체 %d건, %.2f초). 파일은 하나도 안 지웠다.",
	GCDryRunFoot: "아무것도 안 고쳤다. 진짜로 하려면 --dry-run 을 빼라.",
	GCCapped:     "한 번 상한 %d건에 걸렸다. 남은 것은 다음에 한다.",
	GCIndexRan:   "이어서 정리했다 :",
	GCIndexSkip:  "정리는 건너뛰었다 (--no-gc).",

	StatusRepoLine:     "저장소 : %s%s",
	StatusCountLine:    "기억   : %d건 (%s)",
	StatusStateLine:    "         고정 %d · 무효 %d · hot %d / warm %d / cold %d",
	StatusIndexLine2:   "색인   : index.db %.1fMB · 마지막 색인 %s · 마지막 gc %s · 미승격 %d · 실패(bad) %d",
	StatusIndexMissing: "색인   : 아직 없다 (`mem index` 를 돌려라) · 미승격 %d",
	StatusUnindexed2:   "         색인 안 된 파일 %d건 : %s   ← 규격을 벗어났다(자리가 store/YYYY/MM/ 이 아니거나 등). `mem index` 출력에서 이유를 본다",
	StatusInstallLine:  "설치   : mem %s · 규격 판 %d · DB 판 %d · PATH 에 잡힘(%s) · SessionStart 훅 붙음(%s)",
	StatusConfigLine:   "설정   : mem.toml 에 빠진 키 %d · 비밀정보 패턴 %d개 정상%s · 동의어 %d쌍 · 불용어 %d개",
	StatusUsageLine:    "사용   : %s",
	StatusUsageNone:    "아직 셈이 없다 (local/hits.jsonl)",
	StatusYes:          "O",
	StatusNo:           "X",
	StatusNone:         "없음",
	StatusBadPattern:   " · 못 읽는 패턴 %d개",
	StatusHealthBad:    "색인 건강에 문제가 있다. 위 줄을 봐라.",
}

func init() {
	for key, text := range qualityMessages {
		messages[key] = text
	}
}
