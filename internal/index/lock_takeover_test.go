package index

import (
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 죽은 락 문구 하나로 여러 시험이 같이 쓴다.
func deadLockLine(t *testing.T) string {
	t.Helper()
	return "999999 12345 1 " + hostName(t) + "\n"
}

// singleThread 는 고루틴을 한 줄로 세운다. 락 경쟁 버그는 CPU 를 하나로
// 묶었을 때 훨씬 잘 나온다 (2026-08-23 재현).
func singleThread(t *testing.T) {
	t.Helper()
	old := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(old) })
}

// 죽은 락을 열여섯이 동시에 치워도 잡는 사람은 하나여야 한다.
// (옛 판은 산 락을 잠깐 rename 으로 치워, 그 틈에 둘이 잡았다)
func TestStaleLockRaceHasOneWinnerUnderLoad(t *testing.T) {
	singleThread(t)
	for round := 0; round < 60; round++ {
		dir := t.TempDir()
		if err := os.WriteFile(lockPath(dir), []byte(deadLockLine(t)), 0o644); err != nil {
			t.Fatal(err)
		}
		holds := []func(){}
		guard := sync.Mutex{}
		group := sync.WaitGroup{}
		start := make(chan struct{})
		for racer := 0; racer < 16; racer++ {
			group.Add(1)
			go func() {
				defer group.Done()
				<-start
				release, taken, err := TryLock(dir)
				if err != nil || !taken {
					return
				}
				guard.Lock()
				holds = append(holds, release)
				guard.Unlock()
			}()
		}
		close(start)
		group.Wait()
		for _, release := range holds {
			release()
		}
		if len(holds) != 1 {
			t.Fatalf("라운드 %d — 죽은 락을 잡은 사람이 하나가 아니다 : %d", round, len(holds))
		}
	}
}

// 한 번 만들어진 **산 락은 경로에서 사라지지도 바뀌지도 않는다.** 이것이
// 깨지면 다른 사람이 그 틈에 O_EXCL 로 락을 또 만든다.
func TestLiveLockNeverLeavesPath(t *testing.T) {
	singleThread(t)
	for round := 0; round < 30; round++ {
		dir := t.TempDir()
		if err := os.WriteFile(lockPath(dir), []byte(deadLockLine(t)), 0o644); err != nil {
			t.Fatal(err)
		}
		moved := atomic.Bool{}
		stop := make(chan struct{})
		watch := sync.WaitGroup{}
		watch.Add(1)
		go func() {
			defer watch.Done()
			live := lockInfo{}
			seen := false
			for {
				select {
				case <-stop:
					return
				default:
				}
				runtime.Gosched()
				info, ok := readLock(lockPath(dir))
				switch {
				case ok && info.PID == os.Getpid() && !seen:
					live, seen = info, true
				case seen && (!ok || !sameLock(info, live)):
					moved.Store(true)
					return
				}
			}
		}()

		holds := []func(){}
		guard := sync.Mutex{}
		group := sync.WaitGroup{}
		start := make(chan struct{})
		for racer := 0; racer < 8; racer++ {
			group.Add(1)
			go func() {
				defer group.Done()
				<-start
				release, taken, err := TryLock(dir)
				if err != nil || !taken {
					return
				}
				guard.Lock()
				holds = append(holds, release)
				guard.Unlock()
			}()
		}
		close(start)
		group.Wait()
		close(stop)
		watch.Wait()
		for _, release := range holds {
			release()
		}
		if moved.Load() {
			t.Fatalf("라운드 %d — 산 락이 경로에서 움직였다", round)
		}
		if len(holds) != 1 {
			t.Fatalf("라운드 %d — 잡은 사람이 하나가 아니다 : %d", round, len(holds))
		}
	}
}

// 인수 락을 쥔 채 죽은 사람이 있어도 다음 사람이 이어서 치운다.
func TestDeadTakeoverLockIsCleared(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(lockPath(dir), []byte(deadLockLine(t)), 0o644); err != nil {
		t.Fatal(err)
	}
	takeover := lockPath(dir) + takeoverSuffix
	if err := os.WriteFile(takeover, []byte(deadLockLine(t)), 0o644); err != nil {
		t.Fatal(err)
	}
	release, taken, err := TryLock(dir)
	if err != nil || !taken {
		t.Fatalf("만료된 인수 락은 치우고 잡아야 한다 : %v %v", taken, err)
	}
	defer release()
	if _, err := os.Stat(takeover); err == nil {
		t.Fatal("인수 락 파일이 남았다")
	}
}

// 남이 인수 락을 쥐고 있으면 죽은 락에 손대지 않고 물러난다.
func TestLiveTakeoverLockBlocksClearing(t *testing.T) {
	dir := t.TempDir()
	dead := deadLockLine(t)
	if err := os.WriteFile(lockPath(dir), []byte(dead), 0o644); err != nil {
		t.Fatal(err)
	}
	// 지금 이 프로세스가 쥔 것처럼 적으면 산 인수 락이다.
	started, _ := processStart(os.Getpid())
	host, _ := os.Hostname()
	line := formatLock(lockInfo{PID: os.Getpid(), Started: started, Taken: time.Now().Unix(), Host: host})
	takeover := lockPath(dir) + takeoverSuffix
	if err := os.WriteFile(takeover, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if release, taken, err := TryLock(dir); taken || err != nil {
		if taken {
			release()
		}
		t.Fatalf("남이 치우는 중이면 물러나야 한다 : %v %v", taken, err)
	}
	left, err := os.ReadFile(lockPath(dir))
	if err != nil || string(left) != dead {
		t.Fatalf("죽은 락에 손댔다 : %q %v", string(left), err)
	}
}
