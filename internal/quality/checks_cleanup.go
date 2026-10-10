package quality

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/safe"
)

// 점검·정리 설계(2026-10-10) 1단의 검사다 — C16 · C17 · C18 · C20 (가) · D06.
// 전부 후보 등급이고 아무것도 안 고친다. 큐에 올려 사람·AI 가 고르게 할 뿐이다
// (결정 a40ae33f · 불변조건 I5). 쪼개진 짝(C20 (나))은 checks_merge.go, 본문 경로
// (D05)는 checks_stale.go 에 있다.

// addFunc 는 저장소 규칙이 걸림 하나를 붙이는 꼴이다.
type addFunc func(m *model.Memory, rule, reason string, related ...string)

// cleanupRoom 은 까닭 글에 끼우는 남의 글(id·근거·경로) 한 토막의 길이다.
const cleanupRoom = 80

// changeWords 는 「새 기억이 옛 것을 고쳤다」를 알리는 바뀜 말이다 (설계 2절 ① · ⑥).
// 앞 다섯이 C16 의 낱말이고, 뒤 넷은 C17 을 가르려고 더한 것이다. 둘 다 같은 표를 쓴다.
var changeWords = []string{"덮", "대신", "바꾼", "고친", "뒤집", "이제", "더 이상", "폐기", "취소"}

// changeWordIn 은 기억의 제목·요약·본문에 든 첫 바뀜 말이다. 없으면 빈 글이다.
// 요약에 「X 로 바꾼다」라고 적는 일이 흔해 본문만 보지 않는다.
func changeWordIn(m *model.Memory) string {
	text := m.Title + "\n" + m.Summary + "\n" + m.Body
	for _, word := range changeWords {
		if strings.Contains(text, word) {
			return word
		}
	}
	return ""
}

// clip 은 남의 글 한 토막을 까닭 글에 끼울 꼴로 만든다 (불변조건 I3).
func clip(text string) string { return safe.Summary(text, cleanupRoom) }

// memoriesByID 는 id → 기억이다. 같은 id 가 둘이면 뒤엣것이 이긴다.
func memoriesByID(memories []*model.Memory) map[string]*model.Memory {
	out := make(map[string]*model.Memory, len(memories))
	for _, m := range memories {
		if m != nil {
			out[m.ID] = m
		}
	}
	return out
}

// citedNotSuperseded 는 C16 이다. m 이 「새 기억」 쪽이고, 걸림은 m 이 `mem:` 으로
// 가리킨 **옛 기억**에 단다. 짝 하나에 한 번이다.
//
// 넷을 다 만족해야 한다 — 같은 종류 · 같은 scope · 옛 것이 아직 살아 있음 · 새 쪽에
// 바뀜 말. 모음 기억은 원본을 `mem:` 으로 모으는 것이 일이라 새 쪽으로 안 친다.
func citedNotSuperseded(m *model.Memory, byID map[string]*model.Memory, now time.Time, add addFunc) {
	if m.Type == model.TypeObservation {
		return
	}
	word := ""
	said := map[string]bool{}
	for _, source := range m.Sources {
		if model.SourceKind(source) != "mem" {
			continue
		}
		old := byID[strings.TrimSpace(strings.TrimPrefix(source, model.SourceMem))]
		if old == nil || old.ID == m.ID || said[old.ID] {
			continue
		}
		if old.Type != m.Type || old.Scope != m.Scope || !Live(old, now) {
			continue
		}
		if word == "" {
			if word = changeWordIn(m); word == "" {
				return
			}
		}
		said[old.ID] = true
		add(old, RuleCitedNotSuperseded,
			fmt.Sprintf("새 기억 `%s` 가 이 기억을 근거(mem:)로 달고 「%s」 라고 적었는데, 이 기억에 superseded_by 가 없다. 덮였는지 본다",
				clip(m.ID), word),
			m.ID)
	}
}

// stateCold 는 gc 가 본문을 접은 기억의 state 값이다 (index.StateCold 와 같은 글).
// quality 는 index 를 안 들여오므로 글을 따로 둔다.
const stateCold = "cold"

// retiredUnfolded 는 C18 이다. 덮였거나 무효인데 본문이 아직 접히지 않은 기억이다.
// 검색에선 빠지지만 아무 목록에도 안 떠서 쌓이기만 했다 (설계 2절 ④).
// pinned 는 안 올린다 — 처리 길인 `gc --retired` 가 pinned 를 안 접으므로(설계 7절),
// 올리면 큐에 떴는데 접히지 않는 줄이 남는다 (스튜디오 review 75 · gc 74 어긋남).
func retiredUnfolded(m *model.Memory, now time.Time, add addFunc) {
	if Live(m, now) || m.State == stateCold || m.Pinned {
		return
	}
	if m.SupersededBy != "" {
		add(m, RuleRetiredUnfolded,
			fmt.Sprintf("`%s` 가 이 기억을 덮었는데 본문이 접히지 않고 남았다", clip(m.SupersededBy)),
			m.SupersededBy)
		return
	}
	add(m, RuleRetiredUnfolded,
		fmt.Sprintf("무효 날짜(%s)가 지났는데 본문이 접히지 않고 남았다", clip(m.InvalidAt)))
}

// noArtifactSource 는 D06 이다. 근거가 필수인 종류인데 근거가 `mem:`·`note:` 뿐이다.
// 근거가 하나도 없는 것은 F16 몫이라 여기선 안 본다. 근거가 「구체적인지」는 뜻
// 판단이라 안 본다 (설계 결정 8).
func noArtifactSource(m *model.Memory, opt RepoOptions, add addFunc) {
	if m.Type == model.TypeObservation || len(m.Sources) == 0 {
		return
	}
	if opt.typeSpec(m).Sources != model.SourcesRequired {
		return
	}
	for _, source := range m.Sources {
		switch model.SourceKind(source) {
		case "mem", "note":
		default:
			return
		}
	}
	add(m, RuleNoArtifactSource,
		fmt.Sprintf("%s 기억인데 근거가 mem:·note: 뿐이다. 실물(file:·commit:·url:)을 가리키는 근거가 없다", clip(m.Type)))
}

// ── 닮은 짝 (C17 · C19 · C20 (가)) ─────────────────────────────────────────

// nearPairKey 는 짝을 (작은 id, 큰 id) 로 센 열쇠다.
type nearPairKey [2]string

func pairKeyOf(one, other string) nearPairKey {
	if other < one {
		one, other = other, one
	}
	return nearPairKey{one, other}
}

// nearPairOf 는 doc 쪽에서 본 정렬 결과를 (작은 id, 큰 id) 차례의 짝으로 만든다.
// 정렬 글도 id 를 따라 자리를 바꾼다.
func nearPairOf(mine, other string, align float64, mineText, otherText string) NearPair {
	if other < mine {
		return NearPair{Left: other, Right: mine, Align: align, LeftText: otherText, RightText: mineText}
	}
	return NearPair{Left: mine, Right: other, Align: align, LeftText: mineText, RightText: otherText}
}

// nearKept 는 짝으로 남길 만한지다 — 같은 scope 에 둘 다 살아 있고, 어느 쪽도 모음
// 기억·todo 가 아니다. 종류가 달라도 남긴다 (C19 는 종류를 안 가린다). C17·C20 은
// 같은 종류만 보므로 routeNear 가 다시 거른다.
func nearKept(one, other *model.Memory, now time.Time) bool {
	if one == nil || other == nil || one.Scope != other.Scope {
		return false
	}
	for _, m := range []*model.Memory{one, other} {
		if m.Type == model.TypeObservation || m.Type == model.TypeTodo || !Live(m, now) {
			return false
		}
	}
	return true
}

// softMatch 는 중복 판정의 닮은 것 하나가 C02(경고) 꼴인지다 — 합치기 (가) 의 자다.
// C03(본문 같음)과 거절 확신이 선 C01 은 뺀다. 그건 add 가 막았어야 하고 lint 가
// 거절로 말한다.
func softMatch(match Match, opt RepoOptions) bool {
	quality := opt.Config.Quality
	if match.Same || (match.Hard && match.Score >= quality.DupReject) {
		return false
	}
	return match.Score >= quality.DupWarn || match.Partial
}

// clashAndNear 는 한 기억의 후보에서 모순 짝(C13)과 닮은 짝을 **한 걸음에** 고른다.
// 모순 짝은 옛 clashingIDs 와 같은 차례·같은 상한이다. 닮은 짝은 정렬값이 C13 의
// 자(0.35) 이상인데 사실이 안 어긋나는 것과, 중복 판정이 C02 를 낸 것이다.
func clashAndNear(doc *Doc, session *Session, matches []Match, byID map[string]*model.Memory,
	opt RepoOptions) ([]string, []NearPair) {
	cut := staleConflictFloor()
	ids := []string{}
	clashed := map[string]bool{}
	pairs := map[string]NearPair{}
	mine := byID[doc.ID]
	for _, other := range session.Candidates(doc) {
		if other.ID == doc.ID || SameBody(doc, other) {
			continue
		}
		// 부르는 쪽이 보는 가장 낮은 자가 곧 문턱이다.
		info := alignOf(doc, other, cut, cut)
		if info.Best < cut {
			continue
		}
		if factsClash(info.Left, info.Right) {
			clashed[other.ID] = true
			if len(ids) < clashPairMax {
				ids = append(ids, other.ID)
			}
			continue
		}
		if nearKept(mine, byID[other.ID], opt.Now) {
			pairs[other.ID] = nearPairOf(doc.ID, other.ID, info.Best, info.Left, info.Right)
		}
	}
	for _, match := range matches {
		other := match.Doc
		if clashed[other.ID] || !softMatch(match, opt) || !nearKept(mine, byID[other.ID], opt.Now) {
			continue
		}
		pair, seen := pairs[other.ID]
		if !seen {
			// 정렬값이 0.35 아래라 위 걸음이 글을 안 남겼다. 기억당 dupListMax 개뿐이라
			// 끝까지 다시 잰다 — C19 가 NLI 에 보낼 두 문장이 있어야 한다.
			info := alignOf(doc, other, AlignCut, 0)
			pair = nearPairOf(doc.ID, other.ID, info.Best, info.Left, info.Right)
		}
		pair.Soft, pair.Score = true, match.Score
		pairs[other.ID] = pair
	}
	return ids, topNear(pairs)
}

// topNear 는 한 기억이 댈 짝을 nearPairMax 개로 자른다. 합치기 (가) 짝을 먼저,
// 그다음 정렬값이 높은 차례다.
func topNear(pairs map[string]NearPair) []NearPair {
	out := make([]NearPair, 0, len(pairs))
	for _, pair := range pairs {
		out = append(out, pair)
	}
	sortNearByStrength(out)
	if len(out) > nearPairMax {
		out = out[:nearPairMax]
	}
	return out
}

func sortNearByStrength(pairs []NearPair) {
	sort.Slice(pairs, func(a, b int) bool {
		if pairs[a].Soft != pairs[b].Soft {
			return pairs[a].Soft
		}
		if pairs[a].Align != pairs[b].Align {
			return pairs[a].Align > pairs[b].Align
		}
		if pairs[a].Left != pairs[b].Left {
			return pairs[a].Left < pairs[b].Left
		}
		return pairs[a].Right < pairs[b].Right
	})
}

// mergeNearPairs 는 기억마다 본 짝을 (작은 id, 큰 id) 로 한 번씩만 모은다.
//
//	① 어느 쪽에서든 C13(모순 짝)으로 걸린 짝은 뺀다 — contradict 큐가 맡는다
//	② 두 쪽에서 다 본 짝은 하나로 합친다 (Soft 는 어느 쪽이든, 정렬은 높은 쪽)
//	③ 기억 하나가 nearPairMax 짝을 넘게 들지 않게 센 뒤 (Left, Right) 차례로 낸다
func mergeNearPairs(near [][]NearPair, clash [][]string, memories []*model.Memory) []NearPair {
	clashed := map[nearPairKey]bool{}
	for at, ids := range clash {
		for _, id := range ids {
			clashed[pairKeyOf(memories[at].ID, id)] = true
		}
	}
	merged := map[nearPairKey]NearPair{}
	for _, list := range near {
		for _, pair := range list {
			key := nearPairKey{pair.Left, pair.Right}
			if clashed[key] {
				continue
			}
			if old, seen := merged[key]; seen {
				pair = joinNear(old, pair)
			}
			merged[key] = pair
		}
	}
	all := make([]NearPair, 0, len(merged))
	for _, pair := range merged {
		all = append(all, pair)
	}
	sortNearByStrength(all)
	held := map[string]int{}
	out := []NearPair{}
	for _, pair := range all {
		if held[pair.Left] >= nearPairMax || held[pair.Right] >= nearPairMax {
			continue
		}
		held[pair.Left]++
		held[pair.Right]++
		out = append(out, pair)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Left != out[b].Left {
			return out[a].Left < out[b].Left
		}
		return out[a].Right < out[b].Right
	})
	return out
}

// joinNear 는 같은 짝을 두 쪽에서 본 것을 하나로 합친다.
func joinNear(one, other NearPair) NearPair {
	out := one
	if other.Align > one.Align {
		out.Align, out.LeftText, out.RightText = other.Align, other.LeftText, other.RightText
	}
	out.Soft = one.Soft || other.Soft
	if other.Score > out.Score {
		out.Score = other.Score
	}
	return out
}

// newerSiblingTypes 는 C17 이 보는 종류다. decision 은 C05·C08 이 맡는다 (설계 결정 4).
var newerSiblingTypes = map[string]bool{model.TypeCaution: true, model.TypeHowto: true, model.TypeFact: true}

// revisedPair 는 짝 중 「분명히 더 새것이 옛 것을 고쳤다」 꼴인지다. 날짜가 하루
// 이상 다르고(같은 날 둘은 어느 쪽이 고친 것인지 모른다) 새 쪽에 바뀜 말이 있어야
// 한다. 아니면 word 가 빈 글이다.
func revisedPair(one, other *model.Memory) (older, newer *model.Memory, word string) {
	if !model.IsDate(one.Date) || !model.IsDate(other.Date) || one.Date == other.Date {
		return nil, nil, ""
	}
	older, newer = one, other
	if other.Date < one.Date {
		older, newer = other, one
	}
	return older, newer, changeWordIn(newer)
}

// C20 (가) 문턱 (손검사 뒤 보완 · 2026-10-10 밤 2바퀴).
//
// 1바퀴는 정렬 0.25 · 닮음 DupWarn(0.06) 이었다. 1바퀴 뒤 새 10짝에서 「틀리다」 3짝은
// 정렬 0.29 둘 · 닮음 0.065 하나였고, 「맞다」 3짝은 모두 정렬 0.34 이상 · 닮음 0.075
// 이상이었다. 그래서 둘 다 한 칸씩 올린다.
const (
	// mergeAlignMin 은 C20 (가) 가 보는 정렬값의 아래 자다.
	mergeAlignMin = 0.30
	// mergeScoreMin 은 C20 (가) 가 보는 통째 닮음의 아래 자다 (DupWarn 보다 높다).
	mergeScoreMin = 0.07
)

// 둘 다 history 인 짝의 문턱 (2바퀴). 1바퀴는 history 짝을 통째로 뺐는데, 손검사
// 15번(정렬 0.81 · 닮음 0.136 — 예상 공수표가 실제 공수표에 그대로 든 짝)까지 같이
// 빠졌다. history 는 공수표 틀이 같아 서로 닮아 보이므로, 일반 짝보다 훨씬 높은
// 자를 넘을 때만 남긴다.
//
// 3바퀴에서 정렬 자를 0.5 → 0.75 로 올렸다. 2바퀴 뒤 남은 「틀리다」 history 짝 둘은
// 「이어진 단계」 기록(정렬 0.66 · 0.57)이었고, 「맞다」 history 짝 둘은 0.81 · 0.80 이었다.
const (
	mergeHistoryAlignMin = 0.75
	mergeHistoryScoreMin = 0.10
)

// 옮겨 온 짝의 정렬 자 (3바퀴). 둘 다 migrate 가 옮긴 기억(migrated: true)이고 같은 날
// (id 앞 8자리) 것이면 정렬 0.25 · 닮음 DupWarn 으로 본다. v0.1 기억은 한 세션에 짧게
// 몰아 쓴 것이라 같은 말을 다른 낱말로 적어, 정렬값이 낮게 나온다 — 2바퀴에서 「맞다」
// 셋(정렬 0.36·0.25·0.29 · 닮음 0.067~0.087)이 이 꼴로 같이 빠졌다.
const mergeMigratedAlignMin = 0.25

// migratedTwins 는 두 기억이 다 옮겨 온 것이고 id 의 날짜(앞 8자리)가 같은지다.
func migratedTwins(one, other *model.Memory) bool {
	if one == nil || other == nil || !one.Migrated || !other.Migrated {
		return false
	}
	return len(one.ID) >= 8 && len(other.ID) >= 8 && one.ID[:8] == other.ID[:8]
}

// mergeWorthy 는 닮은 짝이 C20 (가) 에 오를 만한지다. 중복 경고(soft) 짝이고, 통째
// 닮음이 DupWarn 과 mergeScoreMin 이상이며 정렬값이 mergeAlignMin 이상이어야 한다.
// Partial 길(닮음은 DupWarn 아래인데 문장 하나만 닮음)만으로는 안 낸다 — 손검사에서
// 7건 다 오탐이었다. migrated 가 참이면(migratedTwins) 옮겨 온 짝의 자를 쓴다.
func mergeWorthy(pair NearPair, dupWarn float64, migrated bool) bool {
	if !pair.Soft || pair.Score < dupWarn {
		return false
	}
	if migrated {
		return pair.Align >= mergeMigratedAlignMin
	}
	return pair.Score >= mergeScoreMin && pair.Align >= mergeAlignMin
}

// historyKept 는 짝이 history 문턱을 넘는지다. 한쪽이라도 history 가 아니면 늘 참이다.
// 둘 다 history 면 정렬 mergeHistoryAlignMin · 닮음 mergeHistoryScoreMin 을 다 넘어야
// 한다. (가)·(나) 둘 다 이 자를 쓴다.
func historyKept(one, other *model.Memory, align, score float64) bool {
	if one.Type != model.TypeHistory || other.Type != model.TypeHistory {
		return true
	}
	return align >= mergeHistoryAlignMin && score >= mergeHistoryScoreMin
}

// routeNear 는 닮은 짝 하나를 **한 큐로만** 보낸다 (설계 ⑥ · 결정 16).
//
//  1. C13 이나 C16 에 이미 걸린 짝 → 건너뜀 (contradict · conflict 가 맡는다).
//     C19 는 review 에서 나므로 review 가 merge 줄을 지운다.
//  2. 정렬값이 0.35(C13 의 자) 이상 · 날짜가 다르고 새 쪽에 바뀜 말 → C17
//     (caution·howto·fact 만, 옛 쪽에 단다). 0.35 아래 soft 짝은 바뀜 말이 있어도
//     C17 로 안 보낸다 — 「바꾼」 같은 말을 엉뚱한 문장에서 읽는다 (손검사 뒤 보완)
//  3. 나머지 중 mergeWorthy 짝 → C20 (둘 다 옮겨 온 같은 날 짝은 옮겨 온 짝의 자).
//     둘 다 history 인데 historyKept 를 못 넘거나
//     decision 짝이 C05·C08 에 걸렸으면 안 낸다
//  4. 쪼개진 짝(나) 중 닮은 짝에 안 든 것 → C20. 둘 다 history 면 같은 historyKept
//     자를 본다 — (나) 는 정렬 0.35 아래 짝만 고르므로 history 짝은 사실상 안 난다
//
// C20 은 기억 하나당 nearPairMax 짝까지다.
func (r *RepoReport) routeNear(memories []*model.Memory, result duplicateResult, dupWarn float64, add addFunc) {
	byID := memoriesByID(memories)
	seen := map[nearPairKey]bool{}
	merged := map[string]int{}
	mergeRoom := func(one, other string) bool {
		return merged[one] < nearPairMax && merged[other] < nearPairMax
	}
	for _, pair := range result.near {
		seen[nearPairKey{pair.Left, pair.Right}] = true
		left, right := byID[pair.Left], byID[pair.Right]
		if left == nil || right == nil || left.Type != right.Type {
			continue
		}
		if r.pairTaken(left.ID, right.ID, RuleStaleConflictPair, RuleCitedNotSuperseded) {
			continue
		}
		if older, newer, word := revisedPair(left, right); word != "" && pair.Align >= staleConflictFloor() {
			if newerSiblingTypes[left.Type] {
				add(older, RuleNewerSibling,
					fmt.Sprintf("더 새 기억 `%s`(%s)가 닮은 말을 하며 「%s」 라고 적었다 (정렬 %.2f). 이 기억이 덮였는지 본다",
						clip(newer.ID), clip(newer.Date), word, pair.Align),
					newer.ID)
				continue
			}
		}
		if !mergeWorthy(pair, dupWarn, migratedTwins(left, right)) || !historyKept(left, right, pair.Align, pair.Score) ||
			r.decisionTaken(left, right) || !mergeRoom(left.ID, right.ID) {
			continue
		}
		merged[left.ID]++
		merged[right.ID]++
		add(left, RuleMergeCandidate,
			fmt.Sprintf("`%s` 와 닮았다 (정렬 %.2f · 닮음 %.3f). 하나로 합칠지 본다", clip(right.ID), pair.Align, pair.Score),
			right.ID)
	}
	for _, one := range result.split {
		if seen[nearPairKey{one.left, one.right}] {
			continue
		}
		left, right := byID[one.left], byID[one.right]
		if left == nil || right == nil || !historyKept(left, right, one.align, one.score) {
			continue
		}
		if r.pairTaken(left.ID, right.ID, RuleStaleConflictPair, RuleCitedNotSuperseded) ||
			r.decisionTaken(left, right) || !mergeRoom(left.ID, right.ID) {
			continue
		}
		merged[left.ID]++
		merged[right.ID]++
		add(left, RuleMergeCandidate,
			fmt.Sprintf("`%s` 와 같은 근거 `%s` 를 달았고 날짜가 %d일 차이다. 한 내용이 둘로 쪼개졌는지 본다",
				clip(right.ID), clip(one.source), one.days),
			right.ID)
	}
}

// decisionTaken 은 decision 짝이 이미 C05·C08(conflict 큐)에 걸렸는지다.
func (r *RepoReport) decisionTaken(one, other *model.Memory) bool {
	return one.Type == model.TypeDecision &&
		r.pairTaken(one.ID, other.ID, RuleDecisionConflictLive, RuleSupersedeMissing)
}

// pairTaken 은 두 기억 중 한쪽이 다른 쪽을 Related 로 든 채 rules 중 하나에 걸렸는지다.
func (r *RepoReport) pairTaken(one, other string, rules ...string) bool {
	return r.points(one, other, rules) || r.points(other, one, rules)
}

func (r *RepoReport) points(from, to string, rules []string) bool {
	for _, found := range r.ByID[from] {
		if !containsString(rules, found.Rule) {
			continue
		}
		if containsString(found.Related, to) {
			return true
		}
	}
	return false
}

func containsString(list []string, value string) bool {
	for _, one := range list {
		if one == value {
			return true
		}
	}
	return false
}
