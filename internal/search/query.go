// Package search 는 질의를 갈라 랭킹으로 보내고, RRF 로 합치고, 완화 사다리를
// 밟고, 0건이면 왜 0건인지 말한다 (설계 6).
package search

import (
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/token"
)

// Term 은 질의의 낱말 하나다. Parts 는 글자종으로 갈라 실제로 던질 조각이고
// Stem 은 조사를 뗀 꼴이다 (설계 6-1·6-1c).
type Term struct {
	// Text 는 조각을 이어 붙인 꼴이다. 사람이 친 원문이 아니다 — 원문을 쓰면
	// 붙은 기호 하나가 동의어·조사떼기·어휘쪼개기를 다 빗나가게 한다 (리뷰B #9).
	Text  string
	Raw   string
	Parts []token.Run
	Stem  string
	Stop  bool
}

// Query 는 파서가 낸 결과 전부다. CLI·eval·훅이 이 한 구조체만 본다 (설계 6-11).
type Query struct {
	Raw      string
	Terms    []Term
	Phrases  []string
	Stopped  []string
	Tags     []string
	Scopes   []string
	Since    time.Time
	Until    time.Time
	TimeWord string
	// Dropped 는 낱말 상한을 넘어 버린 낱말이다. 알림 줄이 쓴다 (리뷰B #3).
	Dropped []string
}

// MaxTerms 는 한 질의가 쓰는 낱말 수 상한이다. 낱말 하나가 색인 질의 여러 벌을
// 던지므로 낱말 수가 그대로 시간이 된다 (리뷰B #3 · 20k 8낱말 1.9초).
const MaxTerms = 8

// FilterOnly 는 낱말은 없고 태그·범위·시간만 준 질의인지다. 이때는 목록을
// 내되 좁힌 조건을 그대로 쓴다 (리뷰B #1).
func (q *Query) FilterOnly() bool {
	return len(q.Terms) == 0 && (len(q.Tags) > 0 || len(q.Scopes) > 0 || q.TimeWord != "")
}

// Empty 는 랭킹에 던질 낱말이 하나도 없는 질의인지다. `#태그` 나 `scope:이름`
// 만 친 질의도 여기 든다 — 그때는 낱말 검색이 아니라 조건 목록이라서
// `--tag` 를 준 것과 같은 답이 나와야 한다.
func (q *Query) Empty() bool {
	return len(q.Terms) == 0
}

// Words 는 사람이 친 낱말 그대로다. 0건 설명과 알림 줄이 쓴다.
func (q *Query) Words() []string {
	out := make([]string, 0, len(q.Terms))
	for _, item := range q.Terms {
		out = append(out, item.Text)
	}
	return out
}

// Live 는 AND 에 실제로 쓰는 낱말이다. 불용어는 뺀다. 다만 불용어만 남으면
// 질의가 통째로 사라지므로 안 뺀다 (설계 6-1a).
//
// **설계 4-3 은 불용어를 「줄인다」 고 적었다. 재 보고 안 줄였다.** 골든셋
// 80건에서 지금 28낱말 0.791 · 8낱말로 줄이면 0.776 · 아예 없애면 0.776
// (MRR 0.646 → 0.630 → 0.615) 이었다. 조사 B 의 −0.031 은 **더 늘렸을 때**
// 값이고, 지금 목록은 이미 그 손해 구간 아래다.
func (q *Query) Live() []Term {
	kept := []Term{}
	for _, item := range q.Terms {
		if item.Stop {
			continue
		}
		kept = append(kept, item)
	}
	if len(kept) == 0 {
		return q.Terms
	}
	return kept
}

// ParseQuery 는 질의 문자열 하나를 Query 로 바꾼다. 파서는 이것 하나뿐이고
// CLI 와 eval 이 똑같이 통과한다 (설계 6-11).
func ParseQuery(text string, stopwords []string) *Query {
	query := Query{Raw: text}
	stop := setOf(stopwords)
	groups := splitQuoted(text)
	words := []string{}
	for _, group := range groups {
		if group.text == "" {
			continue
		}
		if group.quoted {
			query.Phrases = appendUnique(query.Phrases, group.text)
		}
		words = append(words, strings.Fields(group.text)...)
	}
	words = query.takeSpecials(words)
	query.addWholePhrase(words)
	for _, word := range words {
		query.addTerm(word, stop)
	}
	return &query
}

// takeSpecials 는 `#태그`·`scope:이름`·시간 표현을 낱말에서 뽑아낸다. 뽑아 쓴
// 시간 낱말은 남기면 안 된다 — 본문에서 `지난주` 라는 글자를 찾게 된다 (설계 6-8).
func (q *Query) takeSpecials(words []string) []string {
	kept := []string{}
	for at := 0; at < len(words); at++ {
		word := words[at]
		if strings.HasPrefix(word, "#") && len(word) > 1 {
			q.Tags = appendUnique(q.Tags, strings.TrimPrefix(word, "#"))
			continue
		}
		if name, found := strings.CutPrefix(word, "scope:"); found && name != "" {
			q.Scopes = appendUnique(q.Scopes, name)
			continue
		}
		used, span, ok := readTime(words, at, time.Now())
		if ok {
			q.Since, q.Until, q.TimeWord = span.since, span.until, span.label
			at += used - 1
			continue
		}
		kept = append(kept, word)
	}
	return kept
}

// addWholePhrase 는 질의 전체를 구절 후보로 둔다. 따옴표가 셸에 먹혀도
// `훅 왜 느렸지` 가 통째로 본문에 있으면 그건 진짜 신호다 (설계 6-1a).
func (q *Query) addWholePhrase(words []string) {
	if len(words) < 2 {
		return
	}
	q.Phrases = appendUnique(q.Phrases, strings.Join(words, " "))
}

func (q *Query) addTerm(word string, stop map[string]bool) {
	plain := token.NormalizeNumber(word)
	parts := token.Runs(plain)
	if len(parts) == 0 {
		return
	}
	text := joinRuns(parts)
	if q.hasTerm(text) {
		return
	}
	if len(q.Terms) >= MaxTerms {
		q.Dropped = appendUnique(q.Dropped, text)
		return
	}
	item := Term{Text: text, Raw: word, Parts: parts, Stop: stop[text] || stop[plain] || stop[word]}
	if stem, ok := token.StripParticle(text); ok {
		item.Stem = stem
	}
	q.Terms = append(q.Terms, item)
	if item.Stop {
		q.Stopped = appendUnique(q.Stopped, text)
	}
}

// hasTerm 은 같은 낱말이 이미 있는지다. `훅 훅 훅` 이 랭킹을 세 벌 던지면
// 20k 에서 낱말 수만큼 시간이 곱해진다 (리뷰B #4).
func (q *Query) hasTerm(text string) bool {
	for _, item := range q.Terms {
		if item.Text == text {
			return true
		}
	}
	return false
}

// joinRuns 는 글자종 조각을 이어 붙인 꼴이다. 붙은 기호가 여기서 떨어진다.
func joinRuns(parts []token.Run) string {
	out := strings.Builder{}
	for _, part := range parts {
		out.WriteString(part.Text)
	}
	return out.String()
}

// quotedGroup 은 질의 한 조각과 그것이 따옴표 안이었는지다.
type quotedGroup struct {
	text   string
	quoted bool
}

// splitQuoted 는 따옴표 묶음을 살려서 공백으로 가른다. 따옴표는 이제 필터가
// 아니라 가산이라 셸이 먹어도 결과가 크게 안 갈린다 (설계 6-11).
func splitQuoted(query string) []quotedGroup {
	groups := []quotedGroup{}
	current := strings.Builder{}
	inQuote := false
	for _, letter := range query {
		if letter == '"' {
			groups = append(groups, quotedGroup{text: strings.TrimSpace(current.String()), quoted: inQuote})
			current.Reset()
			inQuote = !inQuote
			continue
		}
		if letter == ' ' && !inQuote {
			groups = append(groups, quotedGroup{text: strings.TrimSpace(current.String())})
			current.Reset()
			continue
		}
		current.WriteRune(letter)
	}
	return append(groups, quotedGroup{text: strings.TrimSpace(current.String()), quoted: inQuote})
}

func setOf(list []string) map[string]bool {
	out := map[string]bool{}
	for _, item := range list {
		out[item] = true
	}
	return out
}

func appendUnique(list []string, value string) []string {
	for _, item := range list {
		if item == value {
			return list
		}
	}
	return append(list, value)
}
