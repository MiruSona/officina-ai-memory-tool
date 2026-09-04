// Package review 는 사람이 판정할 것만 모아 한 화면에 보여준다 (설계 3-4).
//
// **판정은 안 한다.** 코드가 못 가리는 것은 「값이 있나」와 「어느 쪽이 지금
// 맞나」 둘뿐이고, 그 둘만 사람에게 보낸다 (설계 결정 28). 규칙 검사는
// `internal/quality` 가 하고, 여기는 그 결과를 네 가지 큐로 나눠 담기만 한다.
package review

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/lint"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
	"github.com/mirusona/officina-ai-memory-tool/internal/safe"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// 큐 네 가지 (`mem review --kind`).
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
)

// Kinds 는 화면에 찍는 차례다. 급한 것이 위다.
var Kinds = []string{KindHeld, KindExpired, KindBasis, KindConflict, KindStale, KindValue, KindCold, KindLink, KindTitle}

// ruleKind 는 규칙 하나가 어느 큐로 가는지다. 여기 없는 규칙은 검토 큐가
// 아니라 `mem lint` 가 말한다.
var ruleKind = map[string]string{
	quality.RuleStaleAfterPassed:     KindExpired,
	quality.RuleDecisionConflictLive: KindConflict,
	quality.RuleSupersedeMissing:     KindConflict,
	quality.RuleStaleSourceChanged:   KindStale,
	quality.RuleUnfixedMarker:        KindStale,
	quality.RuleCold:                 KindCold,
	quality.RuleOrphan:               KindCold,
	// v0.3 에서 는 규칙들. 표에 없으면 큐에 한 건도 안 뜬다 (2D·2B 넘김).
	quality.RuleStaleAge:          KindStale,
	quality.RuleStaleConflictPair: KindStale,
	quality.RuleNotationDrift:     KindStale,
	quality.RuleNoValue:           KindValue,
	// 근거 표류(D02·D03)도 「낡았을지 모르는 것」이다. lint 의 SourceMissing
	// 갈고리가 채워져야 여기까지 온다 (결정 36 ③).
	quality.RuleDeadPath:    KindStale,
	quality.RuleDeadCommit:  KindStale,
	quality.RuleLinkMissing: KindLink,
	// C14 — 근거가 죽었다 (무효화 전파 설계).
	quality.RuleStaleBasis: KindBasis,
}

// Item 은 사람이 볼 한 줄이다.
type Item struct {
	Kind  string `json:"kind"`
	Rule  string `json:"rule"`
	ID    string `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
	Date  string `json:"date"`
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
	Items   []Item         `json:"items"`
	Notes   []string       `json:"notes"`
	Elapsed time.Duration  `json:"-"`
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
	// Near 는 STALE 모순 후보를 데려오는 임베딩이다. nil 이면 안 쓴다 (결정 15).
	Near quality.Vectors
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
	if options.SourceMissing == nil {
		options.SourceMissing = lint.SourceMissing(options.Store.Dir)
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
	report := Report{Checked: len(memories), Counts: map[string]int{}, Items: []Item{}, Notes: []string{}}
	if options.Hits == nil {
		report.note("조회 기록이 없어 차가운 기억은 안 봤다")
	}
	if options.SourceChanged == nil {
		report.note("git 을 못 써서 근거가 바뀐 기억은 안 봤다")
	}
	found := quality.CheckRepo(memories, repoOptions(options))
	report.fill(found, memories, options)
	report.fillHeld(memories, options)
	report.fillTitles(memories, options)
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
			Near: options.Near, Demoted: options.Demoted},
		Hits: options.Hits, SourceChanged: options.SourceChanged,
		SourceMissing: options.SourceMissing,
	}
}

// fill 은 규칙 판정을 큐로 나눠 담는다. 큐마다 상한이 있고, 넘으면 몇 건이 더
// 있는지 한 줄로 말한다 — 자른 것을 말없이 숨기지 않는다.
func (r *Report) fill(found quality.RepoReport, memories []*model.Memory, options Options) {
	byID := map[string]*model.Memory{}
	for _, m := range memories {
		byID[m.ID] = m
	}
	want := wanted(options.Kinds)
	buckets := map[string][]Item{}
	// 건수는 **기억 수**다. C09·C10 처럼 규칙 둘이 같은 기억에 걸리면 셈이
	// 저장소 크기를 넘어 「206건 중 337건이 차갑다」가 된다 (N4).
	counted := map[string]map[string]bool{}
	for _, id := range sortedIDs(found.ByID) {
		for _, one := range found.ByID[id] {
			kind, ok := ruleKind[one.Rule]
			if !ok || !want[kind] {
				continue
			}
			if counted[kind] == nil {
				counted[kind] = map[string]bool{}
			}
			if !counted[kind][id] {
				counted[kind][id] = true
				r.Counts[kind]++
			}
			buckets[kind] = append(buckets[kind], itemOf(kind, one, byID[id]))
		}
	}
	for _, kind := range Kinds {
		if !want[kind] {
			continue
		}
		list := buckets[kind]
		sortItems(list)
		if len(list) > options.Limit {
			r.note(fmt.Sprintf("%s 는 %d줄 중 %d줄만 보여준다", kindName(kind), len(list), options.Limit))
			list = list[:options.Limit]
		}
		r.Items = append(r.Items, list...)
	}
}

// fillTitles 는 **제목이 비어 있는 기억 전부**를 모은다. 규칙 판정이 아니라
// `migrate` 가 남긴 사람 몫이라 CheckRepo 를 안 거친다.
//
// **`migrated` 표식을 안 본다 (리뷰 D5).** 칸별로만 옮겨진 기억은 그 표식이
// 안 붙어서, `migrate` 는 「제목 안 채움 206건」이라 찍는데 큐에는 191건만
// 떴다. 나머지 15건을 사람이 영영 못 봤다. 제목이 빈 기억은 어디서 왔든
// 제목을 써야 한다.
func (r *Report) fillTitles(memories []*model.Memory, options Options) {
	if !wanted(options.Kinds)[KindTitle] {
		return
	}
	list := []Item{}
	for _, m := range memories {
		if m.Title != "" {
			continue
		}
		r.Counts[KindTitle]++
		list = append(list, Item{Kind: KindTitle, Rule: "migrated-title", ID: m.ID,
			Title: titleOf(m), Type: m.Type, Date: m.Date,
			Why:  "제목이 비어 있다. 요약을 베끼지 말고 이 기억이 무엇을 말하는지 한 줄로 쓴다",
			Next: []string{"mem show " + m.ID, "mem set " + m.ID + " --title <제목>"}})
	}
	sortItems(list)
	if len(list) > options.Limit {
		r.note(fmt.Sprintf("%s 는 %d줄 중 %d줄만 보여준다", kindName(KindTitle), len(list), options.Limit))
		list = list[:options.Limit]
	}
	r.Items = append(r.Items, list...)
}

// fillHeld 는 `add --hold` 로 들어와 승격을 기다리는 기억을 모은다 (리뷰 D8).
// 규칙 위반이 아니라 **남은 일**이라 CheckRepo 를 안 거친다.
func (r *Report) fillHeld(memories []*model.Memory, options Options) {
	if !wanted(options.Kinds)[KindHeld] {
		return
	}
	list := []Item{}
	for _, m := range memories {
		if !m.Review {
			continue
		}
		r.Counts[KindHeld]++
		list = append(list, Item{Kind: KindHeld, Rule: "held", ID: m.ID,
			Title: titleOf(m), Type: m.Type, Date: m.Date,
			Why:  "보류로 들어와 승격을 기다린다. 승격 전에는 검색·훅에 안 뜬다",
			Next: []string{"mem show " + m.ID, "mem review --promote " + m.ID}})
	}
	sortItems(list)
	if len(list) > options.Limit {
		r.note(fmt.Sprintf("%s 는 %d줄 중 %d줄만 보여준다", kindName(KindHeld), len(list), options.Limit))
		list = list[:options.Limit]
	}
	r.Items = append(r.Items, list...)
}

// itemOf 는 큐 한 줄이다. `Why`·`Related` 는 기억의 요약·근거 글을 그대로 물고
// 오므로 제목과 **같은 safe 함수**를 지난다 (불변조건 I3 · 결정 59).
func itemOf(kind string, found quality.Finding, m *model.Memory) Item {
	item := Item{Kind: kind, Rule: found.Rule, ID: found.ID, Why: safe.Summary(found.Reason, whyRoom),
		Related: safeList(found.Related), Next: nextFor(kind, found)}
	if m != nil {
		item.Title, item.Type, item.Date = titleOf(m), m.Type, m.Date
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
	}
	return []string{"mem show " + id, "mem gc --fold " + id}
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

// KnownKind 는 --kind 값이 넷 안인지다. cmd 가 모르는 값을 거절하는 데 쓴다.
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
