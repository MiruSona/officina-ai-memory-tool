package embed

// 벡터를 못 만든 건을 알리는 자리 (리뷰 B · H-1).
//
// 한 건이 죽어도 색인은 계속 간다. 그래도 **말없이 넘어가지는 않는다** —
// 처음 몇 줄만 stderr 에 찍고 나머지는 수만 센다. 2만 건 중 절반이 죽으면
// 화면이 만 줄로 덮이는 것이 더 나쁘다.

import (
	"fmt"
	"os"
	"sync"
)

// warnMax 는 화면에 찍을 줄 수다. 넘으면 수만 센다.
const warnMax = 5

var (
	warnLock  sync.Mutex
	warnCount int
)

// Warn 은 건너뛴 건 하나를 알린다.
func Warn(text string) {
	warnLock.Lock()
	defer warnLock.Unlock()
	warnCount++
	if warnCount <= warnMax {
		fmt.Fprintln(os.Stderr, "  "+text)
	}
}

// WarnCount 는 지금까지 건너뛴 건 수다. Build 가 보고에 적는다.
func WarnCount() int {
	warnLock.Lock()
	defer warnLock.Unlock()
	return warnCount
}

// ResetWarn 은 셈을 0 으로 돌린다. 시험이 쓴다.
func ResetWarn() {
	warnLock.Lock()
	defer warnLock.Unlock()
	warnCount = 0
}
