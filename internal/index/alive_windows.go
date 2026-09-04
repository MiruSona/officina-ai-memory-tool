//go:build windows

package index

import "syscall"

// processQueryLimited 는 PROCESS_QUERY_LIMITED_INFORMATION 이다. 프로세스가
// 살아 있는지만 물어보는 데 필요한 가장 작은 권한이라 직접 값을 쓴다.
const processQueryLimited = 0x1000

// stillActive 는 STILL_ACTIVE — 아직 안 끝난 프로세스의 종료 코드다.
const stillActive = 259

// processAlive 는 PID 가 도는 프로세스인지 본다. 접근 거부는 남의 것이라는
// 뜻이라 살아 있는 것으로 센다 — 죽었다고 증명된 것만 락을 뺏는다.
func processAlive(pid int) bool {
	handle, err := syscall.OpenProcess(processQueryLimited, false, uint32(pid))
	if err != nil {
		return err == syscall.ERROR_ACCESS_DENIED
	}
	defer syscall.CloseHandle(handle)
	code := uint32(0)
	if err := syscall.GetExitCodeProcess(handle, &code); err != nil {
		return true
	}
	return code == stillActive
}

// processStart 는 프로세스가 언제 시작했는지다. PID 는 재사용되지만 PID +
// 시작시각은 안 겹친다 (조사E #8).
func processStart(pid int) (int64, bool) {
	handle, err := syscall.OpenProcess(processQueryLimited, false, uint32(pid))
	if err != nil {
		return 0, false
	}
	defer syscall.CloseHandle(handle)
	created, exited, kernel, user := syscall.Filetime{}, syscall.Filetime{}, syscall.Filetime{}, syscall.Filetime{}
	if err := syscall.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return 0, false
	}
	return created.Nanoseconds(), true
}
