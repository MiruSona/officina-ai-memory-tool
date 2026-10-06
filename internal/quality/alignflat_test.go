package quality

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// R1 ③ 시험 — 겹침 수 표로 낸 값이 옛 짝마다 병합한 값과 한 자리도 안 다른지 본다.

// alignOfReference 는 R1 ③ 앞의 alignOf 를 그대로 옮긴 것이다. 견줄 기준이다.
func alignOfReference(left, right *Doc, cut, need float64) alignInfo {
	found := alignInfo{}
	for at := range left.units {
		one := &left.units[at]
		for other := range right.units {
			two := &right.units[other]
			bound := unitBound(one, two)
			if bound <= found.Best && bound < cut {
				continue
			}
			if bound < need {
				continue
			}
			if popcount(one.fp^two.fp) > alignPrefilter {
				continue
			}
			score := gramSimAtLeast(one.grams, one.weight, one.total,
				two.grams, two.weight, two.total, need)
			if score >= cut {
				found.Pairs++
				strict, loose := factsClashUnits(one, two)
				found.Clash = found.Clash || strict
				found.ClashLoose = found.ClashLoose || loose
			}
			if score > found.Best {
				found.Best, found.Left, found.Right = score, one.text, two.text
			}
		}
	}
	return found
}

func popcount(value uint64) int {
	count := 0
	for ; value != 0; value &= value - 1 {
		count++
	}
	return count
}

// randomSet 은 0..space-1 에서 뽑은 정렬된 겹치지 않는 값이다. space 가 작으면 많이 겹친다.
func randomSet(rng *rand.Rand, size, space int) []uint64 {
	seen := map[uint64]bool{}
	out := []uint64{}
	for len(out) < size && len(out) < space {
		one := uint64(rng.Intn(space))
		if !seen[one] {
			seen[one] = true
			out = append(out, one)
		}
	}
	slices.Sort(out)
	return out
}

func TestJaccardFromSharedMatchesAtLeast(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	needs := []float64{0, -1, 0.15, 0.18, 0.5, 1, 0.0001}
	checked := 0
	for round := 0; round < 20000; round++ {
		left := randomSet(rng, rng.Intn(40), 10+rng.Intn(80))
		right := randomSet(rng, rng.Intn(40), 10+rng.Intn(80))
		if round%10 == 0 && len(right) > 0 {
			// 짧은 쪽이 긴 쪽의 앞자리와 같은 꼴 — 어긋난 걸음 없이 끝나는 자리다.
			left = slices.Clone(right[:rng.Intn(len(right))+1])
		}
		need := needs[round%len(needs)]
		if round%7 == 0 {
			need = rng.Float64()
		}
		shared := len(left) + len(right) - countUnion(left, right)
		want := jaccardAtLeast(left, right, need)
		got := jaccardFromShared(left, right, shared, need)
		if got != want {
			t.Fatalf("round %d need %v : got %v want %v (l=%v r=%v)", round, need, got, want, left, right)
		}
		checked++
	}
	t.Logf("짝 %d 개 같음", checked)
}

func countUnion(left, right []uint64) int {
	union := map[uint64]bool{}
	for _, one := range left {
		union[one] = true
	}
	for _, one := range right {
		union[one] = true
	}
	return len(union)
}

func TestSharedCountsMatchesPairwise(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for round := 0; round < 500; round++ {
		left := randomUnits(rng, 1+rng.Intn(maxUnits), 60)
		right := randomUnits(rng, 1+rng.Intn(maxUnits), 60)
		counts := make([]int32, len(left)*len(right))
		sharedCounts(flatOf(left), flatOf(right), len(right), counts)
		for at := range left {
			for other := range right {
				want := len(left[at].grams) + len(right[other].grams) -
					countUnion(left[at].grams, right[other].grams)
				if int(counts[at*len(right)+other]) != want {
					t.Fatalf("round %d (%d,%d) : got %d want %d", round, at, other, counts[at*len(right)+other], want)
				}
			}
		}
	}
}

func randomUnits(rng *rand.Rand, count, space int) []unit {
	out := make([]unit, count)
	for at := range out {
		out[at] = unit{text: fmt.Sprintf("조각 %d 값 %d", at, rng.Intn(5)), grams: randomSet(rng, 1+rng.Intn(30), space)}
	}
	return out
}

// TestAlignOfMatchesReference 는 낱말을 좁은 말뭉치에서 뽑은 글로 Doc 을 만들어,
// 자(cut·need)를 바꿔 가며 옛 판과 alignInfo 가 통째로 같은지 본다.
func TestAlignOfMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	words := strings.Fields("저장소 색인 기억 검색 결정 문턱 조각 지문 정렬 병합 버전 3.14 v0.2 " +
		"main.go 경로 이름 숫자 5000 시험 측정 lint 판 겹침 자카드 후보 문장 표 줄 끝")
	docs := make([]*Doc, 120)
	for at := range docs {
		docs[at] = NewDoc(&model.Memory{ID: fmt.Sprintf("m%03d", at),
			Title: randomText(rng, words, 1, 6), Summary: randomText(rng, words, 1, 8),
			Body: randomText(rng, words, 2+rng.Intn(70), 10)})
	}
	settings := [][2]float64{{AlignCut, alignNeed()}, {AlignCut, 0}, {0.3, 0.3}, {0.05, 0.02}}
	same, switched := 0, 0
	for at, left := range docs {
		for other := at; other < len(docs); other += 1 + rng.Intn(3) {
			right := docs[other]
			for _, set := range settings {
				want := alignOfReference(left, right, set[0], set[1])
				got := alignOf(left, right, set[0], set[1])
				if got != want {
					t.Fatalf("%s·%s cut %v need %v : got %+v want %+v", left.ID, right.ID, set[0], set[1], got, want)
				}
				same++
			}
			if len(left.units)*len(right.units) > 100 {
				switched++
			}
		}
	}
	if switched == 0 {
		t.Fatal("겹침 수 표로 바뀌는 큰 짝이 하나도 없다 — 시험이 새 길을 안 탄다")
	}
	t.Logf("견준 판 %d · 큰 짝 %d", same, switched)
}

// randomText 는 lines 줄짜리 글이다. 줄마다 낱말 1..width 개를 마침표로 끝낸다.
func randomText(rng *rand.Rand, words []string, lines, width int) string {
	out := []string{}
	for line := 0; line < lines; line++ {
		count := 1 + rng.Intn(width)
		picked := make([]string, count)
		for at := range picked {
			picked[at] = words[rng.Intn(len(words))]
		}
		out = append(out, strings.Join(picked, " ")+".")
	}
	return strings.Join(out, "\n")
}

// TestPairCounterTakesTable 는 큰 짝에서 정말 겹침 수 표 길로 가는지 본다.
func TestPairCounterTakesTable(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	words := strings.Fields("가람 나루 다솜 라온 마루 바다 사랑 아라 자람 차오 카누 타래")
	body := randomText(rng, words, 48, 8)
	left := NewDoc(&model.Memory{ID: "a", Body: body})
	right := NewDoc(&model.Memory{ID: "b", Body: body})
	counter := pairCounter{left: left, right: right}
	defer counter.release()
	for at := range left.units {
		for other := range right.units {
			counter.score(&left.units[at], &right.units[other], at, other, alignNeed())
		}
	}
	if counter.counts == nil {
		t.Fatal("짝을 다 돌았는데 겹침 수 표를 안 만들었다")
	}
}

func benchDocs() (*Doc, *Doc) {
	rng := rand.New(rand.NewSource(5))
	words := strings.Fields("저장소 색인 기억 검색 결정 문턱 조각 지문 정렬 병합 버전 경로 이름 숫자 시험 측정 " +
		"판 겹침 자카드 후보 문장 표 줄 끝 가람 나루 다솜 라온 마루 바다 사랑 아라 자람 차오 카누 타래")
	left := NewDoc(&model.Memory{ID: "a", Body: randomText(rng, words, 24, 9)})
	right := NewDoc(&model.Memory{ID: "b", Body: randomText(rng, words, 24, 9)})
	return left, right
}

func BenchmarkAlignOfReference(b *testing.B) {
	left, right := benchDocs()
	for b.Loop() {
		alignOfReference(left, right, AlignCut, alignNeed())
	}
}

func BenchmarkAlignOf(b *testing.B) {
	left, right := benchDocs()
	for b.Loop() {
		alignOf(left, right, AlignCut, alignNeed())
	}
}
