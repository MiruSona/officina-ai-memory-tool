package review

// `mem review --nli` 의 NLI 묻기 한 판 — C19(모순 후보) · C13(숫자 어긋남 짝) · C20(합치기 짝) ·
// obs(모음 기억 줄 ↔ 인용 기억)가 전체 상한을 몫으로 나눠 쓴다.
// 설계 : Docs/History/JudgeModel/Design/2026-10-19-v1.2-적용다섯자리설계.md 3절 · 5절.
//
// 판정은 안 한다. 확신 반대인 merge 짝을 contradict 로 올리고, 까닭에 NLI 답을 붙이고,
// 무관이면 큐 뒤로 미룰 뿐이다. 지우지 않는다 — 덮인·접힌 기억(retired · fold)은 묻지도 않는다.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/llm"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
)

// NLI 를 묻는 자리 이름이다. 이 차례로 묻는다.
const (
	PlaceC19 = "C19"
	PlaceC13 = "C13"
	PlaceC20 = "C20"
	PlaceObs = "obs"
)

// 자리별 몫(%)이다. obs 자리가 돌 때(basis 큐를 볼 때)와 안 돌 때를 나눈다.
var (
	sharesWithObs    = []int{45, 20, 20, 15}
	sharesWithoutObs = []int{50, 25, 25, 0}
)

// nliAsk 는 NLI 물음 하나다. key 는 짝 열쇠, apply 는 받은 답을 걸림에 반영한다.
type nliAsk struct {
	key             [2]string
	scope           string
	evidence, claim string
	apply           func(verdict llm.Verdict)
}

// orderKey 는 큐 안 자리(앞 · 뒤)를 기억할 걸림 하나의 열쇠다.
type orderKey struct {
	rule, id, other string
}

// 큐 안 자리다. 0 이 지금 차례(날짜 순)이고 앞은 그보다 먼저, 뒤는 그보다 나중이다.
const (
	orderFront = -1
	orderBack  = 1
)

// sieveNear 는 쌓아 둔 C19 물음에 C13 · C20 · obs 물음을 더해 몫대로 묻는다. `--nli` 가 없으면
// 아무것도 안 한다 — 출력이 지금과 같다.
func (r *Report) sieveNear(found *quality.RepoReport, byID map[string]*model.Memory, options Options) {
	pending := r.pending
	r.pending = nil
	if options.NLI == nil {
		return
	}
	want := wanted(options.Kinds)
	nearOn := want[KindContradict] || want[KindMerge]
	if !nearOn && !want[KindBasis] {
		return
	}
	run := r.nliFor(options)
	if run == nil {
		return
	}
	scopes := scopeSet(options.Scopes)
	places := [][]nliAsk{pending, nil, nil, nil}
	if nearOn {
		places[1] = r.staleConflictAsks(found, byID, scopes, options)
		places[2] = r.mergeAsks(found, byID, scopes, options)
	}
	shares := sharesWithoutObs
	names := []string{PlaceC19, PlaceC13, PlaceC20}
	if want[KindBasis] {
		shares = sharesWithObs
		names = append(names, PlaceObs)
		places[3] = r.obsCiteAsks(found, byID, scopes, options)
	}
	needs := make([]int, len(names))
	for at := range names {
		needs[at] = len(places[at])
	}
	quotas := allocate(run.pairCap, shares[:len(names)], needs)
	run.summary.Places = make([]NLIPlace, len(names))
	answered := map[string]llm.Verdict{}
	for at, name := range names {
		run.summary.Places[at] = NLIPlace{Name: name, Quota: quotas[at]}
		run.place = &run.summary.Places[at]
		run.limit += quotas[at]
		for _, ask := range places[at] {
			// 같은 짝 · 같은 글을 앞 자리(C19)가 이미 물었으면 그 답을 다시 쓴다.
			cacheKey := ask.key[0] + "\x00" + ask.key[1] + "\x00" + ask.evidence + "\x00" + ask.claim
			if verdict, ok := answered[cacheKey]; ok {
				ask.apply(verdict)
				continue
			}
			if !run.take(ask.scope) {
				continue
			}
			verdict, err := run.ask(ask.evidence, ask.claim)
			if err != nil {
				continue
			}
			answered[cacheKey] = verdict
			ask.apply(verdict)
		}
	}
	r.noteNLILimits()
}

// allocate 는 전체 상한을 자리별 몫으로 나눈다. 먼저 몫(%)만큼 주되 필요한 짝 수를 넘지 않게 하고,
// 남은 것은 앞 자리부터 모자란 만큼 더 준다 — 쓸 짝이 없는 자리의 몫이 버려지지 않는다.
func allocate(total int, shares, needs []int) []int {
	quotas := make([]int, len(needs))
	rest := total
	for at := range needs {
		quotas[at] = min(needs[at], total*shares[at]/100)
		rest -= quotas[at]
	}
	for at := range needs {
		extra := min(rest, needs[at]-quotas[at])
		quotas[at] += extra
		rest -= extra
	}
	return quotas
}

// staleConflictAsks 는 C13 짝 물음이다. 두 기억의 요약(꼬리표 뗌)을 옛 쪽을 근거로 보낸다.
// 반대면 까닭에 붙이고 큐 앞으로, 지지면 「숫자 자리가 다른 것일 수 있다」, 무관이면 큐 뒤로 민다.
func (r *Report) staleConflictAsks(found *quality.RepoReport, byID map[string]*model.Memory,
	scopes map[string]bool, options Options) []nliAsk {
	asks := []nliAsk{}
	for _, key := range rulePairs(found, quality.RuleStaleConflictPair) {
		left, right := byID[key[0]], byID[key[1]]
		if !askable(left, right, scopes, options) {
			continue
		}
		evidence, claim := olderFirst(left, right, left.Summary, right.Summary)
		if evidence == "" || claim == "" {
			continue
		}
		pair := key
		asks = append(asks, nliAsk{key: pair, scope: left.Scope, evidence: evidence, claim: claim,
			apply: func(verdict llm.Verdict) { r.markStaleConflict(found, pair, verdict) }})
	}
	return asks
}

// markStaleConflict 는 C13 짝 하나의 답을 두 쪽 걸림에 반영한다.
func (r *Report) markStaleConflict(found *quality.RepoReport, pair [2]string, verdict llm.Verdict) {
	if verdict.Unsure {
		return
	}
	label := ""
	order := 0
	switch verdict.Letter {
	case llm.LetterContradict:
		label, order = fmt.Sprintf("NLI 반대 %.2f", verdict.Prob), orderFront
	case llm.LetterSupport:
		label = fmt.Sprintf("NLI 지지 %.2f — 숫자 자리가 다른 것일 수 있다", verdict.Prob)
	case llm.LetterUnrelated:
		label, order = fmt.Sprintf("NLI 무관 %.2f", verdict.Prob), orderBack
	default:
		return
	}
	for _, side := range [][2]string{{pair[0], pair[1]}, {pair[1], pair[0]}} {
		list := found.ByID[side[0]]
		for at := range list {
			one := &list[at]
			if one.Rule != quality.RuleStaleConflictPair || !relates(one.Related, side[1]) {
				continue
			}
			prefix := label
			if len(one.Related) > 1 {
				prefix += " (`" + side[1] + "`)"
			}
			one.Reason = prefix + " · " + one.Reason
			r.setOrder(*one, order)
		}
	}
}

// mergeAsks 는 C20 짝 물음이다. 닮은 짝이면 정렬된 두 문장을, 쪼개진 짝이면 두 요약을 보낸다.
// 반대면 C19 꼴 걸림을 더해 dropShadowed 가 merge 줄을 지우게 하고, 무관이면 줄을 큐 뒤로 민다.
func (r *Report) mergeAsks(found *quality.RepoReport, byID map[string]*model.Memory,
	scopes map[string]bool, options Options) []nliAsk {
	near := map[[2]string]quality.NearPair{}
	for _, pair := range found.Near {
		if pair.Right < pair.Left {
			pair.Left, pair.Right = pair.Right, pair.Left
			pair.LeftText, pair.RightText = pair.RightText, pair.LeftText
		}
		near[[2]string{pair.Left, pair.Right}] = pair
	}
	asks := []nliAsk{}
	for _, key := range rulePairs(found, quality.RuleMergeCandidate) {
		left, right := byID[key[0]], byID[key[1]]
		if !askable(left, right, scopes, options) {
			continue
		}
		shown := quality.NearPair{Left: key[0], Right: key[1], LeftText: left.Summary, RightText: right.Summary}
		if pair, ok := near[key]; ok && strings.TrimSpace(pair.LeftText) != "" && strings.TrimSpace(pair.RightText) != "" {
			shown = pair
		}
		evidence, claim := olderFirst(left, right, shown.LeftText, shown.RightText)
		if evidence == "" || claim == "" {
			continue
		}
		pair := key
		// contradict 로 올리는 것은 C19 와 같은 조건(어긋남이 뜻을 갖는 종류 · 같은 scope)일 때만이다.
		promote := contradictTypes[left.Type] && contradictTypes[right.Type] && left.Scope == right.Scope
		asks = append(asks, nliAsk{key: pair, scope: left.Scope, evidence: evidence, claim: claim,
			apply: func(verdict llm.Verdict) { r.markMerge(found, shown, verdict, promote) }})
	}
	return asks
}

// markMerge 는 C20 짝 하나의 답을 반영한다. 반대 · 무관 확신만 본다. promote 가 거짓이면(C19 가 안
// 보는 종류이거나 scope 가 다르면) 반대여도 contradict 로 올리지 않고 merge 줄을 그대로 둔다.
func (r *Report) markMerge(found *quality.RepoReport, pair quality.NearPair, verdict llm.Verdict, promote bool) {
	if verdict.Unsure {
		return
	}
	switch verdict.Letter {
	case llm.LetterContradict:
		if promote && !hasContradict(found, pair.Left, pair.Right) {
			addContradict(found, pair, fmt.Sprintf("NLI 반대 %.2f · merge 짝에서", verdict.Prob))
		}
	case llm.LetterUnrelated:
		for _, side := range [][2]string{{pair.Left, pair.Right}, {pair.Right, pair.Left}} {
			list := found.ByID[side[0]]
			for at := range list {
				one := &list[at]
				if one.Rule != quality.RuleMergeCandidate || !relates(one.Related, side[1]) {
					continue
				}
				one.Reason = fmt.Sprintf("NLI 무관 %.2f · ", verdict.Prob) + one.Reason
				r.setOrder(*one, orderBack)
			}
		}
	}
}

// rulePairs 는 그 규칙에 걸린 짝을 (작은 id, 큰 id) 로 한 번씩, 그 차례로 준다 — 상한에 걸릴 때
// 어느 짝이 밀리는지가 매번 같아야 한다.
func rulePairs(found *quality.RepoReport, rule string) [][2]string {
	seen := map[[2]string]bool{}
	pairs := [][2]string{}
	for _, id := range sortedIDs(found.ByID) {
		for _, one := range found.ByID[id] {
			if one.Rule != rule {
				continue
			}
			for _, other := range one.Related {
				key := pairKey(id, other)
				if key[0] == key[1] || seen[key] {
					continue
				}
				seen[key] = true
				pairs = append(pairs, key)
			}
		}
	}
	sort.Slice(pairs, func(a, b int) bool {
		if pairs[a][0] != pairs[b][0] {
			return pairs[a][0] < pairs[b][0]
		}
		return pairs[a][1] < pairs[b][1]
	})
	return pairs
}

// askable 은 NLI 에 물을 짝인지다 — 둘 다 있고 · 살아 있고 · 접히지 않았고 · `--scope` 안이다.
func askable(left, right *model.Memory, scopes map[string]bool, options Options) bool {
	if left == nil || right == nil || !inScopes(scopes, left) {
		return false
	}
	return alive(left, options) && alive(right, options)
}

// alive 는 덮이거나 무효가 아니고 본문이 접히지(fold) 않은 기억이다.
func alive(m *model.Memory, options Options) bool {
	return quality.Live(m, options.Now) && m.State != index.StateCold
}

// olderFirst 는 꼬리표를 뗀 두 글을 (옛 쪽 = 근거, 새 쪽 = 주장) 차례로 준다. 하나라도 비면 빈 글이다.
// C19 와 같은 글 꼴이라 같은 짝이면 C19 의 답을 다시 쓸 수 있다.
func olderFirst(left, right *model.Memory, leftText, rightText string) (string, string) {
	evidence, claim := quality.StripTags(leftText), quality.StripTags(rightText)
	if right.Date < left.Date {
		evidence, claim = claim, evidence
	}
	if strings.TrimSpace(evidence) == "" || strings.TrimSpace(claim) == "" {
		return "", ""
	}
	return evidence, claim
}

func relates(related []string, id string) bool {
	for _, one := range related {
		if one == id {
			return true
		}
	}
	return false
}

// hasContradict 는 그 짝에 C19 걸림이 이미 있는지다. 같은 짝을 두 번 올리지 않는다.
func hasContradict(found *quality.RepoReport, left, right string) bool {
	for _, one := range found.ByID[left] {
		if one.Rule == quality.RuleContradictCand && relates(one.Related, right) {
			return true
		}
	}
	return false
}

// setOrder 는 걸림 하나의 큐 안 자리를 적는다. 앞(반대)이 한 번 정해지면 뒤로 안 밀린다.
func (r *Report) setOrder(one quality.Finding, order int) {
	if order == 0 {
		return
	}
	if r.order == nil {
		r.order = map[orderKey]int{}
	}
	key := orderKeyOf(one)
	if r.order[key] == orderFront {
		return
	}
	r.order[key] = order
}

func orderKeyOf(one quality.Finding) orderKey {
	other := ""
	if len(one.Related) > 0 {
		other = one.Related[0]
	}
	return orderKey{rule: one.Rule, id: one.ID, other: other}
}
