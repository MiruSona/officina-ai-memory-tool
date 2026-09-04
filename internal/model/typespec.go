package model

// 기억 종류마다 다른 취급을 표 한 장에 모은다. 전에는 validate·quality·search·
// gc·index 여덟 군데에 흩어져 있어, 종류 하나를 늘리면 여덟 곳을 다 고쳐야 했다.
//
// 표에 있는 것은 **고르기**뿐이다 — 검사의 내용(issue 절 이름, howto 번호 판정,
// 관문 임계값, 반감기 수)은 코드에 남는다. toml 은 이름으로 고를 뿐 새 로직을
// 못 만든다.

// Sources 칸이 가질 수 있는 값.
const (
	SourcesNone     = "none"
	SourcesWarn     = "warn"
	SourcesRequired = "required"
)

// Body 칸이 가질 수 있는 값. 검사 내용은 quality 에 있다.
const (
	BodyFree          = "free"
	BodyIssueSections = "issue-sections"
	BodyNumbered      = "numbered"
)

// HalfLife 칸이 가질 수 있는 값. 실제 날수는 search 가 안다.
const (
	HalfLifeNone     = "none"
	HalfLifeHistory  = "history"
	HalfLifeDecision = "decision"
)

// 훅 절 이름. 차례도 이 순서다 (`고정` 과 `알림` 은 종류가 아니라 훅이 만든다).
const (
	HookDecision = "최근 결정"
	HookOpen     = "열린 이슈·할 일"
	HookFact     = "환경"
)

// hookOrder 는 기본표가 쓰는 절 차례다. 표에만 있는 새 절은 이 뒤에 붙는다.
var hookOrder = []string{HookDecision, HookOpen, HookFact}

// TypeSpec 은 기억 종류 하나의 취급이다.
type TypeSpec struct {
	Name  string
	Label string
	// Sources 는 근거를 요구하는 세기다 (none · warn · required).
	Sources string
	// Body 는 본문 꼴 검사다 (free · issue-sections · numbered).
	Body string
	// HalfLife 는 검색 감쇠 반감기 이름이다 (none · history · decision).
	HalfLife string
	// Hook 은 세션 훅에서 실릴 절 이름이다. 비면 안 실린다.
	Hook string
	// Severity·TodoStatus 는 그 칸을 쓰는 종류인지다. 안 쓰는 종류에 붙이면 거절이다.
	Severity   bool
	TodoStatus bool
	// Evidence·Conclusion 은 본문에 확인 가능한 조각·결론 문장을 요구하는지다.
	Evidence   bool
	Conclusion bool
	// OneThing 은 한 건에 하나만 담으라는 자다 (true 면 거절, false 면 경고).
	OneThing bool
	// Gate 는 저장 전 충돌 관문(C04)을 거는지다.
	Gate bool
	// GCKeep 은 gc 가 본문을 안 접는지다.
	GCKeep bool
	// SearchBonus 는 검색 가산을 받는지다.
	SearchBonus bool
	// StaleDays 는 낡음 후보 일수다. 0 이면 안 묻는다.
	StaleDays int
}

// TypeTable 은 이 저장소가 아는 종류 전부다. 차례는 표에 적힌 그대로다.
type TypeTable []TypeSpec

// Get 은 이름으로 한 줄을 찾는다.
func (t TypeTable) Get(name string) (TypeSpec, bool) {
	for _, spec := range t {
		if spec.Name == name {
			return spec, true
		}
	}
	return TypeSpec{}, false
}

// Spec 은 이름으로 찾되 없으면 빈 줄을 돌려준다. 「모르는 종류」는 값 검사가
// 따로 잡으므로, 부르는 쪽마다 ok 를 보지 않아도 되게 한다.
func (t TypeTable) Spec(name string) TypeSpec {
	spec, _ := t.Get(name)
	return spec
}

// Names 는 종류 이름 목록이다.
func (t TypeTable) Names() []string {
	names := make([]string, 0, len(t))
	for _, spec := range t {
		names = append(names, spec.Name)
	}
	return names
}

// Has 는 표에 있는 종류인지다.
func (t TypeTable) Has(name string) bool {
	_, ok := t.Get(name)
	return ok
}

// Where 는 칸 하나가 참인 종류 이름들이다. gc 면제 목록·검색 가산 목록이 쓴다.
func (t TypeTable) Where(pick func(TypeSpec) bool) []string {
	names := []string{}
	for _, spec := range t {
		if pick(spec) {
			names = append(names, spec.Name)
		}
	}
	return names
}

// HookSections 는 훅에 실릴 절 이름을 차례대로 돌려준다. 기본표의 세 절이
// 먼저고, 표에만 있는 새 절이 표에 적힌 차례대로 뒤에 붙는다.
func (t TypeTable) HookSections() []string {
	seen := map[string]bool{}
	for _, spec := range t {
		if spec.Hook != "" {
			seen[spec.Hook] = true
		}
	}
	out := []string{}
	for _, name := range hookOrder {
		if seen[name] {
			out = append(out, name)
			delete(seen, name)
		}
	}
	for _, spec := range t {
		if spec.Hook != "" && seen[spec.Hook] {
			out = append(out, spec.Hook)
			delete(seen, spec.Hook)
		}
	}
	return out
}

// HookTypes 는 한 절에 실을 종류들이다.
func (t TypeTable) HookTypes(section string) []TypeSpec {
	out := []TypeSpec{}
	for _, spec := range t {
		if spec.Hook == section {
			out = append(out, spec)
		}
	}
	return out
}

// DefaultTypes 는 toml 이 없을 때 도는 기본표다. 값은 v0.4 가 코드에 박아
// 두었던 것 그대로다 — 이 표를 고치면 이미 저장된 기억의 등급이 바뀐다.
func DefaultTypes() TypeTable {
	return TypeTable{
		{Name: TypeTodo, Label: "할 일", Sources: SourcesNone, Body: BodyFree,
			HalfLife: HalfLifeNone, Hook: HookOpen, TodoStatus: true},
		{Name: TypeHistory, Label: "기록", Sources: SourcesWarn, Body: BodyFree,
			HalfLife: HalfLifeHistory, Evidence: true, StaleDays: 180},
		{Name: TypeIssue, Label: "이슈", Sources: SourcesRequired, Body: BodyIssueSections,
			HalfLife: HalfLifeNone, Hook: HookOpen, Severity: true, Evidence: true,
			GCKeep: true, SearchBonus: true},
		{Name: TypeCaution, Label: "주의", Sources: SourcesRequired, Body: BodyFree,
			HalfLife: HalfLifeNone, Hook: HookOpen, Severity: true, GCKeep: true},
		{Name: TypeDecision, Label: "결정", Sources: SourcesRequired, Body: BodyFree,
			HalfLife: HalfLifeDecision, Hook: HookDecision, Evidence: true, Conclusion: true,
			OneThing: true, Gate: true, GCKeep: true, SearchBonus: true, StaleDays: 1440},
		{Name: TypeHowto, Label: "하는 법", Sources: SourcesNone, Body: BodyNumbered,
			HalfLife: HalfLifeDecision, Evidence: true, GCKeep: true, StaleDays: 1440},
		{Name: TypeFact, Label: "환경 사실", Sources: SourcesRequired, Body: BodyFree,
			HalfLife: HalfLifeNone, Hook: HookFact, OneThing: true, GCKeep: true, StaleDays: 360},
	}
}
