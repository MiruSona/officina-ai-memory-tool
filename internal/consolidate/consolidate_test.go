package consolidate

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
)

// 묶음 규칙 셋 · 자동 기억 규칙 · 카드 조립 · 이미 있는 카드 맞추기 (자동쌓기설계 3-3).

func mem(id, kind, scope, date, title string) *model.Memory {
	return &model.Memory{ID: id, Type: kind, Date: date, Spec: model.SpecV2, Title: title,
		Summary: "훅이 밀어 넣는 글은 글자 수가 아니라 UTF-8 바이트로 재야 한다 — " + title,
		Tags:    []string{"hook", "index"}, Scope: scope, Author: "human:tester",
		Sources: []string{"file:a.go"}, Body: "본문 한 줄."}
}

func cover(old, newer *model.Memory) {
	old.SupersededBy, old.InvalidAt = newer.ID, newer.Date
}

func ids(group Group) string { return strings.Join(group.Members, ",") }

// fakeCos 는 짝마다 코사인을 정해 둔 벡터 저장소다.
type fakeCos map[[2]string]float64

func (f fakeCos) Cos(a, b string) float64 {
	if value, ok := f[[2]string{a, b}]; ok {
		return value
	}
	return f[[2]string{b, a}]
}

func (f fakeCos) Get(id string) []float32 { return []float32{1} }

func TestChainRuleOrdersByCover(t *testing.T) {
	// 같은 날 덮은 사슬 — id 차례로는 「예전 → 지금」 이 뒤집힌다.
	head := mem("20260901-00000001", model.TypeDecision, "aimemorytool", "2026-09-01", "배율을 다시 잰다")
	middle := mem("20260901-00000009", model.TypeDecision, "aimemorytool", "2026-09-01", "배율을 벽시계로 바꾼다")
	old := mem("20260830-00000005", model.TypeDecision, "aimemorytool", "2026-08-30", "배율을 토큰으로 잰다")
	cover(old, middle)
	cover(middle, head)
	lone := mem("20260901-0000000a", model.TypeDecision, "aimemorytool", "2026-09-01", "혼자 있는 결정")
	found := Find(Input{Memories: []*model.Memory{head, middle, old, lone}, Rule: RuleChain})
	if len(found.Groups) != 1 {
		t.Fatalf("사슬이 하나여야 한다 : %+v", found.Groups)
	}
	if got := ids(found.Groups[0]); got != old.ID+","+middle.ID+","+head.ID {
		t.Fatalf("사슬 차례가 덮음 차례가 아니다 : %s", got)
	}
}

func TestChainSkipsHeldAndObservation(t *testing.T) {
	newer := mem("20260901-00000002", model.TypeDecision, "aimemorytool", "2026-09-01", "새 결정이다")
	old := mem("20260801-00000002", model.TypeDecision, "aimemorytool", "2026-08-01", "옛 결정이다")
	cover(old, newer)
	newer.Review = true
	found := Find(Input{Memories: []*model.Memory{newer, old}, Rule: RuleChain})
	if len(found.Groups) != 0 {
		t.Fatalf("보류된 기억이 사슬에 들었다 : %+v", found.Groups)
	}
}

func linkRing(scope string, count int, prefix string) []*model.Memory {
	out := []*model.Memory{}
	for at := 0; at < count; at++ {
		one := mem(fmt.Sprintf("20260902-%s%06d", prefix, at), model.TypeHistory, scope, "2026-09-02",
			fmt.Sprintf("링크 무리 기억 %d번", at))
		if at > 0 {
			one.Links = []string{out[at-1].ID}
		}
		out = append(out, one)
	}
	return out
}

func TestLinkRuleBoundsAndScope(t *testing.T) {
	small := linkRing("aimemorytool", 2, "aa")
	good := linkRing("aimemorytool", 4, "bb")
	big := linkRing("aimemorytool", 13, "cc")
	mixed := linkRing("officina", 3, "dd")
	mixed[2].Scope = "arttool" // scope 가 다르면 선이 끊긴다 → 2건 + 1건
	all := append(append(append(small, good...), big...), mixed...)
	auto := [][2]string{{good[0].ID, good[3].ID}}
	found := Find(Input{Memories: all, AutoLinks: auto, Rule: RuleLink})
	if len(found.Groups) != 1 || len(found.Groups[0].Members) != 4 {
		t.Fatalf("3~12건 같은 scope 무리 하나만 나와야 한다 : %+v", found.Groups)
	}
	if found.TooBig != 1 {
		t.Fatalf("13건 무리를 큰 무리로 안 셌다 : %d", found.TooBig)
	}
	// auto_links 만으로도 이어진다.
	loose := linkRing("aimemorytool", 3, "ee")
	for _, one := range loose {
		one.Links = nil
	}
	pairs := [][2]string{{loose[0].ID, loose[1].ID}, {loose[1].ID, loose[2].ID}}
	if found := Find(Input{Memories: loose, AutoLinks: pairs, Rule: RuleLink}); len(found.Groups) != 1 {
		t.Fatalf("auto_links 로 이은 무리를 못 찾았다 : %+v", found.Groups)
	}
}

func TestAutoMemberNeedsTwoHumans(t *testing.T) {
	ring := linkRing("aimemorytool", 3, "ff")
	ring[1].Origin = "stop"
	ring[2].Origin = "stop"
	if found := Find(Input{Memories: ring, Rule: RuleLink}); len(found.Groups) != 0 {
		t.Fatalf("사람 기억 1건에 자동 기억 2건 무리를 만들었다 : %+v", found.Groups)
	}
	ring = linkRing("aimemorytool", 4, "gg")
	ring[3].Origin = "card"
	found := Find(Input{Memories: ring, Rule: RuleLink})
	if len(found.Groups) != 1 || len(found.Groups[0].Members) != 4 {
		t.Fatalf("사람 기억 3건이면 자동 기억이 껴야 한다 : %+v", found.Groups)
	}
}

func TestMeaningRuleNeedsVectorsAndSplits(t *testing.T) {
	members := []*model.Memory{}
	for at := 0; at < 10; at++ {
		members = append(members, mem(fmt.Sprintf("20260903-0000%04d", at), model.TypeDecision,
			"aimemorytool", "2026-09-03", fmt.Sprintf("뜻 무리 기억 %d번", at)))
	}
	if found := Find(Input{Memories: members}); !found.NoVector {
		t.Fatal("벡터가 없는데 의미 무리를 돌렸다")
	}
	cos := fakeCos{}
	// 0~9 를 사슬처럼 0.80 으로 이으면 10건이라 8 을 넘는다. 0~3 과 5~9 만 0.90 이다.
	for at := 0; at < 9; at++ {
		value := 0.80
		if at < 3 || at >= 5 {
			value = 0.90
		}
		cos[[2]string{members[at].ID, members[at+1].ID}] = value
	}
	found := Find(Input{Memories: members, Vectors: cos, Rule: RuleMeaning})
	sizes := []int{}
	for _, group := range found.Groups {
		sizes = append(sizes, len(group.Members))
	}
	if fmt.Sprint(sizes) != "[4 5]" {
		t.Fatalf("문턱을 올려 4·5 건으로 쪼개야 한다 : %v", sizes)
	}
	// 계열이 다르면 안 섞는다.
	members[1].Type = model.TypeHistory
	found = Find(Input{Memories: members, Vectors: cos, Rule: RuleMeaning})
	for _, group := range found.Groups {
		for _, id := range group.Members {
			if id == members[1].ID {
				t.Fatal("흐름 계열 기억이 결정 무리에 섞였다")
			}
		}
	}
}

func TestCardIsAssembledFromSources(t *testing.T) {
	head := mem("20260901-00000001", model.TypeDecision, "aimemorytool", "2026-09-01", "배율을 다시 잰다")
	old := mem("20260830-00000005", model.TypeDecision, "aimemorytool", "2026-08-30", "배율을 토큰으로 잰다")
	old.Summary = "요약에 [mem:20260101-deadbeef] 를 적어 둔 옛 결정이다. 본문 인용으로 새면 안 된다"
	cover(old, head)
	byID := map[string]*model.Memory{head.ID: head, old.ID: old}
	group := Find(Input{Memories: []*model.Memory{head, old}}).Groups[0]
	card := Card(group, byID, "2026-10-05")
	if card.Origin != CardOrigin || card.Author != CardAuthor || card.Rev != 1 || card.Type != model.TypeObservation {
		t.Fatalf("머리말이 틀리다 : %+v", card)
	}
	if !model.IsBasisHash(card.BasisHash) {
		t.Fatalf("basis_hash 꼴이 틀리다 : %q", card.BasisHash)
	}
	if !strings.Contains(card.Body, "[예전]") || !strings.Contains(card.Body, "[지금]") {
		t.Fatalf("사슬 카드에 예전·지금 표시가 없다 :\n%s", card.Body)
	}
	built := &model.Memory{ID: "20261005-0000aaaa", Type: card.Type, Date: card.Date, Spec: model.SpecV2,
		Title: card.Title, Summary: card.Summary, Tags: card.Tags, Scope: card.Scope, Author: card.Author,
		Sources: card.Sources, Origin: card.Origin, BasisHash: card.BasisHash, Rev: card.Rev, Body: card.Body}
	if problems := model.Validate(built); len(problems) > 0 {
		t.Fatalf("카드가 규격을 못 지난다 : %v", problems)
	}
	for _, found := range quality.Check(built, quality.Options{}) {
		switch found.Rule {
		case quality.RuleObsCite, quality.RuleBodyThin, quality.RuleFirstLineConclusion,
			quality.RuleSummaryBodyMatch, quality.RuleTagNotSource:
			t.Fatalf("카드가 %s 에 걸렸다 : %s\n%s", found.Rule, found.Reason, card.Body)
		}
	}
	all := map[string]*model.Memory{head.ID: head, old.ID: old, built.ID: built}
	if reason, _ := quality.ObservationStale(built, all); reason != "" {
		t.Fatalf("막 쓴 카드가 낡았다 : %s", reason)
	}
	head.Summary += " 고쳤다"
	if reason, _ := quality.ObservationStale(built, all); reason == "" {
		t.Fatal("근거를 고쳤는데 낡음을 못 잡았다")
	}
}

func TestPlanMatchesExistingCard(t *testing.T) {
	ring := linkRing("aimemorytool", 4, "hh")
	byID := map[string]*model.Memory{}
	for _, one := range ring {
		byID[one.ID] = one
	}
	group := Find(Input{Memories: ring, Rule: RuleLink}).Groups[0]
	request := Card(group, byID, "2026-10-05")
	card := &model.Memory{ID: "20261005-0000bbbb", Type: model.TypeObservation, Date: "2026-10-05",
		Sources: request.Sources, BasisHash: request.BasisHash, Rev: 1, Origin: CardOrigin}
	memories := append(append([]*model.Memory{}, ring...), card)
	plan := MakePlan(Input{Memories: memories, Rule: RuleLink})
	if plan.Same != 1 || plan.New != 0 {
		t.Fatalf("같은 카드가 있는데 새로 만든다 : %+v", plan.Groups)
	}
	extra := mem("20260902-hh000009", model.TypeHistory, "aimemorytool", "2026-09-02", "새로 이어진 기억")
	extra.Links = []string{ring[3].ID}
	memories = append(memories, extra)
	plan = MakePlan(Input{Memories: memories, Rule: RuleLink})
	if plan.Again != 1 || plan.Groups[0].Existing != card.ID || plan.Groups[0].Why != "구성원이 바뀜" {
		t.Fatalf("구성원이 늘었는데 다시로 안 잡았다 : %+v", plan.Groups)
	}
	card.Review = true // 되돌린 카드는 없는 것으로 친다
	if plan = MakePlan(Input{Memories: memories, Rule: RuleLink}); plan.New != 1 {
		t.Fatalf("보류된 카드에 맞췄다 : %+v", plan.Groups)
	}
}
