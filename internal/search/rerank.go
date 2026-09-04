package search

import (
	"sort"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/embed"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/token"
)

// 재순위 (설계 결정 18 · 4-1).
//
// FTS5 의 bm25 는 k1·b 를 못 바꾼다. 그래서 한 칸이 낸 후보 중 **상위
// RerankTop(200)건만** 우리 손으로 다시 매겨(k1=1.2 · b=0.4 · 가중 16/6/1)
// RRF 에 **랭킹 하나로 얹는다.** 대신 매기는 것이 아니다 — 얹는 것이라
// FTS5 랭킹이 이미 잘 짚은 자리는 그대로 남는다.

// phraseFactor 는 본문에 구절이 그대로 있을 때 재순위 점수에 곱하는 값이다.
// 벌은 없다 — 구절이 없다고 깎으면 띄어쓰기 하나에 답이 뒤집힌다 (설계 4-1).
const phraseFactor = 1.2

// rerankInto 는 상위 몇 건을 다시 매겨 merged 에 한 표를 더한다.
func rerankInto(from Source, helper *rungHelper, merged *rungResult, query *Query,
	options Options) error {
	terms := rerankTerms(query)
	if len(terms) == 0 {
		return nil
	}
	docids := merged.docidsOf()
	if len(docids) > options.rerankTop() {
		docids = docids[:options.rerankTop()]
	}
	if len(docids) == 0 {
		return nil
	}
	rows, err := helper.rerankRows(docids)
	if err != nil {
		return err
	}
	freq, err := helper.docFreq(terms)
	if err != nil {
		return err
	}
	stats, err := helper.corpus()
	if err != nil {
		return err
	}
	scored := []scoredDoc{}
	for _, row := range rows {
		value := index.BM25(row, terms, freq, stats, options.weights(), options.k1(), options.b())
		if value <= 0 {
			continue
		}
		if hasPhrase(row.Body, query.Phrases) {
			value *= phraseFactor
		}
		scored = append(scored, scoredDoc{docid: row.Docid, value: value})
	}
	sort.Slice(scored, func(a, b int) bool {
		if scored[a].value != scored[b].value {
			return scored[a].value > scored[b].value
		}
		return scored[a].docid < scored[b].docid
	})
	if len(scored) > rankDepth {
		scored = scored[:rankDepth]
	}
	for place, item := range scored {
		merged.scores[item.docid] += weightRerank / (options.softener() + float64(place+1))
		merged.from[item.docid] = append(merged.from[item.docid], "bm25("+placeText(place+1)+")")
	}
	return nil
}

type scoredDoc struct {
	docid int64
	value float64
}

// rerankTerms 는 재순위가 셀 낱말 조각이다. 색인에 넣을 때 쓴 함수를 그대로
// 지나야 조각 모양이 맞는다 (`색인` → `색인` 바이그램).
func rerankTerms(query *Query) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, item := range query.Live() {
		for _, piece := range strings.Fields(token.ForIndex(item.Text)) {
			if seen[piece] {
				continue
			}
			seen[piece] = true
			out = append(out, piece)
		}
	}
	return out
}

// hasPhrase 는 본문에 따옴표 구절이 그대로 있는지다. FTS5 를 detail=column 으로
// 줄이면 구절 질의를 못 쓰므로 원문 글자 검사로 대신한다 (설계 4-7).
func hasPhrase(body string, phrases []string) bool {
	for _, phrase := range phrases {
		if phrase != "" && strings.Contains(body, phrase) {
			return true
		}
	}
	return false
}

// 임베딩 갈래 (설계 4-8 · 결정 21).
//
// **이미 낱말이 찾아 놓은 후보의 차례만 바꾼다.** 후보를 새로 데려오지
// 않는다 — 그래야 abstain 관문(4-5)이 낱말 쪽 결과 그대로 판정되고,
// 「없으면 없다고 한다」(G2) 가 임베딩 때문에 뒤집히지 않는다.
// 닮음이 `[embed] floor` 아래인 것은 아예 표에서 뺀다.

// embedFieldWeight 는 문서 벡터를 만들 때 칸마다 싣는 무게다
// (제목·메타·요약·본문). 칸마다 따로 정규화한 뒤에 섞는다 — 안 그러면 본문이
// 요약보다 백 배 길어서 요약이 아무 몫도 못 한다.
// 이 길은 v0.2 의 정적 낱말 표(ko.bin) 전용이라 의미 모드 측정에는 안 쓰인다.
// v0.3 의 head 2 를 제목 2 · 메타 1 로 갈랐다.
var embedFieldWeight = [4]float64{2, 1, 2, 1}

// embedInto 는 코사인 순위표를 만들어 RRF 표 한 장만 얹는다.
// 벡터를 어디서 얻는지는 둘이다 — 밖에서 꽂아 준 `Options.Vectors`(3A 의 ORT)가
// 먼저고, 없으면 v0.2 의 정적 표다. 둘 다 없으면 아무 일도 안 한다 (결정 16).
func embedInto(helper *rungHelper, merged *rungResult, query *Query, options Options) error {
	if options.Vectors != nil {
		return vectorsInto(helper, merged, query, options)
	}
	if !options.Embed.Enabled {
		return nil
	}
	table := helper.embedTable(options)
	if table == nil {
		return nil
	}
	wanted := table.Text(embedQueryText(query))
	if wanted == nil {
		return nil
	}
	helper.embedded = true
	docids := merged.docidsOf()
	if len(docids) > options.rerankTop() {
		docids = docids[:options.rerankTop()]
	}
	if len(docids) == 0 {
		return nil
	}
	rows, err := helper.rerankRows(docids)
	if err != nil {
		return err
	}
	floor := options.embedFloor()
	scored := []scoredDoc{}
	for _, row := range rows {
		vector := helper.docVector(table, row)
		if vector == nil {
			continue
		}
		value := embed.Dot(wanted, vector)
		if value < floor {
			continue
		}
		scored = append(scored, scoredDoc{docid: row.Docid, value: value})
	}
	applyEmbed(merged, docids, scored, options)
	return nil
}

// vectorsInto 는 꽂아 준 임베딩으로 코사인 순위표를 만들어 RRF 표 한 장을
// 얹는다. 정적 표 갈래와 규칙이 같다 — **후보를 새로 데려오지 않고**, floor
// 아래 닮음은 표에서 뺀다. 그래야 abstain 관문이 낱말 쪽 결과 그대로 판정된다.
func vectorsInto(helper *rungHelper, merged *rungResult, query *Query, options Options) error {
	wanted := options.Vectors.Query(embedQueryText(query))
	if len(wanted) == 0 {
		return nil
	}
	helper.embedded = true
	docids := merged.docidsOf()
	if len(docids) > options.rerankTop() {
		docids = docids[:options.rerankTop()]
	}
	if len(docids) == 0 {
		return nil
	}
	floor := options.embedFloor()
	scored := []scoredDoc{}
	for docid, vector := range options.Vectors.Docs(docids) {
		value := dot32(wanted, vector)
		if value < floor {
			continue
		}
		scored = append(scored, scoredDoc{docid: docid, value: value})
	}
	applyEmbed(merged, docids, scored, options)
	return nil
}

// dot32 는 단위 벡터 둘의 내적이다. 길이가 다르면 짧은 쪽까지만 센다 —
// 모델을 바꿔 차원이 달라졌을 때 죽지 않는다.
func dot32(left, right []float32) float64 {
	total := 0.0
	size := len(left)
	if len(right) < size {
		size = len(right)
	}
	for at := 0; at < size; at++ {
		total += float64(left[at]) * float64(right[at])
	}
	return total
}

// embedQueryText 는 임베딩이 읽을 질의 글이다. `#태그`·`scope:이름` 같은
// 조건 표시는 뜻이 아니라 좁히기라서 뺀다.
func embedQueryText(query *Query) string {
	kept := []string{}
	for _, word := range strings.Fields(query.Raw) {
		if strings.HasPrefix(word, "#") || strings.Contains(word, ":") {
			continue
		}
		kept = append(kept, word)
	}
	if len(kept) == 0 {
		return query.Raw
	}
	return strings.Join(kept, " ")
}

// embedTable 은 표를 한 질의에 한 번만 읽는다. 파일이 없으면 nil 이고, 그러면
// 낱말 검색만 돈다 (우아한 퇴화).
func (h *rungHelper) embedTable(options Options) *embed.Table {
	if h.tableAsked {
		return h.table
	}
	h.tableAsked = true
	h.table = embed.Open(options.Embed.Path, options.RepoDir)
	return h.table
}

// docVector 는 문서 하나의 단위 벡터다. 같은 문서를 칸마다 다시 세지 않게
// 기억해 둔다.
func (h *rungHelper) docVector(table *embed.Table, row index.RerankRow) []float64 {
	if h.vectors == nil {
		h.vectors = map[int64][]float64{}
	}
	if made, asked := h.vectors[row.Docid]; asked {
		return made
	}
	total := table.Zero()
	fields := [4]string{row.TitleKO, row.MetaKO, row.SummaryKO, row.BodyKO}
	for at, text := range fields {
		part := table.Zero()
		if table.Add(part, strings.Fields(text), 1) == 0 {
			continue
		}
		unit := embed.Unit(part)
		for slot, value := range unit {
			total[slot] += embedFieldWeight[at] * value
		}
	}
	made := embed.Unit(total)
	h.vectors[row.Docid] = made
	return made
}

// 아래 셋은 한 질의 · 한 저장소에서 한 번만 묻는 자리다. 사다리 일곱 칸이
// 같은 것을 되풀이 물으면 그것이 곧 느림이다 (리뷰B #3).

func (h *rungHelper) rerankRows(docids []int64) ([]index.RerankRow, error) {
	if h.rows == nil {
		h.rows = map[int64]index.RerankRow{}
	}
	ask := make([]int64, 0, len(docids))
	for _, docid := range docids {
		if _, have := h.rows[docid]; !have {
			ask = append(ask, docid)
		}
	}
	if len(ask) > 0 {
		found, err := h.db.RerankRows(ask)
		if err != nil {
			return nil, err
		}
		for _, row := range found {
			h.rows[row.Docid] = row
		}
	}
	out := make([]index.RerankRow, 0, len(docids))
	for _, docid := range docids {
		if row, have := h.rows[docid]; have {
			out = append(out, row)
		}
	}
	return out, nil
}

func (h *rungHelper) docFreq(terms []string) (map[string]int, error) {
	if h.freq != nil {
		return h.freq, nil
	}
	found, err := h.db.DocFreq(terms)
	if err != nil {
		return nil, err
	}
	h.freq = found
	return found, nil
}

func (h *rungHelper) corpus() (index.CorpusStats, error) {
	if h.statsAsked {
		return h.stats, nil
	}
	found, err := h.db.CorpusStats()
	if err != nil {
		return found, err
	}
	h.stats, h.statsAsked = found, true
	return found, nil
}
