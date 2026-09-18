package index

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// LockName 은 지금 누가 색인하고 있는지를 적는 파일이다.
const LockName = "index.lock"

// stalePrefix 는 옛 판이 죽은 락을 옮겨 놓던 이름이다. 지금은 안 만들지만
// 앞선 실행이 남긴 것은 치운다.
const stalePrefix = "index.lock.stale."

// takeoverSuffix 는 죽은 락을 치우는 동안만 잡는 인수 락이다. 치우는 사람을
// 하나로 줄여, **산 락이 잠깐이라도 경로에서 사라지는 일**을 없앤다.
const takeoverSuffix = ".takeover"

// staleAfter 는 락이 얼마나 앉아 있으면 주인이 죽었다고 보는지다. 1초 안에
// 답해야 하는 도구(불변조건 4)가 몇 분씩 락을 쥘 일은 없다.
const staleAfter = 90 * time.Second

// takeoverStaleAfter 는 인수 락의 만료다. 인수 락이 하는 일은 파일 하나
// 읽고 지우기뿐이라 밀리초면 끝난다. 쥔 채로 죽어도 금방 풀려야 한다.
const takeoverStaleAfter = 10 * time.Second

// takeoverTries 는 남이 치우는 중일 때 처음부터 다시 해 보는 횟수다. 여기서
// 너무 일찍 물러나면 **아무도 안 잡는** 일이 생긴다 — 죽은 락이 있을 때만
// 도는 길이라 조금 더 기다려도 된다.
const takeoverTries = 8

// takeoverWaitCap 은 그 사이 한 번에 기다리는 최대 시간이다. 다 합쳐도
// 70ms 쯤이라 훅 마감(600ms) 안에 든다.
const takeoverWaitCap = 20 * time.Millisecond

// removeTries 는 지우기가 막혔을 때 다시 해 보는 횟수다 (Windows 공유 위반).
const removeTries = 8

// writeGrace 는 O_EXCL 로 **막 만들어져 아직 한 줄이 안 적힌** 락 파일을 봐
// 주는 시간이다. 만들기와 쓰기 사이에 읽으면 빈 파일이라, 그냥 「못 읽었으니
// 죽었다」 로 보면 남의 **산 락**을 치워 버린다.
const writeGrace = 2 * time.Second

// lockInfo 는 락 파일에 적히는 세 값이다. PID 만으로는 재사용된 PID 를 산
// 주인으로 오인한다 (조사E #8).
type lockInfo struct {
	PID     int
	Started int64
	Taken   int64
	Host    string
}

func lockPath(dir string) string {
	return filepath.Join(dir, LockName)
}

// TryLock 은 하나뿐인 쓰기 락을 잡아 본다. 못 잡는 것은 오류가 아니라 흔한
// 일이다 — 남이 이미 색인하고 있으니 기다릴 이유가 없다.
func TryLock(dir string) (func(), bool, error) {
	path := lockPath(dir)
	for attempt := 0; attempt < takeoverTries; attempt++ {
		release, taken, err := createLock(path)
		if taken || err != nil {
			return release, taken, err
		}
		cleared, busy := clearStaleLock(path)
		if cleared {
			return createLock(path)
		}
		if !busy {
			// 산 락이다. 기다릴 이유가 없다.
			return nil, false, nil
		}
		// 남이 죽은 락을 치우는 중이다. 아주 잠깐 뒤 처음부터 다시 본다.
		wait := time.Millisecond << attempt
		if wait > takeoverWaitCap {
			wait = takeoverWaitCap
		}
		time.Sleep(wait)
	}
	return nil, false, nil
}

func createLock(path string) (func(), bool, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if os.IsExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	host, _ := os.Hostname()
	started, _ := processStart(os.Getpid())
	mine := lockInfo{PID: os.Getpid(), Started: started, Taken: time.Now().Unix(), Host: host}
	_, err = file.WriteString(formatLock(mine))
	file.Close()
	if err != nil {
		os.Remove(path)
		return nil, false, err
	}
	// 내 락일 때만 지운다. 남이 이미 새로 잡은 락을 지우면 둘이 동시에
	// 주인이 된다 (리뷰 A #4).
	return func() { removeOwn(path, mine) }, true, nil
}

// formatLock 은 락 파일 한 줄을 만든다.
func formatLock(info lockInfo) string {
	return fmt.Sprintf("%d %d %d %s\n", info.PID, info.Started, info.Taken, info.Host)
}

// removeOwn 은 파일에 적힌 것이 아직 내가 쓴 그 락일 때만 지운다.
func removeOwn(path string, mine lockInfo) {
	info, ok := readLock(path)
	if !ok || !sameLock(info, mine) {
		return
	}
	removeRetry(path)
}

// removeRetry 는 지운다. Windows 는 **남이 그 파일을 읽고 있는 동안** 지우기가
// 「다른 프로세스가 쓰는 중」으로 잠깐 막힌다. 흔한 일이라 몇 번 더 해 본다 —
// 여기서 그냥 물러나면 내 락이 남아 90초 동안 남을 막는다.
func removeRetry(path string) error {
	var err error
	for try := 0; try < removeTries; try++ {
		err = os.Remove(path)
		if err == nil || os.IsNotExist(err) {
			return nil
		}
		time.Sleep(time.Millisecond)
	}
	return err
}

// sameLock 은 락 파일 두 판이 같은 주인인지 본다.
func sameLock(left, right lockInfo) bool {
	return left.PID == right.PID && left.Started == right.Started &&
		left.Taken == right.Taken && left.Host == right.Host
}

// lockState 는 지금 그 자리에 있는 락이 어떤 꼴인지다.
const (
	lockLive    = iota // 산 락 — 안 건드린다
	lockDead           // 죽은 락 — 치워도 된다
	lockGone           // 없다 — 치울 것도 없다
	lockWriting        // 남이 방금 만들어 아직 쓰는 중 — 잠깐 뒤 다시 본다
)

// lockShape 는 락 파일을 읽어 위 넷 중 어느 것인지 고른다. 만료는 골라서
// 넣는다 — 인수 락은 훨씬 짧게 본다.
func lockShape(path string, limit time.Duration) (int, lockInfo) {
	info, ok := readLock(path)
	if ok {
		if abandonedAfter(info, limit) {
			return lockDead, info
		}
		return lockLive, info
	}
	stat, err := os.Stat(path)
	if err != nil {
		return lockGone, lockInfo{}
	}
	// 만들기(O_EXCL)와 한 줄 쓰기 사이에 읽으면 빈 파일이다. 이것을 죽은
	// 락으로 보면 남의 **산 락**을 치워 버린다.
	if time.Since(stat.ModTime()) < writeGrace {
		return lockWriting, lockInfo{}
	}
	// 반쯤 쓰이다 만 파일이 영원히 막으면 안 된다.
	return lockDead, lockInfo{}
}

// clearStaleLock 은 아무도 안 쥔 락을 치운다. 실제 짝짓기는 createLock 의
// O_EXCL 이 한다 — 여기서 하는 일은 「죽은 락을 치워도 되는가」 뿐이다.
//
// 옛 판은 락 파일을 rename 으로 옮겨 놓고 대조했는데, 그 사이 남이 만든
// **산 락**이 잠깐 경로에서 사라져 그 틈에 또 하나가 O_EXCL 로 락을 잡았다.
// 그래서 지금은 인수 락(`index.lock.takeover`)으로 치우는 사람을 하나로
// 줄이고, **산 락은 어떤 경우에도 경로에서 움직이지 않는다.**
//
// 돌려주는 값은 (치웠다, 남이 치우는 중이다) 이다.
func clearStaleLock(path string) (bool, bool) {
	switch shape, _ := lockShape(path, staleAfter); shape {
	case lockLive:
		return false, false
	case lockWriting:
		return false, true
	}
	takeover := path + takeoverSuffix
	release, mine, got := takeTakeover(takeover)
	if !got {
		return false, true
	}
	defer release()
	// 인수 락 안에서 다시 본다. 기다리는 사이 남이 치우고 산 락을 만들어
	// 놨을 수 있다.
	shape, _ := lockShape(path, staleAfter)
	if shape == lockLive {
		return false, false
	}
	if shape == lockWriting {
		return false, true
	}
	if shape == lockGone {
		// 이미 남이 치웠다. **여기서 지우면 안 된다** — 그 사이 남이 만든 산
		// 락을 지우게 된다.
		return true, false
	}
	// 지우기 직전에 인수 락이 아직 내 것인지 본다.
	if held, heldOK := readLock(takeover); !heldOK || !sameLock(held, mine) {
		return false, true
	}
	// 여기 오면 파일은 **죽은 락 그대로** 다. 죽은 락을 치우는 사람은 인수 락을
	// 쥔 나 하나라, 읽은 뒤 지우기 전에 산 락으로 바뀔 수가 없다.
	if err := removeRetry(path); err != nil {
		// 지우기가 막힌 것은 산 락이라는 뜻이 아니다. 처음부터 다시 본다.
		return false, true
	}
	return true, false
}

// takeTakeover 는 인수 락을 잡아 본다. 인수 락을 쥔 채 죽을 수도 있으니 같은
// 규칙(PID·시작시각·호스트)으로 보되 만료는 아주 짧다.
func takeTakeover(path string) (func(), lockInfo, bool) {
	if release, mine, taken := createTakeover(path); taken {
		return release, mine, true
	}
	shape, info := lockShape(path, takeoverStaleAfter)
	if shape == lockLive || shape == lockWriting {
		// 남이 쥐고 있다. 방금 만들어져 아직 안 쓰인 것도 남의 것이다 —
		// 여기서 지우면 산 인수 락을 치우게 된다.
		return nil, lockInfo{}, false
	}
	if shape == lockDead {
		// 내가 본 그 죽은 인수 락일 때만 지운다.
		if info.PID > 0 {
			removeOwn(path, info)
		} else {
			removeRetry(path)
		}
	}
	release, mine, taken := createTakeover(path)
	return release, mine, taken
}

// createTakeover 는 인수 락을 만들고 거기 적은 내용까지 돌려준다.
func createTakeover(path string) (func(), lockInfo, bool) {
	release, taken, err := createLock(path)
	if err != nil || !taken {
		return nil, lockInfo{}, false
	}
	mine, ok := readLock(path)
	if !ok {
		release()
		return nil, lockInfo{}, false
	}
	return release, mine, true
}

// readLock 은 락 파일을 읽는다. 못 읽으면 죽은 것으로 본다 — 반쯤 쓰인 파일이
// 영원히 막으면 안 된다.
func readLock(path string) (lockInfo, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return lockInfo{}, false
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return lockInfo{}, false
	}
	info := lockInfo{}
	info.PID, _ = strconv.Atoi(fields[0])
	info.Started, _ = strconv.ParseInt(fields[1], 10, 64)
	info.Taken, _ = strconv.ParseInt(fields[2], 10, 64)
	if len(fields) > 3 {
		info.Host = fields[3]
	}
	return info, info.PID > 0
}

// abandoned 는 락을 뺏어도 되는지 본다. PID 가 살아 있고 **시작시각까지 같아야**
// 산 락이다.
func abandoned(info lockInfo) bool {
	return abandonedAfter(info, staleAfter)
}

// abandonedAfter 는 만료를 골라서 보는 판이다. 인수 락은 훨씬 짧게 본다.
func abandonedAfter(info lockInfo, limit time.Duration) bool {
	waited := time.Since(time.Unix(info.Taken, 0))
	// 시계가 앞선 기계가 남긴 락은 time.Since 가 음수라 영영 안 늙는다.
	// 미래에 잡힌 락은 그 자체로 이상한 락이다 (리뷰 A #16).
	if waited < -limit {
		return true
	}
	host, err := os.Hostname()
	if err != nil || info.Host != host {
		// 남의 기계 락은 주인이 사는지 볼 길이 없다. 시간으로만 본다.
		return waited >= limit
	}
	// 주인이 살아 있으면 오래 쥐고 있어도 산 락이다. `mem index --full` 은
	// 몇 분씩 걸리는데 시간만 보고 뺏으면 승격이 두 번 돈다 (리뷰 A1).
	started, known := processStart(info.PID)
	if known {
		return started != info.Started
	}
	if processAlive(info.PID) {
		return false
	}
	return true
}

// clearStaleFiles 는 앞선 실행이 남긴 lock.stale.* 를 치운다.
func clearStaleFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), stalePrefix) {
			// 지우기에 실패해도 다음 실행이 또 해 본다.
			os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}
