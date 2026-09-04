package eval

import (
	"github.com/mirusona/officina-ai-memory-tool/internal/budget"
	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/search"
)

// TopK 는 답을 얼마나 깊이 읽는지다. recall@10 을 재려면 열이 필요하다.
const TopK = 10

// RawDepth 는 오배제를 잴 때 보는 깊이다.
const RawDepth = TopK

// Options 는 한 번 재는 데 필요한 것이다. 색인 핸들은 cmd 가 열어 넘긴다.
type Options struct {
	Sources   []search.Source
	Golden    string
	Stopwords []string
	Synonym   map[string][]string
	HardOnly  bool
	// RRFK 와 BonusCap 은 mem.toml [search] 다. 자와 검색이 같은 값을 써야 한다.
	RRFK     float64
	BonusCap float64
	// Search 는 mem.toml [search] 통째다 — field_weights·k1·b·rerank_top·
	// syn_weight. **`mem search` 가 넘기는 것과 같은 값을 넘겨야 한다.**
	// 안 넘기면 자가 코드 기본값으로 돌아서 실제 검색과 다른 순위를 잰다.
	Search config.SearchConfig
	// Embed 와 RepoDir 은 임베딩 갈래다. 표가 없거나 꺼져 있으면 아무 일도
	// 안 한다 — 그때 값은 임베딩을 안 넣은 판과 똑같아야 한다 (설계 4-8).
	Embed   config.EmbedConfig
	RepoDir string
	// Vectors 는 의미 재정렬이다. nil 이면 낱말만으로 재는 판이다 — 그 값이
	// 임베딩 전 값과 똑같아야 한다 (결정 16).
	Vectors search.Vectors
}

// CaseResult 는 질문 하나가 한 일이다.
type CaseResult struct {
	Q    string `json:"q"`
	Kind string `json:"kind"`
	Lang string `json:"lang"`
	// Variant 는 질의 꼴이다 — literal · paraphrase · longform.
	Variant string `json:"variant"`
	// ExpectMode 가 all 이면 expect 를 **다** 맞혀야 정답이다.
	ExpectMode  string   `json:"expect_mode"`
	Hard        bool     `json:"hard,omitempty"`
	Unreachable bool     `json:"unreachable,omitempty"`
	Expect      []string `json:"expect"`
	Got         []string `json:"got"`
	// All 은 화면에 실제로 나온 답 전부다. 완화 칸 답도 든다. abstain 은
	// 이것이 비어야 "없다고 잘 답했다" 로 센다 — strict 칸만 보면 사람 눈에
	// 다섯 건이 보이는데도 자는 만점을 준다 (리뷰B #15).
	All  []string `json:"all"`
	Rank int      `json:"rank"`
	// WideRank 는 완화 칸까지 세면 몇 위인지다. 합격선에는 안 쓰고 참고로만
	// 보고한다 — 「사람에게는 보였는데 자가 안 세 준 답」 이 얼마나 되는지다.
	WideRank int `json:"wide_rank"`
	// LegacyRank 는 v0.1 자(0·1번 칸까지)로 잰 자리다. 자를 옮겼으므로 옛
	// 값도 나란히 찍는다 (설계 결정 29).
	LegacyRank   int     `json:"legacy_rank"`
	Rung         int     `json:"rung"`
	PushedOut    bool    `json:"pushed_out,omitempty"`
	WrongAnswer  bool    `json:"wrong_answer,omitempty"`
	Milliseconds float64 `json:"ms"`
	Tokens       int     `json:"tokens"`
}

// Report 는 한 번 잰 것 전부다.
type Report struct {
	Golden      string `json:"golden"`
	Cases       int    `json:"cases"`
	Scored      int    `json:"scored"`
	Unreachable int    `json:"unreachable"`
	Hard        int    `json:"hard"`
	// WideRecall5 는 완화 칸을 다 세면 recall@5 가 얼마인지다 (참고용).
	WideRecall5 float64 `json:"wide_recall5"`
	WideHit     int     `json:"wide_hit"`
	HardHit     int     `json:"hard_hit"`
	// Abstain 은 「없다고 답해야 하는」 질문 수, AbstainWide 는 그중 완화 칸
	// 답이 화면에 보인 것이다. 합격선에는 안 쓰고 따로 알린다.
	Abstain     int          `json:"abstain"`
	AbstainWide int          `json:"abstain_wide"`
	Scale       int          `json:"scale"`
	Metrics     []Metric     `json:"metrics"`
	ByKind      []GroupScore `json:"by_kind"`
	ByLang      []GroupScore `json:"by_lang"`
	// ByVariant 는 질의 꼴별 점수다. paraphrase·longform 이 지금 다 틀리는
	// 유형이라 따로 봐야 한다 (설계 4-9).
	ByVariant []GroupScore `json:"by_variant"`
	// ExMultiCases·ExMultiRecall5 는 multisession 을 뺀 recall@5 다. 설계 4-6 이
	// 「multisession 은 검색으로 안 푼다 · links 로 푼다」고 못 박았으므로 그
	// 유형이 자를 얼마나 끌어내리는지 따로 보여준다.
	// **합격선 판정은 설계대로 골든셋 전부로 한다** — 못 푸는 유형을 자에서
	// 빼면 자가 자기 점수를 스스로 매기는 꼴이 된다.
	ExMultiCases   int     `json:"ex_multisession_cases"`
	ExMultiRecall5 float64 `json:"ex_multisession_recall5"`
	// LangGap 은 ko 와 en 의 recall@5 차이다 (G1 : ≤ 0.15).
	LangGap float64  `json:"lang_gap"`
	Unknown []string `json:"unknown_expect,omitempty"`
	// Warnings 는 골든셋 구성이 설계 9-3 을 안 지킬 때의 경고다 (리뷰B #13).
	Warnings []string     `json:"warnings,omitempty"`
	Pass     bool         `json:"pass"`
	Results  []CaseResult `json:"results"`
}

// Run 은 골든셋을 읽고 질문을 다 물어 본 다음 더한다.
func Run(options Options) (*Report, error) {
	set, err := Load(options.Golden)
	if err != nil {
		return nil, err
	}
	report := Report{Golden: options.Golden, Warnings: set.Warnings}
	counts := tally{}
	groups := newGroups()
	for _, item := range set.Cases {
		if options.HardOnly && !item.Hard {
			continue
		}
		outcome, err := runCase(options, item)
		if err != nil {
			return nil, err
		}
		report.Cases++
		report.Results = append(report.Results, outcome)
		if outcome.Unreachable {
			report.Unreachable++
			continue
		}
		counts.add(outcome)
		groups.add(outcome)
	}
	report.Scored = report.Cases - report.Unreachable
	report.WideHit, report.WideRecall5 = wideScore(report.Results)
	report.ExMultiCases, report.ExMultiRecall5 = exMultiScore(report.Results)
	report.Hard, report.HardHit = counts.hard, counts.hardHit
	report.Abstain, report.AbstainWide = counts.abstain, counts.abstainWide
	report.Scale = scaleOf(options)
	report.ByKind, report.ByLang = groups.rows()
	report.ByVariant = groups.variantRows()
	gap, gapMeasured := LangGap(report.ByLang)
	report.LangGap = gap
	report.Metrics = counts.metrics(report.Scale, semanticOn(options), gap, gapMeasured)
	report.Unknown = unknownExpect(options, set)
	report.Pass = allPassed(report.Metrics)
	return &report, nil
}

// runCase 는 질문 하나를 사람이 묻듯 묻는다. 완화 칸(2 이상)에서 온 답은
// 정답으로 안 친다 — 사용자 확정 (설계 9-3).
func runCase(options Options, item Case) (CaseResult, error) {
	result, err := search.Search(askOf(options, item))
	if err != nil {
		return CaseResult{}, err
	}
	strict, rung := strictIDs(result)
	mode := item.ExpectModeOf()
	outcome := CaseResult{
		Q: item.Q, Kind: item.Kind, Lang: item.LangOf(), Variant: item.VariantOf(),
		ExpectMode: mode, Hard: item.Hard,
		Unreachable: item.Unreachable, Expect: item.Expect, Got: strict, Rung: rung,
		All:          allIDs(result),
		Rank:         displayRank(result, item.Expect, mode, search.Strict),
		LegacyRank:   displayRank(result, item.Expect, mode, legacy),
		WideRank:     rankOfMode(allIDs(result), item.Expect, mode),
		Milliseconds: float64(result.Elapsed.Microseconds()) / 1000,
		Tokens:       budget.Estimate(search.Markdown(result, 0)),
	}
	outcome.WrongAnswer = anyOf(strict, item.NotExpect)
	if item.Kind == KindAbstain || outcome.Rank > 0 {
		return outcome, nil
	}
	raw, err := search.Search(rawOf(options, item))
	if err != nil {
		return CaseResult{}, err
	}
	rawIDs, _ := strictIDs(raw)
	outcome.PushedOut = rankOfMode(rawIDs, item.Expect, mode) > 0
	return outcome, nil
}

// allIDs 는 칸을 안 가리고 나온 순서 그대로다. 참고 지표에만 쓴다.
func allIDs(result *search.Result) []string {
	out := make([]string, 0, len(result.Hits))
	for _, hit := range result.Hits {
		out = append(out, hit.ID)
	}
	return out
}

// 자 두 개 (설계 결정 29 · 1절 주). strict 는 v0.2 자(search.Strict — 0·1·2·5),
// legacy 는 v0.1 자(0·1번 칸까지)다. **자를 옮겼으므로 옛 값도 나란히 찍는다.**
func legacy(rung int) bool { return rung <= search.RungAnd }

// strictIDs 는 strict 칸(0·1·2·5)에서 온 답만 남긴다. 넓혀서 찾은 것은
// 「찾았다」 로 안 센다. 자는 search.Strict 하나다 (설계 결정 29 · 4-1a).
func strictIDs(result *search.Result) ([]string, int) {
	out := []string{}
	deepest := 0
	for _, hit := range result.Hits {
		if !search.Strict(hit.Rung) {
			continue
		}
		out = append(out, hit.ID)
		if hit.Rung > deepest {
			deepest = hit.Rung
		}
	}
	return out, deepest
}

func askOf(options Options, item Case) search.Options {
	ask := search.Options{
		Sources: options.Sources, Query: item.Q, Limit: TopK,
		Stopwords: options.Stopwords, Synonym: options.Synonym,
		RRFK: options.RRFK, BonusCap: options.BonusCap, Search: options.Search,
		Embed: options.Embed, RepoDir: options.RepoDir, Vectors: options.Vectors,
	}
	if item.Type != "" {
		ask.Filter.Types = []string{item.Type}
	}
	ask.Filter.Tags = item.Tags
	ask.Filter.Since, _ = search.ParseSince(item.Since)
	return ask
}

// rawOf 는 좁히기·가산·감쇠를 다 끈 같은 질문이다. 여기에는 있는데 위 답에
// 없으면 우리가 밀어낸 것이다 (설계 9-3 오배제율).
func rawOf(options Options, item Case) search.Options {
	return search.Options{
		Sources: options.Sources, Query: item.Q, Limit: RawDepth, Raw: true,
		Stopwords: options.Stopwords, Synonym: options.Synonym,
		RRFK: options.RRFK, BonusCap: options.BonusCap, Search: options.Search,
		Filter: index.Filter{All: true},
	}
}

// displayRank 는 **사람이 보는 화면에서 몇 번째 줄인가**다. 예전에는 strict
// 칸 답만 골라 번호를 다시 매겨서, 완화 칸 답이 위에 낀 질의는 `--limit 5` 로
// 친 사람에게 안 보이는 답이 recall@5 에 들었다 (리뷰B #16).
// 완화 칸에서 온 답은 자리로 세지 않는다 (설계 9-3 사용자 확정).
func displayRank(result *search.Result, expect []string, mode string, ruler func(int) bool) int {
	shown := []string{}
	for _, hit := range result.Hits {
		if !ruler(hit.Rung) {
			continue
		}
		shown = append(shown, hit.ID)
	}
	return rankOfMode(shown, expect, mode)
}

// rankOfMode 는 정답이 몇 번째 줄에서 채워졌는지다.
//
//	any — 처음 맞은 자리
//	all — expect 를 **다** 만난 자리. 하나라도 없으면 0 이다 (설계 결정 22)
func rankOfMode(got, expect []string, mode string) int {
	if mode != ExpectAll {
		return rankOf(got, expect)
	}
	last := 0
	for _, want := range expect {
		place := rankOf(got, []string{want})
		if place == 0 {
			return 0
		}
		if place > last {
			last = place
		}
	}
	return last
}

// rankOf 는 처음 맞은 자리다. 동점은 먼저 나온 순서 그대로다 (설계 9-3).
func rankOf(got, expect []string) int {
	for place, id := range got {
		if contains(expect, id) {
			return place + 1
		}
	}
	return 0
}

func anyOf(got, forbidden []string) bool {
	for _, id := range got {
		if contains(forbidden, id) {
			return true
		}
	}
	return false
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// wideScore 는 완화 칸까지 세면 몇 건이 상위 5에 드는지다 (참고).
func wideScore(results []CaseResult) (int, float64) {
	hit, whole := 0, 0
	for _, item := range results {
		if item.Unreachable || item.Kind == KindAbstain {
			continue
		}
		whole++
		if item.WideRank > 0 && item.WideRank <= 5 {
			hit++
		}
	}
	return hit, ratio(hit, whole, 0)
}

// exMultiScore 는 multisession 을 뺀 recall@5 다. 세는 자리는 본 recall@5 와
// 같다 — abstain 과 못 닿는 질의는 양쪽 다 안 센다.
func exMultiScore(results []CaseResult) (int, float64) {
	hit, whole := 0, 0
	for _, item := range results {
		if item.Unreachable || item.Kind == KindAbstain || item.Kind == KindMultisession {
			continue
		}
		whole++
		if item.Rank > 0 && item.Rank <= 5 {
			hit++
		}
	}
	return whole, ratio(hit, whole, 0)
}

// scaleOf 는 저장소가 몇 건인지다. p95 합격선이 이걸 따라간다.
func scaleOf(options Options) int {
	total := 0
	for _, from := range options.Sources {
		counted, err := from.DB.Count()
		if err != nil {
			continue
		}
		total += counted
	}
	return total
}

// unknownExpect 는 골든셋이 가리키는데 색인에 없는 id 다. 그런 질문은 무슨
// 짓을 해도 못 맞히면서 점수만 깎아 검색 잘못처럼 보인다.
func unknownExpect(options Options, set *GoldenSet) []string {
	missing, seen := []string{}, map[string]bool{}
	for _, item := range set.Cases {
		for _, id := range item.Expect {
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			if !knownID(options, id) {
				missing = append(missing, id)
			}
		}
	}
	return missing
}

func knownID(options Options, id string) bool {
	for _, from := range options.Sources {
		row, err := from.DB.ByID(id)
		if err == nil && row != nil {
			return true
		}
	}
	return false
}

// semanticOn 은 이번 평가가 의미 검색을 켜고 돌았는지다. p95 자가 이걸 따라간다
// (리뷰 D9).
// 재정렬기가 꽂혔는지만 본다. `[embed] enabled` 는 **낱말 임베딩 표(ko.bin)**
// 스위치라 의미 모델을 깔아도 꺼져 있고, 그것까지 보면 자가 늘 100ms 였다
// (재측정 1 R4 : 의미 p95 315ms 가 「미달」로 찍혔다).
func semanticOn(options Options) bool {
	return options.Vectors != nil
}
