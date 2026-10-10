package quality

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/link"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// RepoOptions 는 저장소 전체를 훑을 때의 조건이다. add 관문이 못 막는 것 —
// 시간이 지나야 아는 것만 여기 있다 (설계 3-3).
type RepoOptions struct {
	Options
	// Hits 는 id → 마지막 조회 시각이다. nil 이면 cold·orphan 을 건너뛴다.
	// 조회 기록이 없는데 「한 번도 안 잡혔다」고 말하면 전 저장소가 경고가 된다.
	Hits map[string]time.Time
	// SourceChanged 는 근거가 기억 날짜 뒤에 바뀌었는지다 (git log 를 아는 쪽이
	// 붙인다). nil 이면 C06 을 건너뛴다.
	SourceChanged func(m *model.Memory, source string) bool
	// SourceMissing 은 근거가 이제 없는지다 (D02·D03 · 설계 결정 36 ③).
	// nil 이면 그 둘을 건너뛴다.
	SourceMissing SourceMissing
	// BodyMissing 은 본문에 적은 경로가 이제 없는지다 (D05 · 점검·정리 설계 2절 ②).
	// lint 의 bodyAsk 가 붙인다. nil 이면 D05 를 건너뛴다.
	BodyMissing BodyMissing
}

// RepoReport 는 저장소 한 번 훑기의 결과다.
type RepoReport struct {
	// ByID 는 기억마다 걸린 것이다.
	ByID map[string][]Finding
	// Wide 는 어느 한 기억의 잘못이 아닌 것이다 (태그·scope 쏠림). 여기 든
	// Finding 은 ID 가 비어 있다.
	Wide []Finding
	// Near 는 중복 판정 걸음에서 본 「닮았지만 중복·C13 은 아닌」 짝이다. 짝마다 한 번,
	// (작은 id, 큰 id) 로만 든다. review 의 C19(모순 후보)가 이것을 받아 쓴다 —
	// 후보 뽑기를 다시 하지 않는다 (점검·정리 설계 2절 ⑤).
	Near []NearPair
}

// NearPair 는 닮은 두 기억 한 짝이다. C17(덮임 닮음)·C19(모순 후보)·C20(합치기)이
// 같은 짝을 나눠 쓴다.
type NearPair struct {
	// Left 가 Right 보다 작은 id 다. 짝을 한 번만 센다.
	Left, Right string
	// Align 은 가장 닮은 문장 짝의 정렬값이다 (alignInfo.Best).
	Align float64
	// Score 는 통째 닮음이다. 못 쟀으면 0 이다.
	Score float64
	// LeftText·RightText 는 정렬된 두 문장이다. C19 가 NLI 에 보내는 글이 이것이다.
	LeftText, RightText string
	// Soft 는 중복 경고 자(DupWarn) 이상으로 닮았는지다 — 합치기 (가) 의 자다.
	Soft bool
}

// nearPairMax 는 기억 하나가 댈 닮은 짝 수다 (점검·정리 설계 2절 ⑤ · C13 의 clashPairMax 와 같은 값).
const nearPairMax = 3

// Findings 는 기억 하나에 걸린 것이다.
func (r RepoReport) Findings(id string) []Finding { return r.ByID[id] }

// CheckRepo 는 저장소 전체를 봐야 아는 규칙이다 (F11·F13·B10·C05~C11).
// O(n²) 을 안 만든다 — 중복 후보는 simhash 로 좁히고, 모순 후보는 (scope, 태그)
// 로 묶어 묶음 안에서만 본다.
func CheckRepo(memories []*model.Memory, opt RepoOptions) RepoReport {
	opt.Options = opt.Options.normalized()
	report := RepoReport{ByID: map[string][]Finding{}}
	// (R1) 저장소 규칙은 한 갈래로 20k 에서 약 5초라, 그동안 중복 판정을 옆에서
	// 같이 돌린다. 둘 다 memories·opt 를 읽기만 하고, 중복 쪽은 제 결과만 채워
	// 넘긴다. 합치는 차례는 예전 그대로(규칙 → 중복)라 답이 같다.
	duplicates := make(chan duplicateResult, 1)
	go func() { duplicates <- findDuplicates(memories, opt) }()
	// 이웃 후보 셈(한 갈래 약 5초)도 처음부터 옆에서 돌리고, 붙이는 자리·차례는 그대로 둔다.
	links := make(chan map[string][]link.Candidate, 1)
	go func() { links <- suggestLinks(withoutObservations(memories)) }()
	report.repoRulesWith(memories, opt, func() map[string][]link.Candidate { return <-links })
	// 받는 쪽은 저장소 규칙이 끝난 **뒤에** 기다린다 — 그 전엔 ByID 를 안 만진다.
	report.addDuplicates(memories, opt, <-duplicates)
	return report
}

// repoRulesWith 는 중복 판정을 뺀 저장소 규칙이다. 한 갈래로 돌고 report 만 채운다.
// 이웃 후보(D04)는 links 로 받는다. 받는 때는 붙이기 직전이다.
func (r *RepoReport) repoRulesWith(memories []*model.Memory, opt RepoOptions,
	links func() map[string][]link.Candidate) {
	add := func(m *model.Memory, rule, reason string, related ...string) {
		one := opt.finding(rule, m, reason, nextFor(rule, m, related)...)
		one.Related = related
		r.ByID[m.ID] = append(r.ByID[m.ID], one)
	}

	r.Wide = append(r.Wide, tagBroad(memories, opt)...)
	r.Wide = append(r.Wide, scopeSkew(memories, opt)...)

	newerByTag := newestPerTag(memories)
	pointed := pointedIDs(memories)
	sameSpot := bySpot(memories)
	byID := memoriesByID(memories)
	for _, m := range memories {
		if word := unfixedWord(m); word != "" && hasNewer(m, newerByTag) {
			add(m, RuleUnfixedMarker, fmt.Sprintf("「%s」 라고 적힌 채로 남았는데 같은 태그에 더 새 기억이 있다", word))
		}
		if m.StaleAfter != "" && !model.IsFutureDate(m.StaleAfter, opt.Now) && model.IsDate(m.StaleAfter) {
			add(m, RuleStaleAfterPassed, fmt.Sprintf("다시 볼 날(%s)이 지났다", m.StaleAfter))
		}
		// 이미 덮이거나 무효인 기억은 자기가 물러난 쪽이라 모순 상대를 안 센다.
		if rivals := LiveDecisionRivals(m, sameSpot[spotOf(m)], opt.Now, opt.typeSpec(m)); Live(m, opt.Now) && len(rivals) > 0 {
			add(m, RuleDecisionConflictLive, fmt.Sprintf("같은 자리에 살아 있는 결정이 %d건 더 있다. 어느 쪽이 지금 맞는지 표시가 없다", len(rivals)), idsOf(rivals)...)
			if newer := newestRival(m, rivals); newer != "" && m.SupersededBy == "" {
				add(m, RuleSupersedeMissing, fmt.Sprintf("더 새 결정 `%s` 이 있는데 이 기억에 superseded_by 가 없다", newer), newer)
			}
		}
		if opt.SourceChanged != nil {
			for _, source := range m.Sources {
				if opt.SourceChanged(m, source) {
					add(m, RuleStaleSourceChanged, fmt.Sprintf("근거 `%s` 가 이 기억(%s) 뒤에 바뀌었다", source, m.Date), source)
					break
				}
			}
		}
		if reason := staleAge(m, opt.Now, opt.typeSpec(m)); reason != "" {
			add(m, RuleStaleAge, reason)
		}
		staleSources(m, opt.SourceMissing, add)
		// 점검·정리 1단 (C16 · C18 · D05 · D06). 넷 다 기억 하나씩 보는 O(n) 이다.
		citedNotSuperseded(m, byID, opt.Now, add)
		retiredUnfolded(m, opt.Now, add)
		if Live(m, opt.Now) {
			noArtifactSource(m, opt, add)
			staleBodyPaths(m, opt.BodyMissing, add)
		}
		if opt.Hits != nil {
			r.coldAndOrphan(m, pointed, opt, add)
		}
	}
	staleBasis(memories, opt, add)
	obsStale(memories, add)
	// 모음 기억은 원본 글을 옮겨 적은 것이라 「같은 주제 이웃」·「표기 갈림」 셈에서 뺀다.
	plain := withoutObservations(memories)
	reportLinkMissing(plain, links(), add)
	notationDrift(plain, opt, add)
}

// rivalSpot 은 결정 경쟁(C05)이 견주는 자리다. LiveDecisionRivals 는 종류·scope 가 다르면 안 본다.
type rivalSpot struct{ kind, scope string }

func spotOf(m *model.Memory) rivalSpot { return rivalSpot{m.Type, m.Scope} }

// bySpot 은 기억을 자리별로 원래 차례대로 묶는다. 자리 묶음을 넘겨도 경쟁 목록은 같다
// — 20k 에서 기억마다 전부를 훑던 자리가 lint 의 한 갈래 4초였다 (R1).
func bySpot(memories []*model.Memory) map[rivalSpot][]*model.Memory {
	out := map[rivalSpot][]*model.Memory{}
	for _, m := range memories {
		if m != nil {
			out[spotOf(m)] = append(out[spotOf(m)], m)
		}
	}
	return out
}

// coldAndOrphan 은 조회 기록이 있을 때만 도는 둘이다 (C09·C10).
func (r RepoReport) coldAndOrphan(m *model.Memory, pointed map[string]bool, opt RepoOptions,
	add func(*model.Memory, string, string, ...string)) {
	last, seen := opt.Hits[m.ID]
	cold := opt.Config.Quality.ColdDays
	if cold <= 0 {
		cold = 180
	}
	if !seen || opt.Now.Sub(last) > time.Duration(cold)*24*time.Hour {
		add(m, RuleCold, fmt.Sprintf("%d일 넘게 검색에 한 번도 안 잡혔다", cold))
	}
	if !seen && len(m.Links) == 0 && !pointed[m.ID] {
		add(m, RuleOrphan, "아무 기억도 안 가리키고 이 기억도 아무 데도 안 가리키며 조회도 0이다")
	}
}

// duplicates 는 저장소 안의 중복이다 (C01~C03). 후보를 한 번만 만들어 쓴다.
//
// (리뷰 B) 20k 에서 lint 시간의 절반이 이 자리였다. 기억 하나를 견주는 일은
// 서로를 안 보므로 **자리 번호로 갈라** 일꾼에게 나눠 준다. 표는 다 채운 뒤로는
// 안 바뀌고 결과는 자리 번호대로 모으므로 답은 한 갈래로 돌린 것과 같다.
func (r *RepoReport) duplicates(memories []*model.Memory, opt RepoOptions) {
	r.addDuplicates(memories, opt, findDuplicates(memories, opt))
}

// duplicateResult 는 중복 판정이 낸 것이다. found 는 C01~C03, clash 는 모순 짝(C13)
// 후보로 둘 다 memories 와 같은 차례다. near 는 닮은 짝(C17·C19·C20 몫)을 짝마다
// 한 번 모은 것이고, split 은 쪼개진 짝(C20 (나))이다.
type duplicateResult struct {
	found [][]Finding
	clash [][]string
	near  []NearPair
	split []splitPair
}

// findDuplicates 는 중복 판정을 재기만 하고 report 는 안 만진다. 그래서
// CheckRepo 가 저장소 규칙과 겹쳐 돌릴 수 있다.
func findDuplicates(memories []*model.Memory, opt RepoOptions) duplicateResult {
	docs := make([]*Doc, len(memories))
	InParallel(len(memories), func(from, to int) {
		for at := from; at < to; at++ {
			docs[at] = NewDoc(memories[at])
		}
	})
	table := newSimilarFor(opt.Options)
	for at, doc := range docs {
		// 모음 기억은 원본을 이어 붙인 글이라 원본과 닮은 것이 당연하다 (B1).
		// 중복 표에 안 넣고, 저도 중복 판정을 안 받는다.
		if memories[at].Type == model.TypeObservation {
			continue
		}
		table.Add(doc)
	}
	// (리뷰 B · V3) 중복(C01~C03)과 모순 짝(C13)을 **한 걸음에** 본다. 둘로
	// 갈라 돌면 같은 후보 뽑기를 문서마다 두 번 하게 되고, 그 한 가지가 20k
	// lint 53초의 3분의 1이었다.
	found := make([][]Finding, len(memories))
	clash := make([][]string, len(memories))
	near := make([][]NearPair, len(memories))
	byID := memoriesByID(memories)
	InParallel(len(memories), func(from, to int) {
		session := table.Session()
		for at := from; at < to; at++ {
			if memories[at].Type == model.TypeObservation {
				continue
			}
			// (점검·정리 1단) 닮은 짝(near)도 **같은 후보 뽑기**에서 받는다. 후보는
			// 세션이 바로 앞 것을 되쓰므로 Nearest 와 아래 정렬 걸음이 한 번만 뽑는다.
			matches := session.Nearest(docs[at], opt.Config.Quality.DupWarn, dupListMax)
			found[at] = duplicateFindingsOf(matches, memories[at], opt.Options)
			clash[at], near[at] = clashAndNear(docs[at], session, matches, byID, opt)
		}
	})
	return duplicateResult{found: found, clash: clash,
		near:  mergeNearPairs(near, clash, memories),
		split: splitPairs(memories, docs, opt)}
}

// addDuplicates 는 중복 판정 결과를 기억 자리 차례로 report 에 붙인다. 닮은 짝은
// Near 에 그대로 두고, 그중 덮임(C17)·합치기(C20) 몫을 가른다 — 저장소 규칙(C05·
// C08·C16)이 다 붙은 뒤라 「이미 다른 큐가 맡은 짝」을 여기서 안다.
func (r *RepoReport) addDuplicates(memories []*model.Memory, opt RepoOptions, result duplicateResult) {
	add := func(m *model.Memory, rule, reason string, related ...string) {
		one := opt.finding(rule, m, reason, nextFor(rule, m, related)...)
		one.Related = related
		r.ByID[m.ID] = append(r.ByID[m.ID], one)
	}
	for at, m := range memories {
		r.ByID[m.ID] = append(r.ByID[m.ID], result.found[at]...)
	}
	staleConflictPairs(result.clash, memories, add)
	r.Near = result.near
	r.routeNear(memories, result, opt.Config.Quality.DupWarn, add)
}

// tagBroad 는 한 태그가 저장소를 덮는지다 (규칙 F11). 어느 한 기억의 잘못이
// 아니라서 ID 를 안 붙인다 — 붙이면 그 태그를 단 기억이 다 경고가 된다.
func tagBroad(memories []*model.Memory, opt RepoOptions) []Finding {
	if len(memories) < wideStoreFloor {
		return nil
	}
	counts := map[string]int{}
	for _, m := range memories {
		for _, tag := range uniqueTags(m.Tags) {
			counts[tag]++
		}
	}
	found := []Finding{}
	for _, tag := range sortedKeys(counts) {
		share := float64(counts[tag]) / float64(len(memories))
		if share > opt.Config.Quality.TagBroadRatio {
			found = append(found, Finding{Rule: RuleTagTooBroad, Level: GradeWarn,
				Reason: fmt.Sprintf("태그 `%s` 가 저장소의 %.0f%%(%d/%d)에 붙었다. 이 태그로는 아무것도 못 좁힌다",
					tag, share*100, counts[tag], len(memories))})
		}
	}
	return found
}

// wideStoreFloor 아래에서는 쏠림을 안 잰다. 열 건짜리 저장소에서 20% 는 두 건이다.
const wideStoreFloor = 20

func scopeSkew(memories []*model.Memory, opt RepoOptions) []Finding {
	if len(memories) < wideStoreFloor {
		return nil
	}
	counts := map[string]int{}
	for _, m := range memories {
		counts[m.Scope]++
	}
	found := []Finding{}
	for _, scope := range sortedKeys(counts) {
		share := float64(counts[scope]) / float64(len(memories))
		if share > opt.Config.Quality.ScopeSkewRatio {
			found = append(found, Finding{Rule: RuleScopeSkew, Level: GradeWarn,
				Reason: fmt.Sprintf("scope `%s` 하나가 저장소의 %.0f%%(%d/%d)다. scope 로는 아무것도 못 좁힌다",
					scope, share*100, counts[scope], len(memories))})
		}
	}
	return found
}

func unfixedWord(m *model.Memory) string {
	for _, word := range unfixedWords {
		if strings.Contains(m.Body, word) || strings.Contains(m.Summary, word) {
			return word
		}
	}
	return ""
}

// newestPerTag 는 태그마다 가장 새 기억의 「날짜+id」다.
//
// **(2D) 날짜만 보면 안 된다.** 실기억 206건이 전부 하루 안에 들어와서 날짜가
// 같고, 그래서 B10 이 한 번도 안 켜졌다(STALE 재현율이 0.600 에 묶인 큰 원인).
// id 앞머리가 날짜라 같은 날 안에서는 id 차례가 곧 들어온 차례다 — C08 의
// newestRival 이 이미 쓰는 것과 같은 자다.
func newestPerTag(memories []*model.Memory) map[string]string {
	newest := map[string]string{}
	for _, m := range memories {
		for _, tag := range uniqueTags(m.Tags) {
			if key := m.Date + "|" + m.ID; key > newest[tag] {
				newest[tag] = key
			}
		}
	}
	return newest
}

func hasNewer(m *model.Memory, newest map[string]string) bool {
	mine := m.Date + "|" + m.ID
	for _, tag := range uniqueTags(m.Tags) {
		if newest[tag] > mine {
			return true
		}
	}
	return false
}

// newestRival 은 이 기억보다 새 결정 중 가장 새것이다. 날짜가 같으면 id 가 큰
// 쪽을 새것으로 본다 — id 앞머리가 날짜라 같은 날 안에서는 이것이 들어온 차례다.
func newestRival(m *model.Memory, rivals []*model.Memory) string {
	best := ""
	bestDate := m.Date
	for _, one := range rivals {
		newer := one.Date > bestDate || (one.Date == bestDate && one.ID > m.ID)
		if newer && (best == "" || one.Date > bestDate || one.ID > best) {
			best, bestDate = one.ID, one.Date
		}
	}
	return best
}

// numberToken 은 표기가 흔들리는 수다 (규칙 C11) — 5,000 · 5000 · 5k · 5천.
var numberToken = regexp.MustCompile(`(\d[\d,]*)\s*(k|천|만|억)?`)

// notationDrift 는 같은 수를 다른 표기로 쓴 기억 짝이다. 태그가 겹치는 것끼리만
// 본다 — 저장소 전체에서 숫자만 보면 아무 짝이나 걸린다.
//
// **(2D · 결정 26) 걸린 기억마다 낸다.** v0.2 는 「저장소 전체 이야기」(Wide)로
// 냈는데, 그 자리는 기억 id 가 없어 `review` 큐에도 채점표에도 안 올라간다 —
// 규칙이 있는데 아무도 못 보는 꼴이었다(재현율 0.000 의 진짜 원인). 어느 쪽이
// 정본인지는 여전히 코드가 못 가르므로 짝의 id 를 둘 다 적어 사람이 고르게 한다.
//
// **(파도 G) 짝을 다 도는 대신 값으로 먼저 묶는다.** 뜻은 그대로다 — 값이
// 다르면 어차피 안 걸리던 짝이다. 모든 짝을 보면 20k 에서 2억 번이라 lint 가
// 통째로 멈춘다 (G4 : 20k 10초).
func notationDrift(memories []*model.Memory, opt RepoOptions,
	add func(*model.Memory, string, string, ...string)) {
	type spot struct {
		at    int
		shape string
	}
	byValue := map[int64][]spot{}
	tagsOf := make([]map[string]bool, len(memories))
	for at, m := range memories {
		tagsOf[at] = tagSetOf(m)
		for shape, value := range numberForms(m) {
			if value < notationFloor {
				continue
			}
			byValue[value] = append(byValue[value], spot{at, shape})
		}
	}
	hits := map[string]map[string]bool{} // id → 흔들린 표기
	related := map[string]map[string]bool{}
	for _, value := range sortedValues(byValue) {
		spots := byValue[value]
		// 한 수를 이만큼 많은 기억이 쓰면 표기 흔들림이 아니라 흔한 수다.
		if len(spots) > notationBucketMax {
			continue
		}
		for left := 0; left < len(spots); left++ {
			for right := left + 1; right < len(spots); right++ {
				if spots[left].at == spots[right].at || spots[left].shape == spots[right].shape {
					continue
				}
				if sharedTagCount(tagsOf[spots[left].at], tagsOf[spots[right].at]) < notationTagMin {
					continue
				}
				one, other := memories[spots[left].at].ID, memories[spots[right].at].ID
				addTo(hits, one, spots[left].shape)
				addTo(hits, other, spots[right].shape)
				addTo(related, one, other)
				addTo(related, other, one)
			}
		}
	}
	byID := map[string]*model.Memory{}
	for _, m := range memories {
		byID[m.ID] = m
	}
	for _, id := range sortedSetKeys(hits) {
		add(byID[id], RuleNotationDrift,
			fmt.Sprintf("같은 수를 저장소가 %s 로 갈라 적고 있다. 검색이 한쪽만 찾는다",
				strings.Join(sortedSet(hits[id]), " · ")),
			sortedSet(related[id])...)
	}
}

// notationBucketMax 는 한 수를 쓰는 기억이 이만큼을 넘으면 건너뛰는 선이다.
const notationBucketMax = 200

// sortedValues 는 값 차례다. 보고 순서를 못 박아야 두 번 돌려도 같은 표가 나온다.
func sortedValues[T any](table map[int64][]T) []int64 {
	out := make([]int64, 0, len(table))
	for value := range table {
		out = append(out, value)
	}
	sort.Slice(out, func(a, b int) bool { return out[a] < out[b] })
	return out
}

// tagSetOf 는 기억 하나의 태그 집합이다. 짝마다 다시 만들면 20k 에서 그것만으로
// 몇 억 번이 된다.
func tagSetOf(m *model.Memory) map[string]bool {
	set := map[string]bool{}
	for _, tag := range uniqueTags(m.Tags) {
		set[tag] = true
	}
	return set
}

// notationTagMin 은 같은 수를 갈라 적었다고 말하기 전에 두 기억이 나눠 가져야
// 할 태그 수다.
//
// **(2D) 1 에서 2 로 올렸다.** 1 이면 서로 상관없는 글에 우연히 든 흔한 수까지
// 짝이 돼 대조군 20건 중 2건이 걸렸다(정밀도 0.333). 2 로 올리면 대조군 0 이고
// 골든셋 NOTATION 1건은 그대로 잡힌다 — 그 짝은 태그를 둘 나눠 갖는다.
const notationTagMin = 2

// sharedTagCount 는 두 기억이 나눠 가진 태그 수다. 작은 쪽을 돈다.
func sharedTagCount(left, right map[string]bool) int {
	small, big := left, right
	if len(big) < len(small) {
		small, big = big, small
	}
	count := 0
	for tag := range small {
		if big[tag] {
			count++
		}
	}
	return count
}

func addTo(table map[string]map[string]bool, key, value string) {
	if table[key] == nil {
		table[key] = map[string]bool{}
	}
	table[key][value] = true
}

func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sortedSetKeys(table map[string]map[string]bool) []string {
	out := make([]string, 0, len(table))
	for key := range table {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// notationFloor 아래 수는 표기가 갈릴 일이 없다.
const notationFloor = 1000

// numberForms 는 기억 하나에 나오는 수의 「적힌 꼴 → 값」이다.
func numberForms(m *model.Memory) map[string]int64 {
	forms := map[string]int64{}
	for _, match := range numberToken.FindAllStringSubmatch(m.Summary+"\n"+m.Body, -1) {
		digits := strings.ReplaceAll(match[1], ",", "")
		value, err := strconv.ParseInt(digits, 10, 64)
		if err != nil {
			continue
		}
		switch match[2] {
		case "k", "천":
			value *= 1000
		case "만":
			value *= 10000
		case "억":
			value *= 100000000
		}
		forms[strings.TrimSpace(match[0])] = value
	}
	return forms
}

func uniqueTags(tags []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, tag := range tags {
		tag = strings.ToLower(tag)
		if seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	return out
}

func pointedIDs(memories []*model.Memory) map[string]bool {
	pointed := map[string]bool{}
	for _, m := range memories {
		for _, id := range m.Links {
			pointed[id] = true
		}
		if m.SupersededBy != "" {
			pointed[m.SupersededBy] = true
		}
	}
	return pointed
}

func idsOf(memories []*model.Memory) []string {
	out := make([]string, 0, len(memories))
	for _, m := range memories {
		out = append(out, m.ID)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedListKeys(table map[string][]string) []string {
	keys := make([]string, 0, len(table))
	for key := range table {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
