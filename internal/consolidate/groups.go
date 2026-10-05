// Package consolidate 는 여러 기억을 모음 기억(observation) 카드로 묶는다 (B1 · LLM 0).
//
// 묶음은 코드가 정한다 (자동쌓기설계 3-3).
//
//	① 덮음 사슬   — superseded_by 로 이어진 2건 이상
//	② 링크 무리   — 같은 scope 에서 links ∪ auto_links 로 이어진 3~12건
//	③ 의미 무리   — 같은 scope · 같은 종류 계열 · 코사인 ≥ 문턱인 3~8건 (벡터가 있을 때만)
//
// 자동 기억(origin 있음)은 그 무리에 사람·손 기억이 2건 이상 있을 때만 낀다.
// 카드 글은 원본의 제목·요약·날짜를 코드가 이어 붙인다 — 지어낸 말이 없다.
package consolidate

import (
	"sort"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 묶음 규칙 이름. 명령의 --rule 값과 카드 머리말에 쓴다.
const (
	RuleChain   = "chain"
	RuleLink    = "link"
	RuleMeaning = "meaning"
)

// Rules 는 규칙 셋의 차례다. 계획 표도 이 차례로 찍는다.
var Rules = []string{RuleChain, RuleLink, RuleMeaning}

// 무리 크기 (자동쌓기설계 3-3).
const (
	chainMin   = 2
	linkMin    = 3
	linkMax    = 12
	meaningMin = 3
	meaningMax = 8
)

// 의미 무리의 코사인 문턱. 처음 문턱으로 이은 무리가 8건을 넘으면 문턱을 한 칸씩
// 올려 쪼갠다. 끝 문턱에서도 크면 버린다.
//
// 0.75 는 ko-v2(e5-small 미세조정) 벡터로 스튜디오 사본 833건을 훑어 골랐다
// (B1 측정 · 0.60~0.90). 0.85 위로는 무리가 하나도 안 생기고, 0.70 아래로는
// 큰 덩어리를 쪼개느라 문턱만 올라가 같은 무리가 나온다. 몸통을 바꾸면 다시 잰다.
const (
	MeaningFloor = 0.75
	meaningStep  = 0.01
	meaningTop   = 0.97
)

// Cosine 은 벡터 저장소에서 필요한 것 하나다. embed.Vectors 가 맞춘다.
type Cosine interface {
	Cos(left, right string) float64
	Get(id string) []float32
}

// Input 은 묶음을 찾는 데 쓰는 전부다. Memories 는 파일에서 읽은 기억 전부다
// (보류·덮인 것 포함). AutoLinks 는 색인이 찾아 둔 이웃이다 (없으면 nil).
type Input struct {
	Memories  []*model.Memory
	AutoLinks [][2]string
	Vectors   Cosine
	// Rule·Scope 가 비지 않으면 그 규칙·scope 만 본다.
	Rule  string
	Scope string
	// Floor 가 0 이면 MeaningFloor 다.
	Floor float64
}

// Group 은 묶음 하나다. Members 는 날짜·id 차례다.
type Group struct {
	Rule    string   `json:"rule"`
	Scope   string   `json:"scope"`
	Members []string `json:"members"`
	// Status 는 new(새로) · again(다시) · same(그대로) 중 하나다. Existing 은 맞춘 카드다.
	Status   string `json:"status"`
	Existing string `json:"existing,omitempty"`
	// Why 는 「다시」 까닭이다 — 근거가 바뀜·구성원이 바뀜.
	Why string `json:"why,omitempty"`
}

// Found 는 규칙이 찾은 묶음과 셈이다.
type Found struct {
	Groups []Group
	// TooBig 은 12건을 넘어 건너뛴 링크 무리 수, Split 은 문턱을 올려도 8건을 넘어
	// 버린 의미 무리 수다.
	TooBig  int
	Dropped int
	// NoVector 는 ③ 을 벡터가 없어 건너뛰었는지다.
	NoVector bool
}

// Find 는 세 규칙을 차례로 돌려 묶음을 준다. 구성원이 똑같은 묶음은 앞 규칙 것만 남긴다.
func Find(in Input) Found {
	byID := indexOf(in.Memories)
	out := Found{}
	seen := map[string]bool{}
	keep := func(rule string, sets [][]string) {
		for _, set := range sets {
			set = admitAuto(set, byID)
			if len(set) < minOf(rule) {
				continue
			}
			if rule == RuleChain {
				sortChain(set, byID)
			} else {
				sortMembers(set, byID)
			}
			key := setKey(set)
			if seen[key] {
				continue
			}
			seen[key] = true
			out.Groups = append(out.Groups, Group{Rule: rule, Scope: byID[set[len(set)-1]].Scope, Members: set})
		}
	}
	if wants(in.Rule, RuleChain) {
		keep(RuleChain, chains(in.Memories, in.Scope))
	}
	if wants(in.Rule, RuleLink) {
		sets, big := linkGroups(in, byID)
		out.TooBig = big
		keep(RuleLink, sets)
	}
	if wants(in.Rule, RuleMeaning) {
		if in.Vectors == nil {
			out.NoVector = true
		} else {
			sets, dropped := meaningGroups(in, byID)
			out.Dropped = dropped
			keep(RuleMeaning, sets)
		}
	}
	return out
}

func wants(only, rule string) bool { return only == "" || only == rule }

func minOf(rule string) int {
	switch rule {
	case RuleChain:
		return chainMin
	case RuleLink:
		return linkMin
	}
	return meaningMin
}

func indexOf(memories []*model.Memory) map[string]*model.Memory {
	out := make(map[string]*model.Memory, len(memories))
	for _, one := range memories {
		out[one.ID] = one
	}
	return out
}

// usable 은 무리 구성원이 될 수 있는 기억이다 — 모음 기억이 아니고 보류되지 않았다.
func usable(m *model.Memory) bool {
	return m != nil && m.Type != model.TypeObservation && !m.Review
}

// alive 는 ②③ 의 구성원 자격이다. 덮였거나 무효 날짜가 적힌 것은 사슬(①) 몫이다.
func alive(m *model.Memory) bool {
	return usable(m) && m.SupersededBy == "" && m.InvalidAt == ""
}

// chains 는 ① 이다. superseded_by 를 방향 없는 선으로 보고 이은 덩어리다.
func chains(memories []*model.Memory, scope string) [][]string {
	joined := newUnion()
	byID := indexOf(memories)
	for _, m := range memories {
		if !usable(m) || m.SupersededBy == "" || !usable(byID[m.SupersededBy]) {
			continue
		}
		joined.join(m.ID, m.SupersededBy)
	}
	out := [][]string{}
	for _, set := range joined.sets() {
		if len(set) < chainMin || !inScope(set, byID, scope) {
			continue
		}
		out = append(out, set)
	}
	return out
}

// inScope 는 사슬의 가장 새 기억이 그 scope 인지다. 사슬은 scope 를 넘을 수 있다.
func inScope(set []string, byID map[string]*model.Memory, scope string) bool {
	if scope == "" {
		return true
	}
	sortChain(set, byID)
	return byID[set[len(set)-1]].Scope == scope
}

// linkGroups 는 ② 다. 같은 scope · 살아 있는 기억 사이의 links ∪ auto_links 덩어리다.
func linkGroups(in Input, byID map[string]*model.Memory) ([][]string, int) {
	joined := newUnion()
	edge := func(a, b string) {
		left, right := byID[a], byID[b]
		if !alive(left) || !alive(right) || a == b || left.Scope != right.Scope {
			return
		}
		if in.Scope != "" && left.Scope != in.Scope {
			return
		}
		joined.join(a, b)
	}
	for _, m := range in.Memories {
		for _, target := range m.Links {
			edge(m.ID, target)
		}
	}
	for _, pair := range in.AutoLinks {
		edge(pair[0], pair[1])
	}
	out, big := [][]string{}, 0
	for _, set := range joined.sets() {
		switch {
		case len(set) > linkMax:
			big++
		case len(set) >= linkMin:
			out = append(out, set)
		}
	}
	return out, big
}

// familyOf 는 ③ 의 「같은 종류 계열」이다. 정해 둔 말(결정·사실·주의·방법)과
// 흐름(기록·문제·할 일)을 가른다. 계열이 다른 기억은 같은 주제라도 한 카드에 안 섞는다.
func familyOf(kind string) string {
	switch kind {
	case model.TypeHistory, model.TypeIssue, model.TypeTodo:
		return "flow"
	}
	return "rule"
}

// meaningGroups 는 ③ 이다. scope·계열 칸마다 코사인 ≥ 문턱으로 이은 덩어리를
// 만들고, 8건을 넘으면 그 덩어리 안에서 문턱을 올려 다시 쪼갠다.
func meaningGroups(in Input, byID map[string]*model.Memory) ([][]string, int) {
	floor := in.Floor
	if floor <= 0 {
		floor = MeaningFloor
	}
	bins := map[string][]string{}
	for _, m := range in.Memories {
		if !alive(m) || (in.Scope != "" && m.Scope != in.Scope) || in.Vectors.Get(m.ID) == nil {
			continue
		}
		key := m.Scope + "\x00" + familyOf(m.Type)
		bins[key] = append(bins[key], m.ID)
	}
	keys := make([]string, 0, len(bins))
	for key := range bins {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out, dropped := [][]string{}, 0
	for _, key := range keys {
		ids := bins[key]
		sort.Strings(ids)
		sets, lost := splitByCos(ids, in.Vectors, floor)
		out = append(out, sets...)
		dropped += lost
	}
	return out, dropped
}

// splitByCos 는 ids 를 문턱 floor 로 이어 3~8건 덩어리를 준다. 큰 덩어리는
// 문턱을 올려 다시 쪼갠다.
func splitByCos(ids []string, vectors Cosine, floor float64) ([][]string, int) {
	joined := newUnion()
	for a := 0; a < len(ids); a++ {
		for b := a + 1; b < len(ids); b++ {
			if vectors.Cos(ids[a], ids[b]) >= floor {
				joined.join(ids[a], ids[b])
			}
		}
	}
	out, dropped := [][]string{}, 0
	for _, set := range joined.sets() {
		switch {
		case len(set) <= meaningMax:
			if len(set) >= meaningMin {
				out = append(out, set)
			}
		case floor+meaningStep > meaningTop+1e-9:
			dropped++
		default:
			more, lost := splitByCos(set, vectors, floor+meaningStep)
			out = append(out, more...)
			dropped += lost
		}
	}
	return out, dropped
}

// admitAuto 는 자동 기억 규칙이다. 사람·손 기억이 2건 미만인 무리에서는 자동 기억을 뺀다.
func admitAuto(set []string, byID map[string]*model.Memory) []string {
	human := 0
	for _, id := range set {
		if byID[id].Origin == "" {
			human++
		}
	}
	if human >= 2 {
		return set
	}
	out := []string{}
	for _, id := range set {
		if byID[id].Origin == "" {
			out = append(out, id)
		}
	}
	return out
}

// sortMembers 는 날짜 → id 차례다. 사슬은 옛것이 먼저라 「예전 → 지금」 이 된다.
func sortMembers(set []string, byID map[string]*model.Memory) {
	sort.Slice(set, func(a, b int) bool {
		left, right := byID[set[a]], byID[set[b]]
		if left.Date != right.Date {
			return left.Date < right.Date
		}
		return set[a] < set[b]
	})
}

// sortChain 은 사슬 차례다. 날짜가 같은 날 덮은 일이 흔해 날짜로는 「예전 → 지금」 이
// 뒤집힌다 (B1 측정 20묶음 읽기에서 셋). 덮음 선을 따라 끝(살아 있는 쪽)까지 몇
// 칸인지 세어 먼 것부터 둔다. 같으면 날짜 → id 다.
func sortChain(set []string, byID map[string]*model.Memory) {
	inSet := map[string]bool{}
	for _, id := range set {
		inSet[id] = true
	}
	hops := map[string]int{}
	for _, id := range set {
		count, at, seen := 0, id, map[string]bool{}
		for !seen[at] {
			seen[at] = true
			next := byID[at].SupersededBy
			if next == "" || !inSet[next] {
				break
			}
			count++
			at = next
		}
		hops[id] = count
	}
	sort.Slice(set, func(a, b int) bool {
		if hops[set[a]] != hops[set[b]] {
			return hops[set[a]] > hops[set[b]]
		}
		left, right := byID[set[a]], byID[set[b]]
		if left.Date != right.Date {
			return left.Date < right.Date
		}
		return set[a] < set[b]
	})
}

func setKey(set []string) string {
	sorted := append([]string(nil), set...)
	sort.Strings(sorted)
	key := ""
	for _, id := range sorted {
		key += id + ","
	}
	return key
}

// union 은 덩어리 찾기(union-find)다. 차례가 늘 같도록 sets 가 정렬해 준다.
type union struct{ parent map[string]string }

func newUnion() *union { return &union{parent: map[string]string{}} }

func (u *union) find(id string) string {
	if _, ok := u.parent[id]; !ok {
		u.parent[id] = id
	}
	for u.parent[id] != id {
		u.parent[id] = u.parent[u.parent[id]]
		id = u.parent[id]
	}
	return id
}

func (u *union) join(a, b string) {
	left, right := u.find(a), u.find(b)
	if left == right {
		return
	}
	if left < right {
		u.parent[right] = left
	} else {
		u.parent[left] = right
	}
}

func (u *union) sets() [][]string {
	groups := map[string][]string{}
	for id := range u.parent {
		root := u.find(id)
		groups[root] = append(groups[root], id)
	}
	roots := make([]string, 0, len(groups))
	for root := range groups {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	out := make([][]string, 0, len(roots))
	for _, root := range roots {
		set := groups[root]
		sort.Strings(set)
		out = append(out, set)
	}
	return out
}
