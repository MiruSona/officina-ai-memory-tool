package i18n

// search 와 eval 이 쓰는 문장이다. i18n.go 의 표를 건드리지 않고 여기서
// 덧붙인다 (설계 12-1 : 한글 리터럴은 이 패키지 밖에 두지 않는다).

// 검색 결과 틀.
const (
	SearchHeader       Key = "search-header"
	SearchListHeader   Key = "search-list-header"
	SearchTableHead    Key = "search-table-head"
	SearchFooter       Key = "search-footer"
	SearchRelaxedLine  Key = "search-relaxed-line"
	SearchStoppedLine  Key = "search-stopped-line"
	SearchTimeNarrow   Key = "search-time-narrow"
	SearchTimeRelaxed  Key = "search-time-relaxed"
	SearchNoIndex      Key = "search-no-index"
	SearchTrimmed      Key = "search-trimmed"
	SearchFacetHead    Key = "search-facet-head"
	SearchBadFacet     Key = "search-bad-facet"
	SearchBadSince     Key = "search-bad-since"
	UnknownOption      Key = "unknown-option"
	InvalidMark        Key = "invalid-mark"
	EmptyNoNear        Key = "empty-no-near"
	EmptyFewerWords    Key = "empty-fewer-words"
	SearchFilterOnly   Key = "search-filter-only"
	SearchTimeIgnored  Key = "search-time-ignored"
	SearchTooManyWords Key = "search-too-many-words"
	SearchWordDropped  Key = "search-word-dropped"
	SearchWordsDropped Key = "search-words-dropped"
	SearchLimitCapped  Key = "search-limit-capped"
	SearchBadQuery     Key = "search-bad-query"
	SearchBadBudget    Key = "search-bad-budget"
	SearchFacetWords   Key = "search-facet-words"
	SearchBudgetTiny   Key = "search-budget-tiny"
	EmptyFilters       Key = "empty-filters"
	EmptyFilterHint    Key = "empty-filter-hint"
)

// 0건 설명 (설계 6-9).
const (
	EmptyMissing Key = "empty-missing"
	EmptyFound   Key = "empty-found"
	EmptyNear    Key = "empty-near"
	EmptyNext    Key = "empty-next"
	EmptyScale   Key = "empty-scale"
	EmptyNoWord  Key = "empty-no-word"
	// EmptyStore 는 색인에 기억이 한 건도 없을 때다. 「이 낱말이 없다」와
	// 「아직 아무것도 안 넣었다」를 사람이 갈라 볼 수 있어야 한다 (불변조건 I8).
	// 기억 파일은 있는데 색인만 빈 경우는 그 앞에서 종료 코드 5 로 걸린다.
	EmptyStore       Key = "empty-store"
	EmptyMissingJong Key = "empty-missing-jong"
	EmptyFoundJong   Key = "empty-found-jong"
	EmptyTryOther    Key = "empty-try-other"
	// EmptyHeld 는 보류(review: true)라서 빠진 것이 있을 때다 (뒷정리-3).
	EmptyHeld Key = "empty-held"
)

// 사다리 칸 꼬리표 (설계 6-6).
const (
	RungLabel2 Key = "rung-label-2"
	RungLabel3 Key = "rung-label-3"
	RungLabel4 Key = "rung-label-4"
	RungLabel5 Key = "rung-label-5"
	RungLabel6 Key = "rung-label-6"
)

// --explain (설계 6-10).
const (
	ExplainTitle Key = "explain-title"
	ExplainRRF   Key = "explain-rrf"
	ExplainBonus Key = "explain-bonus"
	ExplainDecay Key = "explain-decay"
	ExplainRung  Key = "explain-rung"
)

// mem eval (설계 9-3).
const (
	EvalHeader       Key = "eval-header"
	EvalTableHead    Key = "eval-table-head"
	EvalKindHead     Key = "eval-kind-head"
	EvalLangHead     Key = "eval-lang-head"
	EvalHardLine     Key = "eval-hard-line"
	EvalUnreachable  Key = "eval-unreachable"
	EvalRelaxedNote  Key = "eval-relaxed-note"
	EvalWideLine     Key = "eval-wide-line"
	EvalMissHead     Key = "eval-miss-head"
	EvalMissLine     Key = "eval-miss-line"
	EvalMissPushed   Key = "eval-miss-pushed"
	EvalMissUnreach  Key = "eval-miss-unreach"
	EvalNoRank       Key = "eval-no-rank"
	EvalAbstainMiss  Key = "eval-abstain-miss"
	EvalUnknownID    Key = "eval-unknown-id"
	EvalFooter       Key = "eval-footer"
	EvalPass         Key = "eval-pass"
	EvalFail         Key = "eval-fail"
	EvalThresholdUp  Key = "eval-threshold-up"
	EvalThresholdLow Key = "eval-threshold-low"
	EvalNoGolden     Key = "eval-no-golden"
	EvalBadGolden    Key = "eval-bad-golden"
	EvalBadVersion   Key = "eval-bad-version"
	EvalNoCases      Key = "eval-no-cases"
	EvalNoQuestion   Key = "eval-no-question"
	EvalBadKind      Key = "eval-bad-kind"
	EvalAbstainWants Key = "eval-abstain-wants"
	EvalNeedExpect   Key = "eval-need-expect"
	EvalThinKind     Key = "eval-thin-kind"
	EvalThinLang     Key = "eval-thin-lang"
	EvalWarnHead     Key = "eval-warn-head"
	EvalNotMeasured  Key = "eval-not-measured"
	EvalAbstainWide  Key = "eval-abstain-wide"
)

// 지표 이름.
const (
	MetricRecall5      Key = "metric-recall5"
	MetricRecall10     Key = "metric-recall10"
	MetricMRR          Key = "metric-mrr"
	MetricAbstain      Key = "metric-abstain"
	MetricMisexclusion Key = "metric-misexclusion"
	SearchModeWord     Key = "search-mode-word"
	SearchModeMeaning  Key = "search-mode-meaning"
	MetricP95          Key = "metric-p95"
	MetricTokens       Key = "metric-tokens"
)

var searchMessages = map[Key]string{
	SearchModeWord:     "[낱말]",
	SearchModeMeaning:  "[낱말+의미]",
	SearchHeader:       "검색 : \"%s\" — 총 %d건 중 상위 %d건 (%dms · 약 %d토큰)",
	SearchListHeader:   "목록 : 총 %d건 중 상위 %d건 (%dms · 약 %d토큰)",
	SearchTableHead:    "| # | id | 종류 | 날짜 | 요약 |\n| --- | --- | --- | --- | --- |",
	SearchFooter:       "본문은 `mem show <id>`. 더 보려면 `--limit`, 좁히려면 `--type`·`--scope`·`--tag`·`--since`.",
	SearchRelaxedLine:  "%d건 중 %d건은 정확히, %d건은 넓혀서 찾았다 (%s)",
	SearchStoppedLine:  "흔한 낱말을 뺐다 : %s",
	SearchTimeNarrow:   "기간을 좁혔다 : %s",
	SearchTimeRelaxed:  "기간 안에는 없어서 전체에서 찾았다.",
	SearchNoIndex:      "아직 색인이 없다. `mem index` 를 한 번 돌려라.",
	SearchTrimmed:      "예산(%d토큰)에 맞추느라 %d줄을 뺐다.",
	SearchFacetHead:    "| 값 | 건수 |\n| --- | --- |",
	SearchBadFacet:     "`--facet` 은 scope 나 tag 여야 한다 : %s",
	SearchBadSince:     "`--since` 는 30d 나 2026-08-01 꼴이어야 한다 : %s",
	UnknownOption:      "모르는 옵션 : %s",
	InvalidMark:        "[무효]",
	EmptyNoNear:        "  · 닮은 낱말도 없다.",
	SearchFilterOnly:   "찾을 낱말이 없어 조건에 맞는 목록만 냈다.",
	SearchTimeIgnored:  "`--since` 를 썼으므로 질의의 시간 표현(%s)은 안 썼다.",
	SearchTooManyWords: "낱말이 많아 앞 %d개만 썼다. 뺀 낱말 : %s",
	SearchWordDropped:  "낱말 하나를 빼고 찾았다 : \"%s\" 제외 — 저장소에 없는 낱말",
	SearchWordsDropped: "낱말 %d개를 빼고 찾았다 : \"%s\" 제외 — 저장소에 없는 낱말",
	SearchLimitCapped:  "`--limit` 은 %d 까지다. 그만큼만 냈다.",
	SearchBadQuery:     "색인이 못 읽은 질의 조각이 있었다. 그만큼은 못 찾았을 수 있다.",
	SearchBadBudget:    "`--budget` 은 0보다 큰 수여야 한다 : %s",
	SearchFacetWords:   "`--facet` 은 질의 낱말을 안 쓴다. 무시한 낱말 : %s",
	SearchBudgetTiny:   "예산(%d토큰)이 너무 작아 답을 잘랐다.",
	EmptyFilters:       "  · 이 조건에 맞는 기억이 없다 : %s",
	EmptyFilterHint:    "  · 조건을 하나씩 빼 보라.",
	EmptyFewerWords:    "  · 다시 해 볼 것 : 낱말을 줄여 보라.",

	EmptyMissing:     "  · 저장소에 \"%s\" 가 들어간 기억이 하나도 없다.",
	EmptyMissingJong: "  · 저장소에 \"%s\" 이 들어간 기억이 하나도 없다.",
	EmptyFoundJong:   "  · \"%s\" 은 %d건 있다.",
	EmptyTryOther:    "  · 다시 해 볼 것 : 낱말을 줄이거나 다른 말로 바꿔라 — `mem search %s` · 종류로 훑으려면 `mem search --type decision`.",
	EmptyFound:       "  · \"%s\" 는 %d건 있다.",
	EmptyNear:        "  · 비슷한 낱말 : %s",
	EmptyNext:        "  · 다시 해 보기 : %s",
	EmptyScale:       "  (전체 %d건 · 마지막 색인 %s)",
	EmptyNoWord:      "  · 물어볼 낱말이 없다. 두 글자 이상으로 다시 쳐라.",
	EmptyStore:       "  · 저장소에 기억이 아직 0건이다. 못 찾은 것이 아니라 넣은 것이 없다. `mem add` 로 하나 넣어 본다.",
	EmptyHeld:        "  · 보류 %d건은 뺐다 — `--include-held` 로 본다.",

	RungLabel2: "[낱말뺌]",
	RungLabel3: "[느슨]",
	RungLabel4: "[동의어]",
	RungLabel5: "[붙은낱말]",
	RungLabel6: "[부분일치]",

	ExplainTitle: "%d. [%s] %s      점수 %.4f",
	ExplainRRF:   "     rrf %.4f — 랭킹 %s",
	ExplainBonus: "     가산 %.2f (%s)",
	ExplainDecay: "     감쇠바닥 %.2f · 신뢰계수 %.2f",
	ExplainRung:  "     %d번 칸 %s",

	EvalHeader:      "검색 품질 — 골든셋 %d건 (합격선 대조) : %s",
	EvalTableHead:   "| 지표 | 값 | 합격선 | 판정 |\n| --- | --- | --- | --- |",
	EvalKindHead:    "| 유형 | 건수 | recall@5 | MRR |\n| --- | --- | --- | --- |",
	EvalLangHead:    "| 말 | 건수 | recall@5 | MRR |\n| --- | --- | --- | --- |",
	EvalHardLine:    "사람이 어렵다고 표시한 %d건 중 %d건을 맞혔다.",
	EvalUnreachable: "못 닿는 질의 %d건 (동의어·임베딩 없이는 안 되는 것). 합격선 계산에서 뺐다.",
	EvalRelaxedNote: "완화 %d칸 아래에서 얻은 답은 정답으로 안 쳤다.",
	EvalWideLine:    "참고 : 완화 칸까지 다 세면 recall@5 는 %.3f (%d건)다. 사람에게는 보이지만 자가 안 세 준 답이다.",
	EvalMissHead:    "못 맞힌 것 :",
	EvalMissLine:    "- %s (%s) — 순위 %s%s",
	EvalMissPushed:  " · 우리가 밀어냈다",
	EvalMissUnreach: " · 못 닿는 질의",
	EvalNoRank:      "없음",
	EvalAbstainMiss: "- %s (abstain) — 없다고 해야 하는데 %d건이 나왔다",
	EvalUnknownID:   "골든셋이 가리키는 id 중 색인에 없는 것 : %s",
	EvalFooter:      "자세히 보려면 `mem search <질의> --explain`.",
	EvalPass:        "합격",
	EvalFail:        "미달",

	EvalThresholdUp:  "%s 이상",
	EvalThresholdLow: "%s 이하",

	EvalNoGolden:     "골든셋이 없어 잴 것이 없다 : %s — 아무것도 안 쟀다",
	EvalBadGolden:    "골든셋을 못 읽었다 : %s — %s",
	EvalBadVersion:   "골든셋 형식이 더 새 판이다 (아는 판 %d, 파일 판 %d).",
	EvalNoCases:      "골든셋에 질문이 하나도 없다 : %s",
	EvalNoQuestion:   "%d번째 질문에 `q` 가 없다.",
	EvalBadKind:      "모르는 `kind` 다 : %s",
	EvalAbstainWants: "`abstain` 질문에는 `expect` 가 없어야 한다 : %s",
	EvalNeedExpect:   "`expect` 가 없다 : %s",
	EvalThinKind:     "골든셋에 `%s` 질문이 %d건뿐이다 (설계 9-3 최소 %d건).",
	EvalThinLang:     "골든셋에 `lang: en` 질문이 %d건뿐이다 (설계 9-3 최소 %d건).",
	EvalWarnHead:     "골든셋 구성 경고 :",
	EvalNotMeasured:  "잰 것 없음",
	EvalAbstainWide:  "없다고 답해야 하는 %d건 중 %d건은 꼬리표 붙은 완화 답이 화면에 보였다 (정답으로는 안 셌다).",

	MetricRecall5:      "recall@5",
	MetricRecall10:     "recall@10",
	MetricMRR:          "MRR",
	MetricAbstain:      "abstain 정답률",
	MetricMisexclusion: "오배제율",
	MetricP95:          "p95",
	MetricTokens:       "평균 토큰",
}

func init() {
	for key, text := range searchMessages {
		messages[key] = text
	}
}
