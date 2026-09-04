package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/gc"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
)

// set 은 add 처럼 큐에 넣기 전에 튕겨야 한다. 그전에는 `set --title <42자>` 가
// 「큐에 넣었다」 며 exit 0 이었다가 `index` 때에야 inbox/bad 로 조용히
// 떨어졌다 (오늘 겪은 것).
func TestSetRejectsBadTitleLength(t *testing.T) {
	memory := newRepo(t)
	id := queueAndIndex(t, "본문")
	longTitle := strings.Repeat("가", 42)
	out, code := capture(t, func() int { return run([]string{"set", id, "--title", longTitle}) })
	if code == exitOK {
		t.Fatalf("42자 제목이 통과했다 : %s", out)
	}
	entries, err := os.ReadDir(filepath.Join(memory, "inbox", "new"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("거절해야 하는데 큐에 들어갔다 : %v", entries)
	}
}

// summary 도 title 과 같은 자리(checkSetValue)를 지나므로 같이 확인한다.
func TestSetRejectsBadSummaryLength(t *testing.T) {
	memory := newRepo(t)
	id := queueAndIndex(t, "본문")
	shortSummary := strings.Repeat("나", 20)
	out, code := capture(t, func() int { return run([]string{"set", id, "--summary", shortSummary}) })
	if code == exitOK {
		t.Fatalf("20자 요약이 통과했다 : %s", out)
	}
	entries, err := os.ReadDir(filepath.Join(memory, "inbox", "new"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("거절해야 하는데 큐에 들어갔다 : %v", entries)
	}
}

// inbox/bad 로 떨어진 파일은 이유를 옆에 남기고, `--clear-bad` 가 보여준 뒤
// 이유 파일까지 같이 지운다.
func TestIndexBadFileGetsReasonAndClearRemovesIt(t *testing.T) {
	memory := newRepo(t)
	newDir := filepath.Join(memory, "inbox", "new")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "1.1.1.json"
	// 고칠 수 없는 칸(bogus)을 담아 승격이 반드시 실패하게 만든다.
	bad := `{"op":"patch","id":"20260101-aaaaaaaa","set":{"bogus":"x"}}`
	if err := os.WriteFile(filepath.Join(newDir, name), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code := capture(t, func() int { return run([]string{"index", "--quiet"}) }); code != exitOK {
		t.Fatalf("index 가 실패했다 : %d", code)
	}
	reasonPath := filepath.Join(memory, "local", "bad-reasons", name+".err")
	reason, err := os.ReadFile(reasonPath)
	if err != nil || len(strings.TrimSpace(string(reason))) == 0 {
		t.Fatalf("이유 파일이 없거나 비었다 : %v", err)
	}
	out, code := capture(t, func() int { return run([]string{"index", "--clear-bad"}) })
	if code != exitOK {
		t.Fatalf("--clear-bad 가 실패했다 : %s", out)
	}
	if !strings.Contains(out, name) || !strings.Contains(out, strings.TrimSpace(string(reason))) {
		t.Fatalf("이유가 화면에 안 보였다 : %s", out)
	}
	if _, err := os.Stat(reasonPath); !os.IsNotExist(err) {
		t.Fatalf("이유 파일이 안 지워졌다 : %v", err)
	}
	if _, err := os.Stat(filepath.Join(memory, "inbox", "bad", name)); !os.IsNotExist(err) {
		t.Fatalf("bad 파일이 안 지워졌다 : %v", err)
	}
}

// A8 — `--quiet` 도 `--json` 처럼 gc 조건을 본다. 예전에는 --quiet 인 회차만
// 도는 훅에서 gc 가 영영 안 걸렸다.
func TestQuietStillRunsGC(t *testing.T) {
	memory := newRepo(t)
	queueAndIndex(t, "본문 한 줄")
	out, code := capture(t, func() int { return run([]string{"index", "--gc", "--quiet"}) })
	if code != exitOK {
		t.Fatalf("index --gc --quiet 가 실패했다 : %d", code)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("--quiet 인데 글이 찍혔다 : %q", out)
	}
	db, err := index.Open(memory)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stamp, err := db.Meta(gc.MetaKey)
	if err != nil || stamp == "" {
		t.Fatal("--quiet 여도 gc 가 돌아야 하는데 안 돈 것 같다")
	}
}
