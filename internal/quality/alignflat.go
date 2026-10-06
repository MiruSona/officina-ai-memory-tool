package quality

import (
	"math"
	"slices"
	"sync"
)

// 짝 조각 겹침 수를 한 번에 세는 길 (R1 ③). 조각 지문을 한 줄로 펴 두고 두 줄을
// 한 번 병합하면 모든 짝의 겹침 수가 나온다. 근거는 진행상황 「lint 속도」 절.

// 조각 자리를 한 바이트에 담는다. maxUnits 가 256 을 넘으면 여기서 빌드가 깨진다.
const _ = uint8(maxUnits - 1)

// unitFlat 은 조각 지문을 값 차례로 한 줄로 편 것이다. owner 는 그 지문이 든 조각
// 자리다. 한 조각 안 지문은 겹치지 않아 같은 값은 조각마다 한 번씩만 나온다.
type unitFlat struct {
	grams []uint64
	owner []uint8
}

// flatOf 는 조각들의 지문을 값 차례로 편다.
func flatOf(units []unit) unitFlat {
	size := 0
	for at := range units {
		size += len(units[at].grams)
	}
	if size == 0 {
		return unitFlat{}
	}
	type entry struct {
		gram  uint64
		owner uint8
	}
	entries := make([]entry, 0, size)
	for at := range units {
		for _, gram := range units[at].grams {
			entries = append(entries, entry{gram, uint8(at)})
		}
	}
	slices.SortFunc(entries, func(one, two entry) int {
		switch {
		case one.gram < two.gram:
			return -1
		case one.gram > two.gram:
			return 1
		}
		return int(one.owner) - int(two.owner)
	})
	out := unitFlat{grams: make([]uint64, size), owner: make([]uint8, size)}
	for at, one := range entries {
		out.grams[at], out.owner[at] = one.gram, one.owner
	}
	return out
}

// sharedCounts 는 모든 조각 짝의 겹침 수를 counts[왼쪽*width+오른쪽] 에 더한다.
// 같은 값이 여러 조각에 있으면 그 값의 왼쪽 무리 × 오른쪽 무리 짝에 하나씩 센다.
func sharedCounts(left, right unitFlat, width int, counts []int32) {
	at, other := 0, 0
	for at < len(left.grams) && other < len(right.grams) {
		one, two := left.grams[at], right.grams[other]
		if one != two {
			// 어긋난 쪽 옮기기를 분기 대신 셈으로 푼다 — 지문은 고른 값이라 갈래 예측이 반은 빗나간다.
			step := 0
			if one < two {
				step = 1
			}
			at += step
			other += 1 - step
			continue
		}
		leftEnd, rightEnd := at+1, other+1
		for leftEnd < len(left.grams) && left.grams[leftEnd] == one {
			leftEnd++
		}
		for rightEnd < len(right.grams) && right.grams[rightEnd] == one {
			rightEnd++
		}
		for _, owner := range left.owner[at:leftEnd] {
			row := counts[int(owner)*width:]
			for _, slot := range right.owner[other:rightEnd] {
				row[slot]++
			}
		}
		at, other = leftEnd, rightEnd
	}
}

// jaccardFromShared 는 겹침 수 shared 로 jaccardAtLeast(left, right, need) 와 **같은 값**을 낸다.
// 그쪽이 0 을 내는 것은 「어긋난 걸음이 한 번이라도 있었고 끝 겹침이 floor 아래」일 때뿐이다.
func jaccardFromShared(left, right []uint64, shared int, need float64) float64 {
	total := len(left) + len(right)
	if need > 0 {
		floor := int(math.Ceil(need*float64(total)/(1+need) - 1e-9))
		if shared < floor && !mergeAllMatch(left, right, shared) {
			return 0
		}
	}
	union := total - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

// mergeAllMatch 는 병합이 어긋난 걸음 없이 끝나는지다 — 짧은 쪽이 긴 쪽의 앞자리와 똑같다.
func mergeAllMatch(left, right []uint64, shared int) bool {
	short := min(len(left), len(right))
	return shared == short && slices.Equal(left[:short], right[:short])
}

// pairCounter 는 alignOf 한 번의 짝 점수를 낸다. 짝마다 병합한 걸음이 펴 둔 두 줄
// 길이를 넘으면 겹침 수 표로 바꾼다. 어느 길이든 값은 같고 빠르기만 다르다.
type pairCounter struct {
	left, right *Doc
	spent       int
	counts      *[maxUnits * maxUnits]int32
}

var countsPool = sync.Pool{New: func() any { return new([maxUnits * maxUnits]int32) }}

// score 는 gramSimAtLeast(one, two, need) 와 같은 값이다. at·other 는 두 조각 자리다.
func (pc *pairCounter) score(one, two *unit, at, other int, need float64) float64 {
	if one.weight != nil && two.weight != nil {
		return weightedJaccard(one.grams, one.weight, one.total, two.grams, two.weight, two.total)
	}
	if pc.counts == nil {
		pc.spent += len(one.grams) + len(two.grams)
		if pc.spent <= len(pc.left.flat.grams)+len(pc.right.flat.grams) || !pc.flatReady() {
			return gramSimAtLeast(one.grams, one.weight, one.total, two.grams, two.weight, two.total, need)
		}
		pc.build()
	}
	shared := int(pc.counts[at*len(pc.right.units)+other])
	return jaccardFromShared(one.grams, two.grams, shared, need)
}

// flatReady 는 두 기억 다 펴 둔 줄이 있는지다. NewDoc 을 안 거친 Doc 은 없다.
func (pc *pairCounter) flatReady() bool {
	return pc.left.flat.grams != nil && pc.right.flat.grams != nil
}

func (pc *pairCounter) build() {
	pc.counts = countsPool.Get().(*[maxUnits * maxUnits]int32)
	width := len(pc.right.units)
	cells := pc.counts[:len(pc.left.units)*width]
	clear(cells)
	sharedCounts(pc.left.flat, pc.right.flat, width, cells)
}

func (pc *pairCounter) release() {
	if pc.counts != nil {
		countsPool.Put(pc.counts)
		pc.counts = nil
	}
}
