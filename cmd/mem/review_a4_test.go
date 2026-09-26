package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// v0.4 리뷰 A R3 — 큐가 많이 밀려 있으면 색인이 오래 걸린다는 것을 먼저 말한다.
// `migrate --apply` 뒤 첫 `index` 가 20k 에서 6분인데 아무 말도 없어서 사람이
// 멈춘 줄 안다.
func TestBigQueueSaysItWillTakeLong(t *testing.T) {
	memory := newRepo(t)
	// 큐에 한 건을 남기려고 락을 쥔 채 add 한다.
	release := holdLock(t, memory)
	if _, code := capture(t, func() int { return run(addArgs("본문 한 줄이다")) }); code != 0 {
		t.Fatal("add 가 실패했다")
	}
	release()
	copyQueue(t, memory, queueNoticeMin+5)
	out, code := capture(t, func() int { return run([]string{"index"}) })
	if code != exitOK {
		t.Fatalf("index 가 실패했다 : %d %s", code, out)
	}
	if !strings.Contains(out, "밀려 있다") {
		t.Fatalf("오래 걸린다는 안내가 없다 : %s", out)
	}
}

// 큐가 몇 건뿐이면 아무 말도 안 한다 — 평소 회차가 시끄러워지면 안 된다.
func TestSmallQueueSaysNothing(t *testing.T) {
	newRepo(t)
	if _, code := capture(t, func() int { return run(addArgs("본문 한 줄이다")) }); code != 0 {
		t.Fatal("add 가 실패했다")
	}
	out, code := capture(t, func() int { return run([]string{"index"}) })
	if code != exitOK {
		t.Fatalf("index 가 실패했다 : %d %s", code, out)
	}
	if strings.Contains(out, "밀려 있다") {
		t.Fatalf("평소 회차에 안내가 떴다 : %s", out)
	}
}

// --json 과 --quiet 에는 사람 글이 한 줄도 안 섞인다.
func TestBigQueueNoticeStaysOutOfJSON(t *testing.T) {
	memory := newRepo(t)
	release := holdLock(t, memory)
	if _, code := capture(t, func() int { return run(addArgs("본문 한 줄이다")) }); code != 0 {
		t.Fatal("add 가 실패했다")
	}
	release()
	copyQueue(t, memory, queueNoticeMin+5)
	out, code := capture(t, func() int { return run([]string{"index", "--json"}) })
	if code != exitOK {
		t.Fatalf("index --json 이 실패했다 : %d %s", code, out)
	}
	if strings.Contains(out, "밀려 있다") {
		t.Fatalf("JSON 에 사람 글이 섞였다 : %s", out)
	}
}

// copyQueue 는 큐에 있는 요청 하나를 이름만 바꿔 여러 벌 늘린다.
func copyQueue(t *testing.T, memory string, want int) {
	t.Helper()
	dir := filepath.Join(memory, "inbox", "new")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("큐가 비었다 : %v %v", entries, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	for at := 0; at < want; at++ {
		name := filepath.Join(dir, fmt.Sprintf("20260105-b%07d.json", at))
		if err := os.WriteFile(name, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
