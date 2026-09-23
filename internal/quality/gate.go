package quality

import (
	"fmt"
	"sort"
	"strings"

	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/safe"
	"github.com/mirusona/officina-ai-memory-tool/internal/secret"
)

// Finding 은 걸린 것 하나다. 사람에게 보이는 문장은 Reason 이고, 기계가 세는
// 것은 Rule 이다.
type Finding struct {
	Rule  string `json:"rule"`
	Level Grade  `json:"level"`
	// ID 는 걸린 기억이다. add 관문에서는 넣으려던 기억이다.
	ID     string `json:"id,omitempty"`
	Reason string `json:"reason"`
	// Related 는 이 판정의 근거가 된 다른 기억이다 (중복·모순).
	Related []string `json:"related,omitempty"`
	// Score 는 닮음 점수 S 다. 중복 규칙에만 있다.
	Score float64 `json:"score,omitempty"`
	// Line 은 비밀정보가 걸린 줄 번호다. 값은 절대 안 담는다.
	Line int `json:"line,omitempty"`
	// Next 는 다음에 칠 명령이다. 거절할 때는 반드시 하나 이상 있어야 한다
	// (설계 3-1 — 거절하면 다음 명령까지 손에 쥐여 준다).
	Next []string `json:"next,omitempty"`
}

// Kind 는 관문 하나의 판정이다. 종료 코드가 여기서 나온다.
type Kind int

const (
	KindPass Kind = iota
	KindWarn
	KindQualityReject  // 종료 코드 2
	KindSecurityReject // 종료 코드 4
)

// ExitCode 는 add 가 끝낼 종료 코드다.
func (k Kind) ExitCode() int {
	switch k {
	case KindQualityReject:
		return 2
	case KindSecurityReject:
		return 4
	}
	return 0
}

// Fix 는 관문이 조용히 고친 것이다. 되돌릴 수 있는 것만 여기 온다.
type Fix struct {
	What string `json:"what"`
	From string `json:"from"`
	To   string `json:"to"`
}

// Verdict 는 관문 한 번의 결과다.
type Verdict struct {
	Kind     Kind      `json:"kind"`
	Findings []Finding `json:"findings"`
	Fixes    []Fix     `json:"fixes"`
	// Memory 는 별칭 치환까지 반영한 기억이다. 통과했으면 이것을 저장한다.
	Memory *model.Memory `json:"-"`
}

// Rejected 는 저장을 막았는지다.
func (v Verdict) Rejected() bool {
	return v.Kind == KindQualityReject || v.Kind == KindSecurityReject
}

// ExitCode 는 add 의 종료 코드다.
func (v Verdict) ExitCode() int { return v.Kind.ExitCode() }

// RejectCount 는 저장을 막은 findings 수다. 경고까지 세면 「거절됨 (n가지)」가
// 고쳐야 할 가짓수를 부풀려 말한다 (리뷰 2026-09-21).
func (v Verdict) RejectCount() int {
	count := 0
	for _, one := range v.Findings {
		if one.Level == GradeReject {
			count++
		}
	}
	return count
}

// Rules 는 걸린 규칙 이름이다. 차례는 Catalog 순이다.
func (v Verdict) Rules() []string {
	seen := map[string]bool{}
	for _, one := range v.Findings {
		seen[one.Rule] = true
	}
	names := []string{}
	for _, rule := range Catalog {
		if seen[rule.Name] {
			names = append(names, rule.Name)
		}
	}
	return names
}

// Reasons 는 거절·경고 문장을 사람이 읽을 차례로 낸다.
func (v Verdict) Reasons() []string {
	out := []string{}
	for _, one := range v.Findings {
		head := "경고"
		if one.Level == GradeReject {
			head = "거절"
		}
		line := fmt.Sprintf("%s : %s (%s", head, one.Reason, one.Rule)
		if one.Score > 0 {
			line += fmt.Sprintf(", S=%.2f", one.Score)
		}
		out = append(out, line+")")
	}
	return out
}

// Repo 는 관문이 견줄 이미 있는 기억을 주는 쪽이다. cmd·lint 가 붙인다.
// nil 이면 중복·결정 관문을 건너뛴다 — 저장소를 못 읽는다고 add 가 죽으면 안 된다.
type Repo interface {
	// Recent 는 견줄 기억을 최근 것부터 돌려준다.
	Recent(limit int) ([]*model.Memory, error)
}

// MemorySlice 는 이미 손에 든 기억 목록을 Repo 로 쓰는 것이다. 시험과 골든셋
// 채점이 쓴다.
type MemorySlice []*model.Memory

// Recent 는 앞에서부터 limit 건을 돌려준다.
func (s MemorySlice) Recent(limit int) ([]*model.Memory, error) {
	if limit <= 0 || limit > len(s) {
		limit = len(s)
	}
	return s[:limit], nil
}

// DefaultRecentLimit 은 견줄 기억 수다. v0.1 `add --check` 와 같은 값이고,
// 후보 좁히기가 simhash 밴딩이라 이 안에서 O(n²) 이 안 생긴다.
const DefaultRecentLimit = 500

// Options 는 관문 한 번의 조건이다.
type Options struct {
	Config config.Config
	Vocab  config.Vocab
	Now    time.Time
	// Repo 가 nil 이면 중복(C01~C03)·결정 관문(C04)을 건너뛴다.
	Repo        Repo
	RecentLimit int
	// Secret·SecretWarn 이 nil 이면 Config 로 만든다. 여러 번 부를 때 미리
	// 만들어 두면 패턴을 다시 컴파일하지 않는다.
	Secret     *secret.Scanner
	SecretWarn *secret.Scanner
	// Demoted 는 eval --quality 가 정밀도를 재서 내린 등급이다 (설계 결정 14).
	Demoted map[string]Grade
	// AllowDuplicate 는 `--new` 다. 정말 다른 주제라고 사람이 말한 것이다.
	AllowDuplicate bool
	// SupersedeOf 는 `--by <id>` 다. 옛 결정을 덮는다고 말한 것이다.
	SupersedeOf string
	// Spec 은 규격 판을 못 박는다. 0 이면 기억이 읽힌 판을 따른다. 이전 전
	// 저장소를 새 자로 재는 eval --quality 만 여기에 SpecV2 를 넣는다.
	Spec int
	// Near 는 중복 후보의 셋째 신호(임베딩)다. nil 이면 안 쓴다 (vectors.go).
	Near Vectors
}

// types 는 이 저장소의 기억 종류 표다. vocab.toml 이 늘릴 수 있다.
func (o Options) types() model.TypeTable { return o.Vocab.TypeTable() }

// typeSpec 은 이 기억 종류의 취급 한 줄이다. 표에 없는 종류면 빈 줄이라
// 아무 검사도 안 걸린다 — 「표 밖 종류」는 값 검사(F03)가 따로 잡는다.
func (o Options) typeSpec(m *model.Memory) model.TypeSpec { return o.types().Spec(m.Type) }

func (o Options) spec(m *model.Memory) int {
	if o.Spec != 0 {
		return o.Spec
	}
	if m.Spec == model.SpecUnknown {
		return model.SpecV2
	}
	return m.Spec
}

// grade 는 이 기억에 대한 규칙의 등급이다. 강등표와 이전분 유예를 여기서 다 본다.
func (o Options) grade(rule string, m *model.Memory) Grade {
	meta, ok := Lookup(rule)
	if !ok {
		return GradeWarn
	}
	level := meta.GradeFor(o.typeSpec(m))
	if down, ok := o.Demoted[rule]; ok && !meta.Security() {
		level = down
	}
	// 이전분은 첫 판에서 F06·F08·C04 를 경고로 본다 (설계 2-5).
	if m.Migrated && level == GradeReject && migrationGrace[rule] {
		level = GradeWarn
	}
	// 낱말 표준을 아직 안 정한 저장소는 F09·F12 를 경고로만 본다. 갓 깐
	// 프로젝트가 첫 add 부터 다 막히면 도구를 아무도 안 쓴다 (실데이터 시험 C5).
	//
	// **태그와 scope 를 따로 본다 (리뷰 D7).** 하나로 묶어 뒀더니 `tags --add-scope`
	// 하나가 태그 규칙까지 같이 세워 좋은 기억을 6/10 거절했다.
	if level == GradeReject && stillLearning(rule, o.Vocab) {
		level = GradeWarn
	}
	return level
}

// stillLearning 은 이 규칙이 아직 「배우는 중」이라 경고로 내려야 하는지다.
func stillLearning(rule string, vocab config.Vocab) bool {
	switch rule {
	case RuleTagStandard:
		return vocab.TagLearning()
	case RuleScopeStandard:
		return vocab.ScopeLearning()
	}
	return false
}

// migrationGrace 는 mem migrate 가 옮긴 기억에 유예를 주는 규칙이다.
var migrationGrace = map[string]bool{RuleTitleShape: true, RuleTagCount: true, RuleDecisionGate: true}

func (o Options) finding(rule string, m *model.Memory, reason string, next ...string) Finding {
	return Finding{Rule: rule, Level: o.grade(rule, m), ID: m.ID, Reason: reason, Next: next}
}

// normalized 는 빠진 문턱을 기본값으로 채운 조건이다. 부르는 쪽이 config 를 안
// 채워 넣어도 관문이 0 을 문턱으로 쓰면 안 된다.
func (o Options) normalized() Options {
	quality := &o.Config.Quality
	if quality.DupReject == 0 {
		quality.DupReject = config.DefaultDupReject
	}
	if quality.DupWarn == 0 {
		quality.DupWarn = config.DefaultDupWarn
	}
	if quality.SimhashHamming == 0 {
		quality.SimhashHamming = config.DefaultSimhashHamming
	}
	if quality.BodyMinLines == 0 {
		quality.BodyMinLines = config.DefaultBodyMinLines
	}
	if quality.BodyWarn == 0 {
		quality.BodyWarn = config.DefaultBodyWarn
	}
	if quality.BodyMax == 0 {
		quality.BodyMax = config.DefaultBodyMax
	}
	if quality.TagMin == 0 {
		quality.TagMin = config.DefaultTagMin
	}
	if quality.TagMax == 0 {
		quality.TagMax = config.DefaultTagMax
	}
	if len(o.Vocab.Tags) == 0 {
		o.Vocab = config.DefaultVocab()
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	if o.RecentLimit == 0 {
		o.RecentLimit = DefaultRecentLimit
	}
	if o.Secret == nil {
		o.Secret = secret.New(secretPatterns(o.Config))
	}
	if o.SecretWarn == nil {
		o.SecretWarn = secret.New(warnPatterns(o.Config))
	}
	return o
}

func secretPatterns(settings config.Config) []string {
	if len(settings.Secret.Patterns) > 0 {
		return settings.Secret.Patterns
	}
	return config.DefaultSecretPatterns()
}

func warnPatterns(settings config.Config) []string {
	if len(settings.Secret.WarnPatterns) > 0 {
		return settings.Secret.WarnPatterns
	}
	return config.DefaultWarnPatterns()
}

// Gate 는 add 관문 여섯 단계다 (설계 3-1).
//
//	① 규격  F·B·X 계열   → 거절이면 종료 코드 2
//	② 보안  E 계열       → 거절이면 종료 코드 4. 여기서 바로 멈춘다
//	③ MULTI M 계열       → decision 이면 거절
//	④ 중복  C01~C03      → S ≥ dup_reject 거절 / dup_warn 부터 경고
//	⑤ 결정 관문 C04      → --by 나 --new 없으면 거절
//	⑥ 는 inbox 큐에 넣는 자리라 여기 없다 — 그것은 cmd 몫이다.
//
// 보안 거절은 그 자리에서 멈춘다. 비밀정보가 든 기억을 두고 「닮은 기억이
// 있다」 목록까지 찍을 이유가 없고, 저장소를 읽는 값도 아깝다.
func Gate(m *model.Memory, opt Options) Verdict {
	opt = opt.normalized()
	fixed, fixes := applyAliases(m, opt.Vocab)
	verdict := Verdict{Memory: fixed, Fixes: fixes}

	verdict.Findings = append(verdict.Findings, checkField(fixed, opt)...)
	verdict.Findings = append(verdict.Findings, checkBody(fixed, opt)...)
	verdict.Findings = append(verdict.Findings, checkValue(fixed, opt)...)

	security := checkSecurity(fixed, opt)
	verdict.Findings = append(verdict.Findings, security...)
	if worst(security) == GradeReject {
		verdict.Kind = KindSecurityReject
		return verdict
	}

	verdict.Findings = append(verdict.Findings, checkMulti(fixed, opt)...)
	verdict.Findings = append(verdict.Findings, checkDuplicate(fixed, opt)...)
	verdict.Kind = kindOf(verdict.Findings)
	return verdict
}

// Check 는 관문 검사를 다 돌리고 걸린 것을 전부 준다 — 보안에서 안 멈춘다.
// eval --quality 와 lint 가 규칙마다 몇 건을 잡는지 세는 데 쓴다.
func Check(m *model.Memory, opt Options) []Finding {
	opt = opt.normalized()
	fixed, _ := applyAliases(m, opt.Vocab)
	found := checkField(fixed, opt)
	found = append(found, checkBody(fixed, opt)...)
	found = append(found, checkValue(fixed, opt)...)
	found = append(found, checkSecurity(fixed, opt)...)
	found = append(found, checkMulti(fixed, opt)...)
	return append(found, checkDuplicate(fixed, opt)...)
}

func kindOf(found []Finding) Kind {
	kind := KindPass
	for _, one := range found {
		meta, _ := Lookup(one.Rule)
		switch {
		case one.Level == GradeReject && meta.Security():
			return KindSecurityReject
		case one.Level == GradeReject:
			kind = KindQualityReject
		case one.Level == GradeWarn && kind == KindPass:
			kind = KindWarn
		}
	}
	return kind
}

func worst(found []Finding) Grade {
	for _, one := range found {
		if one.Level == GradeReject {
			return GradeReject
		}
	}
	return GradeWarn
}

// applyAliases 는 태그·scope 별칭을 표준 이름으로 바꾼다 (규칙 F09·F12).
// 원래 기억은 안 건드리고 사본을 고친다 — 부르는 쪽이 거절당한 기억을 그대로
// 다시 보여줄 수 있어야 한다.
func applyAliases(m *model.Memory, vocab config.Vocab) (*model.Memory, []Fix) {
	copied := *m
	fixes := []Fix{}
	if len(m.Tags) > 0 {
		tags := make([]string, 0, len(m.Tags))
		seen := map[string]bool{}
		for _, tag := range m.Tags {
			name, _, changed := vocab.NormalizeTag(tag)
			if changed {
				fixes = append(fixes, Fix{What: "태그", From: tag, To: name})
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			tags = append(tags, name)
		}
		copied.Tags = tags
	}
	if m.Scope != "" {
		name, _, changed := vocab.NormalizeScope(m.Scope)
		if changed {
			fixes = append(fixes, Fix{What: "scope", From: m.Scope, To: name})
		}
		copied.Scope = name
	}
	return &copied, fixes
}

// checkDuplicate 는 중복(C01~C03)과 결정 관문(C04)이다.
func checkDuplicate(m *model.Memory, opt Options) []Finding {
	if opt.Repo == nil {
		return nil
	}
	existing, err := opt.Repo.Recent(opt.RecentLimit)
	if err != nil || len(existing) == 0 {
		return nil
	}
	table := newSimilarFor(opt)
	live := []*model.Memory{}
	for _, other := range existing {
		if other == nil || other.ID == m.ID {
			continue
		}
		table.Add(NewDoc(other))
		live = append(live, other)
	}
	found := allowNew(DuplicateFindings(NewDoc(m), table, m, opt), opt)
	return append(found, decisionGate(m, live, opt)...)
}

// allowNew 는 `--new` 를 준 add 의 닮음 거절을 경고로 내린다. 거절 문구가
// 「정말 다른 주제라면 --new」 라고 손에 쥐여 주는데 그 옵션이 안 들으면
// 사람이 빠져나갈 길이 없다 (설계 3-1). **본문이 글자까지 같은 것(C03)은
// 그대로 거절이다** — 그건 다른 주제일 수가 없다.
func allowNew(found []Finding, opt Options) []Finding {
	if !opt.AllowDuplicate {
		return found
	}
	for at, one := range found {
		if one.Rule == RuleDuplicateHard || one.Rule == RuleDuplicateSoft {
			found[at].Level = GradeWarn
		}
	}
	return found
}

// Finder 는 닮은 것을 찾아 주는 쪽이다 — 표 자체(*Similar)거나 일꾼마다 든
// 자리(*Session)거나.
type Finder interface {
	Nearest(doc *Doc, floor float64, limit int) []Match
}

// DuplicateFindings 는 이미 만들어 둔 닮음 표에 대고 중복 규칙 셋을 본다.
// 저장소 전체를 훑는 lint 는 표를 한 번만 만들고 이 함수를 건별로 부른다.
func DuplicateFindings(doc *Doc, table Finder, m *model.Memory, opt Options) []Finding {
	matches := table.Nearest(doc, opt.Config.Quality.DupWarn, dupListMax)
	if len(matches) == 0 {
		return nil
	}
	found := []Finding{}
	top := matches[0]
	ids := make([]string, 0, len(matches))
	for _, one := range matches {
		ids = append(ids, one.Doc.ID)
	}
	same := []string{}
	for _, one := range matches {
		if one.Same {
			same = append(same, one.Doc.ID)
		}
	}
	if len(same) > 0 {
		found = append(found, Finding{Rule: RuleSameBody, Level: opt.grade(RuleSameBody, m), ID: m.ID,
			Reason: fmt.Sprintf("본문이 `%s` 와 글자까지 같다", same[0]), Related: same,
			Next: nextForDuplicate(same[0])})
	}
	switch {
	case top.Score < opt.Config.Quality.DupWarn && top.Partial:
		// 부분 중복 — 통째 점수는 낮은데 **같은 문장이 통째로 박혀 있고** 숫자·
		// 경로·이름이 어긋나지 않는다 (설계 결정 30 의 2단). 새 사실이 같이 든
		// 경우가 많아 막지 않고 경고만 한다.
		found = append(found, Finding{Rule: RuleDuplicateSoft, Level: opt.grade(RuleDuplicateSoft, m), ID: m.ID,
			Reason:  fmt.Sprintf("`%s` 와 같은 문장이 들어 있다 (정렬 %.2f). 겹치는 부분을 지우거나 그 기억을 고친다", top.Doc.ID, top.Align),
			Related: ids, Score: top.Score})
	case top.Score >= opt.Config.Quality.DupReject && top.Hard:
		found = append(found, Finding{Rule: RuleDuplicateHard, Level: opt.grade(RuleDuplicateHard, m), ID: m.ID,
			Reason:  fmt.Sprintf("닮은 기억이 있다 — `%s`", top.Doc.ID),
			Related: ids, Score: top.Score, Next: nextForDuplicate(top.Doc.ID)})
	case top.Score >= opt.Config.Quality.DupWarn:
		found = append(found, Finding{Rule: RuleDuplicateSoft, Level: opt.grade(RuleDuplicateSoft, m), ID: m.ID,
			Reason:  fmt.Sprintf("닮은 기억이 있다 — `%s`. 정말 새 사실인지 보고 넣는다", top.Doc.ID),
			Related: ids, Score: top.Score})
	}
	return found
}

const dupListMax = 3

// nextForDuplicate 와 decisionGate 는 「다른 주제다」 줄에 같은 상수
// (newTopicStep)를 쓴다. 부르는 쪽이 같은 글만 겹쳐 지우기 때문에, 한 글자만
// 달라도 「정말 다른 주제다」 가 두 줄로 찍힌다 (실데이터 시험 A6).
func nextForDuplicate(id string) []string {
	return []string{
		fmt.Sprintf("mem set %s --by-new   (그 기억을 이 내용으로 덮는다)", id),
		newTopicStep,
	}
}

// newTopicStep 은 「닮았지만 다른 주제다」 라고 말하는 길이다.
const newTopicStep = "mem add … --new                       (정말 다른 주제다)"

// NewTopicStep 은 화면이 다음 수를 모을 때 이 줄을 맨 앞으로 올리려고 연다.
const NewTopicStep = newTopicStep

// decisionGate 는 C04 다. 같은 scope 에 태그가 겹치는 살아 있는 decision 이
// 있으면 `--by` 나 `--new` 없이는 못 들어온다.
//
// superseded_by 0건 · 모순 10건 · 낡음 25건이 전부 이 한 구멍에서 나왔다.
// 사후 lint 로는 늦다 — 들어온 뒤엔 어느 쪽이 새것인지 코드가 못 가른다.
func decisionGate(m *model.Memory, existing []*model.Memory, opt Options) []Finding {
	if !opt.typeSpec(m).Gate || opt.AllowDuplicate || opt.SupersedeOf != "" {
		return nil
	}
	rivals := gateRivals(m, LiveDecisionRivals(m, existing, opt.Now, opt.typeSpec(m)))
	if len(rivals) == 0 {
		return nil
	}
	top := closestRival(m, rivals)
	// 거절문이 이름 댄 상대(top)가 목록을 자를 때 빠지면 안 된다 — 맨 앞에
	// 두고 나머지를 id 차례로 잇는다.
	ids := make([]string, 0, len(rivals))
	for _, one := range rivals {
		if one.ID != top.ID {
			ids = append(ids, one.ID)
		}
	}
	sort.Strings(ids)
	ids = append([]string{top.ID}, ids...)
	if len(ids) > dupListMax {
		ids = ids[:dupListMax]
	}
	return []Finding{{Rule: RuleDecisionGate, Level: opt.grade(RuleDecisionGate, m), ID: m.ID,
		Reason:  rivalReason(m, top),
		Related: ids,
		// `--new` 가 먼저다. 덮기(`--by`)가 첫 줄이면 가장 쉬운 길이 남의 멀쩡한
		// 결정을 무효로 만드는 길이 된다 (사용 피드백 2026-09-21 떡아이콘·손님그림).
		Next: []string{
			newTopicStep,
			fmt.Sprintf("mem add … --by %s   (그 결정을 이 기억이 뒤집을 때만 덮는다)", top.ID),
		}}}
}

// closestRival 은 요약이 가장 닮은 상대다. 거절문이 id 만 찍으면 사람이
// `mem show` 를 한 번 더 쳐야 「아, 다른 얘기구나」를 안다 — 가장 닮은
// 하나의 제목·점수를 같이 보여 준다. 점수가 같으면 id 차례로 고정한다.
func closestRival(m *model.Memory, rivals []*model.Memory) *model.Memory {
	best := rivals[0]
	bestSim := summarySim(m, best)
	for _, other := range rivals[1:] {
		sim := summarySim(m, other)
		if sim > bestSim || (sim == bestSim && other.ID < best.ID) {
			best, bestSim = other, sim
		}
	}
	return best
}

// rivalTitleRoom 은 거절문에 싣는 상대 제목의 룬 수다. 제목 상한(40)과 같다.
const rivalTitleRoom = 40

// rivalReason 은 C04 거절 까닭이다. 왜 막혔는지 알아야 고치거나 넘길 수 있어
// 부딪힌 상대의 제목 · 요약 닮음과 문턱 · 겹친 태그를 한 줄에 싣는다.
// 제목은 남이 쓴 글이라 한 줄로 접고 중화한다 (불변조건 I3).
func rivalReason(m *model.Memory, rival *model.Memory) string {
	return fmt.Sprintf("같은 자리(scope %s)에 살아 있는 결정이 있다 — `%s` 「%s」 · 요약 닮음 %.3f (문턱 %.3f) · 겹친 태그 %s",
		m.Scope, rival.ID, safe.Summary(rival.DisplayTitle(), rivalTitleRoom),
		summarySim(m, rival), gateSummaryCut, strings.Join(commonTags(m.Tags, rival.Tags), "·"))
}

// commonTags 는 두 태그 목록이 같이 가진 것을 내 쪽 차례로 준다.
func commonTags(mine, theirs []string) []string {
	other := map[string]bool{}
	for _, tag := range theirs {
		other[strings.ToLower(tag)] = true
	}
	shared := []string{}
	for _, tag := range mine {
		if other[strings.ToLower(tag)] {
			shared = append(shared, strings.ToLower(tag))
		}
	}
	return shared
}

// gateRivals 는 C04(add 관문)가 막을 상대만 남긴다 (설계 결정 39).
//
// C05(lint 후보)는 넓게 봐도 된다 — 사람이 큐에서 보고 넘기면 그만이다. 그런데
// C04 는 **저장을 막는다.** 실기억 206건은 scope 가 사실상 하나(`aimemorytool`
// 194건)라 scope 조건이 늘 참이고, 태그 하나만 겹쳐도 막혀서 부딪히지 않는
// 결정끼리 서로를 막았다. 그래서 둘을 더 요구한다.
//
//	① 태그 겹침 2 이상   — 「같은 자리」의 뜻을 좁힌다
//	② 요약 닮음 ≥ θ      — 정말 같은 것을 두고 다르게 정했는지 본다
//
// v0.2 설계 3-2 가 이미 「거짓 관문이 잦으면 2 로 올린다」를 대비로 적어 뒀다.
func gateRivals(m *model.Memory, rivals []*model.Memory) []*model.Memory {
	mine := map[string]bool{}
	for _, tag := range m.Tags {
		mine[strings.ToLower(tag)] = true
	}
	kept := []*model.Memory{}
	for _, other := range rivals {
		if tagOverlap(mine, other.Tags) < gateTagMin {
			continue
		}
		if !summaryBlocks(summarySim(m, other)) {
			continue
		}
		kept = append(kept, other)
	}
	return kept
}

// summaryBlocks 는 요약 닮음이 C04 문턱에 닿았는지다. 시험이 조사 문서에서
// 잰 닮음 값을 그대로 넣어 볼 수 있게 비교 한 줄을 따로 뒀다.
func summaryBlocks(sim float64) bool {
	return sim >= gateSummaryCut
}

// summarySim 은 두 결정이 「같은 것을 두고 말하는가」다. 제목과 요약만 본다 —
// 본문은 근거라 길이가 제각각이고, 결정이 부딪히는지는 머리말에 다 나온다.
func summarySim(left, right *model.Memory) float64 {
	return jaccard(grams(normalizeText(left.DisplayTitle()+" "+left.Summary)),
		grams(normalizeText(right.DisplayTitle()+" "+right.Summary)))
}

// gateTagMin·gateSummaryCut 은 C04 가 막기 전에 요구하는 둘이다.
//
// θ 처음 값 0.025 는 실기억 130건(태그 둘 겹치는 짝 126개)의 90분위로 잡았다.
// 결정이 늘며 그 자리가 분포 가운데로 내려앉아 오탐이 잦아졌다 (사용 피드백
// 2026-09-21 떡아이콘·손님그림). 2026-09-23 다시 쟀다 — 근거
// `Docs/Research/2026-09-23-결정관문오탐조사.md` :
//
//	두 저장소 합쳐 태그 둘+ 겹치는 살아 있는 결정 짝 688개
//	0.025 는 16% 를 막고 그중 80% 넘게가 오탐이다
//	0.04 는 6~8% 를 막아 S05 `add-reject-rate` 목표(5~20%) 안이다
//	사람이 `--by` 로 덮은 정답지 짝 중 관문이 잡던 11짝은 11짝 다 그대로 잡는다
//	오탐은 91 → 37짝 (59% 제거). 0.05 는 정답지 한 짝(0.0444)을 놓친다
//
// 재현한 오탐 짝 (재료 아이콘 ↔ 건물 0.0265 · 손님 그림 ↔ 캔버스 0.0388)은 새
// 문턱에서 지난다. 문턱을 올려 놓치는 짝은 C05(lint 후보)가 태그 하나 겹침으로
// 넓게 잡는다. 태그 조건(2)은 그대로다 — scope 이름·흔한 태그를 빼면 정답지를
// 절반 넘게 놓친다 (조사 문서 「대안 비교」).
const (
	gateTagMin     = 2
	gateSummaryCut = 0.04
)

// LiveDecisionRivals 는 같은 scope 에 태그가 겹치는 살아 있는 decision 이다.
// C04(add 관문)와 C05(lint 후보)가 같은 함수를 쓴다.
func LiveDecisionRivals(m *model.Memory, existing []*model.Memory, now time.Time,
	kind model.TypeSpec) []*model.Memory {
	if !kind.Gate {
		return nil
	}
	mine := map[string]bool{}
	for _, tag := range m.Tags {
		mine[strings.ToLower(tag)] = true
	}
	rivals := []*model.Memory{}
	for _, other := range existing {
		if other == nil || other.ID == m.ID || other.Type != m.Type {
			continue
		}
		if other.Scope != m.Scope || !Live(other, now) {
			continue
		}
		if tagOverlap(mine, other.Tags) == 0 {
			continue
		}
		rivals = append(rivals, other)
	}
	return rivals
}

// Live 는 아직 살아 있는 기억인지다 — 덮이지도 무효가 되지도 않은 것.
func Live(m *model.Memory, now time.Time) bool {
	if m.SupersededBy != "" {
		return false
	}
	if m.InvalidAt == "" {
		return true
	}
	return model.IsFutureDate(m.InvalidAt, now)
}

func tagOverlap(mine map[string]bool, tags []string) int {
	count := 0
	for _, tag := range tags {
		if mine[strings.ToLower(tag)] {
			count++
		}
	}
	return count
}
