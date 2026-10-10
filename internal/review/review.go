// Package review 는 사람이 판정할 것만 모아 한 화면에 보여준다 (설계 3-4).
//
// **판정은 안 한다.** 코드가 못 가리는 것은 「값이 있나」와 「어느 쪽이 지금
// 맞나」 둘뿐이고, 그 둘만 사람에게 보낸다 (설계 결정 28). 규칙 검사는
// `internal/quality` 가 하고, 여기는 그 결과를 종류별 큐(Kinds 표)로 나눠 담기만 한다.
// 예외는 하나다 — C19(모순 후보)는 판정 사다리(`internal/llm`)를 불러야 해서 quality 가
// 아니라 여기(contradict.go)서 난다. lint·훅의 「LLM 0」 경계를 지키려는 자리다.
package review

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/lint"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
	"github.com/mirusona/officina-ai-memory-tool/internal/safe"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
	"github.com/mirusona/officina-ai-memory-tool/internal/token"
)

// 큐 종류 (`mem review --kind`). 차례는 아래 Kinds 표다.
const (
	// KindExpired 는 「다시 볼 날」이 지난 것이다 (C07).
	KindExpired = "expired"
	// KindConflict 는 같은 자리에 살아 있는 결정이 둘 이상인 것이다 (C05·C08).
	KindConflict = "conflict"
	// KindStale 은 근거가 그 뒤에 바뀌었거나 「안 고침」인 채로 남은 것이다 (C06·B10).
	KindStale = "stale"
	// KindCold 는 오래 아무도 안 본 것이다 (C09·C10).
	KindCold = "cold"
	// KindTitle 은 이전된 기억 중 제목이 비어 있는 것이다. `migrate` 가 제목을
	// 지어내지 않고 사람에게 넘긴 자리라 규칙 위반이 아니라 **남은 일**이다.
	KindTitle = "title"
	// KindValue 는 「값이 있나」다 (B12 · 결정 28). 코드가 못 가리는 두 가지 중
	// 하나라 사람에게 온다.
	KindValue = "value"
	// KindLink 는 기계가 찾은 링크 후보다 (D04). 자동 링크는 파생 표에만 있고,
	// 머리말 `links` 로 올리는 것은 사람이 한다 (결정 42 고침).
	KindLink = "link"
	// KindBasis 는 근거로 삼은 기억이 죽은 것이다 (C14). `stale` 에 안 섞는다 —
	// 거기는 이미 규칙 여섯이 몰려 있어 갈래 상한 20줄에 새것이 밀려 안 보인다.
	KindBasis = "basis"
	// KindHeld 는 `add --hold` 로 들어와 승격을 기다리는 것이다 (머리말
	// `review: true`). 검색·훅에 안 뜨니 이 큐가 없으면 사람이 **승격 대상을
	// 목록으로 볼 길이 없다** (리뷰 D8).
	KindHeld = "held"
	// KindRetired 는 덮였거나 무효인데 본문이 안 접힌 것이다 (C18 · 점검·정리 설계 5절).
	// 처리는 `mem gc --retired` 다.
	KindRetired = "retired"
	// KindGone 은 실물(근거 경로·커밋·본문 경로)이 사라진 것이다 (D02·D03·D05).
	// 예전엔 stale 에 섞였다 — 거기는 규칙 여섯이 몰려 상한에 밀린다 (같은 설계 결정 7).
	KindGone = "gone"
	// KindContradict 는 서로 어긋날 수 있는 짝이다 (C13·C19).
	KindContradict = "contradict"
	// KindMerge 는 합칠 만한 짝이다 (C20) — 중복에 못 미치게 닮았거나, 한 내용이
	// 두 기억으로 쪼개진 것. 합쳐 새로 쓰고 옛 둘을 `--by` 로 덮는다.
	KindMerge = "merge"
)

// Kinds 는 화면에 찍는 차례다. 급한 것이 위다.
var Kinds = []string{KindHeld, KindRetired, KindExpired, KindGone, KindBasis, KindConflict,
	KindContradict, KindMerge, KindStale, KindValue, KindCold, KindLink, KindTitle}

// ruleKind 는 규칙 하나가 어느 큐로 가는지다. 여기 없는 규칙은 검토 큐가
// 아니라 `mem lint` 가 말한다.
var ruleKind = map[string]string{
	quality.RuleStaleAfterPassed:     KindExpired,
	quality.RuleDecisionConflictLive: KindConflict,
	quality.RuleSupersedeMissing:     KindConflict,
	quality.RuleStaleSourceChanged:   KindStale,
	quality.RuleUnfixedMarker:        KindStale,
	quality.RuleObsStale:             KindStale,
	quality.RuleCold:                 KindCold,
	quality.RuleOrphan:               KindCold,
	// v0.3 에서 는 규칙들. 표에 없으면 큐에 한 건도 안 뜬다 (2D·2B 넘김).
	quality.RuleStaleAge:      KindStale,
	quality.RuleNotationDrift: KindStale,
	quality.RuleNoValue:       KindValue,
	quality.RuleLinkMissing:   KindLink,
	// C14 — 근거가 죽었다 (무효화 전파 설계).
	quality.RuleStaleBasis: KindBasis,
	// 점검·정리 설계(2026-10-10) 5절. 실물 사라짐(D02·D03)은 stale 에서 gone 으로,
	// 숫자 어긋남(C13)은 stale 에서 contradict 로 옮겼다 (같은 설계 결정 7).
	// D02·D03 은 lint 의 SourceMissing 갈고리가 채워져야 여기까지 온다 (결정 36 ③).
	quality.RuleDeadPath:           KindGone,
	quality.RuleDeadCommit:         KindGone,
	quality.RuleDeadBodyPath:       KindGone,
	quality.RuleStaleConflictPair:  KindContradict,
	quality.RuleContradictCand:     KindContradict,
	quality.RuleCitedNotSuperseded: KindConflict,
	quality.RuleNewerSibling:       KindConflict,
	quality.RuleRetiredUnfolded:    KindRetired,
	quality.RuleMergeCandidate:     KindMerge,
	quality.RuleNoArtifactSource:   KindValue,
}

// Item 은 사람이 볼 한 줄이다.
type Item struct {
	Kind  string `json:"kind"`
	Rule  string `json:"rule"`
	ID    string `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
	Date  string `json:"date"`
	// Scope 는 걸린 기억의 scope 다. `--table` 이 이것으로 묶는다.
	Scope string `json:"scope"`
	Why   string `json:"why"`
	// Related 는 같이 봐야 할 다른 기억이다 (모순 상대·바뀐 근거).
	Related []string `json:"related,omitempty"`
	// Next 는 사람이 그다음에 칠 명령이다. **도구가 대신 치지 않는다.**
	Next []string `json:"next"`
}

// Report 는 검토 큐 한 번이다.
type Report struct {
	Checked int            `json:"checked"`
	Counts  map[string]int `json:"counts"`
	// ByScope 는 scope → 종류 → 기억 수다. Counts 를 scope 로 쪼갠 것이다.
	ByScope map[string]map[string]int `json:"by_scope"`
	Items   []Item                    `json:"items"`
	Notes   []string                  `json:"notes"`
	Elapsed time.Duration             `json:"-"`
}

// Options 는 검토 큐 한 번의 조건이다.
type Options struct {
	Store  *store.Store
	Config config.Config
	Vocab  config.Vocab
	Now    time.Time
	// Kinds 가 비면 넷 다 본다 (`--kind`).
	Kinds []string
	// Limit 은 큐 하나에 몇 줄까지 보여줄지다. 0 이면 기본값.
	Limit int
	// Hits 는 id → 마지막 조회 시각이다. nil 이면 차가움(C09·C10)을 건너뛴다.
	Hits map[string]time.Time
	// SourceChanged 는 근거가 기억 날짜 뒤에 바뀌었는지다. nil 이면 C06 을
	// 건너뛴다. git 을 아는 쪽(cmd·lint)이 붙여 준다.
	SourceChanged func(m *model.Memory, source string) bool
	// SourceMissing 은 근거가 이제 없는지다 (D02·D03 · 결정 36 ③). nil 이면
	// 근거 표류를 건너뛴다.
	SourceMissing quality.SourceMissing
	// BodyMissing 은 본문에 적은 경로가 이제 없는지다 (D05). nil 이면 건너뛴다.
	BodyMissing quality.BodyMissing
	// Scopes 가 있으면 그 scope 의 기억만 큐에 담는다 (`--scope`). 검사는 저장소
	// 전체로 돈다 — 근거(C14)·닮은 짝이 다른 scope 에 걸쳐 있을 수 있다.
	Scopes []string
	// ByScope 면 상한을 (종류, scope) 마다 건다 (`--table`). 아니면 종류마다다.
	ByScope bool
	// All 이면 상한을 없앤다 (`--all`). AI 가 한 번에 다 받을 때 쓴다.
	All bool
	// NLI 는 C19 의 NLI 단이다 (`--nli`). nil 이면 규칙 층만 돈다.
	NLI *NLIOptions
	// Near 는 STALE 모순 후보를 데려오는 임베딩이다. nil 이면 안 쓴다 (결정 15).
	Near quality.Vectors
	// Canon 은 mem.toml [canon] 대표말 표다. B08 의 norm·canon 대조에 쓴다. nil 이면 대조 없이 돈다.
	Canon *token.Canon
	// Demoted 는 `mem eval --quality` 가 정밀도를 재서 내린 등급이다 (결정 14).
	// `add`·`lint` 는 이미 이걸 보는데 `review` 만 안 봐서, 강등된 규칙이 검토
	// 큐에는 옛 등급 그대로 떴다 (리뷰 T17).
	Demoted map[string]quality.Grade
}

// Prepare 는 조회 기록과 git 을 붙인다. 안 붙이면 차가움·낡음을 못 본다.
// cmd 가 `review.Prepare(&opt)` 한 줄로 부르는 자리다.
func Prepare(options *Options) {
	if options.Hits == nil {
		options.Hits = lint.HitTimes(options.Store.Dir)
	}
	if options.SourceChanged == nil {
		options.SourceChanged = lint.SourceChanged(options.Store.Dir)
	}
	// 근거(D02·D03)와 본문 경로(D05)는 한 경로 집합으로 같이 만든다 — 프로젝트를
	// 두 번 훑지 않는다 (lint.Missing).
	if options.SourceMissing == nil && options.BodyMissing == nil {
		options.SourceMissing, options.BodyMissing = lint.Missing(options.Store.Dir)
	}
}

// DefaultLimit 은 큐 하나에 보여줄 줄 수다. 한 화면에 안 들어오면 사람이 안 본다.
const DefaultLimit = 20

// Run 은 저장소를 한 번 훑어 검토 큐를 만든다.
func Run(options Options) (*Report, error) {
	started := time.Now()
	if options.Now.IsZero() {
		options.Now = started
	}
	if options.Limit <= 0 {
		options.Limit = DefaultLimit
	}
	memories, err := loadMemories(options.Store)
	if err != nil {
		return nil, err
	}
	report := Report{Checked: len(memories), Counts: map[string]int{}, ByScope: map[string]map[string]int{},
		Items: []Item{}, Notes: []string{}}
	if options.Hits == nil {
		report.note("조회 기록이 없어 차가운 기억은 안 봤다")
	}
	if options.SourceChanged == nil {
		report.note("git 을 못 써서 근거가 바뀐 기억은 안 봤다")
	}
	repo := repoOptions(options)
	found := quality.CheckRepo(memories, repo)
	byID := idTable(memories)
	want := wanted(options.Kinds)
	if want[KindValue] {
		addValue(found.ByID, memories, repo.Options)
	}
	// 모순 후보는 merge 큐만 볼 때도 돈다 — 같은 짝의 merge 줄을 지우려면 C19 를 알아야 한다.
	if want[KindContradict] || want[KindMerge] {
		report.addContradicts(&found, byID, options)
	}
	dropShadowed(found.ByID)
	report.fill(found, byID, options)
	report.noteMissingScopes(options.Scopes, memories)
	report.Elapsed = time.Since(started)
	return &report, nil
}

func repoOptions(options Options) quality.RepoOptions {
	vocab := options.Vocab
	if len(vocab.Tags) == 0 {
		vocab = config.DefaultVocab()
	}
	return quality.RepoOptions{
		Options: quality.Options{Config: options.Config, Vocab: vocab, Now: options.Now,
			Near: options.Near, Demoted: options.Demoted, Canon: options.Canon},
		Hits: options.Hits, SourceChanged: options.SourceChanged,
		SourceMissing: options.SourceMissing, BodyMissing: options.BodyMissing,
	}
}

// addValue 는 B12(값이 있나)를 기억마다 돌려 found 에 붙인다. B12 는 기억 하나씩
// 보는 검사라 CheckRepo 에 없다 — 그래서 value 큐가 늘 비었다 (점검·정리 설계 4절).
// 모음 기억(observation)은 ValueFindings 가 스스로 뺀다.
func addValue(byID map[string][]quality.Finding, memories []*model.Memory, opt quality.Options) {
	for at, list := range quality.ValueFindings(memories, opt) {
		for _, one := range list {
			if one.ID == "" {
				one.ID = memories[at].ID
			}
			byID[memories[at].ID] = append(byID[memories[at].ID], one)
		}
	}
}

// dropShadowed 는 한 짝을 한 큐에만 보낸다. C13·C19(어긋남)에 걸린 짝의 C20(합치기)·
// C17(덮임 닮음) 줄을 지운다 — 어긋난 둘을 합치면 틀린 쪽이 섞인다 (점검·정리 설계 ⑥).
// C19 는 review 에서 나므로 이 정리도 review 가 한다.
func dropShadowed(byID map[string][]quality.Finding) {
	clash := map[[2]string]bool{}
	for id, list := range byID {
		for _, one := range list {
			if one.Rule != quality.RuleStaleConflictPair && one.Rule != quality.RuleContradictCand {
				continue
			}
			for _, other := range one.Related {
				clash[pairKey(id, other)] = true
			}
		}
	}
	if len(clash) == 0 {
		return
	}
	for id, list := range byID {
		kept := list[:0]
		for _, one := range list {
			if (one.Rule == quality.RuleMergeCandidate || one.Rule == quality.RuleNewerSibling) &&
				clashesWith(clash, id, one.Related) {
				continue
			}
			kept = append(kept, one)
		}
		byID[id] = kept
	}
}

func clashesWith(clash map[[2]string]bool, id string, related []string) bool {
	for _, other := range related {
		if clash[pairKey(id, other)] {
			return true
		}
	}
	return false
}

// pairKey 는 짝 하나의 열쇠다. 차례와 상관없이 (작은 id, 큰 id) 다.
func pairKey(left, right string) [2]string {
	if right < left {
		left, right = right, left
	}
	return [2]string{left, right}
}

func idTable(memories []*model.Memory) map[string]*model.Memory {
	byID := make(map[string]*model.Memory, len(memories))
	for _, m := range memories {
		byID[m.ID] = m
	}
	return byID
}

// fill 은 규칙 판정을 큐로 나눠 담는다. 큐마다 상한이 있고, 넘으면 몇 건이 더
// 있는지 한 줄로 말한다 — 자른 것을 말없이 숨기지 않는다.
func (r *Report) fill(found quality.RepoReport, byID map[string]*model.Memory, options Options) {
	want := wanted(options.Kinds)
	scopes := scopeSet(options.Scopes)
	buckets := map[string][]Item{}
	// 건수는 **기억 수**다. C09·C10 처럼 규칙 둘이 같은 기억에 걸리면 셈이
	// 저장소 크기를 넘어 「206건 중 337건이 차갑다」가 된다 (N4).
	counted := map[string]map[string]bool{}
	for _, id := range sortedIDs(found.ByID) {
		if !inScopes(scopes, byID[id]) {
			continue
		}
		for _, one := range found.ByID[id] {
			kind, ok := ruleKind[one.Rule]
			if !ok || !want[kind] {
				continue
			}
			if counted[kind] == nil {
				counted[kind] = map[string]bool{}
			}
			item := itemOf(kind, one, byID[id])
			if !counted[kind][id] {
				counted[kind][id] = true
				r.count(item)
			}
			buckets[kind] = append(buckets[kind], item)
		}
	}
	for _, kind := range Kinds {
		if want[kind] {
			r.keep(kind, buckets[kind], options)
		}
	}
	r.fillHeld(byID, scopes, options)
	r.fillTitles(byID, scopes, options)
}

// count 는 한 기억을 종류·scope 셈에 넣는다. 같은 기억은 한 번만 부른다.
func (r *Report) count(item Item) {
	r.Counts[item.Kind]++
	if r.ByScope[item.Scope] == nil {
		r.ByScope[item.Scope] = map[string]int{}
	}
	r.ByScope[item.Scope][item.Kind]++
}

// keep 은 큐 하나를 정렬하고 상한대로 잘라 Items 에 붙인다. 상한은 기본이 종류마다,
// `--table` 이면 (종류, scope) 마다다. `--all` 이면 안 자른다.
func (r *Report) keep(kind string, list []Item, options Options) {
	if len(list) == 0 {
		return
	}
	if !options.ByScope {
		sortItems(list)
		if !options.All && len(list) > options.Limit {
			r.note(fmt.Sprintf("%s 는 %d줄 중 %d줄만 보여준다", kindName(kind), len(list), options.Limit))
			list = list[:options.Limit]
		}
		r.Items = append(r.Items, list...)
		return
	}
	groups := map[string][]Item{}
	for _, item := range list {
		groups[item.Scope] = append(groups[item.Scope], item)
	}
	for _, scope := range sortedKeys(groups) {
		group := groups[scope]
		sortItems(group)
		if !options.All && len(group) > options.Limit {
			r.note(fmt.Sprintf("%s · %s 는 %d줄 중 %d줄만 보여준다", kindName(kind), scopeName(scope),
				len(group), options.Limit))
			group = group[:options.Limit]
		}
		r.Items = append(r.Items, group...)
	}
}

// fillTitles 는 **제목이 비어 있는 기억 전부**를 모은다. 규칙 판정이 아니라
// `migrate` 가 남긴 사람 몫이라 CheckRepo 를 안 거친다.
//
// **`migrated` 표식을 안 본다 (리뷰 D5).** 칸별로만 옮겨진 기억은 그 표식이
// 안 붙어서, `migrate` 는 「제목 안 채움 206건」이라 찍는데 큐에는 191건만
// 떴다. 나머지 15건을 사람이 영영 못 봤다. 제목이 빈 기억은 어디서 왔든
// 제목을 써야 한다.
func (r *Report) fillTitles(byID map[string]*model.Memory, scopes map[string]bool, options Options) {
	if !wanted(options.Kinds)[KindTitle] {
		return
	}
	list := []Item{}
	for _, id := range sortedKeys(byID) {
		m := byID[id]
		if m.Title != "" || !inScopes(scopes, m) {
			continue
		}
		item := Item{Kind: KindTitle, Rule: "migrated-title", ID: m.ID,
			Title: titleOf(m), Type: m.Type, Date: m.Date, Scope: scopeOf(m),
			Why:  "제목이 비어 있다. 요약을 베끼지 말고 이 기억이 무엇을 말하는지 한 줄로 쓴다",
			Next: []string{"mem show " + m.ID, "mem set " + m.ID + " --title <제목>"}}
		r.count(item)
		list = append(list, item)
	}
	r.keep(KindTitle, list, options)
}

// fillHeld 는 `add --hold` 로 들어와 승격을 기다리는 기억을 모은다 (리뷰 D8).
// 규칙 위반이 아니라 **남은 일**이라 CheckRepo 를 안 거친다.
func (r *Report) fillHeld(byID map[string]*model.Memory, scopes map[string]bool, options Options) {
	if !wanted(options.Kinds)[KindHeld] {
		return
	}
	list := []Item{}
	for _, id := range sortedKeys(byID) {
		m := byID[id]
		// `review --reject` 로 버린 보류 기억은 접혀(cold) 있다. 다시 안 보인다.
		if !m.Review || m.State == index.StateCold || !inScopes(scopes, m) {
			continue
		}
		item := Item{Kind: KindHeld, Rule: "held", ID: m.ID,
			Title: titleOf(m), Type: m.Type, Date: m.Date, Scope: scopeOf(m),
			Why:  "보류로 들어와 승격을 기다린다. 승격 전에는 검색·훅에 안 뜬다",
			Next: []string{"mem show " + m.ID, "mem review --promote " + m.ID}}
		r.count(item)
		list = append(list, item)
	}
	r.keep(KindHeld, list, options)
}

// scopeSet 은 `--scope` 거름이다. 비면 nil — 다 담는다.
func scopeSet(scopes []string) map[string]bool {
	if len(scopes) == 0 {
		return nil
	}
	set := map[string]bool{}
	for _, scope := range scopes {
		if trimmed := strings.TrimSpace(scope); trimmed != "" {
			set[trimmed] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// noteMissingScopes 는 `--scope` 에 준 이름 중 기억이 하나도 없는 것을 「못 본 것」에
// 적는다. 오타(`aimemory` ↔ `aimemorytool`)면 큐가 말없이 비어 「볼 것 없음」으로 읽힌다.
func (r *Report) noteMissingScopes(scopes []string, memories []*model.Memory) {
	asked := scopeSet(scopes)
	if asked == nil {
		return
	}
	have := map[string]bool{}
	for _, m := range memories {
		have[m.Scope] = true
	}
	for _, scope := range sortedKeys(asked) {
		if !have[scope] {
			r.note(fmt.Sprintf("scope `%s` 인 기억이 없다. 이름을 확인한다", safe.Summary(scope, titleRoom)))
		}
	}
}

// inScopes 는 기억이 거름을 지나는지다. 거름이 없으면 늘 참이다.
func inScopes(scopes map[string]bool, m *model.Memory) bool {
	if scopes == nil {
		return true
	}
	return m != nil && scopes[m.Scope]
}

// scopeOf 는 화면에 찍을 scope 다. 머리말 글이라 safe 를 지난다.
func scopeOf(m *model.Memory) string {
	if m == nil {
		return ""
	}
	return safe.Summary(m.Scope, titleRoom)
}

// scopeName 은 scope 묶음 머리에 찍는 이름이다. 빈 scope 도 이름이 있어야 표를 찾는다.
func scopeName(scope string) string {
	if scope == "" {
		return "(scope 없음)"
	}
	return scope
}

func sortedKeys[V any](table map[string]V) []string {
	out := make([]string, 0, len(table))
	for key := range table {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// itemOf 는 큐 한 줄이다. `Why`·`Related` 는 기억의 요약·근거 글을 그대로 물고
// 오므로 제목과 **같은 safe 함수**를 지난다 (불변조건 I3 · 결정 59).
func itemOf(kind string, found quality.Finding, m *model.Memory) Item {
	item := Item{Kind: kind, Rule: found.Rule, ID: found.ID, Why: safe.Summary(found.Reason, whyRoom),
		Related: safeList(found.Related), Next: nextFor(kind, found)}
	if m != nil {
		item.Title, item.Type, item.Date, item.Scope = titleOf(m), m.Type, m.Date, scopeOf(m)
	}
	return item
}

// nextFor 는 사람이 칠 명령이다. 「보여주고 끝」이 아니라 다음 한 수를 손에
// 쥐여 준다 (설계 3-1의 반려 경로와 같은 생각).
func nextFor(kind string, found quality.Finding) []string {
	id := found.ID
	switch kind {
	case KindExpired:
		return []string{"mem show " + id, "mem set " + id + " --stale-after <YYYY-MM-DD>"}
	case KindConflict:
		newer := id
		if len(found.Related) > 0 {
			newer = found.Related[len(found.Related)-1]
		}
		return []string{"mem show " + id, "mem set " + id + " --by " + newer}
	case KindStale:
		return []string{"mem show " + id, "mem set " + id + " --by-new"}
	case KindBasis:
		// 사람이 고르는 둘이 그대로 두 줄이다 — 덮음 · 그대로 둠. 문구는
		// quality 한 곳에 있다.
		return quality.BasisNext(id)
	case KindLink:
		// 기계가 찾은 링크 후보다. 머리말 `links` 로 올리는 것은 사람이 한다
		// (결정 42 고침).
		return []string{"mem show " + id, "mem set " + id + " --links " + relatedIDs(found)}
	case KindValue:
		// 「값이 있나」는 코드가 못 가린다 (결정 28). 남길 값이 없으면 접는다.
		return []string{"mem show " + id, "mem set " + id + " --body <숫자·경로·결론을 넣어 다시>"}
	case KindRetired:
		// 덮음·무효는 사람이 이미 정했다. 접기만 남았다 (점검·정리 설계 7절).
		return []string{"mem show " + id, "mem gc --retired   (미리보기 · 접으려면 --apply)"}
	case KindGone:
		return []string{"mem show " + id, "mem set " + id + " --sources <고친 근거>",
			"mem set " + id + " --by-new   (실물과 함께 지웠다면)"}
	case KindContradict:
		other := firstRelated(found)
		return []string{"mem show " + id, "mem show " + other, "mem set " + id + " --by " + other + "   (한쪽을 고치거나 덮는다)"}
	case KindMerge:
		// 합쳐 새로 쓴 뒤 옛 둘을 덮는다 (기억 쓰기 규칙 넷 ①). `mem set` 은 id 하나씩 받는다.
		other := firstRelated(found)
		return []string{"mem show " + id, "mem show " + other, "mem add …   (둘을 합쳐 새로 쓴다)",
			"mem set " + id + " --by <새 id>", "mem set " + other + " --by <새 id>"}
	}
	return []string{"mem show " + id, "mem gc --fold " + id}
}

// firstRelated 는 같이 볼 첫 기억이다. 없으면 자리표를 준다.
func firstRelated(found quality.Finding) string {
	if len(found.Related) == 0 {
		return "<id>"
	}
	return found.Related[0]
}

// relatedIDs 는 링크 후보로 나온 기억 id 들이다. 사람이 그대로 복사해 칠 수
// 있게 쉼표로 잇는다.
func relatedIDs(found quality.Finding) string {
	if len(found.Related) == 0 {
		return "<id>"
	}
	return strings.Join(found.Related, ",")
}

// sortItems 는 오래된 것부터 놓는다. 오래 묵을수록 판정이 급하다.
func sortItems(items []Item) {
	sort.SliceStable(items, func(a, b int) bool {
		if items[a].Date != items[b].Date {
			return items[a].Date < items[b].Date
		}
		return items[a].ID < items[b].ID
	})
}

func wanted(kinds []string) map[string]bool {
	want := map[string]bool{}
	if len(kinds) == 0 {
		for _, kind := range Kinds {
			want[kind] = true
		}
		return want
	}
	for _, kind := range kinds {
		want[strings.TrimSpace(kind)] = true
	}
	return want
}

// KnownKind 는 --kind 값이 Kinds 표 안인지다. cmd 가 모르는 값을 거절하는 데 쓴다.
func KnownKind(kind string) bool {
	for _, known := range Kinds {
		if known == kind {
			return true
		}
	}
	return false
}

func kindName(kind string) string {
	switch kind {
	case KindExpired:
		return "다시 볼 날이 지난 것"
	case KindConflict:
		return "어느 쪽이 맞는지 모르는 것"
	case KindStale:
		return "낡았을지 모르는 것"
	case KindTitle:
		return "제목을 써야 하는 것"
	case KindHeld:
		return "승격을 기다리는 것"
	case KindValue:
		return "남길 값이 있는지 봐야 하는 것"
	case KindLink:
		return "링크로 이을지 봐야 하는 것"
	case KindBasis:
		return "근거가 죽은 것"
	case KindRetired:
		return "덮였거나 무효인데 본문이 남은 것"
	case KindGone:
		return "실물이 사라진 것"
	case KindContradict:
		return "서로 어긋날 수 있는 것"
	case KindMerge:
		return "합칠 만한 것"
	}
	return "아무도 안 보는 것"
}

// titleOf 는 화면에 찍을 제목이다. 제목이 비면 요약 앞머리를 쓰는데, 둘 다
// 사람·AI 가 쓴 못 믿을 글이라 safe 를 지난다 — review 출력은 백틱 안에
// 찍히고 AI 가 자주 읽는다 (리뷰 A → B 넘김 · 불변조건 I3).
func titleOf(m *model.Memory) string {
	if m.Title != "" {
		return safe.Summary(m.Title, titleRoom)
	}
	return safe.Summary(m.Summary, titleRoom)
}

// titleRoom 은 제목 자리에 쓰는 룬 수다.
const titleRoom = 40

// whyRoom 은 까닭 한 줄에 쓰는 룬 수다. 제목보다 길게 두되 화면 한 줄은 안
// 넘는다 — 규칙이 낸 글에 기억 요약·근거가 통째로 박혀 온다.
const whyRoom = 120

// safeList 는 같이 볼 목록을 씻는다. 여기 오는 것은 id 만이 아니라 근거 글
// (`file:…`)이기도 해서 그대로 찍으면 블록이 닫힌다.
func safeList(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, one := range items {
		out = append(out, safe.Summary(one, titleRoom))
	}
	return out
}

// note 는 「못 본 것」 한 줄이다. 규칙 글이 섞여 들어올 수 있어 같이 씻는다.
func (r *Report) note(line string) {
	r.Notes = append(r.Notes, safe.Summary(line, whyRoom))
}

func sortedIDs(table map[string][]quality.Finding) []string {
	out := make([]string, 0, len(table))
	for id := range table {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// loadMemories 는 저장소의 기억을 다 읽는다. 못 읽는 파일은 lint 가 말할 일이라
// 여기서는 조용히 건너뛴다.
func loadMemories(opened *store.Store) ([]*model.Memory, error) {
	files, err := opened.ListMemories()
	if err != nil {
		return nil, err
	}
	out := make([]*model.Memory, 0, len(files))
	for _, file := range files {
		read, err := opened.ReadListed(file)
		if err != nil || read.Memory == nil {
			continue
		}
		out = append(out, read.Memory)
	}
	return out, nil
}

// Markdown 은 사람이 읽는 화면이다. --json 이 같은 것을 준다.
func Markdown(report *Report) string {
	out := strings.Builder{}
	out.WriteString(fmt.Sprintf("검토 큐 — 기억 %d건에서 사람이 볼 것 %d줄 (%.2f초)\n",
		report.Checked, len(report.Items), report.Elapsed.Seconds()))
	out.WriteString("판정은 사람이 한다. 도구는 아무것도 안 고친다.\n")
	if len(report.Items) == 0 {
		out.WriteString("\n볼 것이 없다.\n")
		return withNotes(out.String(), report)
	}
	last := ""
	for _, item := range report.Items {
		if item.Kind != last {
			last = item.Kind
			out.WriteString(fmt.Sprintf("\n## %s (%d건)\n", kindName(item.Kind), report.Counts[item.Kind]))
		}
		out.WriteString(fmt.Sprintf("- %s %s `%s`\n", item.Date, item.ID, item.Title))
		out.WriteString("    " + item.Why + "\n")
		if len(item.Related) > 0 {
			out.WriteString("    같이 볼 것 : " + relatedLine(item.Related) + "\n")
		}
		for _, next := range item.Next {
			out.WriteString("    → " + next + "\n")
		}
	}
	return withNotes(out.String(), report)
}

// relatedMax 는 한 줄에 찍는 기억 id 개수다. 20k 저장소에서 105개가 한 줄로
// 나와 화면을 넘겼다 — 「사람이 볼 40줄」이라고 해 놓고 못 읽는 줄을 내면 안 된다.
const relatedMax = 6

// IDLine 은 기억 id 목록 한 줄이다. 자른 것은 몇 개를 잘랐는지 말한다.
// `mem set --by` 알림도 같은 자를 쓴다 — 두 벌이면 한쪽만 고쳐진다.
func IDLine(ids []string) string {
	if len(ids) <= relatedMax {
		return strings.Join(ids, " ")
	}
	return strings.Join(ids[:relatedMax], " ") + i18n.T(i18n.ListMore, len(ids)-relatedMax)
}

func relatedLine(ids []string) string { return IDLine(ids) }

func withNotes(text string, report *Report) string {
	if len(report.Notes) == 0 {
		return text
	}
	return text + "\n못 본 것 :\n- " + strings.Join(report.Notes, "\n- ") + "\n"
}

// Table 은 `--table` 화면이다. 큐마다 `## 종류 (n건)` 아래 scope 별 `### scope (n건)` 과
// 표 한 벌을 찍는다. n 은 자르기 전 기억 수다 — 잘린 줄 수는 「못 본 것」이 말한다.
func Table(report *Report) string {
	out := strings.Builder{}
	out.WriteString(fmt.Sprintf("검토 큐 — 기억 %d건에서 사람이 볼 것 %d줄 (%.2f초)\n",
		report.Checked, len(report.Items), report.Elapsed.Seconds()))
	out.WriteString("판정은 사람이 한다. 도구는 아무것도 안 고친다.\n")
	if len(report.Items) == 0 {
		out.WriteString("\n볼 것이 없다.\n")
		return withNotes(out.String(), report)
	}
	lastKind, lastScope := "", ""
	for at, item := range report.Items {
		newKind := at == 0 || item.Kind != lastKind
		if newKind {
			lastKind = item.Kind
			out.WriteString(fmt.Sprintf("\n## %s (%d건)\n", kindName(item.Kind), report.Counts[item.Kind]))
		}
		if newKind || item.Scope != lastScope {
			lastScope = item.Scope
			out.WriteString(fmt.Sprintf("\n### %s (%d건)\n\n", scopeName(item.Scope), report.ByScope[item.Scope][item.Kind]))
			out.WriteString("| 날짜 | id | 제목 | 까닭 | 같이 볼 것 | 다음 |\n")
			out.WriteString("| --- | --- | --- | --- | --- | --- |\n")
		}
		out.WriteString("| " + strings.Join([]string{cell(item.Date), cell(item.ID), cell(item.Title),
			cell(item.Why), cell(IDLine(item.Related)), cell(strings.Join(item.Next, " ; "))}, " | ") + " |\n")
	}
	return withNotes(out.String(), report)
}

// cell 은 표 칸 하나다. 글은 이미 safe 를 지났고, 칸을 깨는 `|` 만 막는다.
func cell(text string) string {
	return strings.ReplaceAll(text, "|", `\|`)
}

// IDs 는 `--ids` 화면이다. 한 줄에 `종류<TAB>규칙<TAB>id<TAB>같이 볼 것(쉼표)` 이고
// 머리말·꾸밈·「못 본 것」은 없다 — AI 세션이 줄 단위로 읽는다.
func IDs(report *Report) string {
	out := strings.Builder{}
	for _, item := range report.Items {
		related := make([]string, 0, len(item.Related))
		for _, one := range item.Related {
			related = append(related, plainField(one))
		}
		out.WriteString(strings.Join([]string{plainField(item.Kind), plainField(item.Rule),
			plainField(item.ID), strings.Join(related, ",")}, "\t") + "\n")
	}
	return out.String()
}

// plainField 는 `--ids` 한 칸이다. 칸 가름(탭)·줄 가름·쉼표가 끼면 줄 꼴이 깨지니 공백으로 바꾼다.
func plainField(text string) string {
	return strings.Map(func(letter rune) rune {
		switch letter {
		case '\t', '\n', '\r', ',':
			return ' '
		}
		return letter
	}, text)
}
