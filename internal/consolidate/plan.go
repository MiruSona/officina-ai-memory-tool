package consolidate

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// 묶음 상태.
const (
	StatusNew   = "new"
	StatusAgain = "again"
	StatusSame  = "same"
)

// 카드 머리말 값 (자동쌓기설계 3-2·2-4).
const (
	CardOrigin = "card"
	CardAuthor = "mem-consolidate/b1"
)

// matchFloor 는 묶음과 이미 있는 카드를 같은 것으로 보는 구성원 겹침(자카드)이다.
// 절반 이상 겹치면 새 카드를 만들지 않고 그 카드를 「다시」 쓴다.
const matchFloor = 0.5

// Plan 은 `mem consolidate --plan` 의 결과다.
type Plan struct {
	Found
	New, Again, Same int
}

// MakePlan 은 묶음마다 이미 있는 카드를 맞추고 새로·다시·그대로를 매긴다.
func MakePlan(in Input) Plan {
	found := Find(in)
	byID := indexOf(in.Memories)
	cards := liveCards(in.Memories)
	used := map[string]bool{}
	plan := Plan{Found: found}
	for at := range plan.Groups {
		group := &plan.Groups[at]
		// 같은 규칙 카드부터 맞춘다. 못 찾으면 rule 칸이 없는 옛 카드를 본다. 다른 규칙
		// 카드는 안 가로챈다 — 앞 묶음이 남의 카드를 「다시」 로 가져가면 원래 묶음이
		// 카드를 하나 더 만든다 (리뷰 2026-10-05).
		card, score := bestCard(group.Members, cardsOfRule(cards, group.Rule), used)
		if card == nil || score < matchFloor {
			card, score = bestCard(group.Members, cardsOfRule(cards, ""), used)
		}
		if card == nil || score < matchFloor {
			group.Status = StatusNew
			plan.New++
			continue
		}
		used[card.ID] = true
		group.Existing = card.ID
		group.Status, group.Why = againOrSame(group.Members, card, byID)
		if group.Status == StatusSame {
			plan.Same++
		} else {
			plan.Again++
		}
	}
	return plan
}

// liveCards 는 맞춰 볼 모음 기억이다. 보류·덮인 카드는 없는 것으로 친다 —
// `mem auto undo` 로 돌린 카드를 다시 쓰면 되돌리기가 무의미해진다.
// **consolidate 가 쓴 카드(origin: card)만이다.** 사람이 손으로 쓴 observation 은
// 절대 고쳐 쓰지 않는다 — 그 기억엔 origin 이 없어 `auto undo` 로도 못 되돌린다
// (리뷰 2026-10-05).
func liveCards(memories []*model.Memory) []*model.Memory {
	out := []*model.Memory{}
	for _, m := range memories {
		if m.Type == model.TypeObservation && m.Origin == CardOrigin && !m.Review && m.SupersededBy == "" {
			out = append(out, m)
		}
	}
	return out
}

// cardsOfRule 은 rule 칸이 그 값인 카드다. 빈 값이면 rule 칸이 없는 옛 카드다.
func cardsOfRule(cards []*model.Memory, rule string) []*model.Memory {
	out := []*model.Memory{}
	for _, card := range cards {
		if card.CardRule == rule {
			out = append(out, card)
		}
	}
	return out
}

func bestCard(members []string, cards []*model.Memory, used map[string]bool) (*model.Memory, float64) {
	var best *model.Memory
	bestScore := 0.0
	for _, card := range cards {
		if used[card.ID] {
			continue
		}
		score := jaccard(members, model.MemSources(card.Sources))
		if score > bestScore {
			best, bestScore = card, score
		}
	}
	return best, bestScore
}

func jaccard(left, right []string) float64 {
	set := map[string]int{}
	for _, id := range left {
		set[id] |= 1
	}
	for _, id := range right {
		set[id] |= 2
	}
	both := 0
	for _, mark := range set {
		if mark == 3 {
			both++
		}
	}
	if len(set) == 0 {
		return 0
	}
	return float64(both) / float64(len(set))
}

// againOrSame 은 구성원이 같고 근거가 안 바뀌었으면 그대로, 아니면 다시다.
func againOrSame(members []string, card *model.Memory, byID map[string]*model.Memory) (string, string) {
	if setKey(members) != setKey(model.MemSources(card.Sources)) {
		return StatusAgain, "구성원이 바뀜"
	}
	if reason, _ := quality.ObservationStale(card, byID); reason != "" {
		return StatusAgain, "근거가 바뀜"
	}
	return StatusSame, ""
}

// Card 는 묶음 하나를 카드 요청으로 만든다. 글은 원본의 제목·요약·날짜를 그대로 잇는다.
func Card(group Group, byID map[string]*model.Memory, today string) store.AddRequest {
	members := make([]*model.Memory, 0, len(group.Members))
	parts := make([]model.BasisPart, 0, len(group.Members))
	sources := make([]string, 0, len(group.Members))
	for _, id := range group.Members {
		members = append(members, byID[id])
		parts = append(parts, model.PartOf(byID[id]))
		sources = append(sources, model.SourceMem+id)
	}
	title, summary := cardHead(group, members)
	return store.AddRequest{
		Type: model.TypeObservation, Date: today, Title: title, Summary: summary,
		Tags: cardTags(members), Scope: group.Scope, Author: CardAuthor, Sources: sources,
		Origin: CardOrigin, BasisHash: model.BasisHash(parts), Rev: 1, CardRule: group.Rule,
		Body: cardBody(group, members),
	}
}

// cardHead 는 제목·요약이다. 사슬은 「흐름」, 나머지는 「모음」 이다.
func cardHead(group Group, members []*model.Memory) (string, string) {
	oldest, newest := members[0], members[len(members)-1]
	if group.Rule == RuleChain {
		title := clip("흐름 · "+newest.DisplayTitle(), 40)
		summary := fmt.Sprintf("%s 에서 %s 로 바뀐 흐름 %d건", clip(oldest.DisplayTitle(), 30),
			clip(newest.DisplayTitle(), 30), len(members))
		return title, fitSummary(summary, oldest.Date, newest.Date)
	}
	center := centerOf(members)
	title := clip("모음 · "+center.DisplayTitle(), 40)
	names := []string{}
	for _, m := range members {
		if m != center {
			names = append(names, m.DisplayTitle())
		}
	}
	summary := fmt.Sprintf("%s 와 이어진 기억 %d건: %s", clip(center.DisplayTitle(), 24), len(members)-1,
		strings.Join(names, " · "))
	return title, fitSummary(summary, oldest.Date, newest.Date)
}

// centerOf 는 주제 카드의 대표다 — 가장 새 기억이다. 덮은 말·고친 말이 대개 뒤에 온다.
func centerOf(members []*model.Memory) *model.Memory { return members[len(members)-1] }

// fitSummary 는 요약을 30~120자로 맞춘다. 짧으면 날짜 범위를 덧붙이고 길면 자른다.
func fitSummary(summary, from, to string) string {
	if utf8.RuneCountInString(summary) < 30 {
		summary += fmt.Sprintf(" (%s ~ %s)", from, to)
	}
	return clip(summary, 120)
}

// clip 은 글자 수로 자른다. 잘랐으면 끝을 「…」 로 둔다.
func clip(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

// cardTags 는 구성원 태그 중 많이 나온 차례로 셋이다. 둘이 안 되면 observation 을 채운다.
func cardTags(members []*model.Memory) []string {
	count := map[string]int{}
	first := map[string]int{}
	order := 0
	for _, m := range members {
		for _, tag := range m.Tags {
			if _, ok := first[tag]; !ok {
				first[tag] = order
				order++
			}
			count[tag]++
		}
	}
	tags := make([]string, 0, len(count))
	for tag := range count {
		tags = append(tags, tag)
	}
	sort.Slice(tags, func(a, b int) bool {
		if count[tags[a]] != count[tags[b]] {
			return count[tags[a]] > count[tags[b]]
		}
		return first[tags[a]] < first[tags[b]]
	})
	if len(tags) > 3 {
		tags = tags[:3]
	}
	for _, pad := range []string{model.TypeObservation, "card"} {
		if len(tags) >= 2 {
			break
		}
		tags = append(tags, pad)
	}
	return tags
}

// cardBody 는 번호 줄 본문이다. 줄마다 끝에 [mem:id] 가 붙는다 (자동쌓기설계 3-2).
// 사슬은 앞줄들에 「예전」, 끝줄에 「지금」 을 단다.
func cardBody(group Group, members []*model.Memory) string {
	lines := make([]string, 0, len(members))
	for at, m := range members {
		mark := ""
		if group.Rule == RuleChain {
			mark = "[예전] "
			if at == len(members)-1 {
				mark = "[지금] "
			}
		}
		lines = append(lines, fmt.Sprintf("%d. %s%s %s — %s [mem:%s]", at+1, mark, m.Date,
			oneLine(m.DisplayTitle()), oneLine(m.Summary), m.ID))
	}
	return strings.Join(lines, "\n")
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(text, "[mem:", "[mem ")), " ")
}

// Rewrite 는 「다시」 묶음에서 이미 있는 카드에 덮어쓸 칸이다. 본문은 따로 amend 한다.
func Rewrite(card store.AddRequest, rev int) map[string]any {
	return map[string]any{
		"title": card.Title, "summary": card.Summary, "tags": card.Tags,
		"sources": card.Sources, "basis_hash": card.BasisHash, "rev": rev, "rule": card.CardRule,
	}
}
