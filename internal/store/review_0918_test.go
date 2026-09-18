package store

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// 리뷰 A3 — 같은 때 도는 여러 mem 이 기록을 적어도 줄이 사라지면 안 된다.
// 통째로 다시 쓰던 판은 읽기와 쓰기 사이에 남이 적은 줄을 덮었다.
func TestAppendLogKeepsConcurrentLines(t *testing.T) {
	dir := t.TempDir()
	const count = 40
	group := sync.WaitGroup{}
	for i := 0; i < count; i++ {
		group.Add(1)
		go func(n int) {
			defer group.Done()
			if err := AppendLog(dir, time.Now(), LogAdded, fmt.Sprintf("줄 %02d", n)); err != nil {
				t.Errorf("기록에 실패했다 : %v", err)
			}
		}(i)
	}
	group.Wait()
	text, err := ReadLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < count; i++ {
		if !strings.Contains(text, fmt.Sprintf("줄 %02d\n", i)) {
			t.Fatalf("줄 %02d 가 사라졌다 (남은 줄 %d개)", i,
				strings.Count(text, "- "+LogAdded))
		}
	}
}

// 날짜 절은 하루에 한 번만 붙고, 줄은 그 밑에 쌓인다.
func TestAppendLogKeepsOneHeadingPerDay(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 9, 18, 10, 0, 0, 0, time.Local)
	for i := 0; i < 3; i++ {
		if err := AppendLog(dir, at, LogAdded, fmt.Sprintf("줄 %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	text, err := ReadLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(text, "## 2026-09-18"); got != 1 {
		t.Fatalf("날짜 절이 %d 개다 :\n%s", got, text)
	}
	if got := strings.Count(text, "- "+LogAdded); got != 3 {
		t.Fatalf("줄이 %d 개다 :\n%s", got, text)
	}
}
