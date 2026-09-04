package search

import (
	"strconv"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/budget"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/safe"
)

// summaryCut 은 표 한 칸에 보일 요약 글자 수다.
const summaryCut = 60

// LabelOf 는 결과 한 줄에 붙는 꼬리표다. 0·1 칸은 아무것도 안 붙는다 (설계 6-6).
func LabelOf(rung int) string {
	switch rung {
	case RungDropWord:
		return i18n.T(i18n.RungLabel2)
	case RungOr:
		return i18n.T(i18n.RungLabel3)
	case RungSynonym:
		return i18n.T(i18n.RungLabel4)
	case RungSplit:
		return i18n.T(i18n.RungLabel5)
	case RungLike:
		return i18n.T(i18n.RungLabel6)
	}
	return ""
}

// Markdown 은 사람과 AI 가 같이 읽는 표다. 예산이 있으면 줄을 아래에서부터
// 뺀다 — 예산은 하드 상한이라 넘느니 줄을 잃는 편이 낫다.
func Markdown(result *Result, limit int) string {
	rows := tableRows(result.Hits)
	dropped := 0
	for {
		text := assemble(result, rows, dropped, limit)
		if limit <= 0 || budget.Estimate(text) <= limit {
			return text
		}
		if len(rows) == 0 {
			// 줄을 다 빼도 예산을 넘으면 글자로 자른다. 잘랐다는 말이 먼저
			// 나와야 사람이 "이게 전부인가" 하고 착각하지 않는다 (리뷰B #29).
			clipped, _ := budget.Clip(i18n.T(i18n.SearchBudgetTiny, limit)+"\n"+text, limit)
			return clipped
		}
		rows = rows[:len(rows)-1]
		dropped++
	}
}

func assemble(result *Result, rows []string, dropped, limit int) string {
	first := build(result, rows, dropped, limit, 0)
	return build(result, rows, dropped, limit, budget.Estimate(first))
}

func build(result *Result, rows []string, dropped, limit, tokens int) string {
	out := strings.Builder{}
	out.WriteString(headerOf(result, len(rows), tokens) + "\n\n")
	for _, note := range noteLines(result) {
		out.WriteString(note + "\n")
	}
	if len(noteLines(result)) > 0 {
		out.WriteString("\n")
	}
	if dropped > 0 {
		out.WriteString(i18n.T(i18n.SearchTrimmed, limit, dropped) + "\n\n")
	}
	if len(rows) > 0 {
		out.WriteString(i18n.T(i18n.SearchTableHead) + "\n")
		out.WriteString(strings.Join(rows, "\n") + "\n\n")
	}
	if result.Why != nil {
		out.WriteString(whyLines(result) + "\n")
	}
	out.WriteString(i18n.T(i18n.SearchFooter) + "\n")
	return out.String()
}

func headerOf(result *Result, shown, tokens int) string {
	// 공백만 친 질의는 목록이다. `검색 : "   "` 라고 찍으면 저장소 전체 목록이
	// 검색 결과처럼 보인다 (리뷰B #2 · 이상한 입력 #2).
	if strings.TrimSpace(result.Query) == "" {
		return modeMark(result) + i18n.T(i18n.SearchListHeader, result.Total, shown, result.Ms, tokens)
	}
	return modeMark(result) + i18n.T(i18n.SearchHeader, strings.ReplaceAll(result.Query, `"`, ""),
		result.Total, shown, result.Ms, tokens)
}

// modeMark 는 머리에 붙는 모드 표시다 (결정 16). 낱말만 쓴 답과 뜻까지 본 답이
// 섞이면 「왜 내 결과가 다르지」가 된다.
func modeMark(result *Result) string {
	if result.Mode == ModeMeaning {
		return i18n.T(i18n.SearchModeMeaning) + " "
	}
	return i18n.T(i18n.SearchModeWord) + " "
}

// noteLines 는 무엇을 넓히거나 좁혔는지 알리는 줄이다. 한 칸이라도 내려갔으면
// 반드시 한 줄이 붙는다 (설계 6-6).
func noteLines(result *Result) []string {
	lines := []string{}
	if len(result.Stopped) > 0 {
		lines = append(lines, i18n.T(i18n.SearchStoppedLine, strings.Join(result.Stopped, " · ")))
	}
	if result.TimeWord != "" && !result.TimeRelaxed {
		lines = append(lines, i18n.T(i18n.SearchTimeNarrow, result.TimeWord))
	}
	if result.TimeRelaxed {
		lines = append(lines, i18n.T(i18n.SearchTimeRelaxed))
	}
	if result.TimeIgnored != "" {
		lines = append(lines, i18n.T(i18n.SearchTimeIgnored, result.TimeIgnored))
	}
	if result.FilterOnly {
		lines = append(lines, i18n.T(i18n.SearchFilterOnly))
	}
	if len(result.MissingWords) == 1 {
		lines = append(lines, i18n.T(i18n.SearchWordDropped, result.MissingWords[0]))
	}
	if len(result.MissingWords) > 1 {
		lines = append(lines, i18n.T(i18n.SearchWordsDropped, len(result.MissingWords),
			strings.Join(result.MissingWords, "\" · \"")))
	}
	if len(result.Dropped) > 0 {
		lines = append(lines, i18n.T(i18n.SearchTooManyWords, MaxTerms, strings.Join(result.Dropped, " · ")))
	}
	if result.LimitCapped {
		lines = append(lines, i18n.T(i18n.SearchLimitCapped, MaxLimit))
	}
	if result.BadQuery {
		lines = append(lines, i18n.T(i18n.SearchBadQuery))
	}
	if result.Relaxed {
		lines = append(lines, relaxedLine(result))
	}
	if result.NoIndex {
		lines = append(lines, i18n.T(i18n.SearchNoIndex))
	}
	return lines
}

func relaxedLine(result *Result) string {
	exact, wide, parts := 0, 0, []string{}
	for rung := 0; rung < rungCount; rung++ {
		count := result.Rungs[rung]
		if count == 0 {
			continue
		}
		if Strict(rung) {
			exact += count
			continue
		}
		wide += count
		parts = append(parts, strings.Trim(LabelOf(rung), "[]")+" "+strconv.Itoa(count))
	}
	return i18n.T(i18n.SearchRelaxedLine, exact+wide, exact, wide, strings.Join(parts, " · "))
}

// whyLines 는 0건일 때의 네 가지다 (설계 6-9).
func whyLines(result *Result) string {
	why := result.Why
	lines := []string{}
	if why.Reason == ReasonEmptyStore {
		return i18n.T(i18n.EmptyStore) + "\n" + i18n.T(i18n.EmptyScale, why.Total, orDash(why.LastIndex))
	}
	if len(why.Filters) > 0 {
		lines = append(lines, i18n.T(i18n.EmptyFilters, strings.Join(why.Filters, " ")))
		lines = append(lines, i18n.T(i18n.EmptyFilterHint))
		if why.Held > 0 {
			lines = append(lines, i18n.T(i18n.EmptyHeld, why.Held))
		}
		lines = append(lines, i18n.T(i18n.EmptyScale, why.Total, orDash(why.LastIndex)))
		return strings.Join(lines, "\n")
	}
	if len(why.Missing) == 0 && len(why.Found) == 0 {
		lines = append(lines, i18n.T(i18n.EmptyNoWord))
	}
	for _, word := range why.Missing {
		lines = append(lines, i18n.T(missingKey(word), word))
	}
	for _, word := range sortedWords(why.Found) {
		lines = append(lines, i18n.T(foundKey(word), word, why.Found[word]))
	}
	if len(why.Near) > 0 {
		lines = append(lines, i18n.T(i18n.EmptyNear, nearText(why.Near)))
	}
	// ③ 과 ④ 는 늘 나온다. 없으면 "없다" 고 적는 것이 조용한 빈칸보다 낫다
	// (설계 6-9 · 실데이터 시험 3-2).
	if len(why.Near) == 0 {
		lines = append(lines, i18n.T(i18n.EmptyNoNear))
	}
	// ④ 는 언제나 나온다 (불변조건 9 · 설계 6-9). 저장소에 있는 낱말이 하나도
	// 없으면 "낱말을 줄여 보라" 는 말이 헛돌므로, 가장 짧은 낱말로 다시 치는
	// 실제 명령과 종류로 훑는 길을 같이 준다.
	if why.Next != "" {
		lines = append(lines, i18n.T(i18n.EmptyNext, why.Next))
	}
	if why.Next == "" && len(why.Missing) > 0 {
		lines = append(lines, i18n.T(i18n.EmptyTryOther, shortestWord(why.Missing)))
	}
	if why.Next == "" && len(why.Missing) == 0 {
		lines = append(lines, i18n.T(i18n.EmptyFewerWords))
	}
	if why.Held > 0 {
		lines = append(lines, i18n.T(i18n.EmptyHeld, why.Held))
	}
	lines = append(lines, i18n.T(i18n.EmptyScale, why.Total, orDash(why.LastIndex)))
	return strings.Join(lines, "\n")
}

// missingKey·foundKey 는 조사를 낱말에 맞춘다. 받침이 있으면 「이 · 은」,
// 없으면 「가 · 는」 이다.
func missingKey(word string) i18n.Key {
	if hasJongseong(word) {
		return i18n.EmptyMissingJong
	}
	return i18n.EmptyMissing
}

func foundKey(word string) i18n.Key {
	if hasJongseong(word) {
		return i18n.EmptyFoundJong
	}
	return i18n.EmptyFound
}

// hasJongseong 은 마지막 글자에 받침이 있는지다. 한글이 아니면 없다고 본다.
func hasJongseong(word string) bool {
	letters := []rune(word)
	if len(letters) == 0 {
		return false
	}
	last := letters[len(letters)-1]
	if last < 0xAC00 || last > 0xD7A3 {
		return false
	}
	return (last-0xAC00)%28 != 0
}

// shortestWord 는 다시 쳐 볼 낱말이다. 짧은 것이 걸릴 확률이 높다.
func shortestWord(words []string) string {
	best := words[0]
	for _, word := range words {
		if len([]rune(word)) < len([]rune(best)) {
			best = word
		}
	}
	return best
}

func sortedWords(found map[string]int) []string {
	out := make([]string, 0, len(found))
	for word := range found {
		out = append(out, word)
	}
	for a := 1; a < len(out); a++ {
		for b := a; b > 0 && found[out[b]] > found[out[b-1]]; b-- {
			out[b], out[b-1] = out[b-1], out[b]
		}
	}
	return out
}

func nearText(near []index.VocabCount) string {
	parts := make([]string, 0, len(near))
	for _, item := range near {
		parts = append(parts, item.Name+"("+strconv.Itoa(item.Count)+")")
	}
	return strings.Join(parts, " · ")
}

func orDash(text string) string {
	if text == "" {
		return "—"
	}
	return text
}

func tableRows(hits []Hit) []string {
	rows := make([]string, 0, len(hits))
	for place, hit := range hits {
		mark := ""
		tail := LabelOf(hit.Rung)
		if tail != "" {
			tail = " " + tail
		}
		if hit.Invalid {
			tail = " " + i18n.T(i18n.InvalidMark) + tail
		}
		rows = append(rows, "| "+strconv.Itoa(place+1)+" | "+hit.ID+" | "+kindOf(hit)+" | "+
			shortDate(hit.Date)+" | "+mark+shorten(hit.Summary)+groupMark(hit)+tail+" |")
	}
	return rows
}

// FacetMarkdown 은 --facet 의 표다.
func FacetMarkdown(counts []index.VocabCount) string {
	out := strings.Builder{}
	out.WriteString(i18n.T(i18n.SearchFacetHead) + "\n")
	for _, item := range counts {
		out.WriteString("| " + item.Name + " | " + strconv.Itoa(item.Count) + " |\n")
	}
	return out.String()
}

// Explain 은 항목마다 점수를 쪼개 보여준다. stdout 의 답을 더럽히지 않게
// 부르는 쪽이 stderr 로 보낸다 (설계 6-10).
func Explain(result *Result) string {
	lines := []string{}
	for place, hit := range result.Hits {
		lines = append(lines, i18n.T(i18n.ExplainTitle, place+1, hit.ID,
			safe.Summary(hit.Title, summaryCut), hit.Score))
		lines = append(lines, i18n.T(i18n.ExplainRRF, hit.Parts.RRF, strings.Join(hit.Parts.From, " / ")))
		lines = append(lines, i18n.T(i18n.ExplainBonus, hit.Parts.Bonus, strings.Join(hit.Parts.Why, " · ")))
		lines = append(lines, i18n.T(i18n.ExplainDecay, hit.Parts.Decay, hit.Parts.Trust))
		lines = append(lines, i18n.T(i18n.ExplainRung, hit.Rung, LabelOf(hit.Rung)))
	}
	return strings.Join(lines, "\n")
}

func kindOf(hit Hit) string {
	if hit.Severity == "" {
		return hit.Type
	}
	return hit.Type + " " + hit.Severity
}

func shortDate(date string) string {
	if len(date) == 10 {
		return date[5:]
	}
	return date
}

// shorten 은 표 한 칸에 넣을 요약이다. 기억 요약은 남이 쓴 글이고 search
// 결과는 그대로 AI 컨텍스트로 들어가므로 훅과 같은 중화를 지난다 (M-1).
// `|` 는 표를 깨뜨려서 중화 전에 먼저 바꾼다.
func shorten(text string) string {
	return safe.Summary(strings.ReplaceAll(text, "|", "/"), summaryCut)
}

// Neutralize 는 JSON 으로 내보낼 결과의 남의 글을 중화한다. `--json` 값도
// 그대로 AI 컨텍스트로 들어가므로 표와 같은 자를 쓴다. 색인·평가가 쓰는
// 원본은 안 건드리고 복사본을 준다 (M-1).
func Neutralize(result *Result) *Result {
	if result == nil {
		return nil
	}
	copied := *result
	copied.Hits = make([]Hit, len(result.Hits))
	for at, hit := range result.Hits {
		hit.Title = safe.Neutralize(safe.OneLine(hit.Title))
		hit.Summary = safe.Neutralize(safe.OneLine(hit.Summary))
		copied.Hits[at] = hit
	}
	return &copied
}
