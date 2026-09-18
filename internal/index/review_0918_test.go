package index

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 리뷰 A1 — `mem index --full` 은 몇 분씩 걸린다. 주인이 살아 있으면 90초가
// 지나도 산 락이다. 시간만 보고 뺏으면 승격이 두 번 돈다.
func TestLongHeldOwnLockIsNotStale(t *testing.T) {
	host, err := os.Hostname()
	if err != nil {
		t.Skip("호스트 이름을 못 읽는다")
	}
	started, _ := processStart(os.Getpid())
	long := lockInfo{PID: os.Getpid(), Started: started,
		Taken: time.Now().Add(-10 * time.Minute).Unix(), Host: host}
	if abandoned(long) {
		t.Fatal("몇 분째 도는 내 락을 죽었다고 봤다")
	}
	dir := t.TempDir()
	if err := os.WriteFile(lockPath(dir), []byte(formatLock(long)), 0o644); err != nil {
		t.Fatal(err)
	}
	release, taken, err := TryLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if taken {
		release()
		t.Fatal("오래 쥔 산 락을 뺏었다")
	}
}

// 리뷰 A4 — 규격을 어긴 파일은 고칠 때까지 판마다 알려야 한다. mtime·size 를
// 적어 두면 다음 판이 Skipped 로 조용히 넘긴다.
func TestBadFileIsCountedEveryRun(t *testing.T) {
	opened := newStore(t)
	dir := filepath.Join(opened.Dir, "store", "2026", "01")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := "---\nid: 20260105-eeee1111\n---\n\n머리말이 모자란 파일이다.\n"
	path := filepath.Join(dir, "20260105-eeee1111.md")
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	// 방금 쓴 파일은 「거친 mtime」 구간이라 늘 다시 읽는다. 그 구간을 벗어난
	// 파일이라야 조용히 넘기는 길이 열린다.
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	first := runIndex(t, opened)
	if first.Bad != 1 {
		t.Fatalf("첫 판이 규격 위반을 못 셌다 : %+v", first)
	}
	second := runIndex(t, opened)
	if second.Bad != 1 {
		t.Fatalf("둘째 판이 조용히 넘겼다 : %+v", second)
	}
}

// 리뷰 A7 — 예산을 넘겨 멈춰도 이미 치운 큐 이름의 승격 표시는 지워야 한다.
// 안 지우면 inbox_seen 이 30일 동안 index.db 에 앉는다.
func TestOverBudgetStillForgetsArchived(t *testing.T) {
	opened := newStore(t)
	const count = 20
	for i := 0; i < count; i++ {
		if _, err := opened.WriteAdd(addRequest(longSummary, fmt.Sprintf("본문 %02d", i))); err != nil {
			t.Fatal(err)
		}
	}
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	partial := false
	for round := 0; round < 30; round++ {
		before, err := opened.ListInbox()
		if err != nil {
			t.Fatal(err)
		}
		if len(before) == 0 {
			break
		}
		result := database.CatchUpBy(opened, settings().GC, settings().Secret,
			time.Now().Add(3*time.Millisecond))
		after, err := opened.ListInbox()
		if err != nil {
			t.Fatal(err)
		}
		left := map[string]bool{}
		for _, name := range after {
			left[name] = true
		}
		for _, name := range before {
			if left[name] {
				continue
			}
			seen, err := database.seenInbox(name)
			if err != nil {
				t.Fatal(err)
			}
			if seen {
				t.Fatalf("큐에서 치운 %s 의 승격 표시가 남았다 (판 %d)", name, round)
			}
		}
		if result.OverBudget && len(after) < len(before) {
			partial = true
		}
	}
	if !partial {
		t.Log("예산을 넘기고도 일부는 처리한 판을 못 봤다 — 이 판은 시험이 약하다")
	}
}
