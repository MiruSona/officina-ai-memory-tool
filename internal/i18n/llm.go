package i18n

// 바깥 LLM 판정(K)이 쓰는 말이다 — llm.toml 읽기, R3 경고, `mem judge` (자동쌓기설계 5절 K).

const (
	LLMBadURL           Key = "llm-bad-url"
	LLMBadTimeout       Key = "llm-bad-timeout"
	AutoJudgeSkippedWhy Key = "auto-judge-skipped-why"

	JudgeUsage          Key = "judge-usage"
	JudgeSupportUsage   Key = "judge-support-usage"
	JudgeOff            Key = "judge-off"
	JudgeConfigLine     Key = "judge-config-line"
	JudgeFailed         Key = "judge-failed"
	JudgeVerdictLine    Key = "judge-verdict-line"
	JudgeFileUnreadable Key = "judge-file-unreadable"
	JudgeBadLine        Key = "judge-bad-line"
	JudgeFileSummary    Key = "judge-file-summary"
	JudgeSupport        Key = "judge-support"
	JudgeContradict     Key = "judge-contradict"
	JudgeUnrelated      Key = "judge-unrelated"
	JudgeCached         Key = "judge-cached"
	JudgeUnsure         Key = "judge-unsure"
	JudgePromptUsage    Key = "judge-prompt-usage"
	JudgeYes            Key = "judge-yes"
	JudgeNo             Key = "judge-no"
	JudgeCleanUsage     Key = "judge-clean-usage"
	JudgeCleanPlan      Key = "judge-clean-plan"
	JudgeCleanDone      Key = "judge-clean-done"
	JudgeCleanNothing   Key = "judge-clean-nothing"
	JudgeCleanFailed    Key = "judge-clean-failed"
	JudgeCleanLinked    Key = "judge-clean-linked"
)

var llmMessages = map[Key]string{
	LLMBadURL:           "llm.toml 의 url 이 http(s) 주소가 아니다. 바깥 LLM 을 끈 것으로 친다.",
	LLMBadTimeout:       "llm.toml 의 timeout_ms(%d)는 1~%d 여야 한다. 기본 %d 로 읽었다.",
	AutoJudgeSkippedWhy: "근거 지지 판정(R3)을 못 받아 건너뛰었다 (%s).",

	JudgeUsage:          "쓰는 법 : mem judge config | mem judge support --evidence <근거> --claim <주장> | mem judge support --file <쌍.jsonl> [--prompt v2|v3|v3-strict] | mem judge clean [--older <일>] [--apply]",
	JudgeSupportUsage:   "support 는 --evidence 와 --claim 을 같이 주거나, --file 하나만 준다.",
	JudgeOff:            "LLM 주소 없음 — 건너뜀 (설정 파일 : %s)",
	JudgeConfigLine:     "설정 파일 : %s\n주소 : %s\n판정 프로필 : %s · 생성 프로필 : %s\n제한 시간 : %dms · 키 : %s",
	JudgeFailed:         "판정을 못 받았다 (%s). 재시도는 안 한다.",
	JudgeVerdictLine:    "%s (%s) 확률 %.3f · %s · %dms%s",
	JudgeFileUnreadable: "판정할 쌍 파일을 못 읽었다 : %s",
	JudgeBadLine:        "%d번째 줄이 {\"evidence\":…, \"claim\":…} 꼴의 JSON 이 아니다.",
	JudgeFileSummary:    "판정 %d쌍 · 받음 %d · 못 받음 %d · 맞힘 %d/%d · 모른다 %d · 지연 p50 %dms · p95 %dms (새로 물은 %d쌍 기준)",
	JudgeSupport:        "지지",
	JudgeContradict:     "반대",
	JudgeUnrelated:      "무관",
	JudgeCached:         " · 기록에서 읽음",
	JudgeUnsure:         " · 모른다 (확률이 낮거나 동점)",
	JudgePromptUsage:    "--prompt 는 v2 · v3 · v3-strict 중 하나다.",
	JudgeYes:            "있음",
	JudgeNo:             "없음",
	JudgeCleanUsage:     "쓰는 법 : mem judge clean [--older <일>] [--apply] [--json] — --older 는 1 이상 정수다.",
	JudgeCleanPlan:      "지울 판정 기록 %d건 (%d일 넘음 · %s) — --apply 로 지운다.",
	JudgeCleanDone:      "판정 기록 %d건을 지웠다.",
	JudgeCleanNothing:   "%d일 넘은 판정 기록이 없다.",
	JudgeCleanFailed:    "판정 기록 %d건은 못 지웠다 (다른 프로그램이 쥐고 있을 수 있다).",
	JudgeCleanLinked:    "판정 기록 폴더가 링크다. 따라가지 않고 멈춘다 : %s",
}

func init() {
	for key, text := range llmMessages {
		messages[key] = text
	}
	commandHelp["judge"] = `mem judge — 바깥 LLM 판정(SemIf 방식)을 직접 불러 본다 (K · 측정용)

쓰는 법 : mem judge config [--json]
          mem judge support --evidence <근거> --claim <주장> [--fresh] [--json]
          mem judge support --file <쌍.jsonl> [--fresh] [--prompt v2|v3|v3-strict]
          mem judge clean [--older <일>] [--apply] [--json]
  config            이 기계의 llm.toml 자리와 읽은 값을 보인다. 서버에는 안 묻는다. 키는 있음/없음만
  support           (근거, 주장) 쌍이 지지(A)·반대(B)·무관(C) 중 무엇인지 글자 하나로 판정한다
                    add --origin 의 R3 와 같은 물음 · 같은 판정 기록(Memory/local/judge/)을 쓴다
  clean             판정 기록 중 --older 일(기본 30) 넘은 것을 고른다. 미리보기가 기본 · --apply 로 지운다
  --evidence <글>   근거 문장
  --claim <글>      주장 (자동 관문에서는 기억 요약)
  --file <파일>     한 줄에 {"id","evidence","claim","want"} 인 jsonl. 순차로 묻고 줄마다 JSON 을 낸다
                    want(A·B·C 또는 support·contradict·unrelated)가 있으면 맞힘을 센다. 요약은 stderr
  --fresh           판정 기록을 안 읽고 다시 묻는다 (새 결과로 기록을 덮는다)
  --prompt <판>     물음 글 판을 고른다 (측정용 · v2 · v3 · v3-strict). 안 주면 기본 판(support-v3)
                    최고 확률이 0.40 아래거나 동점이면 「모른다」 꼬리표가 붙는다 (글자는 그대로)
  --json            결과를 JSON 으로
  --repo <폴더>     저장소를 직접 가리킨다
llm.toml 자리 : ~/.aimemory/llm.toml (환경 변수 MEM_LLM_CONFIG 로 바꿀 수 있다). 없으면 「LLM 주소 없음」 한 줄 · 종료 0
비밀 꼴이 든 글은 서버로 보내지 않는다. 재시도는 없다
종료 코드 : 0 정상 · 1 사용법 잘못 · 2 판정을 못 받음(서버 안 닿음 · 시간 넘김 · 확률 없음) · 3 저장소 없음`
}
