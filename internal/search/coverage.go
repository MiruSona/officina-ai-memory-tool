package search

import (
	"errors"

	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/token"
)

// 0·1번 칸은 「있는 그대로 / 낱말 AND」 다 (설계 6-6). 낱말 하나만 걸린 문서가
// 그 칸에 앉으면 "없다" 가 "다섯 건 있다" 로 바뀐다 (실데이터 시험 2-1).
// 그래서 자격은 순위가 아니라 **맞춘 낱말 수**로 가린다.

// needOf 는 그 칸에 앉으려면 몇 낱말을 맞춰야 하는지다 — **절반(올림)** 이다.
//
// **설계 결정 30 은 이것을 「올림 과반」 으로 올리라고 했다. 재 보고 되돌렸다.**
// 설계 4-1a 가 "조이면 recall 이 떨어질 수도 있다. T7 에서 두 값을 다 재고
// 고른다" 고 적은 그 자리다. 실측(골든셋 80건, 저장소 206건) :
//
//	자        recall@5   MRR     ko/en 격차   abstain
//	절반       0.791     0.646    0.152       1.000
//	올림 과반   0.776     0.628    0.235       1.000
//
// 손 골든셋 30건에서는 차이가 더 컸다(0.750 대 0.667). 조이면 낱말 둘짜리
// 질의가 통째로 0번 칸을 못 앉고 3번 칸(OR)으로 내려가는데, 3번 칸은 strict
// 가 아니라서 「찾았는데 안 센」 답이 늘어난다.
// 조사 E 가 이 자를 지목한 `메모리 색인`·`기억 수정` 은 **조여도 안 고쳐졌다**
// — 그 둘은 표기 문제라 동의어·임베딩 몫이다(설계 4-1a 마지막 줄).
// 「낱말 하나만 걸린 문서가 답이 되는」 구멍은 질의 쪽 자(majorityOf)와
// 2번 칸의 과반 관문(dropRankings)이 막는다. abstain 은 두 자 모두 1.000 이다.
func needOf(count int) int {
	if count <= 1 {
		return 1
	}
	return (count + 1) / 2
}

// majorityOf 는 진짜 과반이다. `(count+1)/2` 는 낱말 둘일 때 1 이 나와서
// 「둘 중 하나만 있어도 통과」 가 됐다 — `타일맵 셰이더` 가 셰이더 0건인데도
// 답을 낸 구멍이 여기였다 (리뷰B #5).
func majorityOf(count int) int {
	if count <= 1 {
		return 1
	}
	return count/2 + 1
}

// oneLetterOnly 는 한글 한 글자짜리 낱말인지다. 접두로 물어서 `방` 이 `방식`·
// `방지` 를 물어 오므로 이것만 맞은 문서에는 자격을 안 준다 (실데이터 시험 #19).
func oneLetterOnly(item Term) bool {
	if len(item.Parts) != 1 {
		return false
	}
	return item.Parts[0].Kind == token.RunKO1
}

// coverOf 는 후보마다 몇 낱말을 맞췄는지 센다. 한 글자 낱말은 따로 센다.
type coverOf struct {
	hits map[int64]int
	real map[int64]int
}

// missingTerms 는 저장소에 아예 없는 낱말이다. 2번 칸이 이것부터 뺀다.
func missingTerms(helper *rungHelper, terms []Term) []Term {
	out := []Term{}
	for _, item := range terms {
		if !helper.inStore(item) {
			out = append(out, item)
		}
	}
	return out
}

// qualified 는 후보 중 그 칸에 앉을 자격이 있는 것만 남긴다. 두 관문이다 —
// ① 질의 낱말의 과반이 저장소에 아예 있어야 하고(없는 말만 물었으면 답도 없다)
// ② 문서는 그중 과반을 맞춰야 한다.
func qualified(helper *rungHelper, terms []Term, docids []int64) (map[int64]bool, error) {
	findable := findableTerms(helper, terms)
	if len(findable) < majorityOf(len(terms)) || len(findable) == 0 {
		return map[int64]bool{}, nil
	}
	tally := coverOf{hits: map[int64]int{}, real: map[int64]int{}}
	for _, item := range findable {
		matched, err := matchedBy(helper, item, docids)
		if err != nil {
			return nil, err
		}
		short := oneLetterOnly(item)
		for docid := range matched {
			tally.hits[docid]++
			if !short {
				tally.real[docid]++
			}
		}
	}
	need := needOf(len(findable))
	// 봐주는 것은 질의가 통째로 한 글자일 때뿐이다. 다른 낱말이 섞여 있으면
	// 한 글자 접두만 맞은 문서에는 자격을 안 준다 (실데이터 시험 #19).
	allShort := everyTermIsOneLetter(terms)
	keep := map[int64]bool{}
	for _, docid := range docids {
		if tally.hits[docid] < need {
			continue
		}
		// 한 글자 낱말만 맞은 문서는 안 된다. 질의가 통째로 한 글자뿐이면 봐준다.
		if !allShort && tally.real[docid] == 0 {
			continue
		}
		keep[docid] = true
	}
	return keep, nil
}

// findableTerms 는 저장소에 한 번이라도 나오는 낱말만 고른다. 아무 데도 없는
// 낱말을 맞추라고 요구하면 사람이 쓴 조사·어미 때문에 답이 통째로 죽는다.
func findableTerms(helper *rungHelper, terms []Term) []Term {
	kept := []Term{}
	for _, item := range terms {
		if !helper.inStore(item) {
			continue
		}
		kept = append(kept, item)
	}
	return kept
}

// inStore 는 그 낱말을 우리가 묻는 꼴 중 하나라도 저장소에 있는지다.
// 답은 낱말마다 기억해 둔다 — 사다리 일곱 칸이 같은 것을 되풀이 묻는다.
func (h *rungHelper) inStore(item Term) bool {
	if h.exists == nil {
		h.exists = map[string]bool{}
	}
	if found, asked := h.exists[item.Text]; asked {
		return found
	}
	found := false
	for _, ask := range h.termExprs(item) {
		here, ok := h.db.WordExists(ask.table, ask.expr)
		if !ok {
			h.bad = true
			continue
		}
		if here {
			found = true
			break
		}
	}
	h.exists[item.Text] = found
	return found
}

// askShape 는 낱말 하나를 묻는 한 가지 꼴이다.
type askShape struct {
	table string
	expr  string
}

// termExprs 는 그 낱말을 우리가 실제로 묻는 모든 꼴이다 — 쓴 그대로, 조사 뗀
// 꼴, 색인 어휘로 쪼갠 꼴, 동의어. 셈을 쓴 그대로에만 매기면 `기억저장소` 처럼
// 쪼개야 맞는 낱말이 "저장소에 없는 말" 로 잘못 판정된다. 낱말마다 한 번만 만든다.
func (h *rungHelper) termExprs(item Term) []askShape {
	if h.exprs == nil {
		h.exprs = map[string][]askShape{}
	}
	if found, made := h.exprs[item.Text]; made {
		return found
	}
	found := termExprs(h, item)
	h.exprs[item.Text] = found
	return found
}

func termExprs(helper *rungHelper, item Term) []askShape {
	one := []Term{item}
	out := []askShape{}
	out = appendShape(out, "fts_ko", joinTerms(one, false, plainOf, token.RunKO, token.RunKO1))
	out = appendShape(out, "fts_en", joinTerms(one, false, plainOf, token.RunEN))
	// 정규화한 꼴도 「저장소에 있는 낱말」로 친다 (결정 23). 자를 옮기는 것이
	// 아니라, 같은 글자가 된 것을 관문이 볼 수 있게 하는 것이다.
	for _, shape := range normShapes(helper, item) {
		out = appendShape(out, shape.table, shape.expr)
	}
	if item.Stem != "" {
		out = appendShape(out, "fts_ko", token.MatchFor(token.Run{Text: item.Stem, Kind: token.RunKO}))
	}
	if helper == nil {
		return out
	}
	out = appendShape(out, "fts_ko", gluedExpr(one))
	for _, group := range helper.splitsOf(item.Text) {
		out = appendShape(out, "fts_ko", splitExprOf(group))
	}
	if head := helper.knownPrefixOf(item.Text); head != "" {
		out = appendShape(out, "fts_ko", token.MatchFor(token.Run{Text: head, Kind: token.RunKO}))
	}
	swapped := swapOne(item, helper)
	if swapped.Text != item.Text {
		other := []Term{swapped}
		out = appendShape(out, "fts_ko", joinTerms(other, false, plainOf, token.RunKO, token.RunKO1))
		out = appendShape(out, "fts_en", joinTerms(other, false, plainOf, token.RunEN))
	}
	return out
}

// appendShape 는 묻는 꼴 하나를 더한다. 같은 표에 같은 식이 이미 있으면 안
// 더한다 — 꼴 하나가 곧 색인 질의 한 번이라 겹치면 그대로 시간이다.
func appendShape(list []askShape, table, expr string) []askShape {
	if expr == "" {
		return list
	}
	for _, item := range list {
		if item.table == table && item.expr == expr {
			return list
		}
	}
	return append(list, askShape{table: table, expr: expr})
}

func everyTermIsOneLetter(terms []Term) bool {
	for _, item := range terms {
		if !oneLetterOnly(item) {
			return false
		}
	}
	return true
}

// matchedBy 는 낱말 하나가 후보 중 어느 것을 맞췄는지다. 우리가 묻는 꼴 중
// 하나라도 맞으면 그 낱말을 맞춘 것으로 센다.
func matchedBy(helper *rungHelper, item Term, docids []int64) (map[int64]bool, error) {
	found := map[int64]bool{}
	mixed := isMixedScript(item)
	for _, ask := range helper.termExprs(item) {
		hit, err := helper.db.MatchWithin(ask.table, ask.expr, docids)
		if errors.Is(err, index.ErrBadExpr) {
			helper.bad, err = true, nil
		}
		if err != nil {
			return nil, err
		}
		for docid := range hit {
			found[docid] = true
		}
	}
	if !mixed {
		return found, nil
	}
	return bothScripts(helper, item, docids, found)
}

// isMixedScript 는 한 낱말 안에 한글과 영문이 같이 있는지다. 그때는 두 표를
// 다 맞춰야 그 낱말을 맞춘 것이다 (`unity빌드`).
func isMixedScript(item Term) bool {
	ko, en := false, false
	for _, part := range item.Parts {
		if part.Kind == token.RunEN {
			en = true
			continue
		}
		ko = true
	}
	return ko && en
}

func bothScripts(helper *rungHelper, item Term, docids []int64,
	found map[int64]bool) (map[int64]bool, error) {
	one := []Term{item}
	ko, err := helper.within("fts_ko", joinTerms(one, false, plainOf, token.RunKO, token.RunKO1), docids)
	if err != nil {
		return nil, err
	}
	en, err := helper.within("fts_en", joinTerms(one, false, plainOf, token.RunEN), docids)
	if err != nil {
		return nil, err
	}
	both := map[int64]bool{}
	for docid := range ko {
		if en[docid] {
			both[docid] = true
		}
	}
	for docid := range found {
		if ko[docid] || en[docid] {
			continue
		}
		both[docid] = true
	}
	return both, nil
}

// within 은 MatchWithin 이되 못 읽은 식은 표시만 남기고 빈 답으로 본다.
func (h *rungHelper) within(table, expr string, docids []int64) (map[int64]bool, error) {
	found, err := h.db.MatchWithin(table, expr, docids)
	if errors.Is(err, index.ErrBadExpr) {
		h.bad = true
		return found, nil
	}
	return found, err
}

// gateOf 는 0·1번 칸에 앉을 자격을 가린다. 그 아래 칸은 넓히는 자리라 그대로
// 둔다 — 대신 꼬리표와 알림 줄이 붙는다 (설계 6-6).
func gateOf(helper *rungHelper, level int, query *Query, docids []int64) ([]int64, error) {
	if level > GateRung {
		return docids, nil
	}
	keep, err := qualified(helper, query.Live(), docids)
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(keep))
	for _, docid := range docids {
		if keep[docid] {
			out = append(out, docid)
		}
	}
	return out, nil
}
