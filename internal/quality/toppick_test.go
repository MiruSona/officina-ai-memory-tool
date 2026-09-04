package quality

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"
)

// 후보를 다 정렬해서 앞 k 개 고르던 것을 힙으로 바꿨다 (리뷰 B · V3).
// **답이 한 자리도 달라지면 안 된다** — 여기서 그것을 못 박는다.
func TestTopPickerMatchesFullSort(t *testing.T) {
	dice := rand.New(rand.NewSource(7))
	docs := make([]*Doc, 400)
	for at := range docs {
		docs[at] = &Doc{ID: string(rune('a'+at%26)) + string(rune('a'+at/26))}
	}
	for round := 0; round < 200; round++ {
		count := dice.Intn(300) + 1
		limit := dice.Intn(60) + 1
		items := make([]ranked, 0, count)
		for at := 0; at < count; at++ {
			items = append(items, ranked{doc: docs[at], at: at,
				shared: dice.Intn(3), gap: dice.Intn(4), sketched: dice.Intn(2) == 0})
		}
		picker := topPicker{limit: limit}
		for _, one := range items {
			picker.push(one)
		}
		got := picker.sorted()
		want := append([]ranked{}, items...)
		sort.SliceStable(want, func(a, b int) bool { return rankBefore(want[a], want[b]) })
		if limit < len(want) {
			want = want[:limit]
		}
		if len(got) != len(want) {
			t.Fatalf("건수가 다르다 : %d ≠ %d", len(got), len(want))
		}
		for at := range got {
			if got[at].at != want[at].at {
				t.Fatalf("%d 회차 %d 번째가 다르다 : %d ≠ %d", round, at, got[at].at, want[at].at)
			}
		}
	}
}

// 상한이 0 이하면 아무것도 안 고른다.
func TestTopPickerZeroLimit(t *testing.T) {
	picker := topPicker{limit: 0}
	picker.push(ranked{doc: &Doc{ID: "a"}})
	if len(picker.sorted()) != 0 {
		t.Fatal("상한 0 인데 골랐다")
	}
}

// 태그 표식은 **거를 뿐** 답을 안 바꾼다. 겹치는 태그 수가 그대로여야 한다.
func TestSharedTagsWithBits(t *testing.T) {
	// (v0.4 C2) 태그는 이제 표가 매긴 번호로 견준다. 재는 뜻은 그대로다 —
	// 겹치는 태그 수가 그대로여야 한다.
	table := NewSimilar(8)
	seat := 0
	make := func(tags ...string) *Doc {
		doc := &Doc{Scope: "s", tags: lowerTagSet(tags)}
		doc.tagBits = tagBitsOf(doc.tags)
		seat++
		doc.ID = fmt.Sprintf("20260824-%08d", seat)
		table.Add(doc)
		return doc
	}
	cases := []struct {
		left, right []string
		want        int
	}{
		{[]string{"ui", "build"}, []string{"ui"}, 1 + 1},
		{[]string{"ui", "build"}, []string{"ui", "build"}, 2 + 1},
		{[]string{"ui"}, []string{"net"}, 0 + 1},
		{nil, []string{"net"}, 0 + 1},
	}
	for _, one := range cases {
		left, right := make(one.left...), make(one.right...)
		if got := shared(left, table.idsFor(right)); got != one.want {
			t.Fatalf("%v vs %v : %d ≠ %d", one.left, one.right, got, one.want)
		}
	}
}

// 중간에 그만두는 자카드는 need 위에서 값이 같아야 한다.
func TestJaccardAtLeastMatches(t *testing.T) {
	dice := rand.New(rand.NewSource(3))
	pick := func(count int) []uint64 {
		out := map[uint64]bool{}
		for at := 0; at < count; at++ {
			out[uint64(dice.Intn(40))] = true
		}
		list := []uint64{}
		for one := range out {
			list = append(list, one)
		}
		sort.Slice(list, func(a, b int) bool { return list[a] < list[b] })
		return list
	}
	for round := 0; round < 3000; round++ {
		left, right := pick(dice.Intn(20)+1), pick(dice.Intn(20)+1)
		need := float64(dice.Intn(100)) / 100
		want := jaccard(left, right)
		got := jaccardAtLeast(left, right, need)
		if want >= need && got != want {
			t.Fatalf("need %.2f 위인데 값이 다르다 : %.4f ≠ %.4f", need, got, want)
		}
		if want < need && got != 0 && got != want {
			t.Fatalf("need 아래인데 엉뚱한 값이 났다 : %.4f (참값 %.4f)", got, want)
		}
	}
}
