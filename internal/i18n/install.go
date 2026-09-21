package i18n

// install · init · undo 가 쓰는 문장이다. i18n.go 의 표를 건드리지 않고 여기서
// 덧붙인다 (설계 12-1 : 한글 리터럴은 이 패키지 밖에 두지 않는다).

// 표와 칸.
const (
	InitTableHeader Key = "init-table-header"
	InitStepStore   Key = "init-step-store"
	InstallStepExe  Key = "install-step-exe"
	InstallStepPath Key = "install-step-path"
)

// 「지금」 칸.
const (
	InitStateMissing      Key = "init-state-missing"
	InitStatePresent      Key = "init-state-present"
	InitStateMissingParts Key = "init-state-missing-parts"
	InitStateNoBlock      Key = "init-state-no-block"
	InitStateHasBlock     Key = "init-state-has-block"
	InitStateOldBlock     Key = "init-state-old-block"
	InitStateHooks        Key = "init-state-hooks"
	InitStateMemHook      Key = "init-state-mem-hook"
	InitStateUnreadable   Key = "init-state-unreadable"
	InitStateOddShape     Key = "init-state-odd-shape"
	InitStateRaced        Key = "init-state-raced"
	InitStateKeysMissing  Key = "init-state-keys-missing"
	InitStateBadConfig    Key = "init-state-bad-config"
	InitStateBadVocab     Key = "init-state-bad-vocab"
	InstallStateSelf      Key = "install-state-self"
	InstallStateSameExe   Key = "install-state-same-exe"
	InstallStateInPath    Key = "install-state-in-path"
	InstallStateNotInPath Key = "install-state-not-in-path"
	InstallStateNoPathAPI Key = "install-state-no-path-api"
	InstallStateNoBackup  Key = "install-state-no-backup"
)

// 「할 일」 칸.
const (
	InitTodoCreate         Key = "init-todo-create"
	InitTodoKeep           Key = "init-todo-keep"
	InitTodoRemove         Key = "init-todo-remove"
	InitTodoReplace        Key = "init-todo-replace"
	InitTodoManual         Key = "init-todo-manual"
	InitTodoLines          Key = "init-todo-lines"
	InitTodoHook           Key = "init-todo-hook"
	InitTodoAllow          Key = "init-todo-allow"
	InitTodoHookAllow      Key = "init-todo-hook-allow"
	InitTodoFillKeys       Key = "init-todo-fill-keys"
	InstallTodoCopy        Key = "install-todo-copy"
	InstallTodoAddPath     Key = "install-todo-add-path"
	InstallTodoRestorePath Key = "install-todo-restore-path"
	InstallTodoUndoManual  Key = "install-todo-undo-manual"
)

// 꼬리말과 안내.
const (
	InitDryRunFooter    Key = "init-dry-run-footer"
	InstallDryRunFooter Key = "install-dry-run-footer"
	InitDoneNothing     Key = "init-done-nothing"
	InitDoneCount       Key = "init-done-count"
	UndoDoneNothing     Key = "undo-done-nothing"
	UndoDoneCount       Key = "undo-done-count"
	UndoKeepsMemory     Key = "undo-keeps-memory"
	InitManualHook      Key = "init-manual-hook"
	InitPathFound       Key = "init-path-found"
	InitPathMissing     Key = "init-path-missing"
	InitSoloNote        Key = "init-solo-note"
	InstallNewTerminal  Key = "install-new-terminal"
	InstallPathBackedUp Key = "install-path-backed-up"
	InstallExeBusy      Key = "install-exe-busy"
	InstallCheck        Key = "install-check"
	InstallPathSkipped  Key = "install-path-skipped"
	InstallUndoExeNote  Key = "install-undo-exe-note"
	HookStatusMessage   Key = "hook-status-message"
)

// status --doctor 가 쓰는 점검 이름과 안내다 (설계 6-1).
const (
	DoctorHookOne      Key = "doctor-hook-one"
	DoctorHookMissing  Key = "doctor-hook-missing"
	DoctorAllow        Key = "doctor-allow"
	DoctorAllowMissing Key = "doctor-allow-missing"
	DoctorAllowShell   Key = "doctor-allow-shell"
	DoctorTrust        Key = "doctor-trust"
	DoctorTrustNote    Key = "doctor-trust-note"
	DoctorPath         Key = "doctor-path"
	DoctorDB           Key = "doctor-db"
	DoctorDBMissing    Key = "doctor-db-missing"
	DoctorDBBroken     Key = "doctor-db-broken"
	DoctorDBOK         Key = "doctor-db-ok"
	DoctorLock         Key = "doctor-lock"
	DoctorLockStuck    Key = "doctor-lock-stuck"
	DoctorLockLeftover Key = "doctor-lock-leftover"
	DoctorShadow       Key = "doctor-shadow"
	DoctorShadowNone   Key = "doctor-shadow-none"
	DoctorShadowFound  Key = "doctor-shadow-found"
)

var installMessages = map[Key]string{
	InitTableHeader: "| 무엇 | 지금 | 할 일 |\n| --- | --- | --- |",
	InitStepStore:   "Memory/ 뼈대",
	InstallStepExe:  "mem.exe",
	InstallStepPath: "사용자 PATH",

	InitStateMissing:      "없음",
	InitStatePresent:      "있음",
	InitStateMissingParts: "%d 가지 빠짐",
	InitStateNoBlock:      "mem 블록 없음",
	InitStateHasBlock:     "mem 블록 있음",
	InitStateOldBlock:     "옛 mem 블록 있음",
	InitStateHooks:        "훅 %d개 있음 (mem 아님)",
	InitStateMemHook:      "mem 훅 있음",
	InitStateUnreadable:   "JSON 을 못 읽음",
	InitStateOddShape:     "hooks 가 아는 모양이 아님",
	InitStateRaced:        "다른 프로그램이 방금 고침",
	InitStateKeysMissing:  "키 %d 개 빠짐",
	InitStateBadConfig:    "mem.toml 을 못 읽음",
	InitStateBadVocab:     "vocab.toml 을 못 읽음",
	InstallStateSelf:      "이미 그 자리에서 돌고 있음",
	InstallStateSameExe:   "같은 판이 있음",
	InstallStateInPath:    "이미 들어 있음",
	InstallStateNotInPath: "없음",
	InstallStateNoPathAPI: "PATH 를 읽지 못함",
	InstallStateNoBackup:  "되돌릴 백업이 없음",

	InitTodoCreate:         "만듦",
	InitTodoKeep:           "그대로",
	InitTodoRemove:         "뺌",
	InitTodoReplace:        "새 문안으로 갈아 끼움",
	InitTodoManual:         "손으로 (건너뜀)",
	InitTodoLines:          "%d줄 덧붙임",
	InitTodoHook:           "%d번째로 덧붙임 (기존 %d개 그대로)",
	InitTodoAllow:          "allow 규칙 %d개 넣음",
	InitTodoHookAllow:      "훅을 %d번째로 덧붙임 (기존 %d개 그대로) · allow 규칙 %d개 넣음",
	InitTodoFillKeys:       "빠진 키만 채움",
	InstallTodoCopy:        "복사함",
	InstallTodoAddPath:     "맨 앞에 넣음",
	InstallTodoRestorePath: "백업 값으로 되돌림",
	InstallTodoUndoManual:  "손으로 지워라 (돌고 있는 exe 는 못 지운다)",

	InitDryRunFooter:    "아무것도 고치지 않았다. 진짜로 하려면 --dry-run 을 빼라.",
	InstallDryRunFooter: "아무것도 고치지 않았다. 실제로 적용하려면 `mem install --apply` 를 써라.",
	InstallPathSkipped:  "PATH 는 안 건드렸다 (MEM_INSTALL_NO_PATH 가 켜져 있다).",
	InstallUndoExeNote:  "exe 는 %s 에 그대로 뒀다. 돌고 있는 파일이라 우리가 못 지운다 — 필요하면 사람이 지워라.",
	InitDoneNothing:     "이미 다 붙어 있다. 고친 것은 없다.",
	InitDoneCount:       "%d 가지를 고쳤다.",
	UndoDoneNothing:     "뗄 것이 없었다.",
	UndoDoneCount:       "%d 가지를 뗐다.",
	UndoKeepsMemory:     "Memory/ 와 .gitignore 는 그대로 뒀다. 기억은 툴보다 오래 산다. 지우려면 사람이 폴더를 지워라.",
	InitManualHook: "%s 을 못 읽어서 손대지 않았다. 아래 첫 줄을 hooks.SessionStart 배열에,\n" +
		"  둘째 줄을 hooks.SubagentStart 배열에 손으로 넣어라.\n" +
		`  {"matcher": "startup|resume|clear|compact|fork", "hooks": [{"type": "command", ` +
		`"command": "mem", "args": ["hook", "session-start"], "timeout": 10}]}` + "\n" +
		`  {"hooks": [{"type": "command", "command": "mem", ` +
		`"args": ["hook", "subagent-start"], "timeout": 10}]}`,
	InitPathFound: "PATH 에서 mem 을 찾았다 : %s",
	InitPathMissing: "PATH 에 mem 이 없다. 훅이 조용히 논다. 한 번 돌려라 : mem install\n" +
		"  (손으로 하려면 설정 > 시스템 > 정보 > 고급 시스템 설정 > 환경 변수 에서\n" +
		`   사용자 Path 맨 앞에 %USERPROFILE%\.aimemory\bin 을 넣는다.)`,
	InitSoloNote: "혼자 쓴다면 .claude/settings.json 에 \"autoMemoryEnabled\": false 를 넣어\n" +
		"  Claude 내장 기억을 꺼도 된다. 우리가 대신 끄지는 않았다 — 안전망은 사람이 정한다.",
	InstallNewTerminal:  "새 터미널을 열어야 PATH 가 잡힌다. 열고 `mem status` 로 확인해라.",
	InstallPathBackedUp: "고치기 전 PATH 를 여기에 남겼다 : %s",
	InstallExeBusy:      "%s 를 덮어쓰지 못했다. 돌고 있는 mem 을 닫고 다시 해라.",
	InstallCheck:        "확인 : 새 터미널에서 `mem status` → 저장소 자리와 판이 나오면 된 것이다.",
	HookStatusMessage:   "기억 불러오는 중",

	DoctorHookOne:      "%s 훅",
	DoctorHookMissing:  "훅이 안 붙어 있다. `mem init` 을 한 번 돌려라.",
	DoctorAllow:        "auto 모드 allow 규칙",
	DoctorAllowMissing: "규칙 %d개가 빠졌다. 없으면 mem 을 부를 때마다 분류기를 탄다. `mem init` 을 돌려라.",
	DoctorAllowShell:   "PowerShell 꼴 %d개가 없다. Windows 의 Claude Code 는 PowerShell 도구로 명령을 돌려서 Bash 꼴만으로는 안 맞는다. `mem init` 을 다시 돌려라.",
	DoctorTrust:        "폴더 신뢰",
	DoctorTrustNote:    "Claude Code 를 열고 폴더 신뢰(trust)를 눌러야 allow 규칙이 산다. 훅은 신뢰 전에도 돈다.",
	DoctorPath:         "PATH 의 mem",
	DoctorDB:           "색인",
	DoctorDBMissing:    "색인이 없다. `mem index` 를 한 번 돌려라.",
	DoctorDBBroken:     "색인을 못 읽는다. 지우고 `mem index --full` 로 다시 만들어라.",
	DoctorDBOK:         "%d건",
	DoctorLock:         "락",
	DoctorLockStuck:    "락이 오래 잡혀 있다. 도는 mem 이 없으면 다음 `mem index` 가 치운다.",
	DoctorLockLeftover: "죽은 락 잔해 %d개가 남았다. 다음 `mem index` 가 치운다.",
	DoctorShadow:       "그림자 exe",
	DoctorShadowNone:   "저장소 자리에 mem 이 없다",
	DoctorShadowFound:  "저장소 자리에 `%s` 가 있다. 설치된 것 대신 이게 먼저 잡힌다. 지우거나 tools/ 로 옮겨라.",
}

func init() {
	for key, text := range installMessages {
		messages[key] = text
	}
}

// InstallUsageName 은 mem init 이 Memory/ 에 쓰는 AI 용 요약 문서다.
const InstallUsageName = "사용법.md"

// InstallUsagePath 는 그 문서를 글로 가리킬 때 쓰는 프로젝트 상대 경로다.
const InstallUsagePath = "Memory/" + InstallUsageName

// InstallLogName 은 도구가 한 줄씩 덧붙이는 시간순 기록이다 (설계 결정 27).
const InstallLogName = "log.md"

// 규칙 블록과 .gitignore 블록을 다시 알아보게 하는 표식이다.
const (
	InstallBlockOpen     = "<!-- >>> mem >>> -->"
	InstallBlockClose    = "<!-- <<< mem <<< -->"
	InstallIgnoreOpen    = "# >>> mem >>>"
	InstallIgnoreClose   = "# <<< mem <<<"
	InstallRulesHeading  = "## 기억 저장소 (mem)"
	InstallAttributeName = ".gitattributes"
)

// InstallRulesBlock 은 AGENTS.md 끝에 붙는 규칙 여섯 줄이다 (설계 5-5).
// 셋째 줄이 제일 중요하다 — Claude 내장 auto memory 가 2026-02 부터 기본으로
// 켜져 있어서, 안 가르면 같은 것이 두 곳에 갈려 쌓인다 (설계 결정 25).
const InstallRulesBlock = InstallBlockOpen + "\n" +
	InstallRulesHeading + "\n\n" +
	"- 지난 결정·기록·할 일·조사 결과는 `mem` 에 있다. 세션을 열면 훅이 요약을 밀어 넣는다.\n" +
	"- 찾을 때 `mem search <낱말> <낱말>`, 남길 때 `mem add --type … --title … --tags … --sources …`.\n" +
	"- **팀이 나눠 볼 결정·기록·조사 결과는 `mem` 에 남긴다. 개인 취향·말투는 Claude 내장 기억에 둔다.**\n" +
	"- 결정을 뒤집을 때는 새로 쓰지 말고 `mem set <옛id> --by-new` 로 덮는다.\n" +
	"- 확신 없이 자동으로 남기는 기억은 `mem add --hold` 로 보류한다. 사람이 보기 전엔 검색에 안 뜬다.\n" +
	"- 작업 완료 보고를 쓰기 전에 '남길 것 있나' 를 한 번 본다. 자세한 건 `" + InstallUsagePath + "`.\n" +
	InstallBlockClose + "\n"

// InstallIgnoreLines 는 .gitignore 에 들어갈 줄이다 (설계 6-3 의 「제외」 칸).
var InstallIgnoreLines = []string{
	"Memory/index.db",
	"Memory/index.db-wal",
	"Memory/index.db-shm",
	"Memory/index.lock",
	"Memory/index.lock.stale.*",
	// vectors.bin 은 색인처럼 파생물이다 — 20k 에서 수십 MB 라 커밋하면 안 된다
	// (결정 12·13).
	"Memory/vectors.bin",
	"Memory/inbox/",
	// 저장소 안에 떨어진 mem.exe 는 **그림자 exe** 다 — PATH 보다 먼저 잡혀
	// 낡은 판이 조용히 돈다. 커밋까지 되면 팀 전체가 그것을 쓴다
	// (설계 10절 · 보안연동 시험 M-4).
	"mem.exe",
	"Memory/local/",
	".claude/*.mem-bak",
	".gemini/*.mem-bak",
}

// InstallAttributeLines 는 .gitattributes 두 줄이다. 기억 파일은 autocrlf 와
// 상관없이 LF 로 두고, 아카이브는 gzip 이라 텍스트로 다루지 않는다.
// v0.1 이 적었던 `merge=union` 은 뺐다 — gzip 에는 원리상 안 먹는다 (설계 6-3).
var InstallAttributeLines = []string{
	"Memory/store/**/*.md      text eol=lf",
	"Memory/archive/*.jsonl.gz binary",
}

// InstallLogDoc 은 log.md 의 첫 줄이다. 도구가 그 아래에 한 줄씩 덧붙인다.
const InstallLogDoc = `# 기억 기록

들어온 기억 · 덮인 결정 · 접힌 기억을 도구가 시간순으로 한 줄씩 적는다.
사람이 손으로 고쳐도 되지만, 도구는 늘 맨 뒤에만 덧붙인다.
`

// InstallUsageDoc 은 AI 가 읽는 요약이다. 있으면 덮어쓰지 않는다.
// 「무엇을 안 넣나」 와 「그대로 따라 칠 수 있는 예시 하나」 가 이 문서의 핵심이다 —
// 자리표시자만 있으면 1년차가 --summary 를 얼마나 길게 쓸지 감을 못 잡는다.
//
// **여기 적힌 예시는 실제로 돌려서 통과한 것만 적는다.** 갓 `mem init` 한
// 빈 프로젝트에서 아래 두 명령을 그대로 쳐서 종료 코드 0 · 경고 0 을 확인했다.
const InstallUsageDoc = `# mem 사용법 (AI 용)

## 문서와 mem 의 역할
- 기억(지난 결정·기록·할 일·조사 결과)은 문서가 아니라 mem 에 넣는다.
- 문서로 남는 것은 셋뿐 : README · 규칙·컨벤션·명세 가이드 · 자료 파일(testdata 등).
- **팀이 나눠 볼 것만 mem 에 넣는다.** 개인 취향·말투는 Claude 내장 기억(auto memory)에 둔다.
  mem 은 git 에 들어가 팀이 나눠 쓰고, 내장 기억은 그 기계 안에만 있다.
- 개인용 전역 저장소와 --global 옵션은 없앴다. 그 자리를 Claude 내장 기억이 대신한다.

## 처음 한 번 — 이 저장소의 이름 정하기
- ` + "`mem tags --add-scope <이름>`" + ` 로 **이 저장소의 scope 를 하나 정한다** (예 : ` + "`unity-build`" + `).
- 정하기 전까지는 목록 밖 태그·scope 를 **경고로만** 알린다. 첫날부터 다 막히지 않게 한 것이다.
- 정하고 나면 목록 밖은 **거절**이다. 늘릴 때는 ` + "`mem tags --add <태그>`" + ` · ` + "`mem tags --add-scope <이름>`" + `.
- 지금 무엇이 목록 밖인지는 ` + "`mem tags --check`" + ` 가 세어 주고, 늘리는 명령까지 같이 찍어 준다.

## 안 넣는 것
- 코드를 보면 아는 것 (폴더 구조·의존성 목록·함수 이름) — git 이 안다.
- 이번 세션에만 쓸 임시 메모 — 다음 세션에 쓸모가 없으면 기억이 아니다.
- 대화 기록 전문·긴 코드 조각 — ` + "`파일:줄`" + ` 포인터로 대신한다.
- 비밀값·개인정보 — ` + "`mem add`" + ` 가 그 자리에서 막는다 (**종료 코드 4**). 사적 경로는 경고만 붙고 막지 않는다.
- 이미 있는 기억과 같은 얘기 — 중복 검사가 기본으로 켜져 있어 닮으면 거절한다.
- "~해라" 꼴 지시문 — 기억은 자료지 지시가 아니다.
- 넣는 기준 한 줄 : 코드에서도 규칙 문서에서도 알 수 없고, 다음 세션에 쓸모 있는 것.

## 종류 7개
- ` + "`todo`" + ` 다음에 할 일 · ` + "`history`" + ` 끝난 일의 경위·공수 · ` + "`issue`" + ` 증상→원인→해결
- ` + "`caution`" + ` 다시 밟지 말 함정 · ` + "`decision`" + ` 뒤집을 수 있는 선택 · ` + "`howto`" + ` 하는 법
- ` + "`fact`" + ` 잘 안 바뀌는 환경 사실 (회선·장비·계정 같은 것). 근거를 꼭 달고 한 건에 하나만 담는다.
- 종류를 늘리려면 ` + "`Memory/vocab.toml`" + ` 에 ` + "`[type.<이름>]`" + ` 절을 적는다.

## 넣을 때 — mem add
- ` + "`mem add --type <종류> --title \"제목\" --summary \"한 줄\" --tags a,b --scope <범위> --sources file:… --body \"자세히\"`" + `
- **필수 칸이 늘었다** : ` + "`--title`" + `(6~40자) · ` + "`--sources`" + `(` + "`decision`·`issue`·`caution`·`fact`" + ` 는 1개 이상).
  ` + "`--author`" + ` 는 안 주면 도구 이름이 자동으로 들어간다. 사람이 확인한 기억이면 ` + "`--author human:<아이디>`" + ` 로 밝혀 준다.
- ` + "`--sources`" + ` 는 접두 다섯 중 하나로 시작한다 : ` + "`file:` `commit:` `url:` `mem:` `note:`" + `.
  ` + "`note:`" + ` 만 대면 경고가 붙는다 — 기계가 낡았는지 볼 수 없어서다.
  **근거 여럿은 쉼표로 나눈다** (` + "`--sources file:a,commit:b`" + `). 공백으로 이으면 뒤가 통째로 사라진다.
- ` + "`--title`" + ` 은 요약을 베끼지 않는다. 요약 앞 20자와 같으면 거절한다.
- ` + "`--summary`" + ` 는 30~120자, ` + "`--tags`" + ` 는 2~5개.
  **태그는 ` + "`Memory/vocab.toml`" + ` 표준 목록에서 고른다.** 지금 무엇이 표준인지는 ` + "`mem tags --list`" + ` 가 보여준다
  (` + "`--json`" + ` 도 된다). 늘리려면 ` + "`mem tags --add <태그>`" + `,
  별칭(` + "`docs`→`doc`" + ` 같은 것)은 도구가 조용히 바꿔 준다.
- ` + "`--scope`" + ` 는 **툴·부품 이름 통째 하나**다. 주제를 붙이지 않는다
  (` + "`unity-build`" + ` 는 되고 ` + "`unity-build-캐시`" + ` 는 아니다). 이것도 표준 목록을 본다.
- ` + "`todo`" + ` 는 ` + "`--todo-status open|doing|done`" + ` — **안 주면 ` + "`open`" + ` 으로 들어간다.**
- ` + "`issue`·`caution`" + ` 은 ` + "`--severity low|mid|high`" + ` 까지 필수다. ` + "`medium`" + ` 이라 써도 ` + "`mid`" + ` 로 바꿔 받는다.
- **한 건에 결정 하나.** 결정 여러 개를 한 건에 넣으면 거절한다 (하나가 낡으면 나머지도 못 믿는다).
- 본문은 **실질 3줄 이상**이라야 한다. 빈 줄·제목 줄은 안 센다.
- 본문은 **첫 줄이 결론.** 120줄을 넘으면 경고, **300줄을 넘으면 거절**이다.
- ` + "`issue`" + ` 는 본문에 ` + "`## 증상`" + ` 과 ` + "`## 해결`" + ` 두 절을 **각 2줄 이상** 둔다. ` + "`howto`" + ` 는 번호 목록으로.
- 본문이 길면 ` + "`--stdin`" + `. 한 세션에 보통 0~3건.
- ` + "`--pin`" + ` 은 세션마다 꼭 보여야 하는 것에만. 저장소 전체 10건 안쪽으로 아껴 쓴다.
- ` + "`--check`" + ` 는 **미리보기다** — 관문만 돌리고 무슨 일이 있어도 저장하지 않는다.
- **끝줄이 늘 결과다** : ` + "`저장됨 : <id>`" + ` 면 들어갔고 ` + "`거절됨`" + ` 이면 아무것도 안 들어갔다.
  ` + "`--json`·`--jsonl`" + ` 은 빼고 — 그 둘은 기계가 읽는 꼴이다.
- 종료 코드 : 0 통과(경고가 붙어도 들어간다) · 1 사용법 잘못 · 2 품질 거절 · 3 저장소 없음 · 4 보안 차단.
  **경고는 막지 않는다.** 경고가 뜨고 id 가 찍혔으면 들어간 것이다.

### 이렇게 쓴다 (그대로 따라 쳐도 된다)
` + "```" + `
mem tags --add-scope unity-build

mem add --type issue --scope unity-build --severity high \
  --title "IL2CPP 빌드가 3배 느려짐" \
  --summary "안드로이드 IL2CPP 빌드가 12분에서 36분으로 늘었다. Gradle 캐시가 꺼져 있던 탓이다" \
  --tags build,unity \
  --sources file:ProjectSettings/ProjectSettings.asset \
  --body "결론 : Gradle 캐시를 켜면 12분으로 돌아온다.

## 증상
빌드 시간이 12분에서 36분으로 늘었다.
오류는 안 나고 조용히 느려지기만 한다.
CI 와 사람 기계에서 똑같이 재현된다.

## 해결
Player Settings 에서 Gradle 캐시를 다시 켰다.
켜고 다시 재니 12분으로 돌아왔다.
캐시를 끈 것은 지난 릴리스 때 디스크를 비우려던 조치였다."
` + "```" + `

## 찾을 때 — mem search
- ` + "`mem search 낱말 낱말`" + ` — 띄어서, 2글자 이상. 전부(AND)로 0건이면 알아서 완화해 다시 찾는다.
- 조건만 줘도 된다 : ` + "`mem search --type decision --scope unity-build --since 30d`" + `
- 왜 그 결과가 나왔는지는 ` + "`--explain`" + `, 태그·scope 목록은 ` + "`--facet`" + `.
- 본문은 ` + "`mem show <id>`" + `.

## 고칠 때 — mem set
- 본문과 요약은 ` + "`set`" + ` 으로 못 고친다. 파일을 손으로 고치고 ` + "`mem index`" + ` 를 돌린다.
- **결정이 뒤집혔으면 새로 쓰지 말고 덮는다** : 새 기억을 넣고 ` + "`mem set <옛id> --by <새id>`" + `.
  덮을 새 기억이 아직 없으면 ` + "`mem set <옛id> --by-new`" + ` 로 검토 큐에 올려 둔다.
  덮은 사실은 ` + "`Memory/log.md`" + ` 에 남는다.
- ` + "`--todo-status done`" + ` 은 todo 를 끝냄으로 바꾼다. ` + "`--stale-after`" + ` 는 검토 예약이다.

## 다 넣고 나서
- ` + "`mem index`" + ` 로 실제로 들어갔는지 본다. ` + "`못 씀`" + ` 이 0 이 아니면 규격을 어긴 것이다.
- ` + "`mem lint`" + ` 로 문서 품질을 본다. **오류가 0 이어야 한다.** 경고는 봐 가며 고친다.
- ` + "`mem review`" + ` 는 사람이 볼 큐다 — 만료·모순 후보·낡음 후보·차가운 기억. 판정은 사람이 한다.
- 쌓인 실패 파일은 ` + "`mem index --clear-bad`" + ` 로 치운다 (지우기 전에 목록을 보여준다).
- 시간순 기록은 ` + "`mem status --log`" + `, 환경 점검은 ` + "`mem status --doctor`" + `.

## 오류가 나면
- 작업을 멈추지 않는다. 그 기억은 포기하고 보고에 '기억 못 넣음' 한 줄만 적는다.

자세한 건 ` + "`mem help`" + `.
`
