package quality

import (
	"math"
	"math/rand"
	"testing"
)

// v0.4 갈래 C2 — **빠르게 고친 자리가 답을 안 바꾸는지** 못 박는 시험이다.
// 규칙의 뜻을 바꿔서 빨라지는 것은 안 된다 (설계 결정 14).

// slowJaccardAtLeast 는 고치기 전 셈법 그대로다. 견줄 자로만 쓴다.
func slowJaccardAtLeast(left, right []uint64, need float64) float64 {
	if need <= 0 {
		return jaccard(left, right)
	}
	total := len(left) + len(right)
	floor := int(math.Ceil(need*float64(total)/(1+need) - 1e-9))
	shared, at, other := 0, 0, 0
	for at < len(left) && other < len(right) {
		if left[at] == right[other] {
			shared++
			at++
			other++
			continue
		}
		if left[at] < right[other] {
			at++
		} else {
			other++
		}
		if shared+min(len(left)-at, len(right)-other) < floor {
			return 0
		}
	}
	union := total - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

// sortedSample 은 정렬·중복 제거된 지문 배열이다 (grams 가 주는 꼴).
func sortedSample(rnd *rand.Rand, size, spread int) []uint64 {
	seen := map[uint64]bool{}
	out := []uint64{}
	for len(out) < size {
		one := uint64(rnd.Intn(spread))
		if seen[one] {
			continue
		}
		seen[one] = true
		out = append(out, one)
	}
	for at := 1; at < len(out); at++ {
		one := out[at]
		to := at
		for to > 0 && out[to-1] > one {
			out[to] = out[to-1]
			to--
		}
		out[to] = one
	}
	return out
}

// TestJaccardAtLeastSame 은 건너뛰기 판이 한 칸씩 걷던 판과 **값이 같은지**다.
func TestJaccardAtLeastSame(t *testing.T) {
	rnd := rand.New(rand.NewSource(7))
	for round := 0; round < 20000; round++ {
		spread := 1 + rnd.Intn(400)
		left := sortedSample(rnd, rnd.Intn(min(spread, 120)), spread)
		right := sortedSample(rnd, rnd.Intn(min(spread, 120)), spread)
		for _, need := range []float64{0, 0.1, 0.15, 0.18, 0.5} {
			want := slowJaccardAtLeast(left, right, need)
			got := jaccardAtLeast(left, right, need)
			if want != got {
				t.Fatalf("need %.2f 에서 값이 다르다 : %v ≠ %v\n왼 %v\n오 %v", need, got, want, left, right)
			}
		}
	}
}
