package quality

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// InParallel 은 0..count-1 을 토막으로 갈라 CPU 수만큼의 일꾼에게 맡긴다.
//
// 일꾼은 자기 토막만 보고 **결과를 자리 번호대로** 남긴다. 그래서 일꾼이
// 몇이든 나오는 표는 똑같다 — 규칙 검사는 기억 하나하나가 서로를 안 본다.
// 20k lint 가 규칙에만 18.5초를 쓰던 자리를 가르는 곳이다 (설계 G4 : 10초).
//
// 저장소를 고치는 쪽(--fix)·git 을 묻는 쪽은 여기 안 태운다. git 문답은 캐시와
// 호출 상한을 같이 쓰기 때문에 차례가 흐트러지면 답이 달라진다.
// **토막을 잘게 나눠 먼저 끝난 일꾼이 다음 토막을 집는다** (리뷰 B · V3).
// 일꾼마다 같은 크기를 미리 나눠 주면 무거운 기억이 몰린 토막 하나가 끝날
// 때까지 나머지가 논다 — 20k lint 에서 CPU 를 12갈래 중 7.8갈래만 썼다.
func InParallel(count int, work func(from, to int)) {
	if count <= 0 {
		return
	}
	workers := runtime.GOMAXPROCS(0)
	if workers > count {
		workers = count
	}
	if workers <= 1 {
		work(0, count)
		return
	}
	size := blockSize(count, workers)
	next := atomic.Int64{}
	wait := sync.WaitGroup{}
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for {
				from := int(next.Add(1)-1) * size
				if from >= count {
					return
				}
				to := from + size
				if to > count {
					to = count
				}
				work(from, to)
			}
		}()
	}
	wait.Wait()
}

// blockSize 는 한 번에 집는 크기다. 일꾼 수의 여덟 배쯤으로 잘라야 일이 고르게
// 퍼지고, 너무 잘게 자르면 토막마다 드는 자리(Session)를 다시 만드는 값이 든다.
func blockSize(count, workers int) int {
	size := (count + workers*8 - 1) / (workers * 8)
	if size < 1 {
		size = 1
	}
	return size
}
