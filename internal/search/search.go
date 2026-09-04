package search

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/token"
)

// DefaultLimit 은 아무 말도 없을 때 몇 건을 내는지다 (설계 6-6).
const DefaultLimit = 5

// MaxLimit 은 한 번에 낼 수 있는 최대 건수다. 상한이 없으면 `--limit 999999`
// 하나가 사다리 일곱 칸을 다 밟고 저장소를 통째로 뱉는다 (리뷰B #21).
const MaxLimit = 100

// Source 는 읽을 저장소 하나다. 색인은 프로세스당 한 번만 열고 그 핸들을
// 여기로 넘겨 쓴다 (설계 7-4a).
type Source struct {
	DB *index.DB
}

// Options 는 읽기 명령이 주는 것 전부다.
type Options struct {
	Sources    []Source
	Query      string
	Stopwords  []string
	Synonym    map[string][]string
	Filter     index.Filter
	Limit      int
	Explain    bool
	BoostScope string
	// Raw 는 좁히기·가산·감쇠를 다 끄고 관련도만 본다. eval 의 오배제율 말고는
	// 아무도 쓰지 않는다 (설계 9-3).
	Raw bool
	// LimitCapped 는 부르는 쪽이 --limit 을 상한으로 깎았는지다 (리뷰B #21).
	LimitCapped bool
	// MissPath 는 0건·느슨 질의를 남길 파일이다. 비면 안 남긴다 (설계 6-7 · 리뷰B #6).
	MissPath string
	// RRFK 와 BonusCap 은 mem.toml [search] 다. 0 이면 config 의 기본값이다.
	RRFK     float64
	BonusCap float64
	// Search 는 재순위·동의어 손잡이다. 안 주면 칸마다 config 기본값이다
	// (16/6/1 · k1 1.2 · b 0.4 · 상위 200 · 동의어 ×0.5).
	Search config.SearchConfig
	// Embed 는 임베딩 갈래다. Enabled 가 false 면 아무 일도 안 한다 (설계 4-8).
	Embed config.EmbedConfig
	// RepoDir 은 저장소 폴더다. 임베딩 표를 `<저장소>/model/ko.bin` 에서도
	// 찾으려고 받는다. 비어 있어도 된다.
	RepoDir string
	// Types 는 이 저장소의 기억 종류 표다. 비면 기본 7종이다 (감쇠·가산이 본다).
	Types model.TypeTable
	// Vectors 는 문장 임베딩 자리다. nil 이면 낱말 검색만 돈다 (결정 16).
	// 3A(ORT)가 여기에 꽂는다 — 꽂히기 전에도 어느 명령도 실패하지 않는다.
	Vectors Vectors
}

// Vectors 는 임베딩 갈래가 꽂는 자리다 (결정 15·62). 검색은 **이미 낱말이
// 찾아 놓은 후보의 차례만** 바꾸므로, 이 자리는 「질의 한 줄의 벡터」와
// 「후보 문서의 벡터」 둘만 알면 된다.
type Vectors interface {
	// Query 는 질의 한 줄의 단위 벡터다. 못 만들면 nil 이고, 그러면 그냥
	// 건너뛴다.
	Query(text string) []float32
	// Docs 는 후보 문서의 단위 벡터다. 벡터가 없는 문서는 답에서 빠진다.
	Docs(docids []int64) map[int64][]float32
}

// 모드 표시 (결정 16). `--json` 의 `mode` 칸과 결과 머리에 같은 값이 간다.
// 「왜 내 결과가 저 사람과 다르지」를 막는 유일한 장치라, 임베딩이 **실제로
// 돌았을 때만** 의미 모드로 적는다 — 켜 놓고 모델이 없으면 낱말 모드다.
const (
	ModeWord    = "word"
	ModeMeaning = "word+meaning"
)

// weights 는 title·meta·summary·body 순 재순위 가중이다 (설계 4-3).
func (o *Options) weights() [4]float64 {
	for _, value := range o.Search.FieldWeights {
		if value <= 0 {
			return config.DefaultFieldWeights
		}
	}
	return o.Search.FieldWeights
}

func (o *Options) k1() float64 {
	if o.Search.K1 <= 0 {
		return config.DefaultK1
	}
	return o.Search.K1
}

func (o *Options) b() float64 {
	if o.Search.B <= 0 {
		return config.DefaultB
	}
	return o.Search.B
}

// rerankTop 은 우리 bm25 로 다시 매길 건수다 (설계 결정 18).
func (o *Options) rerankTop() int {
	if o.Search.RerankTop <= 0 {
		return config.DefaultRerankTop
	}
	return o.Search.RerankTop
}

// synWeight 는 동의어로 찾은 것의 랭킹 몫이다. 정확히 쓴 낱말이 늘 위에 오게
// 절반으로 시작한다 (설계 4-3).
func (o *Options) synWeight() float64 {
	if o.Search.SynWeight <= 0 {
		return config.DefaultSynWeight
	}
	return o.Search.SynWeight
}

// softener 는 이 질의가 쓸 RRF 완충값이다.
func (o *Options) softener() float64 {
	if o.RRFK <= 0 {
		return config.DefaultRRFK
	}
	return o.RRFK
}

// embedFloor 는 이 아래 코사인은 후보로 안 치는 자다 (설계 4-5). 이 한 줄이
// 없으면 임베딩이 무엇을 물어도 무언가를 내놓아 abstain 이 무너진다.
func (o *Options) embedFloor() float64 {
	if o.Embed.Floor <= 0 {
		return config.DefaultEmbedFloor
	}
	return o.Embed.Floor
}

// embedWeight 는 임베딩 랭킹의 RRF 몫이다. 낮게 시작한다 (설계 4-8).
func (o *Options) embedWeight() float64 {
	if o.Embed.RRFWeight <= 0 {
		return config.DefaultEmbedRRFWeight
	}
	return o.Embed.RRFWeight
}

// cap 은 이 질의가 쓸 가산 상한이다.
func (o *Options) cap() float64 {
	if o.BonusCap <= 0 {
		return config.DefaultBonusCap
	}
	return o.BonusCap
}

// Hit 은 답 한 줄이다.
type Hit struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	Date    string   `json:"date"`
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Scope   string   `json:"scope"`
	Tags    []string `json:"tags"`
	// TodoStatus 와 Author 는 v0.2 이름이다. JSON 키도 `todo_status`·`author`
	// 로 바뀌었다 — 옛 `status`·`source` 는 없다 (파도 B 스키마).
	TodoStatus   string  `json:"todo_status,omitempty"`
	Severity     string  `json:"severity,omitempty"`
	Author       string  `json:"author,omitempty"`
	Pinned       bool    `json:"pinned,omitempty"`
	InvalidAt    string  `json:"invalid_at,omitempty"`
	SupersededBy string  `json:"superseded_by,omitempty"`
	Score        float64 `json:"score"`
	Rung         int     `json:"rung"`
	// Invalid 는 뒤집혔거나 기한이 지난 기억이다. --all 일 때만 보인다.
	Invalid bool `json:"invalid,omitempty"`
	// Group 은 같은 주제로 묶인 답의 번호다. 0 이면 묶음이 없다 (결정 44).
	Group int   `json:"group,omitempty"`
	Parts Parts `json:"parts,omitempty"`
}

// Why 는 0건일 때 반드시 말해야 하는 네 가지다 (불변조건 9 · 설계 6-9).
type Why struct {
	Missing   []string           `json:"missing"`
	Found     map[string]int     `json:"found"`
	Near      []index.VocabCount `json:"near"`
	Next      string             `json:"next"`
	Total     int                `json:"total"`
	LastIndex string             `json:"last_index,omitempty"`
	// Filters 는 낱말 없이 조건만 줘서 0건이 났을 때 그 조건들이다.
	// 조건만 준 검색도 왜 0건인지 말해야 한다 (불변조건 9 · 리뷰C #10).
	Filters []string `json:"filters,omitempty"`
	// BadQuery 는 색인이 질의를 못 읽어 삼킨 것이 있는지다 (리뷰B #12).
	BadQuery bool `json:"bad_query,omitempty"`
	// Reason 은 왜 0건인지 **기계가 읽는** 한 낱말이다. 사람 문장은 format.go
	// 가 만든다 — `--json` 만 보는 쪽도 I8 의 네 가지를 알아야 하는데, 특수문자
	// 질의처럼 낱말이 하나도 안 남는 경우 missing·found·near 가 다 비어
	// 아무 말도 안 하게 된다 (스트레스 시험 D10·D11).
	Reason string `json:"reason,omitempty"`
	// Held 는 `--include-held` 를 켰으면 몇 건이 더 나왔을지다. 보류라서
	// 조용히 빠진 것을 "못 찾았다" 로 착각하면 안 된다 (뒷정리-3).
	Held int `json:"held,omitempty"`
}

// Why.Reason 값 넷.
const (
	// ReasonEmptyStore 는 색인에 기억이 한 건도 없는 것이다. 기억 파일은 있는데
	// 색인만 빈 경우는 그 앞에서 「색인이 깨졌거나 비었다」로 걸린다.
	ReasonEmptyStore = "empty-store"
	// ReasonNoWord 는 질의에서 쓸 낱말이 하나도 안 나온 것이다 (`!!!`).
	ReasonNoWord = "no-word"
	// ReasonFilters 는 낱말 없이 조건만 줘서 0건인 것이다.
	ReasonFilters = "filters"
	// ReasonNoMatch 는 낱말은 있는데 맞는 기억이 없는 것이다.
	ReasonNoMatch = "no-match"
)

// Result 는 답 한 벌이다.
type Result struct {
	Query   string        `json:"query"`
	Hits    []Hit         `json:"hits"`
	Total   int           `json:"total"`
	Ms      int64         `json:"ms"`
	Why     *Why          `json:"why,omitempty"`
	Rungs   map[int]int   `json:"rungs,omitempty"`
	Elapsed time.Duration `json:"-"`
	// Stopped 는 AND 에서 뺀 흔한 낱말이다.
	Stopped []string `json:"stopped,omitempty"`
	// TimeWord 는 좁힌 기간, TimeRelaxed 는 그 안에 없어서 다시 푼 경우다.
	TimeWord    string `json:"time_word,omitempty"`
	TimeRelaxed bool   `json:"time_relaxed,omitempty"`
	// TimeIgnored 는 --since 와 겹쳐서 안 쓴 질의 속 시간 표현이다 (리뷰B #18).
	TimeIgnored string `json:"time_ignored,omitempty"`
	// FilterOnly 는 낱말 없이 조건만 줘서 목록을 낸 경우다 (리뷰B #1).
	FilterOnly bool `json:"filter_only,omitempty"`
	// Dropped 는 낱말 상한을 넘어 안 쓴 낱말이다 (리뷰B #3).
	Dropped []string `json:"dropped,omitempty"`
	// MissingWords 는 저장소에 없어서 2번 칸이 뺀 낱말이다 (설계 6-6 칸 2).
	MissingWords []string `json:"missing_words,omitempty"`
	// LimitCapped 는 --limit 을 상한으로 깎았는지다 (리뷰B #21).
	LimitCapped bool `json:"limit_capped,omitempty"`
	// BadQuery 는 색인이 못 읽은 식이 있었는지다 (리뷰B #12).
	BadQuery bool `json:"bad_query,omitempty"`
	// Relaxed 는 0·1 칸보다 아래 칸을 밟았는지다.
	Relaxed bool `json:"relaxed,omitempty"`
	// Groups 는 같은 주제로 묶인 덩이 수다 (결정 44 묶음 반환).
	Groups  int  `json:"groups,omitempty"`
	NoIndex bool `json:"no_index,omitempty"`
	// Mode 는 이번 답이 낱말만으로 나온 것인지, 뜻까지 본 것인지다 (결정 16).
	Mode string `json:"mode"`
}

// Search 는 완화 사다리를 밟아 limit 을 채운다. 한 건 나왔다고 멈추지 않는다 —
// v0.0 의 가장 큰 버그가 거기 있었다 (설계 6-6).
func Search(options Options) (*Result, error) {
	started := time.Now()
	query := ParseQuery(options.Query, options.Stopwords)
	options.Synonym = config.SynonymBoth(options.Synonym)
	result := Result{Query: options.Query, Stopped: query.Stopped, Rungs: map[int]int{},
		Dropped: query.Dropped, LimitCapped: options.LimitCapped, Mode: ModeWord}
	if len(options.Sources) == 0 {
		result.NoIndex = true
		result.Hits = []Hit{}
		result.Elapsed = time.Since(started)
		return &result, nil
	}
	// 좁히기를 먼저 만든다. Empty 검사가 앞에 있으면 `지난주` 처럼 시간 낱말만
	// 있는 질의가 시간 범위를 통째로 잃고 저장소 전체 목록이 된다 (리뷰B #1).
	narrow, word, ignored := narrowOf(options, query)
	result.TimeWord, result.TimeIgnored = word, ignored
	if query.Empty() {
		return emptyQuery(&result, options, query, narrow, started)
	}
	found, err := climb(options, query, narrow)
	if err != nil {
		return nil, err
	}
	if len(found.hits) == 0 && word != "" {
		result.TimeRelaxed = true
		found, err = climb(options, query, options.Filter)
		if err != nil {
			return nil, err
		}
		narrow = options.Filter
	}
	result.BadQuery, result.MissingWords = found.bad, found.dropped
	if found.embedded {
		result.Mode = ModeMeaning
	}
	finish(&result, found.hits, options, query, narrow, started)
	recordMiss(&result, options, query)
	return &result, nil
}

// emptyProblem 은 낱말이 하나도 안 나온 질의를 갈라 준다. 셋 중 하나다 —
// 아무것도 안 친 목록 · 조건만 준 목록 · 물어볼 낱말이 없는 0건 (리뷰B #1·#2).
func emptyQuery(result *Result, options Options, query *Query, narrow index.Filter,
	started time.Time) (*Result, error) {
	if query.FilterOnly() {
		result.FilterOnly = true
		return listInto(result, options, narrow, started)
	}
	if strings.TrimSpace(options.Query) == "" {
		return listInto(result, options, options.Filter, started)
	}
	// 글자는 쳤는데 물어볼 낱말이 하나도 안 나왔다. 목록으로 새면 사람은
	// 그것을 검색 결과로 읽는다 — 0건보다 나쁘다 (설계 6-9 · 리뷰B #2).
	result.Hits = []Hit{}
	result.Total = 0
	result.Why = explainEmpty(options, query, narrow)
	result.Elapsed = time.Since(started)
	result.Ms = result.Elapsed.Milliseconds()
	recordMiss(result, options, query)
	return result, nil
}

// narrowOf 는 질의에서 뽑은 시간 범위와 태그·scope 를 옵션 필터에 얹는다.
// 셋째 답은 --since 와 겹쳐서 안 쓴 시간 낱말이다 — 조용히 버리면 사람은
// 자기가 쓴 `지난주` 가 먹혔다고 믿는다 (리뷰B #18).
func narrowOf(options Options, query *Query) (index.Filter, string, string) {
	narrow := options.Filter
	if len(query.Tags) > 0 {
		narrow.Tags = append(append([]string{}, narrow.Tags...), query.Tags...)
	}
	if len(query.Scopes) == 1 && narrow.Scope == "" {
		narrow.Scope = query.Scopes[0]
	}
	if query.TimeWord == "" {
		return narrow, "", ""
	}
	if !narrow.Since.IsZero() {
		return narrow, "", query.TimeWord
	}
	narrow.Since, narrow.Until = query.Since, query.Until
	return narrow, query.TimeWord, ""
}

// climb 은 0번 칸부터 6번 칸까지 밟는다. 이미 나온 문서는 처음 나온 칸으로
// 기록하고, limit 이 차면 멈춘다 (설계 6-6).
// 도우미는 **저장소마다 하나**를 만들어 일곱 칸이 같이 쓴다. 칸마다 새로
// 만들면 같은 낱말을 일곱 번 묻는다 (리뷰B #3).
func climb(options Options, query *Query, narrow index.Filter) (climbed, error) {
	limit := limitOf(options)
	found := []Hit{}
	seen := map[string]bool{}
	now := time.Now()
	bad := false
	helpers := make([]*rungHelper, len(options.Sources))
	for at, from := range options.Sources {
		helpers[at] = &rungHelper{db: from.DB, synonym: options.Synonym,
			synWeight: options.synWeight(), norms: normMapOf(query)}
	}
	for level := 0; level < rungCount; level++ {
		if len(found) >= limit {
			break
		}
		for at, from := range options.Sources {
			// 관문은 0·1 칸 자격만 정한다. 2번 칸부터는 설계 6-6 대로 계속
			// 밟되 꼬리표와 알림 줄이 붙는다 — 사다리를 통째로 끊으면
			// `사운드 믹서` 처럼 남은 낱말이 정답을 짚는 질의까지 죽는다.
			hits, err := rungHits(from, helpers[at], level, query, options, narrow, now)
			if err != nil {
				return climbed{}, err
			}
			for _, hit := range hits {
				if seen[hit.ID] {
					continue
				}
				seen[hit.ID] = true
				found = append(found, hit)
			}
		}
	}
	out := climbed{hits: found}
	for _, helper := range helpers {
		bad = bad || helper.bad
		out.embedded = out.embedded || helper.embedded
		for _, word := range helper.dropped {
			out.dropped = appendUnique(out.dropped, word)
		}
	}
	out.bad = bad
	return out, nil
}

// climbed 는 사다리 한 벌이 낸 것 전부다.
type climbed struct {
	hits    []Hit
	dropped []string
	bad     bool
	// embedded 는 임베딩 신호가 실제로 한 표라도 던졌는지다 (결정 16 모드 표시).
	embedded bool
}

// rungHits 는 저장소 하나에서 한 칸을 돌려 점수까지 매긴다.
func rungHits(from Source, helper *rungHelper, level int, query *Query, options Options,
	narrow index.Filter, now time.Time) ([]Hit, error) {
	ranks := buildRung(level, query, helper)
	if len(ranks) == 0 {
		return nil, nil
	}
	merged, err := runRung(helper, ranks, options.softener())
	if err != nil {
		return nil, err
	}
	if len(merged.scores) == 0 {
		return nil, nil
	}
	// 재순위와 임베딩은 RRF 표를 한 장씩 더 얹는 자리다. 관문보다 앞에 둬야
	// 「잘 맞았는데 FTS5 가 낮게 본 것」 이 후보 안으로 들어온다 (설계 4-1).
	if err := rerankInto(from, helper, merged, query, options); err != nil {
		return nil, err
	}
	if err := embedInto(helper, merged, query, options); err != nil {
		return nil, err
	}
	candidates, err := gateOf(helper, level, query, merged.docidsOf())
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	rows, err := from.DB.FetchRows(candidates, narrow, now)
	if err != nil {
		return nil, err
	}
	shared, err := rulesFor(from.DB, rows, query, options, now)
	if err != nil {
		return nil, err
	}
	return rankRows(rows, merged, level, shared), nil
}

// types 는 이 저장소의 종류 표다. 안 받았으면 기본 7종이다.
func (o Options) types() model.TypeTable {
	if len(o.Types) == 0 {
		return defaultTypes
	}
	return o.Types
}

func rulesFor(database *index.DB, rows []index.SearchRow, query *Query, options Options, now time.Time) (rules, error) {
	shared := rules{Raw: options.Raw, Cap: options.cap(), BoostScope: options.BoostScope, Now: now,
		Phrase: map[int64]bool{}, Types: options.types()}
	ids := make([]string, 0, len(rows))
	docids := make([]int64, 0, len(rows))
	for _, item := range rows {
		ids = append(ids, item.ID)
		docids = append(docids, item.Docid)
	}
	linked, err := database.LinkTargets(ids)
	if err != nil {
		return shared, err
	}
	shared.Linked = linked
	for _, phrase := range query.Phrases {
		hit, err := database.PhraseDocids(docids, phrase)
		if err != nil {
			return shared, err
		}
		for docid := range hit {
			shared.Phrase[docid] = true
		}
	}
	return shared, nil
}

func rankRows(rows []index.SearchRow, merged *rungResult, level int, shared rules) []Hit {
	hits := make([]Hit, 0, len(rows))
	for _, row := range rows {
		total, broken := scoreOf(row, merged.scores[row.Docid], level, shared)
		broken.From = merged.from[row.Docid]
		hit := hitOf(row, shared.Now)
		hit.Score = total
		hit.Rung = level
		hit.Parts = broken
		hits = append(hits, hit)
	}
	sortHits(hits)
	return hits
}

// finish 는 정렬·자르기·0건 설명을 한다.
func finish(result *Result, hits []Hit, options Options, query *Query, narrow index.Filter, started time.Time) {
	sortHits(hits)
	// 관문을 지난 뒤 차례 : MMR → 1-hop 가산 (설계 4절 그림 · 결정 44).
	// 둘 다 후보를 안 늘리고 안 자른다.
	edges, err := edgesAmong(options.Sources, hits)
	if err != nil {
		edges = linkEdges{}
	}
	hits = diversify(hits, options.Raw)
	hits = hopBonus(hits, edges, options.cap(), options.Raw)
	result.Total = len(hits)
	for _, hit := range hits {
		result.Rungs[hit.Rung]++
		if !Strict(hit.Rung) {
			result.Relaxed = true
		}
	}
	result.Hits = cut(hits, limitOf(options))
	result.Groups = groupHits(result.Hits)
	if len(result.Hits) == 0 {
		result.Hits = []Hit{}
		result.Why = explainEmpty(options, query, narrow)
	}
	result.Elapsed = time.Since(started)
	result.Ms = result.Elapsed.Milliseconds()
}

// listInto 는 질의 없이 조건만 준 목록이다. 점수 없이 날짜 내림이다.
// narrow 는 질의에서 뽑은 시간·태그·범위까지 얹은 조건이다 (리뷰B #1).
func listInto(result *Result, options Options, narrow index.Filter, started time.Time) (*Result, error) {
	now := time.Now()
	hits := []Hit{}
	whole := 0
	for _, from := range options.Sources {
		rows, err := from.DB.ListRows(narrow, limitOf(options), now)
		if err != nil {
			return nil, err
		}
		counted, err := from.DB.CountRows(narrow, now)
		if err != nil {
			return nil, err
		}
		whole += counted
		for _, row := range rows {
			hits = append(hits, hitOf(row, now))
		}
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].Date != hits[b].Date {
			return hits[a].Date > hits[b].Date
		}
		return hits[a].ID < hits[b].ID
	})
	result.Total = whole
	result.Hits = cut(hits, limitOf(options))
	if len(result.Hits) == 0 {
		// 조건만 준 검색도 0건이면 왜 0건인지 말해야 한다 (불변조건 9 · 리뷰C #10).
		result.Hits = []Hit{}
		result.Why = whyFilters(options, narrow, result.TimeWord)
	}
	result.Elapsed = time.Since(started)
	result.Ms = result.Elapsed.Milliseconds()
	return result, nil
}

// Facet 은 scope 나 tag 를 값마다 몇 건인지 센다 (설계 8-1 `--facet`).
func Facet(options Options, kind string) ([]index.VocabCount, error) {
	totals := map[string]int{}
	now := time.Now()
	for _, from := range options.Sources {
		counts, err := from.DB.FacetCounts(kind, options.Filter, now)
		if err != nil {
			return nil, err
		}
		for _, item := range counts {
			totals[item.Name] += item.Count
		}
	}
	out := make([]index.VocabCount, 0, len(totals))
	for name, count := range totals {
		out = append(out, index.VocabCount{Name: name, Count: count})
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Count != out[b].Count {
			return out[a].Count > out[b].Count
		}
		return out[a].Name < out[b].Name
	})
	return out, nil
}

// explainEmpty 는 0건일 때 왜 0건인지 넷을 모은다 (설계 6-9).
func explainEmpty(options Options, query *Query, narrow index.Filter) *Why {
	why := Why{Found: map[string]int{}, Near: []index.VocabCount{}, Missing: []string{}}
	for _, term := range query.Terms {
		count := 0
		for _, from := range options.Sources {
			count += countTerm(from.DB, term)
		}
		if count == 0 {
			why.Missing = append(why.Missing, term.Text)
			continue
		}
		why.Found[term.Text] = count
	}
	why.Near = nearWords(options, why.Missing)
	why.Next = nextCommand(why.Found, len(query.Terms))
	why.Total, why.LastIndex = wholeCount(options)
	why.Reason = reasonOf(&why, len(query.Terms))
	why.Held = heldMatchCount(options, query, narrow)
	return &why
}

// heldMatchCount 는 지금 질의로 `--include-held` 를 켜면 몇 건이 나왔을지다.
// 0건이 "못 찾았다" 가 아니라 "보류라 안 보였다" 일 수 있어서 잰다 (뒷정리-3).
func heldMatchCount(options Options, query *Query, narrow index.Filter) int {
	if narrow.IncludeHeld {
		return 0
	}
	held := narrow
	held.IncludeHeld = true
	found, err := climb(options, query, held)
	if err != nil {
		return 0
	}
	return len(found.hits)
}

// reasonOf 는 0건의 까닭 하나를 고른다. 색인이 빈 것을 먼저 본다 — 그것이면
// 나머지 설명은 다 헛말이다.
func reasonOf(why *Why, terms int) string {
	switch {
	case why.Total == 0:
		return ReasonEmptyStore
	case terms == 0:
		return ReasonNoWord
	default:
		return ReasonNoMatch
	}
}

// whyFilters 는 조건만 준 검색이 0건일 때의 설명이다. 어떤 조건을 걸었는지
// 그대로 되읊어 준다 — 오타 난 --type 이 조용한 0건이 되는 것을 막는다.
func whyFilters(options Options, narrow index.Filter, timeWord string) *Why {
	why := Why{Found: map[string]int{}, Near: []index.VocabCount{}, Missing: []string{},
		Filters: filterWords(narrow, timeWord)}
	why.Total, why.LastIndex = wholeCount(options)
	why.Reason = ReasonFilters
	if why.Total == 0 {
		why.Reason = ReasonEmptyStore
	}
	why.Held = heldFilterCount(options, narrow)
	return &why
}

// heldFilterCount 는 같은 조건에 `--include-held` 만 더하면 몇 건이 느는지다.
// 조건만 준 목록(listInto)은 이미 이 narrow 로 0건을 확인한 뒤라, 여기서
// IncludeHeld 만 켠 값 그대로가 보류 때문에 빠진 건수다 (뒷정리-3).
func heldFilterCount(options Options, narrow index.Filter) int {
	if narrow.IncludeHeld {
		return 0
	}
	held := narrow
	held.IncludeHeld = true
	now := time.Now()
	total := 0
	for _, from := range options.Sources {
		count, err := from.DB.CountRows(held, now)
		if err == nil {
			total += count
		}
	}
	return total
}

// filterWords 는 걸린 조건을 사람이 읽는 한 줄짜리 조각으로 만든다.
func filterWords(narrow index.Filter, timeWord string) []string {
	out := []string{}
	for _, item := range narrow.Types {
		out = append(out, "--type "+item)
	}
	if narrow.Scope != "" {
		out = append(out, "--scope "+narrow.Scope)
	}
	for _, item := range narrow.Tags {
		out = append(out, "--tag "+item)
	}
	if narrow.Status != "" {
		out = append(out, "--status "+narrow.Status)
	}
	if narrow.Severity != "" {
		out = append(out, "--severity "+narrow.Severity)
	}
	if narrow.Pinned {
		out = append(out, "--pinned")
	}
	if timeWord != "" {
		out = append(out, timeWord)
	}
	if !narrow.Since.IsZero() && timeWord == "" {
		out = append(out, "--since "+narrow.Since.Format("2006-01-02"))
	}
	return out
}

// wholeCount 는 저장소를 다 더한 건수와 가장 최근 색인 시각이다. 예전에는
// 첫 저장소 것만 쓰면 수가 틀렸다 (리뷰B #28).
func wholeCount(options Options) (int, string) {
	total, last := 0, ""
	for _, from := range options.Sources {
		counted, err := from.DB.Count()
		if err == nil {
			total += counted
		}
		when, err := from.DB.LastIndexAt()
		if err == nil && when > last {
			last = when
		}
	}
	return total, last
}

func countTerm(database *index.DB, term Term) int {
	total := 0
	total += database.WordCount("fts_ko", joinTerms([]Term{term}, false, plainOf, token.RunKO, token.RunKO1))
	total += database.WordCount("fts_en", joinTerms([]Term{term}, false, plainOf, token.RunEN))
	return total
}

// nearWords 는 제목·요약에 있는 닮은 낱말이다. 예전에는 어휘표(vocab_ko)를
// 봤는데 그것은 바이그램이라 앞 두 글자로 찾으면 자기 자신밖에 안 나왔다 —
// ③ 이 늘 "닮은 낱말도 없다" 였던 까닭이다 (리뷰B #19).
func nearWords(options Options, missing []string) []index.VocabCount {
	out := []index.VocabCount{}
	for _, word := range missing {
		letters := []rune(word)
		if len(letters) < 2 || !token.HasHangul(word) {
			continue
		}
		for _, from := range options.Sources {
			found, err := from.DB.HeadWordsLike(string(letters[:2]), 3)
			if err != nil {
				continue
			}
			out = append(out, found...)
		}
	}
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

// nextWords 는 다시 쳐 볼 때 남길 낱말 수다. 다 남기면 방금 0건 난 질의와
// 똑같은 명령을 권하게 된다 (리뷰B #20).
const nextWords = 2

// nextCommand 는 바로 칠 수 있는 다음 명령이다. 저장소에 있는 낱말 중 가장
// 많이 나오는 둘만 남긴다. 질의를 그대로 되돌려 주면 도움이 아니다.
func nextCommand(found map[string]int, asked int) string {
	words := make([]string, 0, len(found))
	for word := range found {
		words = append(words, word)
	}
	if len(words) == 0 {
		return ""
	}
	sort.Slice(words, func(a, b int) bool {
		if found[words[a]] != found[words[b]] {
			return found[words[a]] > found[words[b]]
		}
		return words[a] < words[b]
	})
	if len(words) > nextWords {
		words = words[:nextWords]
	}
	// 남은 것이 물어본 낱말 전부라면 같은 질의다. 그러면 아무 말도 안 한다.
	if len(words) == asked {
		return ""
	}
	return "mem search " + strings.Join(words, " ")
}

// recordMiss 는 0건이거나 느슨한 칸에서만 답이 난 질의를 local/miss.jsonl 에
// 한 줄 남긴다. lint 의 synonym-candidate 가 이 파일을 읽어 동의어 후보를
// 들어 준다 — 여태 쓰는 코드가 없어 되먹임 고리가 죽어 있었다 (설계 6-7 · 리뷰B #6).
func recordMiss(result *Result, options Options, query *Query) {
	if options.MissPath == "" || len(query.Terms) == 0 {
		return
	}
	if len(result.Hits) > 0 && !result.Relaxed {
		return
	}
	line, err := json.Marshal(missRecord{Query: query.Raw, Words: query.Words(), At: time.Now().Unix()})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(options.MissPath), 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(options.MissPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Size() > missLogCap {
		return
	}
	file.Write(append(line, '\n'))
}

// missLogCap 은 기록 파일 상한이다. 넘으면 그냥 안 쓴다 — 검색이 디스크를
// 채우는 일은 없어야 한다.
const missLogCap = 1 << 20

// missRecord 는 miss.jsonl 한 줄이다. lint(repo.go missLine)가 읽는 칸 이름과
// 똑같아야 한다.
type missRecord struct {
	Query string   `json:"q"`
	Words []string `json:"words"`
	At    int64    `json:"at"`
}

func hitOf(row index.SearchRow, now time.Time) Hit {
	return Hit{
		ID: row.ID, Type: row.Type, Date: time.Unix(row.CreatedAt, 0).Format("2006-01-02"),
		Title: row.Title, Summary: row.Summary, Scope: row.Scope, Tags: strings.Fields(row.Tags),
		TodoStatus: row.TodoStatus, Severity: row.Severity, Author: row.Author, Pinned: row.Pinned,
		InvalidAt: dateOf(row.InvalidAt), SupersededBy: row.SupersededBy,
		Invalid: invalidated(row, now),
	}
}

// invalidated 는 이 기억이 무효인지다 — 기한이 지났거나 새 기억이 덮었다.
// 좁히기 SQL(index.whereOf)과 같은 규칙이어야 한다. 아직 안 온 invalid_at 은
// 무효가 아니다 — 그때까지는 맞는 말이다.
func invalidated(row index.SearchRow, now time.Time) bool {
	if row.SupersededBy != "" {
		return true
	}
	return row.InvalidAt != 0 && row.InvalidAt <= now.Unix()
}

func dateOf(stamp int64) string {
	if stamp == 0 {
		return ""
	}
	return time.Unix(stamp, 0).Format("2006-01-02")
}

// sortHits 는 점수 내림, 동점은 날짜 내림 → id 오름이다 (설계 6-5).
func sortHits(hits []Hit) {
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].Score != hits[b].Score {
			return hits[a].Score > hits[b].Score
		}
		if hits[a].Date != hits[b].Date {
			return hits[a].Date > hits[b].Date
		}
		return hits[a].ID < hits[b].ID
	})
}

func cut(hits []Hit, limit int) []Hit {
	if len(hits) <= limit {
		return hits
	}
	return hits[:limit]
}

func limitOf(options Options) int {
	if options.Limit <= 0 {
		return DefaultLimit
	}
	return options.Limit
}
