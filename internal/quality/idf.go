package quality

import "math"

// IDF 가중 (설계 결정 34).
//
// 문턱 0.08/0.06 이 낮은 것은 `기억`·`저장소` 처럼 어느 기억에나 있는 말이 다
// 겹쳐 값을 뭉개기 때문이다. 조각마다 「저장소에서 몇 건에 나오나」로 무게를
// 주면 흔한 말은 거의 안 세고 드문 말이 값을 낸다.
//
// **식 모양(S = 내용 + 곁 × 내용/0.80)은 안 바꾼다.** 무게는 내용 몫을 재는
// 바이그램 겹침 안에만 들어간다 — 곁(태그·scope)이 혼자 문턱에 못 닿게 하는
// 성질이 v0.2 리뷰 B 의 대조군 거짓 경보 3 → 0 을 만든 자국이라서다.
//
// 무게는 표를 다 채운 뒤 한 번만 재고 **조각 차례에 맞춘 배열**로 든다. 짝마다
// map 을 뒤지면 20k 에서 그 조회가 그대로 시간이다 (리뷰 B 의 정렬 지문과 같은 꼴).

// weightsFor 는 조각 차례에 맞춘 무게 배열과 그 합이다. 무게는 **평균이 1이
// 되게 나눠** 둔다 — 그래야 자 범위와 문턱(dup_reject·dup_warn)이 무게 없던
// 때와 같은 자리에 남는다. 안 그러면 점수 전체가 통째로 움직여 문턱이 뜻을 잃는다.
func weightsFor(grams []uint64, df map[uint64]int32, docs int, mean float32) ([]float32, float32) {
	if len(grams) == 0 {
		return nil, 0
	}
	if mean <= 0 {
		mean = 1
	}
	out := make([]float32, len(grams))
	total := float32(0)
	for at, one := range grams {
		weight := idfOf(df[one], docs) / mean
		out[at] = weight
		total += weight
	}
	return out, total
}

// meanIDF 는 저장소에 나온 모든 조각의 무게 평균이다.
func meanIDF(df map[uint64]int32, docs int) float32 {
	if len(df) == 0 {
		return 1
	}
	sum, count := float32(0), float32(0)
	for _, one := range df {
		sum += idfOf(one, docs) * float32(one)
		count += float32(one)
	}
	if count == 0 {
		return 1
	}
	return sum / count
}

// idfOf 는 조각 하나의 무게다. 바닥을 idfFloor 로 둬서 아주 흔한 조각도 0 이
// 되지는 않게 한다 — 0 이면 그 조각만으로 이뤄진 짧은 글의 자카드가 0/0 이 된다.
func idfOf(df int32, docs int) float32 {
	if df < 1 {
		df = 1
	}
	weight := math.Log(float64(docs+1) / float64(df))
	if weight < idfFloor {
		weight = idfFloor
	}
	return float32(weight)
}

// useIDF 는 IDF 가중을 켜는지다. **기본은 꺼져 있다.**
//
// 설계 결정 34 는 IDF 를 넣으라고 했지만 **골든셋 130건에서 재 보니 값을 잃는다.**
// IDF 는 점수 자를 통째로 아래로 옮겨서, 문턱을 같이 내려 조건을 맞춰도
// 「대조군 거짓 경보 0」 자리에서의 DUP 재현율이 **0.778 → 0.667** 로 떨어진다
// (`TestSweepIDFFair` 표. 무가중 0.08/0.06 vs IDF 0.07/0.05 가 각각 그 둘의 가장 좋은 자리다).
// 이 저장소는 「같은 회차에 같은 말투로 쓴 서로 다른 발견」이 많아, 드문 낱말에
// 무게를 실으면 **진짜 중복 쌍의 흔한 말 겹침이 같이 죽는다.**
//
// 셈법은 그대로 두고 스위치만 꺼 둔다 — 저장소가 커지면 다시 재 보면 된다.
var useIDF = false

// useAlignIDF 는 **문장 정렬 쪽에만** IDF 무게를 싣는지다. 이쪽도 **꺼 둔다** —
// `TestSweepAlignIDF` 에서 거짓 경보 0 자리의 DUP 재현율이 0.778 → 0.667 로
// 떨어졌다. 짧은 조각(12~40글자)에서는 무게가 몇 개 조각에 쏠려 값이 튄다.
var useAlignIDF = false

// idfFloor 는 무게 바닥이다. 저장소 전부에 나오는 조각도 이만큼은 센다.
const idfFloor = 0.05

// weightedJaccard 는 무게를 실은 겹침 비율이다. 두 조각열은 정렬돼 있고 무게
// 배열은 같은 차례라 병합하며 한 번에 센다.
func weightedJaccard(left []uint64, leftW []float32, leftTotal float32,
	right []uint64, rightW []float32, rightTotal float32) float64 {
	shared := float32(0)
	at, other := 0, 0
	for at < len(left) && other < len(right) {
		switch {
		case left[at] == right[other]:
			shared += leftW[at]
			at++
			other++
		case left[at] < right[other]:
			at++
		default:
			other++
		}
	}
	union := leftTotal + rightTotal - shared
	if union <= 0 {
		return 0
	}
	return float64(shared / union)
}
