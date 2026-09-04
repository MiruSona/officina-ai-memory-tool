package search

import (
	"errors"
	"sort"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/embed"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/token"
)

// 사다리 칸 이름. 결과 줄 꼬리표와 --explain 이 그대로 쓴다 (설계 6-6).
const (
	RungPlain    = 0
	RungAnd      = 1
	RungDropWord = 2
	RungOr       = 3
	RungSynonym  = 4
	RungSplit    = 5
	RungLike     = 6
	rungCount    = 7
)

// TrustOf 는 칸마다 곱하는 신뢰 계수다. 정확히 맞은 것이 언제나 위에 온다 (설계 6-6).
func TrustOf(rung int) float64 {
	switch rung {
	case RungPlain, RungAnd:
		return 1.0
	case RungDropWord, RungOr:
		return 0.85
	case RungSynonym:
		return 0.7
	case RungSplit:
		return 0.6
	}
	return 0.5
}

// strictRungs 는 「정확히 찾았다」 로 세는 칸이다 (설계 결정 29 · 4-1a).
// 자는 신뢰계수 0.60 이고, 거기서 **동의어(4)와 LIKE(6)만 뺀다** — 그 둘은
// 뜻을 바꿀 수 있다. OR(3)도 뺀다 : 낱말 하나만 맞아도 앉는 칸이라 넣으면
// abstain 이 무너진다(G2). 설계 표의 "신뢰계수 ≥ 0.60" 을 칸 목록으로 못
// 박은 것이고, 다른 것은 이 한 줄이다.
var strictRungs = [rungCount]bool{
	RungPlain: true, RungAnd: true, RungDropWord: true, RungSplit: true,
}

// Strict 는 그 칸에서 얻은 답을 「정확히 찾았다」 로 세는지다. eval 과 훅이
// 이 하나만 본다 (설계 4-1a). 예전의 `StrictRung` 상수는 칸 번호가 이어져
// 있다고 가정해서 5번 칸을 못 담았다.
func Strict(rung int) bool {
	if rung < 0 || rung >= rungCount {
		return false
	}
	return strictRungs[rung]
}

// GateRung 은 자격 관문이 도는 마지막 칸이다. 2번 칸부터는 넓히는 자리라
// 관문을 걸면 사다리 자체가 죽는다 (설계 4-1a).
const GateRung = RungAnd

// 기본 k 와 상한은 config 에 있다. 설계가 적은 60 을 쓰지 않는 이유는 거기
// 주석에 적었다 (config.SearchConfig).
// 옛 설명 :
// rrfK 는 RRF 의 완충값이다. 설계는 Elasticsearch·Qdrant 기본값 60 을 적었지만
// 그 값은 수천 줄짜리 목록을 합칠 때 것이다. 우리 목록은 rankDepth(50)줄이라
// 60 이면 1위와 50위의 차이가 1.8배로 눌려 가산이 관련도를 덮는다(조사E 유형 C).
// 목록 길이의 5분의 1로 잡는다 — 골든셋에서 MRR 0.440 → 0.523 이었다.

// 랭킹 가중 (설계 6-4). 동의어 랭킹은 정확히 쓴 낱말이 늘 위에 오게 절반이다.
const (
	weightKO   = 1.0
	weightEN   = 1.0
	weightTag  = 0.6
	weightLike = 0.5
	weightStem = 0.8
	// weightRerank 는 우리 bm25 재순위 랭킹의 몫이다. FTS5 랭킹과 같은
	// 무게로 시작한다 — 재순위는 대신 매기는 것이 아니라 한 표를 더 얹는
	// 것이다 (설계 4-1 랭킹 합치기).
	weightRerank = 1.0
	// weightWord 는 낱말 하나짜리 랭킹의 가중이다. AND 가 죽어도 낱말마다
	// 한 표씩은 나와야 한다 — 다 맞은 문서는 AND 표까지 받아 늘 위에 온다.
	weightWord = 0.4
	// oneLetterFactor 는 한글 한 글자 접두 랭킹을 깎는 비율이다.
	oneLetterFactor = 0.5
)

// maxSynonyms 는 한 낱말이 데려올 수 있는 동의어 수다 (설계 6-7).
const maxSynonyms = 3

// rankDepth 는 랭킹 하나를 몇 위까지 읽는지다. recall@10 을 재려면 열보다는
// 깊어야 하고, 너무 깊으면 RRF 가 잡음을 물고 온다.
const rankDepth = 50

// ranking 은 한 칸에서 던지는 순위표 하나다.
type ranking struct {
	name   string
	table  string
	expr   string
	weight float64
}

// rungResult 는 한 칸이 낸 후보와 왜 그렇게 됐는지다.
type rungResult struct {
	scores map[int64]float64
	from   map[int64][]string
	ranks  []ranking
}

// buildRung 은 칸 하나가 던질 랭킹 목록을 만든다. 빈 목록이면 그 칸은 건너뛴다.
func buildRung(level int, query *Query, helper *rungHelper) []ranking {
	switch level {
	case RungPlain:
		return matchRankings(query.Terms, false, query, helper)
	case RungAnd:
		return andRankings(query, helper)
	case RungDropWord:
		return dropRankings(query, helper)
	case RungOr:
		return matchRankings(query.Live(), true, query, helper)
	case RungSynonym:
		return synonymRankings(query, helper)
	case RungSplit:
		return splitRankings(query, helper)
	}
	return likeRankings(query)
}

// rungHelper 는 칸을 만들 때 색인에 물어봐야 하는 것들이다. **한 질의 · 한
// 저장소에 하나만 만들어 일곱 칸이 같이 쓴다.** 칸마다 새로 만들면 같은 낱말을
// 일곱 번 묻게 되고, 20k 에서 그것이 곧 500ms 다 (리뷰B #3).
type rungHelper struct {
	db      *index.DB
	synonym map[string][]string
	// synWeight 는 동의어 랭킹의 몫이다 (mem.toml [search] syn_weight).
	synWeight float64
	// splits 는 낱말 하나를 쪼갠 결과를 기억해 둔다. 한 질의에서 칸을 여러 번
	// 밟아도 어휘표에 묻는 횟수가 낱말당 상수로 묶인다 (설계 6-1b).
	splits map[string][][]string
	// heads 는 붙은 낱말의 아는 앞머리다.
	heads map[string]string
	// ranks 는 던진 랭킹의 답이다. 칸이 달라도 식이 같으면 다시 안 묻는다.
	ranks map[string][]index.Ranked
	// norms 는 낱말마다 정규화된 꼴이다. 질의 하나에 한 번만 만든다 (결정 21).
	norms map[string]string
	// exists 는 그 식이 저장소에 있는지, exprs 는 낱말을 묻는 꼴 목록이다.
	exists map[string]bool
	exprs  map[string][]askShape
	// bad 는 FTS5 가 못 읽은 식이 하나라도 있었는지다 (리뷰B #12).
	bad bool
	// dropped 는 2번 칸이 저장소에 없어서 뺀 낱말이다. 알림 줄이 쓴다.
	dropped []string
	// 아래 셋은 재순위 재료다. 한 질의에 한 번만 묻는다.
	rows       map[int64]index.RerankRow
	freq       map[string]int
	stats      index.CorpusStats
	statsAsked bool
	// table 과 vectors 는 임베딩 갈래 것이다. 표는 한 질의에 한 번만 읽고
	// 문서 벡터는 칸마다 다시 안 센다 (설계 4-8).
	table      *embed.Table
	tableAsked bool
	vectors    map[int64][]float64
	// embedded 는 임베딩 신호가 실제로 돌았는지다. 모드 표시가 이것만 본다
	// (결정 16) — 켜 놓고 모델이 없으면 낱말 모드라고 적어야 한다.
	embedded bool
}

// splitsOf 는 붙여 쓴 낱말의 쪼개기 후보다. 같은 낱말은 한 번만 묻는다.
func (h *rungHelper) splitsOf(word string) [][]string {
	if h.splits == nil {
		h.splits = map[string][][]string{}
	}
	found, asked := h.splits[word]
	if asked {
		return found
	}
	found = token.SplitVocab(word, h.db.KnownWord)
	h.splits[word] = found
	return found
}

// prefixOf 는 붙여 쓴 낱말에서 색인이 아는 가장 긴 앞머리다. 같은 낱말은 한 번만 묻는다.
func (h *rungHelper) knownPrefixOf(word string) string {
	if h.heads == nil {
		h.heads = map[string]string{}
	}
	found, asked := h.heads[word]
	if asked {
		return found
	}
	found, _ = token.KnownPrefix(word, h.db.KnownWord)
	h.heads[word] = found
	return found
}

// matchRankings 는 R1·R2·R1′·R3 을 한 벌 만든다.
func matchRankings(terms []Term, anyWord bool, query *Query, helper *rungHelper) []ranking {
	if len(terms) == 0 {
		return nil
	}
	out := []ranking{}
	out = appendRanking(out, ranking{name: "R1", table: "fts_ko", weight: weightKO,
		expr: joinTerms(terms, anyWord, plainOf, token.RunKO, token.RunKO1)})
	out = appendRanking(out, ranking{name: "R2", table: "fts_en", weight: weightEN,
		expr: joinTerms(terms, anyWord, plainOf, token.RunEN)})
	out = appendRanking(out, ranking{name: "R1'", table: "fts_ko", weight: weightStem,
		expr: stemExpr(terms)})
	out = appendRanking(out, ranking{name: "R3", table: "tag", weight: weightTag,
		expr: strings.Join(tagWords(query), "\n")})
	out = appendRanking(out, ranking{name: "원형", table: "fts_ko", weight: weightStem,
		expr: gluedExpr(terms)})
	out = appendNormRankings(out, terms, anyWord)
	out = append(out, wordRanks(terms)...)
	return append(out, synonymRanks(terms, helper, anyWord)...)
}

// wordRanks 는 낱말마다 한 표씩 주는 랭킹이다. 질의의 한 낱말이 정답 문서에
// 아예 없으면 AND 는 통째로 죽는데, 그때도 남은 낱말이 뽑아 주는 것이 있어야
// 한다. 다 맞은 문서는 AND 표와 낱말 표를 겹쳐 받아 언제나 위에 온다 (설계 6-4).
func wordRanks(terms []Term) []ranking {
	if len(terms) < 2 {
		return nil
	}
	out := []ranking{}
	for _, item := range terms {
		one := []Term{item}
		// 한 글자 한글은 접두로 물어서 엉뚱한 낱말을 잘 물어 온다. 표는 주되
		// 무게를 절반으로 깎는다 (실데이터 시험 #19).
		weight := weightWord
		if oneLetterOnly(item) {
			weight *= oneLetterFactor
		}
		out = appendRanking(out, ranking{name: "낱말:" + item.Text, table: "fts_ko", weight: weight,
			expr: joinTerms(one, false, plainOf, token.RunKO, token.RunKO1)})
		out = appendRanking(out, ranking{name: "낱말:" + item.Text, table: "fts_en", weight: weight,
			expr: joinTerms(one, false, plainOf, token.RunEN)})
	}
	return out
}

// spacingRanks 는 띄어쓰기가 달라서 안 맞는 자리를 메운다. AI 는 붙여 쓰고
// 사람은 띄어 쓴다. **5번 칸에서만 돈다** — 넓혀서 찾은 것이 정확히 맞은 것과
// 같은 대우를 받으면 안 된다 (설계 6-6 · 리뷰B #10).
func spacingRanks(terms []Term, helper *rungHelper) []ranking {
	out := []ranking{}
	out = appendRanking(out, ranking{name: "쪼갬", table: "fts_ko", weight: weightStem,
		expr: vocabSplitExpr(terms, helper)})
	out = appendRanking(out, ranking{name: "붙임", table: "fts_ko", weight: weightStem,
		expr: despacedExpr(terms)})
	return out
}

// gluedExpr 는 한글과 다른 글자종이 붙은 낱말을 쪼개지 않고 통째로 묻는 식이다.
// 색인은 `5,000건` 을 `5000` `건` 말고 합친 `5000건` 으로도 담는다 (설계 6-1 · 리뷰B #25).
func gluedExpr(terms []Term) string {
	pieces := []string{}
	for _, item := range terms {
		if len(item.Parts) < 2 || !token.HasHangul(item.Text) {
			continue
		}
		pieces = append(pieces, token.GluedFor(item.Text))
	}
	if len(pieces) == 0 {
		return ""
	}
	return strings.Join(pieces, " AND ")
}

// synonymRanks 는 R1″ 이다. 랭킹 하나로 늘 같이 던지되 가중이 절반이라 정확히
// 쓴 낱말이 언제나 위에 온다 (설계 6-4).
func synonymRanks(terms []Term, helper *rungHelper, anyWord bool) []ranking {
	swapped := swapWords(terms, helper)
	if len(swapped) == 0 {
		return nil
	}
	out := []ranking{}
	out = appendRanking(out, ranking{name: "R1s", table: "fts_ko", weight: helper.synWeight,
		expr: joinTerms(swapped, anyWord, plainOf, token.RunKO, token.RunKO1)})
	out = appendRanking(out, ranking{name: "R2s", table: "fts_en", weight: helper.synWeight,
		expr: joinTerms(swapped, anyWord, plainOf, token.RunEN)})
	return out
}

// swapWords 는 낱말마다 동의어 하나로 바꾼 목록이다. 낱말 수를 그대로 둬야
// AND 칸에서도 같은 모양으로 물을 수 있다 (설계 6-7).
func swapWords(terms []Term, helper *rungHelper) []Term {
	swapped := []Term{}
	for _, item := range terms {
		swapped = append(swapped, swapOne(item, helper))
	}
	if sameWords(swapped, terms) {
		return nil
	}
	return swapped
}

func swapOne(item Term, helper *rungHelper) Term {
	found := helper.synonym[item.Text]
	if len(found) > maxSynonyms {
		found = found[:maxSynonyms]
	}
	for _, other := range found {
		if other == "" || other == item.Text {
			continue
		}
		return Term{Text: other, Parts: token.Runs(other)}
	}
	return item
}

func sameWords(left, right []Term) bool {
	for at := range left {
		if left[at].Text != right[at].Text {
			return false
		}
	}
	return true
}

func andRankings(query *Query, helper *rungHelper) []ranking {
	live := query.Live()
	if len(live) == len(query.Terms) {
		return nil
	}
	return matchRankings(live, false, query, helper)
}

// dropRankings 는 낱말 하나를 빼고 다시 AND 한다 (설계 6-6 칸 2).
// **저장소에 아예 없는 낱말이 있으면 그것부터 뺀다.** 없는 말을 맞추라고
// 요구하면 이 칸도 통째로 죽는다 — `사운드 믹서` 는 `사운드` 를 빼고
// `믹서` 로 찾아 꼬리표를 달아 보여주는 것이 맞다 (리뷰B #5 되고침).
func dropRankings(query *Query, helper *rungHelper) []ranking {
	live := query.Live()
	if len(live) < 2 {
		return nil
	}
	if kept := withoutMissing(live, helper); kept != nil {
		if len(kept) < majorityOf(len(live)) {
			// 넉 낱말 중 하나만 남기고 다 뺀 답은 「정확히 찾았다」 가 아니다.
			// 2번 칸이 strict 로 올라왔으므로(결정 29) 여기도 과반 자를 쓴다 —
			// 안 그러면 `인앱 결제 영수증 검증` 이 `검증` 만으로 정답이 된다.
			// 답 자체는 3번 칸(OR)이 그대로 낸다. 꼬리표만 달라진다.
			return nil
		}
		return matchRankings(kept, false, query, helper)
	}
	if len(live) < 3 {
		// 낱말이 둘이면 하나를 빼는 순간 과반이 깨진다. 3번 칸에 넘긴다.
		return nil
	}
	worst, best := 0, -1
	for at, item := range live {
		count := helper.db.WordCount("fts_ko", joinTerms([]Term{item}, false, plainOf, token.RunKO, token.RunKO1))
		count += helper.db.WordCount("fts_en", joinTerms([]Term{item}, false, plainOf, token.RunEN))
		if count > best {
			worst, best = at, count
		}
	}
	kept := append(append([]Term{}, live[:worst]...), live[worst+1:]...)
	return matchRankings(kept, false, query, helper)
}

// withoutMissing 은 저장소에 없는 낱말을 뺀 목록이다. 뺄 것이 없거나 다
// 없으면 nil 이라 부르는 쪽이 옛 규칙(가장 흔한 낱말 빼기)으로 간다.
// 뺀 낱말은 도우미에 적어 두고 알림 줄이 그대로 읊는다.
func withoutMissing(live []Term, helper *rungHelper) []Term {
	missing := missingTerms(helper, live)
	if len(missing) == 0 || len(missing) == len(live) {
		return nil
	}
	kept := []Term{}
	for _, item := range live {
		if helper.inStore(item) {
			kept = append(kept, item)
		}
	}
	for _, item := range missing {
		helper.dropped = appendUnique(helper.dropped, item.Text)
	}
	return kept
}

// synonymRankings 는 동의어로만 넓힌 랭킹이다. 원래 낱말은 0·1·3 칸이 이미
// 다 물었으므로 여기서 다시 던지지 않는다 (설계 6-7 · 리뷰B #27).
func synonymRankings(query *Query, helper *rungHelper) []ranking {
	swapped := []Term{}
	for _, item := range query.Live() {
		found := helper.synonym[item.Text]
		if len(found) > maxSynonyms {
			found = found[:maxSynonyms]
		}
		for _, other := range found {
			if other == "" || other == item.Text {
				continue
			}
			swapped = append(swapped, Term{Text: other, Parts: token.Runs(other)})
		}
	}
	if len(swapped) == 0 {
		return nil
	}
	out := []ranking{}
	out = appendRanking(out, ranking{name: "R1\"", table: "fts_ko", weight: helper.synWeight,
		expr: joinTerms(swapped, true, plainOf, token.RunKO, token.RunKO1)})
	out = appendRanking(out, ranking{name: "R2\"", table: "fts_en", weight: helper.synWeight,
		expr: joinTerms(swapped, true, plainOf, token.RunEN)})
	return out
}

// splitRankings 는 접두·띄어쓰기 지운 꼴·어휘 기반 쪼개기다 (설계 6-1b·6-6 칸 5).
func splitRankings(query *Query, helper *rungHelper) []ranking {
	live := query.Live()
	out := spacingRanks(live, helper)
	out = appendRanking(out, ranking{name: "접두", table: "fts_ko", weight: weightKO,
		expr: joinTerms(live, true, prefixOf, token.RunKO, token.RunKO1)})
	out = appendRanking(out, ranking{name: "접두en", table: "fts_en", weight: weightEN,
		expr: joinTerms(live, true, prefixOf, token.RunEN)})
	return out
}

func likeRankings(query *Query) []ranking {
	out := []ranking{}
	for _, item := range query.Live() {
		out = appendRanking(out, ranking{name: "R4", table: "like", weight: weightLike, expr: item.Text})
	}
	return out
}

func appendRanking(list []ranking, item ranking) []ranking {
	if item.expr == "" {
		return list
	}
	return append(list, item)
}

// plainOf 와 prefixOf 는 조각 하나를 FTS 꼴로 바꾸는 두 가지 방법이다.
// plainOf 는 조각 그대로다. 한글 한 글자만 제목·요약 칸으로 가둔다 (리뷰B #3).
func plainOf(part token.Run) string  { return token.HeadMatchFor(part) }
func prefixOf(part token.Run) string { return token.PrefixFor(part.Text) }

// joinTerms 는 낱말들을 하나의 MATCH 식으로 묶는다. 한 낱말 안의 글자종 조각은
// 늘 AND 다 — `unity빌드` 는 둘 다 있어야 그 낱말이다.
func joinTerms(terms []Term, anyWord bool, shape func(token.Run) string, kinds ...string) string {
	joiner := " AND "
	if anyWord {
		joiner = " OR "
	}
	pieces := []string{}
	for _, item := range terms {
		inner := []string{}
		for _, part := range item.Parts {
			if !hasKind(kinds, part.Kind) {
				continue
			}
			inner = append(inner, orStem(shape(part), item, part, shape))
		}
		if len(inner) == 0 {
			continue
		}
		pieces = append(pieces, "("+strings.Join(inner, " AND ")+")")
	}
	if len(pieces) == 0 {
		return ""
	}
	return strings.Join(pieces, joiner)
}

// orStem 은 조사를 뗀 꼴을 원래 꼴과 나란히 둔다. 뗀 꼴만 던지면 `가로`·`정보`
// 처럼 조사가 아닌 것을 잘못 떼고, 원래 꼴만 던지면 AND 가 통째로 죽는다 (설계 6-1c).
func orStem(base string, item Term, part token.Run, shape func(token.Run) string) string {
	if item.Stem == "" || part.Kind == token.RunEN || part.Text != item.Text {
		return base
	}
	return "(" + base + " OR " + shape(token.Run{Text: item.Stem, Kind: token.RunKO}) + ")"
}

// stemExpr 는 조사를 뗀 꼴만 모은 식이다. 하나도 안 뗐으면 빈 식이다.
func stemExpr(terms []Term) string {
	pieces := []string{}
	for _, item := range terms {
		if item.Stem == "" {
			continue
		}
		pieces = append(pieces, "("+token.MatchFor(token.Run{Text: item.Stem, Kind: token.RunKO})+")")
	}
	if len(pieces) == 0 {
		return ""
	}
	return strings.Join(pieces, " AND ")
}

// despacedExpr 는 띄어 쓴 낱말들을 붙인 꼴이다. `기억 저장소` 가 `기억저장소` 로
// 적힌 기억을 찾는 자리다 (설계 6-1).
func despacedExpr(terms []Term) string {
	glued := ""
	for _, item := range terms {
		if !token.HasHangul(item.Text) {
			return ""
		}
		glued += item.Text
	}
	if len(terms) < 2 || len([]rune(glued)) < 3 {
		return ""
	}
	return token.MatchFor(token.Run{Text: glued, Kind: token.RunKO})
}

// vocabSplitExpr 는 붙여 쓴 낱말을 색인 어휘로 쪼갠 식이다 (설계 6-1b).
func vocabSplitExpr(terms []Term, helper *rungHelper) string {
	pieces := []string{}
	for _, item := range terms {
		for _, group := range helper.splitsOf(item.Text) {
			pieces = append(pieces, splitExprOf(group))
		}
		if head := helper.knownPrefixOf(item.Text); head != "" {
			pieces = append(pieces, token.MatchFor(token.Run{Text: head, Kind: token.RunKO}))
		}
	}
	if len(pieces) == 0 {
		return ""
	}
	return strings.Join(pieces, " OR ")
}

// splitExprOf 는 쪼갠 조각들을 다 있어야 한다는 식으로 묶는다.
func splitExprOf(group []string) string {
	inner := make([]string, 0, len(group))
	for _, part := range group {
		inner = append(inner, token.MatchFor(token.Run{Text: part, Kind: token.RunKO}))
	}
	return "(" + strings.Join(inner, " AND ") + ")"
}

// tagWords 는 태그·scope 정확 일치 랭킹에 넣을 낱말이다.
func tagWords(query *Query) []string {
	words := append([]string{}, query.Tags...)
	return append(words, query.Scopes...)
}

func hasKind(kinds []string, kind string) bool {
	for _, item := range kinds {
		if item == kind {
			return true
		}
	}
	return false
}

// runRung 은 한 칸의 랭킹을 전부 던져 RRF 로 합친다 (설계 6-4).
// 글자종이 갈린 낱말의 AND 는 0·1 칸의 자격 관문(bothScripts)이 잡는다 —
// 옛 must/keepAll 은 아무도 세우지 않던 죽은 코드라 지웠다 (리뷰B #11).
func runRung(helper *rungHelper, ranks []ranking, softener float64) (*rungResult, error) {
	out := rungResult{scores: map[int64]float64{}, from: map[int64][]string{}, ranks: ranks}
	for _, item := range ranks {
		found, err := helper.rankOf(item)
		if err != nil {
			return nil, err
		}
		for place, hit := range found {
			out.scores[hit.Docid] += item.weight / (softener + float64(place+1))
			out.from[hit.Docid] = append(out.from[hit.Docid], item.name+"("+placeText(place+1)+")")
		}
	}
	return &out, nil
}

// rankOf 는 랭킹 하나의 답이다. 같은 표·같은 식은 한 질의에서 한 번만 묻는다 —
// 사다리 일곱 칸이 같은 식을 되풀이 던지는 것이 20k 느림의 주범이었다 (리뷰B #3).
func (h *rungHelper) rankOf(item ranking) ([]index.Ranked, error) {
	if h.ranks == nil {
		h.ranks = map[string][]index.Ranked{}
	}
	key := item.table + "\x00" + item.expr
	if found, asked := h.ranks[key]; asked {
		return found, nil
	}
	found, err := runRanking(h.db, item)
	if errors.Is(err, index.ErrBadExpr) {
		h.bad, found, err = true, nil, nil
	}
	if err != nil {
		return nil, err
	}
	h.ranks[key] = found
	return found, nil
}

func runRanking(database *index.DB, item ranking) ([]index.Ranked, error) {
	switch item.table {
	case "fts_ko":
		return database.MatchKO(item.expr, rankDepth)
	case "fts_norm":
		return database.MatchNorm(item.expr, rankDepth)
	case "fts_en":
		return database.MatchEN(item.expr, rankDepth)
	case "tag":
		return database.TagScopeRank(strings.Split(item.expr, "\n"), rankDepth)
	}
	return database.LikeHeadRank(item.expr, rankDepth)
}

// docidsOf 는 점수 높은 것부터 담은 docid 목록이다.
func (r *rungResult) docidsOf() []int64 {
	out := make([]int64, 0, len(r.scores))
	for docid := range r.scores {
		out = append(out, docid)
	}
	sort.Slice(out, func(a, b int) bool {
		if r.scores[out[a]] != r.scores[out[b]] {
			return r.scores[out[a]] > r.scores[out[b]]
		}
		return out[a] < out[b]
	})
	return out
}

func placeText(place int) string {
	digits := []byte{}
	for place > 0 {
		digits = append([]byte{byte('0' + place%10)}, digits...)
		place /= 10
	}
	if len(digits) == 0 {
		return "0"
	}
	return string(digits)
}
