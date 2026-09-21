// Package i18n 은 이 도구가 사람에게 보이는 한글 문장을 전부 담는다.
// 문장 하나에 함수 하나가 아니라 표 하나로 둔다 (설계 12-1).
package i18n

import "fmt"

// Key 는 문장 하나를 가리키는 이름이다.
type Key string

// 기억 파일 머리말·값 검사.
const (
	BadFrontMatter      Key = "bad-front-matter"
	UnclosedFrontMatter Key = "unclosed-front-matter"
	BadYAML             Key = "bad-yaml"
	MissingField        Key = "missing-field"
	BadType             Key = "bad-type"
	BadSource           Key = "bad-source"
	BadID               Key = "bad-id"
	BadDate             Key = "bad-date"
	BadScope            Key = "bad-scope"
	BadSummaryLength    Key = "bad-summary-length"
	BadImportance       Key = "bad-importance"
	BadTagCount         Key = "bad-tag-count"
	BadTag              Key = "bad-tag"
	BadStatus           Key = "bad-status"
	BadSeverity         Key = "bad-severity"
	BadSupersededBy     Key = "bad-superseded-by"
	BadBodyLines        Key = "bad-body-lines"

	// v0.2 규격이 새로 보는 칸들 (설계 2-2 · 규칙표 F06·F16·F17·F19).
	MissingSources      Key = "missing-sources"
	BadAuthor           Key = "bad-author"
	BadTitleLength      Key = "bad-title-length"
	TitleEchoesSummary  Key = "title-echoes-summary"
	BadSourceEntry      Key = "bad-source-entry"
	SupersedePairBroken Key = "supersede-pair-broken"
	StatusOnlyTodo      Key = "status-only-todo"
	SeverityOnlyIssue   Key = "severity-only-issue"
)

// 저장소·파일 계층.
const (
	NoRepository      Key = "no-repository"
	SkippedConfigLine Key = "skipped-config-line"
	OldFieldWeights   Key = "old-field-weights"
	BadFieldWeights   Key = "bad-field-weights"
	BadWeightValue    Key = "bad-weight-value"
	QueueWait         Key = "queue-wait"
	ReadOnlyStore     Key = "read-only-store"
	NotPatchable      Key = "not-patchable"
	UnknownOp         Key = "unknown-op"
	CannotDecode      Key = "cannot-decode"
	OutsidePath       Key = "outside-path"
	NotRegularFile    Key = "not-regular-file"
	ArchiveTooBig     Key = "archive-too-big"
	ArchiveNoRoom     Key = "archive-no-room"
)

// 색인·승격.
const (
	ExeTooOld      Key = "exe-too-old"
	IndexBroken    Key = "index-broken"
	BadIndexEntry  Key = "bad-index-entry"
	IDPathMismatch Key = "id-path-mismatch"
	AppendHeading  Key = "append-heading"
	NoSuchMemory   Key = "no-such-memory"
	SecretFound    Key = "secret-found"
	SecretFoundIn  Key = "secret-found-in"
	SecretInStore  Key = "secret-in-store"
	IndexUnusable  Key = "index-unusable"
	IndexLocked    Key = "index-locked"
	IndexReport    Key = "index-report"
	IndexPromote   Key = "index-promote"
	IndexRebuilt   Key = "index-rebuilt"
	IndexUnindexed Key = "index-unindexed"
	// IndexLinkBlocked 는 너무 커서 건너뛴 이웃 후보 통 수다 (스트레스 V8).
	IndexLinkBlocked   Key = "index-link-blocked"
	SetQueued          Key = "set-queued"
	SetNothing         Key = "set-nothing"
	BadImportanceValue Key = "bad-importance-value"
)

// 명령줄.
const (
	Usage          Key = "usage"
	UnknownCommand Key = "unknown-command"
	NeedArgument   Key = "need-argument"
	MemoryNotFound Key = "memory-not-found"
)

// add · set · show 가 쓰는 문장이다.
const (
	SecretFix        Key = "secret-fix"
	AddQueuedNote    Key = "add-queued-note"
	AddQueuedNoBy    Key = "add-queued-no-by"
	AddRejectedNote  Key = "add-rejected-note"
	AddStatusDefault Key = "add-status-default"
	AddStrayWords    Key = "add-stray-words"
	ShowQueued       Key = "show-queued"
	CheckHeader      Key = "check-header"
	CheckNone        Key = "check-none"
	CheckNoIndex     Key = "check-no-index"
	CheckSameBody    Key = "check-same-body"
	JSONLBadLine     Key = "jsonl-bad-line"
	JSONLAt          Key = "jsonl-at"
	JSONLDone        Key = "jsonl-done"
	JSONLFixed       Key = "jsonl-fixed"
	JSONLEmpty       Key = "jsonl-empty"
	BadHeadValue     Key = "bad-head-value"
	ArchiveNoMemory  Key = "archive-no-memory"
	IndexClearBad    Key = "index-clear-bad"
	IndexClearNone   Key = "index-clear-none"
	IndexBadKept     Key = "index-bad-kept"
	HookOverBudget   Key = "hook-over-budget"
	BadBoolValue     Key = "bad-bool-value"
	NotBoolValue     Key = "not-bool-value"
	VocabTypeName    Key = "vocab-type-name"
	VocabTypeKey     Key = "vocab-type-key"
	VocabTypeValue   Key = "vocab-type-value"
	VocabScopeName   Key = "vocab-scope-name"
)

// messages 는 키 하나에 문장 하나다. 서식은 fmt 그대로 쓴다.
var messages = map[Key]string{
	BadFrontMatter:      "머리말이 없다. 파일은 `---` 로 감싼 머리말로 시작해야 한다.",
	UnclosedFrontMatter: "머리말이 안 닫혔다. 끝에 `---` 줄이 있어야 한다.",
	BadYAML:             "머리말 YAML 을 못 읽었다 : %s",
	MissingField:        "머리말에 `%s` 칸이 없다.",
	BadType:             "`type` 이 %s 중 하나가 아니다 : %s",
	BadSource:           "`source` 는 user·ai·hook·import 중 하나여야 한다 : %s",
	BadID:               "`id` 는 YYYYMMDD-여덟자리 꼴이어야 한다 : %s",
	BadDate:             "날짜는 YYYY-MM-DD 꼴이어야 한다 : %s",
	BadScope:            "`scope` 는 영어 소문자·숫자·하이픈 1~30자여야 한다 : %s",
	BadSummaryLength:    "`summary` 는 30~120자여야 한다. 지금 %d 자다.",
	BadImportance:       "`importance` 는 1~5 여야 한다. 지금 %d 다.",
	BadTagCount:         "`tags` 는 %d~%d개여야 한다. 지금 %d 개다.",
	MissingSources:      "`%s` 는 근거(`sources`)가 하나 이상 있어야 한다. `file:` `commit:` `url:` `mem:` `note:` 중 하나로 적는다.",
	BadAuthor:           "`author` 는 `human:<아이디>` · `<도구>/<버전>` · `hook:<이름>` 중 하나여야 한다 : %s",
	BadTitleLength:      "`title` 은 6~40자여야 한다. 지금 %d 자다.",
	TitleEchoesSummary:  "`title` 이 `summary` 앞머리를 그대로 베꼈다. 무엇을 정했는지가 드러나는 제목을 쓴다.",
	BadSourceEntry:      "`sources` 항목은 `file:` `commit:` `url:` `mem:` `note:` 로 시작해야 한다 : %s",
	SupersedePairBroken: "`superseded_by` 와 `invalid_at` 은 한 짝이다. 둘 다 적거나 둘 다 비운다.",
	StatusOnlyTodo:      "`todo_status` 는 `todo` 에만 붙는다. 지금 type 은 %s 다.",
	SeverityOnlyIssue:   "`severity` 는 `issue`·`caution` 에만 붙는다. 지금 type 은 %s 다.",
	BadTag:              "`tags` 값은 영어 소문자·숫자·하이픈이어야 한다 : %s",
	BadStatus:           "`status` 는 open·doing·done 중 하나여야 한다 : %s",
	BadSeverity:         "`severity` 는 low·mid·high 중 하나여야 한다 : %s",
	BadSupersededBy:     "`superseded_by` 는 기억 id 꼴이어야 한다 : %s",
	BadBodyLines:        "본문이 %d줄이다. %d줄을 넘었다. 한 파일 한 주제로 쪼개라.",

	ExeTooOld:          "이 mem 은 낡았다 (아는 판 %d, DB 판 %d). 새 판을 받아라.",
	IndexBroken:        "색인 파일이 깨졌다 : %s\n`mem index --full` 로 다시 만든다. 기억 파일은 그대로다.",
	BadIndexEntry:      "색인에서 뺐다 : %s — %s",
	IDPathMismatch:     "id 와 파일 자리가 다르다 : id '%s' 는 %s 에 있어야 한다",
	AppendHeading:      "## 덧붙임 %s",
	NoSuchMemory:       "그 기억이 없다 : %s",
	SecretFound:        "비밀정보로 보이는 것이 있다 : %d번째 줄, 규칙 '%s'. 저장하지 않았다.",
	SecretFoundIn:      "비밀정보로 보이는 것이 있다 : %s 칸, 규칙 '%s'. 저장하지 않았다.",
	IndexUnusable:      "색인이 깨졌거나 비었다 : %s — `mem index --full` 로 다시 만들어라. 기억 파일(md)은 그대로다.",
	SecretInStore:      "비밀정보로 보이는 것이 있다 (규칙 '%s'). 색인에 안 넣었다. 값은 안 찍는다. 그 줄을 지우고 `mem index` 를 다시 돌려라.",
	IndexLocked:        "다른 데서 색인하는 중이라 건너뛰었다.",
	IndexPromote:       "승격 : 새로 %d · 합침 %d · 중복 %d · 고침 %d · 못 씀 %d · 남김 %d",
	IndexReport:        "색인 : 새로 %d · 그대로 %d · 지움 %d · 전체 %d (%.2f초)",
	IndexRebuilt:       "색인을 처음부터 다시 만들었다.",
	IndexLinkBlocked:   "이웃 후보 통 %d개가 너무 커서 건너뛰었다 (가장 큰 통 %d건 · 상한 %d). 그 통에 든 기억끼리는 자동 링크를 안 만든다 — 태그를 좁히면 살아난다.",
	IndexUnindexed:     "색인 안 된 파일 %d건 (규격을 벗어났다 — 위 줄에서 이유를 본다. 흔한 원인은 store/YYYY/MM/ 자리가 아니다)",
	SetQueued:          "고칠 것을 큐에 넣었다 : %s (`mem index` 가 반영한다)",
	SetNothing:         "고칠 것을 하나도 안 줬다.",
	BadImportanceValue: "`--importance` 는 1~5 여야 한다 : %s",

	NoRepository:      "기억 저장소를 못 찾았다. 프로젝트 뿌리에서 `mem init` 을 돌려라.",
	SkippedConfigLine: "설정 파일 %d 번째 줄을 못 읽어서 건너뛰었다 (나머지는 그대로 쓴다) : %s",
	VocabTypeName:     "vocab.toml 의 `[type.%s]` 는 종류 이름 규격(영어 소문자·숫자·하이픈 2~20자)이 아니라 통째로 버렸다.",
	VocabTypeKey:      "vocab.toml 의 `[type.%s]` 에 모르는 칸 `%s` 가 있어 무시했다.",
	VocabTypeValue:    "vocab.toml 의 `[type.%s]` 칸 `%s` 값 `%s` 를 못 읽어 기본값을 썼다.",
	VocabScopeName:    "vocab.toml 의 `[scope]` 키 `%s` 는 이름 규격(영어 소문자·숫자·하이픈 30자까지)이 아니라 버렸다.",
	OldFieldWeights:   "[search] field_weights 가 옛 세 값이다. 열이 넷으로 늘어 메타(태그+scope) 자리를 기본값 %g 로 채웠다. `mem init` 이 파일을 고쳐 준다.",
	BadFieldWeights:   "[search] field_weights 를 못 읽었다 (값 %d개). 넷(제목·메타·요약·본문)이라야 한다. 기본값으로 돈다.",
	BadWeightValue:    "[search] field_weights 의 %d번째 값이 0 이상의 숫자가 아니다 : %s. 기본값으로 돈다.",
	QueueWait:         "큐에 %d건이 밀려 있다. 이번 색인은 그것을 다 반영하느라 오래 걸린다 (2만 건이면 6분쯤). 다음 색인부터는 평소 속도다.",
	ReadOnlyStore:     "이 저장소는 읽기 전용이라 쓸 수 없다.",
	NotPatchable:      "`%s` 칸은 고칠 수 없다.",
	UnknownOp:         "모르는 `op` 다 : %s (add · patch · amend 여야 한다)",
	CannotDecode:      "파일을 읽지 못했다. UTF-8 도 cp949 도 아니다 : %s",
	OutsidePath:       "저장소 밖을 가리키는 경로다. 읽지도 쓰지도 않았다 : %s",
	NotRegularFile:    "보통 파일이 아니다 (링크이거나 특수 파일). 열지 않았다 : %s",
	ArchiveTooBig:     "아카이브가 상한보다 크다. 끝까지 읽지 않았다 : %s",
	ArchiveNoRoom:     "%s 달 아카이브 파일이 전부 깨져 붙일 곳이 없다.",

	Usage: `mem — AI 기억 저장소

쓰는 법 : mem <하위명령> [인자]

  install  exe 를 사용자 폴더에 넣고 PATH 에 붙인다 (기계마다 한 번)
  init     이 프로젝트에 기억 저장소를 붙인다
  add      기억 한 건을 쓰기 큐에 넣는다
  set      이미 있는 기억의 머리말을 고친다
  search   기억을 찾는다
  show     기억 한 건을 id 로 보여준다
  hook     훅 진입점 (사람이 직접 칠 일은 없다)
  index    새 기억을 승격하고 바뀐 파일을 색인한다
  migrate  옛 규격 기억을 지금 규격으로 옮긴다
  gc       오래된 기억을 접고 아카이브로 옮긴다
  lint     기억 문서의 품질을 검사한다
  review   사람이 판정할 것만 모아 보여준다
  tags     태그 표준 목록을 보고 손본다 (mem tags --list)
  eval     골든셋으로 검색·문서 품질을 잰다
  status   저장소 자리·건수·품질·환경을 보여준다
  version  이 실행 파일의 판·빌드한 커밋·시각
  help     이 도움말

한 명령의 자세한 도움말 : mem help <하위명령>  (또는 mem <하위명령> --help)
종료 코드 : 0 정상 · 1 사용법 잘못 · 2 검사 실패 · 3 저장소 없음
           4 보안 차단 · 5 파일·권한·DB 손상 · 6 락을 못 얻음`,
	UnknownCommand: "모르는 명령이다 : %s (`mem help` 를 보라)",
	NeedArgument:   "`%s` 에 값이 필요하다.",
	MemoryNotFound: "그런 기억이 없다 : %s",

	SecretFix: "빠져나갈 길 : 이 패턴이 오탐이면 `Memory/mem.toml` 의 `[secret] patterns` 에서 고친다.",
	AddQueuedNote: "저장됨 : %s — 색인 대기다. mem index 뒤에 검색·show 에 보인다 " +
		"(닮은 기억에 합쳐지면 id 가 바뀐다).",
	AddQueuedNoBy: "저장됨 : %s — 다만 --by 덮기는 실패했다. 옛 기억 %s 은 그대로다 " +
		"(`mem set <옛id> --by <새id>` 로 다시 건다).",
	AddRejectedNote: "거절됨 — 저장하지 않았다 (%d가지). 고친 뒤 다시 친다. " +
		"먼저 돌려 보려면 같은 명령 끝에 --check 를 붙인다.",
	AddStatusDefault: "todo_status 를 안 줘서 `%s` 로 뒀다 (바꾸려면 --todo-status doing|done).",
	AddStrayWords:    "옵션 값이 아닌 낱말이 있다 : %s — 여러 값은 쉼표로 잇는다 (--sources a,b · --tags a,b)",
	ShowQueued:       "아직 색인 전이다 : %s — mem index 를 돌린 뒤에 보인다",
	CheckHeader:      "닮은 기억 %d건 (--check 라서 넣지 않았다) :",
	CheckNone:        "닮은 기억이 없다. 넣어도 된다. (--check 라서 넣지 않았다)",
	CheckNoIndex:     "색인이 없어 못 견줬다. `mem index` 를 먼저 돌려라. (--check 라서 넣지 않았다)",
	CheckSameBody:    "본문이 똑같은 기억이 이미 있다 : %s",
	JSONLBadLine:     "%d번째 줄 : %s",
	JSONLAt:          "%d번째 줄에서 걸렸다. 묶음 전체를 취소했다.",
	JSONLDone:        "%d 건을 큐에 넣었다.",
	JSONLFixed:       "고쳐 넣은 칸 : severity 별칭 %d건 · todo_status 기본값 %d건.",
	JSONLEmpty:       "표준입력에 읽을 줄이 없다.",
	BadHeadValue:     "`--head` 는 1 이상의 수여야 한다 : %s",
	ArchiveNoMemory:  "아카이브에도 그 기억이 없다 : %s",
	IndexClearBad:    "inbox/bad 에서 %d건을 지웠다.",
	IndexClearNone:   "inbox/bad 가 비어 있다. 지울 것이 없다.",
	IndexBadKept:     "inbox/bad 로 못 옮겼다. 그 자리에 그대로 뒀다 : %s",
	HookOverBudget:   "훅 시간 예산을 넘어 색인 따라잡기를 건너뛰었다.",
	BadBoolValue:     "참·거짓 옵션에 모르는 값을 줬다 : %s (true 나 false 여야 한다)",
	NotBoolValue:     "`%s` 칸은 참·거짓이어야 한다. 따옴표 친 \"true\" 나 1 은 안 받는다.",
}

// T 는 키로 문장을 찾아 인자를 끼운다. 없는 키는 키 이름을 그대로 돌려준다.
func T(key Key, args ...any) string {
	text, found := messages[key]
	if !found {
		return string(key)
	}
	if len(args) == 0 {
		return text
	}
	return fmt.Sprintf(text, args...)
}
