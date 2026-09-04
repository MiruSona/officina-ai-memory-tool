package search

import (
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/token"
)

// norm 신호 (설계 결정 21·23·46 · v0.3 물결 2A).
//
// 색인 쪽은 `fts_norm` 에 **정규화가 글자를 바꾼 열만** 담는다. 안 바뀐 열은
// `fts_ko` 가 이미 같은 글자로 담고 있어서다. 그래서 질의 쪽은 정규화한 식을
// **두 표에 다 던지고 RRF 로 합친다** — `fts_norm` 만 물으면 「문서가 대표말로
// 적혀 있고 질의가 다른 말인 경우」(`인덱싱` 으로 `색인` 찾기)가 통째로 죽는다.
//
// 정규화기는 색인이 쓴 것 하나(`index.Normalize`)를 그대로 쓴다. 질의 쪽이
// 표를 따로 들고 오면 언젠가 둘이 어긋난다 — `token.NormQueryExpr(q, canon)` 과
// 같은 식이 나오되, **같은 함수를 탄다는 것이 코드로 보장된다**.

// weightNorm 은 norm 랭킹 두 장의 RRF 몫이다. 골든셋 세 벌 스윕으로 골랐다
// (v2 80건 · 손 30건 · 42건 · 물결 2A 측정표). v2 80건 r@5 :
// 0.5 → 0.701 · 1.0·1.2 → 0.701 · **1.5 → 0.716** · 2.0·3.0 → 0.716(더 안 는다).
// 1.5 아래에서는 대표말로 찾은 답이 원래 낱말로 찾은 답을 못 밀어내 5줄 안에
// 못 든다. 세 벌 다 같은 방향이라 한 벌 과적합이 아니다.
const weightNorm = 1.5

// normText 는 색인이 쓴 정규화기를 그대로 태운 글이다.
func normText(text string) string { return index.Normalize(text) }

// normMapOf 는 낱말마다 「정규화하면 무엇이 되는가」 다. **질의 전체를 한 번에**
// 정규화해서 어절 수가 그대로면 자리끼리 짝지어 준다 — `[canon]` 은 `기억 수정`
// 처럼 여러 낱말 구를 담을 수 있어서(결정 25), 낱말 하나씩 정규화하면 그 줄이
// 통째로 안 걸린다. 어절 수가 달라지면 낱말마다 따로 정규화한다.
func normMapOf(query *Query) map[string]string {
	out := map[string]string{}
	texts := make([]string, 0, len(query.Terms))
	for _, item := range query.Terms {
		texts = append(texts, item.Text)
	}
	if len(texts) == 0 {
		return out
	}
	whole := strings.Fields(normText(strings.Join(texts, " ")))
	if len(whole) == len(texts) {
		for at, text := range texts {
			out[text] = whole[at]
		}
		return out
	}
	for _, text := range texts {
		out[text] = normText(text)
	}
	return out
}

// normFor 는 그 낱말의 정규화된 꼴이다. 표가 없으면 그 자리에서 센다.
func (h *rungHelper) normFor(text string) string {
	if found, made := h.norms[text]; made {
		return found
	}
	return normText(text)
}

// normExprOf 는 낱말들을 정규화해 만든 MATCH 식이다. 정규화가 아무것도 안
// 바꿨으면 빈 식이라 랭킹이 안 붙는다 — 같은 식을 두 번 던지면 RRF 가 그
// 낱말을 두 번 센다.
func normExprOf(terms []Term, anyWord bool) string {
	texts := make([]string, 0, len(terms))
	for _, item := range terms {
		texts = append(texts, item.Text)
	}
	if len(texts) == 0 {
		return ""
	}
	joined := strings.Join(texts, " ")
	normalized := normText(joined)
	if normalized == "" || normalized == joined {
		return ""
	}
	joiner := " AND "
	if anyWord {
		joiner = " OR "
	}
	pieces := []string{}
	for _, word := range strings.Fields(normalized) {
		pieces = append(pieces, "("+token.ForQuery(word)+")")
	}
	if len(pieces) == 0 {
		return ""
	}
	return strings.Join(pieces, joiner)
}

// appendNormRankings 는 norm 랭킹 두 장을 얹는다 (`fts_norm` 과 `fts_ko`).
// 이미 같은 표에 같은 식이 있으면 안 얹는다 — 같은 목록을 두 번 세면 그 낱말의
// 몫이 조용히 두 배가 된다.
func appendNormRankings(list []ranking, terms []Term, anyWord bool) []ranking {
	expr := normExprOf(terms, anyWord)
	if expr == "" {
		return list
	}
	for _, table := range []string{"fts_norm", "fts_ko"} {
		if hasRanking(list, table, expr) {
			continue
		}
		list = append(list, ranking{name: "R1n", table: table, weight: weightNorm, expr: expr})
	}
	return list
}

func hasRanking(list []ranking, table, expr string) bool {
	for _, item := range list {
		if item.table == table && item.expr == expr {
			return true
		}
	}
	return false
}

// normShapes 는 자격 관문이 낱말 하나를 묻는 norm 꼴이다. 관문이 `fts_ko`·
// `fts_en` 만 보면 `인덱싱` 이 「저장소에 없는 말」로 판정돼 0·1번 칸이 통째로
// 비고, 그러면 RRF 에 얹은 신호가 strict 자에는 한 건도 안 잡힌다 (결정 23).
func normShapes(helper *rungHelper, item Term) []askShape {
	if helper == nil {
		return nil
	}
	normalized := helper.normFor(item.Text)
	if normalized == "" || normalized == item.Text {
		return nil
	}
	expr := token.QueryExpr(normalized)
	return []askShape{{table: "fts_norm", expr: expr}, {table: "fts_ko", expr: expr}}
}
