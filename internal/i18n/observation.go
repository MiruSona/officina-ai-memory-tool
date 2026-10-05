package i18n

// 모음 기억(observation · B1)과 `mem consolidate` 가 쓰는 말이다 (자동쌓기설계 3절).

const (
	BadBasisHash Key = "bad-basis-hash"
	StaleMark    Key = "stale-mark"
	BasisMore    Key = "basis-more"

	ConsolidateUsage    Key = "consolidate-usage"
	ConsolidateNoLLM    Key = "consolidate-no-llm"
	ConsolidateHead     Key = "consolidate-head"
	ConsolidateGroup    Key = "consolidate-group"
	ConsolidateMember   Key = "consolidate-member"
	ConsolidateMoreMem  Key = "consolidate-more-member"
	ConsolidateNone     Key = "consolidate-none"
	ConsolidateNoVector Key = "consolidate-no-vector"
	ConsolidateDryRun   Key = "consolidate-dry-run"
	ConsolidateApplied  Key = "consolidate-applied"
	ConsolidateLog      Key = "consolidate-log"
	ConsolidateBadRule  Key = "consolidate-bad-rule"
	ConsolidateTooBig   Key = "consolidate-too-big"
	ConsolidateUnpromo  Key = "consolidate-unpromoted"
	ObsCiteObservation  Key = "obs-cite-observation"
)

var observationMessages = map[Key]string{
	BadBasisHash: "basis_hash 가 틀렸다 : %q — 소문자 16진 8자다. 도구가 쓰는 칸이라 손으로 고치지 않는다.",
	StaleMark:    "[낡음]",
	BasisMore:    "… 외 %d건",

	ConsolidateUsage:    "쓰는 법 : mem consolidate --plan | --apply [옵션]. `mem help consolidate` 를 본다.",
	ConsolidateNoLLM:    "`--llm` (생성 LLM 글 · B2) 은 아직 없다. 지금은 코드가 조립하는 카드(--apply)만 쓴다.",
	ConsolidateHead:     "묶음 %d개 — 사슬 %d · 링크 %d · 의미 %d (새로 %d · 다시 %d · 그대로 %d) · %.0fms",
	ConsolidateGroup:    "%d. [%s] %s · %d건 · %s%s",
	ConsolidateMember:   "     · %s  %s  %s (구성원)",
	ConsolidateMoreMem:  "     … 외 %d건",
	ConsolidateNone:     "묶을 것이 없다.",
	ConsolidateNoVector: "의미 무리는 건너뛴다 — 벡터가 없다 (mem install 로 모델을 깔고 mem index).",
	ConsolidateDryRun:   "목록만 찍었다. 카드를 쓰려면 --apply 를 준다 (새로·다시 묶음만 쓴다).",
	ConsolidateApplied:  "카드 %d건을 큐에 넣었다 (새로 %d · 다시 %d). 되돌리기 : mem auto undo --origin card --apply",
	ConsolidateLog:      "카드 %d건 (새로 %d · 다시 %d)",
	ConsolidateBadRule:  "--rule 은 chain · link · meaning 중 하나다 : %q",
	ConsolidateTooBig:   "너무 큰 링크 무리 %d개는 건너뛰었다 (12건 넘음).",
	ConsolidateUnpromo: "카드 큐 %d건이 store 까지 못 갔다 (inbox 에 남음). `mem index` 로 먹인 뒤 --plan 을 다시 본다 — " +
		"그 전에 --apply 를 또 돌리면 카드가 겹칠 수 있다.",
	ObsCiteObservation: "근거 `mem:%s` 가 모음 기억이다. 모음 기억은 원본 기억만 근거로 삼는다",
}

func init() {
	for key, text := range observationMessages {
		messages[key] = text
	}
	commandHelp["consolidate"] = `mem consolidate — 여러 기억을 모음 기억(observation) 카드로 묶는다 (B1 · LLM 0)

쓰는 법 : mem consolidate --plan | --apply [옵션]
  --plan          묶음 목록만 찍는다. 아무것도 안 쓴다 (--apply 가 없을 때 기본)
  --apply         「새로」·「다시」 묶음마다 카드를 쓴다. 카드는 원본의 제목·요약·날짜를
                  코드가 그대로 이어 붙인 것이다. 머리말 origin: card 라
                  mem auto undo --origin card --apply 로 한꺼번에 보류로 돌린다
  --rule <규칙>   chain(덮음 사슬) · link(같은 scope 링크 무리 3~12건) · meaning(같은 scope ·
                  같은 계열 · 벡터가 가까운 3~8건) 중 하나만
  --scope <이름>  그 scope 만
  --max <수>      --apply 가 쓸 카드 상한
  --floor <값>    의미 무리의 코사인 문턱 (기본 0.75). 무리가 8건을 넘으면 0.01 씩 올려 쪼갠다
  --json          목록을 JSON 으로 준다
  --repo <폴더>   저장소를 직접 가리킨다
낡음 : 근거가 덮이거나·보류되거나·고쳐지면 색인이 그 카드를 [낡음] 으로 친다 (검색 신뢰 ×0.5 ·
       mem review --kind stale). --plan 이 「다시」로 잡고 --apply 가 다시 쓴다 (옛 본문은 archive).
--llm (생성 LLM 이 쓰는 글 · B2) 은 아직 없다.
종료 코드 : 0 정상 · 1 사용법 잘못 · 2 --apply 의 카드가 승격까지 못 감 · 3 저장소 없음`
}
