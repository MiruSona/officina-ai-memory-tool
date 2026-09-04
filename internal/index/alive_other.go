//go:build !windows

package index

import (
	"errors"
	"syscall"
)

// processAlive 는 신호 0 으로 물어보기만 한다. 신호를 못 보내는 프로세스도
// 도는 프로세스다.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// processStart 는 이 판에서는 모른다. 모르면 PID 생존 여부만 본다.
func processStart(pid int) (int64, bool) {
	return 0, false
}
