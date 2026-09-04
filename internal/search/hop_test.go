package search

import (
	"testing"
)

// hop·MMR·묶음은 답 목록 위에서만 도는 셈법이라 DB 없이도 잰다.
func fakeHit(id, scope string, tags []string, score float64) Hit {
	return Hit{ID: id, Type: "decision", Title: id, Summary: id + " 요약",
		Scope: scope, Tags: tags, Score: score, Parts: Parts{RRF: score, Bonus: 1, Decay: 1, Trust: 1}}
}

// 1-hop 은 앞에 선 답이 가리키는 기억의 값을 올린다.
func TestHopLiftsLinkedNeighbour(t *testing.T) {
	hits := []Hit{
		fakeHit("a", "one", []string{"x"}, 1.0),
		fakeHit("b", "two", []string{"y"}, 0.9),
		fakeHit("c", "three", []string{"z"}, 0.85),
	}
	edges := linkEdges{"a": {"c": true}}
	got := hopBonus(hits, edges, 1.5, false)
	order := hitIDs(got)
	if place(order, "c") > place(order, "b") {
		t.Fatalf("이어진 c 가 안 이어진 b 보다 아래다 : %v", order)
	}
	for _, one := range got {
		if one.ID == "c" && one.Parts.Bonus <= 1 {
			t.Fatalf("가산이 안 붙었다 : %+v", one.Parts)
		}
	}
}

// 가산 상한(bonus_cap)을 넘지 않는다 (고삐 ②).
func TestHopKeepsCap(t *testing.T) {
	hits := []Hit{fakeHit("a", "one", []string{"x"}, 1.0), fakeHit("b", "two", []string{"y"}, 0.5)}
	hits[1].Parts.Bonus = 1.5
	edges := linkEdges{"a": {"b": true}}
	got := hopBonus(hits, edges, 1.5, false)
	if got[1].Parts.Bonus > 1.5 {
		t.Fatalf("상한을 넘었다 : %v", got[1].Parts.Bonus)
	}
	if got[1].Score != 0.5 {
		t.Fatalf("상한에 닿았는데 점수가 움직였다 : %v", got[1].Score)
	}
}

// 후보를 안 늘린다 — 링크만 있고 순위에 없던 기억은 절대 안 들어온다 (고삐 ①).
func TestHopAddsNoCandidate(t *testing.T) {
	hits := []Hit{fakeHit("a", "one", []string{"x"}, 1.0)}
	edges := linkEdges{"a": {"없던기억": true}}
	if got := hopBonus(hits, edges, 1.5, false); len(got) != 1 {
		t.Fatalf("없던 기억이 답에 들어왔다 : %v", hitIDs(got))
	}
}

// eval 이 오배제를 잴 때 쓰는 raw 는 손대지 않는다.
func TestRawUntouched(t *testing.T) {
	hits := []Hit{fakeHit("a", "one", []string{"x"}, 1.0), fakeHit("b", "one", []string{"x"}, 0.9)}
	edges := linkEdges{"a": {"b": true}}
	got := hopBonus(diversify(append([]Hit{}, hits...), true), edges, 1.5, true)
	for at := range got {
		if got[at].Score != hits[at].Score {
			t.Fatalf("raw 인데 점수가 바뀌었다 : %+v", got)
		}
	}
}

// MMR 은 앞에 선 것과 닮은 답의 값을 깎는다 (λ=0.7).
func TestMMRPushesTwinDown(t *testing.T) {
	hits := []Hit{
		fakeHit("a", "one", []string{"x", "y"}, 1.0),
		fakeHit("twin", "one", []string{"x", "y"}, 0.9),
		fakeHit("other", "two", []string{"z"}, 0.85),
	}
	got := diversify(hits, false)
	if got[1].ID != "other" {
		t.Fatalf("닮은 것이 안 밀렸다 : %v", hitIDs(got))
	}
	if got[2].Parts.Diverse >= 1 || got[2].Parts.Diverse <= 0 {
		t.Fatalf("깎은 몫이 안 적혔다 : %v", got[2].Parts.Diverse)
	}
}

// 1위는 MMR 이 안 깎는다.
func TestMMRKeepsTop(t *testing.T) {
	hits := []Hit{fakeHit("a", "one", []string{"x"}, 1.0), fakeHit("b", "one", []string{"x"}, 0.9)}
	if got := diversify(hits, false); got[0].ID != "a" || got[0].Score != 1.0 {
		t.Fatalf("1위가 움직였다 : %+v", got[0])
	}
}

// 같은 주제끼리 묶음 번호가 붙는다. 혼자인 것은 0 이다.
func TestGroupsSameTopic(t *testing.T) {
	hits := []Hit{
		fakeHit("a", "one", []string{"x", "y"}, 1.0),
		fakeHit("solo", "two", []string{"q", "r"}, 0.9),
		fakeHit("b", "one", []string{"x", "y"}, 0.8),
	}
	if count := groupHits(hits); count != 1 {
		t.Fatalf("묶음이 하나여야 한다 : %d", count)
	}
	if hits[0].Group != 1 || hits[2].Group != 1 || hits[1].Group != 0 {
		t.Fatalf("묶음 번호가 틀렸다 : %d %d %d", hits[0].Group, hits[1].Group, hits[2].Group)
	}
	if groupMark(hits[0]) == "" || groupMark(hits[1]) != "" {
		t.Fatal("표 꼬리표가 틀렸다")
	}
}

// 묶음은 차례를 안 바꾼다 — 자(eval)가 재는 것이 달라지면 안 된다.
func TestGroupKeepsOrder(t *testing.T) {
	hits := []Hit{
		fakeHit("a", "one", []string{"x", "y"}, 1.0),
		fakeHit("solo", "two", []string{"q", "r"}, 0.9),
		fakeHit("b", "one", []string{"x", "y"}, 0.8),
	}
	before := hitIDs(hits)
	groupHits(hits)
	if got := hitIDs(hits); !sameOrder(before, got) {
		t.Fatalf("묶음이 차례를 바꿨다 : %v → %v", before, got)
	}
}

func hitIDs(hits []Hit) []string {
	out := make([]string, len(hits))
	for at := range hits {
		out[at] = hits[at].ID
	}
	return out
}

func place(ids []string, want string) int {
	for at, one := range ids {
		if one == want {
			return at
		}
	}
	return len(ids)
}

func sameOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for at := range a {
		if a[at] != b[at] {
			return false
		}
	}
	return true
}
