package i18n

// 명령 16개의 한글 도움말이다 (설계 6-1). `mem help <명령>` 과
// `mem <명령> --help` 가 같은 문장을 쓴다.

// HelpTopics 는 도움말이 있는 명령 이름을 설계 표 차례대로 준다.
func HelpTopics() []string {
	return []string{
		"install", "init", "add", "set", "search", "show", "hook",
		"index", "migrate", "gc", "lint", "review", "tags", "eval", "status", "version", "help",
	}
}

// CommandHelp 는 명령 하나의 도움말이다. 없는 이름이면 두 번째 값이 거짓이다.
func CommandHelp(name string) (string, bool) {
	text, found := commandHelp[name]
	return text, found
}

var commandHelp = map[string]string{
	"install": `mem install — exe 를 사용자 폴더에 복사하고 PATH 에 넣는다 (기계마다 한 번)

쓰는 법 : mem install [옵션]
  --apply       실제로 적용한다. 안 주면 계획 표만 보여주고 아무것도 안 고친다
  --dry-run     무엇을 할지만 보여준다 (--apply 없을 때가 기본이라 안 줘도 같다)
  --no-path     PATH 는 안 건드린다 (--apply 와 같이 써야 뜻이 있다)
                환경 변수 MEM_INSTALL_NO_PATH=1 로도 막는다 (시험·스크립트용)
  --no-embed    의미 검색(모델·런타임)을 빼고 낱말 검색만 깐다. 기본은 넣는 것이다
  --bundle <폴더> 인터넷 대신 그 꾸러미 폴더에서 모델·런타임을 가져온다
  --model <이름>  쓸 모델 이름 (안 주면 기본 모델)
  --check       무엇이 깔렸고 해시가 맞는지만 본다. 아무것도 안 고친다
  --undo        install 이 고친 사용자 PATH 를 path-backup.txt 값으로 되돌린다
                (--apply 와 같이 줘야 진짜로 되돌린다). exe·모델은 안 지운다
종료 코드 : 0 정상 · 1 사용법 잘못 · 2 --check 가 모자란 것을 찾음`,

	"init": `mem init — 이 프로젝트에 기억 저장소를 붙인다 (여러 번 돌려도 안전)

쓰는 법 : mem init [옵션]
  --repo <폴더>  저장소를 만들 자리를 직접 가리킨다
  --dry-run      무엇을 할지만 보여준다
  --no-hook      settings.json 훅은 안 붙인다 (SessionStart·SubagentStart 둘 다)
  --no-subagent-hook  SubagentStart 훅만 뺀다. SessionStart 는 그대로 붙인다
  --gemini       GEMINI.md 에도 규칙 세 줄을 붙인다
  --solo         혼자 쓰는 사람이다. 내장 auto memory 를 끄자고 권하기만 한다
  --undo         init 이 붙인 것을 떼어낸다
종료 코드 : 0 정상 · 1 사용법 잘못`,

	"add": `mem add — 기억 한 건을 관문에 걸고 통과하면 쓰기 큐에 넣는다

쓰는 법 : mem add --type <종류> --title <제목> --summary <한 줄> --tags a,b --scope <범위> [옵션]
  --type <종류>      todo history issue caution decision howto fact (필수)
                     vocab.toml 의 [type.<이름>] 으로 늘릴 수 있다
  --title <제목>     6~40자 한 줄 (필수). 요약 앞머리를 그대로 베끼면 거절이다
  --summary <한 줄>  30~120자 (필수)
  --tags a,b         표준 목록 안의 소문자 태그 2~5개 (필수). 별칭은 자동으로 바뀐다
                     목록을 늘리려면 mem tags --add <태그>
  --scope <범위>     표준 목록 안의 툴·부품 이름 통째 하나 (필수)
                     목록을 늘리려면 mem tags --add-scope <이름>
  --sources a,b      근거. file: commit: url: mem: note: 로 시작한다
                     (decision·issue·caution·fact 는 하나 이상 필수)
                     **여러 개는 쉼표로 잇는다.** 공백으로 이으면 뒤가 사라진다
  --author <누가>    human:<아이디> · <도구>/<버전> · hook:<이름> (기본 mem/판)
  --body <본문>      본문. 안 주면 표준입력을 읽는다
                     **실질 3줄 이상**이라야 한다. howto 는 1. 2. 번호 차례로 쓴다
  --stdin            본문을 표준입력에서 읽는다 (--body 를 안 줄 때와 같다)
  --date <날짜>      기억의 날짜 (기본 오늘)
  --invalid-at <날짜> 이 날부터 이 기억은 무효다
  --todo-status <값> open doing done. 안 주면 open 이다
  --severity <세기>  issue·caution 일 때 필수 : low mid high (medium 도 받아 mid 로 바꾼다)
  --by <옛id>        그 결정을 이 기억이 덮는다 (옛 기억에 덮임 표시를 단다)
  --new              닮은 기억이 있어도 정말 다른 주제다
  --hold             보류로 넣는다. 검색·훅에 안 뜨고 사람이 mem review --promote 로 연다
                     AI 가 확신 없이 자동 저장할 때 쓴다
  --importance <1~5> 중요도 (기본 3)
  --stale-after <날짜> 이 날 지나면 다시 보자고 예약한다
  --link a,b         이어지는 기억 id
  --pin              접기·정리에서 뺀다
  --check            미리보기. 관문만 돌리고 무슨 일이 있어도 저장하지 않는다
  --json             판정을 한 줄 JSON 으로 준다
  --jsonl            여러 건을 표준입력에서 한 줄에 하나씩 받는다 (한 줄이라도 걸리면 전부 취소)
  --repo <폴더>      저장소를 직접 가리킨다
별칭 : --links(=--link) · --source(=--sources) · --status(=--todo-status)
끝줄이 늘 결과다 — 「저장됨 : <id>」 면 들어갔고 「거절됨」 이면 아무것도 안 들어갔다
(--json·--jsonl 은 빼고. 그 둘은 기계가 읽는 꼴이라 끝줄이 결과 줄이 아니다).
경고는 저장을 막지 않는다 — 경고가 뜨고 id 가 찍혔으면 들어간 것이다.
표준 scope 를 하나도 안 정한 저장소에서는 목록 밖 태그·scope 를 경고로만 알린다
(mem tags --add-scope 로 첫 이름을 정하면 그때부터 거절이다).
종료 코드 : 0 정상 · 1 사용법 잘못 · 2 품질 관문 거절 · 3 저장소 없음 · 4 보안 차단`,

	"set": `mem set — 있는 기억의 도구 칸을 고친다 (본문·요약은 파일을 고치고 mem index)

쓰는 법 : mem set <id> [옵션]
  --by <새id>        이 기억을 그 기억이 덮었다 (superseded_by 와 invalid_at 을 같이 채운다)
                     덮은 뒤 이 기억을 근거로 삼은 기억이 몇 건인지 알려준다 (mem review --kind basis)
  --by-new           덮을 새 기억이 아직 없다. 오늘부터 검토 큐에 올리고 다음 명령을 알려준다
  --todo-status <값> open doing done
  --stale-after <날짜> 다시 볼 날을 예약한다
  --pin / --unpin    고정을 켜고 끈다
  --done             todo_status 를 done 으로 바꾼다
  --summary --title --tags --scope --severity --importance --invalid-at
  --links a,b        이어지는 기억 id 를 통째로 갈아 끼운다
  --link <id>        이어지는 기억 id 를 하나 더한다 (있던 것은 그대로)
  --body <본문> / --stdin     본문을 통째로 갈아 끼운다
  --repo <폴더>      저장소를 직접 가리킨다
별칭 : --status(=--todo-status)
종료 코드 : 0 정상 · 1 사용법 잘못 · 2 검사 실패 · 3 저장소 없음 · 4 보안 차단`,

	"search": `mem search — 기억을 찾는다. 질의가 없으면 조건에 맞는 목록만 준다

쓰는 법 : mem search [낱말 …] [옵션]
  --type --scope --tag --status --severity   조건을 좁힌다
  --pinned                        고정한 것만
  --since <날짜>                  이 날 뒤의 것만
  --limit <수>                    몇 건까지
  --budget <토큰>                 토큰 예산에 맞춰 자른다
  --all                           낡은 것까지 다 본다
  --include-held                  보류 기억(add --hold)까지 같이 본다
  --facet scope|tag               종류·범위별 건수를 같이 준다
  --explain                       점수가 어떻게 나왔는지 보여준다
  --json                          한 줄 JSON (0건이면 [])
  --no-index                      시작할 때 색인을 안 따라잡는다
  --repo <폴더>                   저장소를 직접 가리킨다
종료 코드 : 0 정상 · 1 사용법 잘못 · 3 저장소 없음`,

	"show": `mem show — id 로 기억 한 건의 본문을 꺼낸다

쓰는 법 : mem show <id> [옵션]
  --head <수>       앞에서 몇 줄만
  --raw             중화 없이 원문 그대로 (기본은 주입용 중화를 거친다)
  --from-archive    아카이브에서 찾는다
  --no-index        시작할 때 색인을 안 따라잡는다
  --json            한 줄 JSON (used_by 도 같이 준다)
  --repo <폴더>     저장소를 직접 가리킨다
「나를 근거로 삼은 기억」 절이 뒤에 붙는다. 0건이면 안 찍고, 색인이 없으면 안 찍는다.
그 값은 색인이 만든 파생값이라 기억 파일에는 안 들어간다.
종료 코드 : 0 정상 · 1 사용법 잘못 · 3 저장소 없음`,

	"hook": `mem hook — 훅 진입점. 훅 JSON 을 표준입력으로 받는다 (사람이 직접 칠 일은 없다)

쓰는 법 : mem hook <이벤트> [--dry-run] [--json] [--budget <토큰>]
이벤트는 둘이다 : session-start (세션 시작) · subagent-start (서브에이전트 시작).
  subagent-start 는 세션 블록과 같은 요약에, 시작·끝에 무엇을 하는지 적은 줄과
  쓸 수 있는 scope 목록을 맨 위에 얹는다. 상한은 [hook] subagent_max_bytes ·
  [budget] subagent 이고, [hook] subagent_skip 에 든 agent_type 은 건너뛴다.
  SubagentStop 은 안 다룬다 — 걸어 놔도 조용히 종료 0 이다.
  --dry-run        만든 블록을 감싸지 않고 그대로 찍는다
  --json           주입 대신 잰 값(바이트·토큰·밀리초)을 찍는다
  --budget <토큰>  이 한 번만 쓸 토큰 예산 (안 주면 계기별 기본값)
저장소는 훅 입력 JSON 의 cwd 가 고른다. **--repo 는 안 받는다.**
JSON 이 들어왔는데 cwd 가 비면 (어느 저장소인지 몰라) 아무것도 안 열고 빈 출력으로 끝난다.
표준입력이 아예 비면 (JSON 자체가 없으면) 사람이 손으로 칠 때처럼 지금 폴더를 연다.
모르는 옵션은 stderr 로만 알리고 무시한다 — 훅은 종료 0 이라야 한다.
종료 코드 : 늘 0 (훅은 무슨 일이 있어도 세션을 막지 않는다.
             사람이 이벤트 이름을 잘못 친 것만 1 이다)`,

	"index": `mem index — 새 기억을 승격하고 바뀐 파일을 색인한다

쓰는 법 : mem index [옵션]
  --full        처음부터 다시 만든다
  --clear-bad   inbox/bad 에 쌓인 실패 파일을 보여주고 지운다
  --gc        정리도 같이 한다
  --no-gc     정리 조건 검사도 건너뛴다
  --quiet     아무 말도 안 한다
  --verify    색인이 파일과 맞는지 검사한다
  --json      한 줄 JSON (센 수만 담는다. 비밀정보 값은 안 찍고 건수만 준다)
  --repo <폴더>  저장소를 직접 가리킨다
종료 코드 : 0 정상 · 2 색인 못 한 파일이 있다 · 3 저장소 없음 · 4 비밀정보로 막힌 것이 있다`,

	"migrate": `mem migrate — 옛 규격(판 1) 기억을 지금 규격(판 2)으로 옮긴다

쓰는 법 : mem migrate [옵션]
  (기본)      --dry-run. 무엇을 고칠지만 보여주고 아무것도 안 고친다
  --apply     진짜로 고친다. 고치기 전에 원본을 archive/<날짜>-migrate 에 통째로 남긴다
  --restore   그 아카이브에서 되돌린다
  --author <아이디>  옛 source 가 user 인 기억의 사람 아이디 (기본은 git user.name)
  --json      한 줄 JSON
  --repo <폴더>  저장소를 직접 가리킨다
자동으로 못 채우는 것은 표로 찍고 mem review 로 넘긴다. 억지로 안 채운다.
한 칸이 막혀도 나머지 칸은 옮긴다. 막힌 칸은 「칸별로만 옮길 것」 표에 적는다.
종료 코드 : 0 정상 · 1 사용법 잘못 · 2 되돌릴 것 없음 · 3 저장소 없음 · 4 비밀정보 · 6 락을 못 얻음`,

	"gc": `mem gc — 오래된 기억을 접고 아카이브로 옮긴다

쓰는 법 : mem gc [옵션]
  --fold <id>     그 기억 하나를 손으로 접는다 (쉼표로 여러 개). 바로 접는다
  --restore <id>  접은 본문을 아카이브에서 되돌린다 (쉼표로 여러 개)
  --dry-run       무엇을 옮길지만 보여준다 (--fold 에도 붙일 수 있다)
  --json          한 줄 JSON
  --repo <폴더>   저장소를 직접 가리킨다
--fold 는 --dry-run 없이 바로 접는다. 되돌리려면 mem gc --restore <id> 를 친다.
--fold · --restore 를 주면 나이로 고르는 자동 정리는 안 돈다. 파일은 하나도 안 지운다.
종료 코드 : 0 정상`,

	"lint": `mem lint — 기억 문서의 품질을 검사한다

쓰는 법 : mem lint [옵션]
  --fix          고칠 수 있는 것은 고친다
  --json         한 줄 JSON
  --rule <이름>  그 규칙 하나만 돌린다
  --synonym      0건이 자꾸 나는 낱말(동의어 후보)을 같이 찍는다
  --no-git       git 을 안 부른다 (git 이 없거나 느린 자리에서)
  --repo <폴더>  저장소를 직접 가리킨다
종료 코드 : 0 정상 · 1 사용법 잘못 · 2 오류 등급 지적이 있다 · 3 저장소 없음`,

	"review": `mem review — 사람이 판정할 것만 모아 보여준다 (도구는 판정하지 않는다)

쓰는 법 : mem review [옵션]
  --kind <갈래>   expired(다시 볼 날 지남) · basis(근거가 죽음) · conflict(모순 후보)
                  stale(낡음 후보) · value(남길 값이 있나) · cold(차가움)
                  link(링크 후보) · title(제목 없음)
                  held(승격 대기 — add --hold 로 들어온 것)
                  쉼표로 여러 개를 준다
  --limit <수>    갈래마다 몇 줄까지 (기본 20)
  --promote <id>  자동으로 만들어진 기억(머리말 review: true)을 사람이 승격한다
                  **이 옵션만 auto 모드 allow 규칙 밖이라 승인 창이 뜬다**
  --json          한 줄 JSON
  --repo <폴더>   저장소를 직접 가리킨다
종료 코드 : 0 정상 · 1 사용법 잘못 · 3 저장소 없음`,

	"tags": `mem tags — 태그·scope 표준 목록을 보고 손본다

쓰는 법 : mem tags [옵션]
  --list                    표준 태그·scope 를 그대로 본다 (안 쓴 태그까지 다 나온다. --json 도 된다)
  --add <태그[=상위]>       표준 태그 목록에 넣는다
  --add-scope <이름>        표준 scope 목록에 넣는다
  --alias <별칭=표준>       태그 별칭을 넣는다. add·lint 가 조용히 바꿔 준다
  --alias-scope <별칭=표준> scope 별칭을 넣는다
  --rename <옛=새>          기억 파일의 태그를 바꾼다. **미리보기가 기본**이고 --apply 를 줘야 고친다
  --check                   표준 밖·못 쓰는 태그와 scope 를 세고 늘리는 명령을 알려준다
  --suggest                 동의어·대표말 후보를 제안한다. **아무것도 안 고친다**
  --apply                   --rename 을 진짜로 적용한다 (기본은 미리보기)
  --dry-run                 --rename 미리보기 (--apply 를 안 준 것과 같다)
  --json                    한 줄 JSON
  --repo <폴더>             저장소를 직접 가리킨다
표준 scope 가 하나도 없는 동안은 목록 밖 태그·scope 가 경고로만 뜬다.
--add-scope 로 첫 이름을 정하는 순간 그때부터 거절이다 — 그 전에 목록을 다듬어라.
종료 코드 : 0 정상 · 1 사용법 잘못 · 2 표준 밖 태그·scope 가 있다 · 6 락을 못 얻음`,

	"eval": `mem eval — 골든셋으로 검색 품질과 문서 품질을 잰다

쓰는 법 : mem eval [옵션]
  --quality     품질 규칙이 실제로 듣는지 잰다 (정밀도·재현율·G6 판정)
  --tune-dup    중복 문턱 곡선을 그리고 권장값을 고른다
  --json        한 줄 JSON
  --hard-only   어려운 문제만 돌린다
  --search      검색 품질만 잰다 (--quality·--tune-dup 을 안 준 것과 같다)
  --no-index    시작할 때 색인을 안 따라잡는다
  --golden <파일>  골든셋을 직접 가리킨다
  --repo <폴더>    저장소를 직접 가리킨다
종료 코드 : 0 정상 · 1 사용법 잘못 · 2 합격선 미달 · 3 저장소 없음 또는 골든셋이 없어 잴 것이 없다`,

	"status": `mem status — 저장소 자리·건수·색인 건강을 보여준다

쓰는 법 : mem status [옵션]
  --quality   품질 지표 S01~S06
  --db        색인 표별 크기 (부피가 어디서 나오는지)
  --doctor    환경 점검 — 훅·allow 규칙·PATH·DB·락 (v0.1 의 mem doctor)
  --log       Memory/log.md 를 본다
  --embed     의미 검색이 켜졌는지 · 모델·런타임·벡터 상태를 본다
  --since <날짜>  --log 를 그 날부터 자른다
  --json      한 줄 JSON
  --repo <폴더>   저장소를 직접 가리킨다
종료 코드 : 0 정상 · 2 검사 실패(--doctor·--embed 가 모자란 것을 찾음) · 3 저장소 없음`,

	"version": `mem version — 이 실행 파일의 판·빌드한 커밋·빌드 시각을 찍는다

쓰는 법 : mem version
  bin\ 은 git 에 안 올라간다. 소스를 새로 받은 뒤에는 .\build.ps1 로 다시 빌드한다 —
  안 그러면 옛 exe 가 그대로 남아 「고쳤는데 그대로다」가 된다.
  커밋·시각이 dev 면 build.ps1 없이 go build 로 만든 것이다.
  커밋 뒤에 -dirty 가 붙으면 그 커밋에 없는 변경(cmd·internal·go.mod)까지 넣고 빌드한 판이다.
종료 코드 : 0 정상`,

	"help": `mem help — 한글 도움말을 보여준다

쓰는 법 : mem help [하위명령]
  mem help          명령 목록
  mem help add      add 하나의 도움말 (mem add --help 와 같다)
종료 코드 : 0 정상 · 1 모르는 명령`,
}
