package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

func TestJudgeCleanOldOnlyAndPreview(t *testing.T) {
	memory := newRepo(t)
	dir := filepath.Join(store.LocalDir(memory), judgeDirName)
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, strings.Repeat("a", 64)+".json")
	fresh := filepath.Join(dir, strings.Repeat("b", 64)+".json")
	odd := filepath.Join(dir, "note.json")
	for _, path := range []string{old, fresh, odd} {
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-40 * 24 * time.Hour)
	for _, path := range []string{old, odd, filepath.Join(dir, "sub")} {
		os.Chtimes(path, past, past)
	}
	out, code := capture(t, func() int { return run([]string{"judge", "clean"}) })
	if code != exitOK || !strings.Contains(out, "1건") {
		t.Fatalf("미리보기가 틀리다 (%d) : %s", code, out)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("미리보기인데 지웠다")
	}
	if _, code := capture(t, func() int { return run([]string{"judge", "clean", "--apply"}) }); code != exitOK {
		t.Fatalf("--apply 실패 : %d", code)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("오래된 기록이 남았다")
	}
	for _, path := range []string{fresh, odd, filepath.Join(dir, "sub")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s 가 지워졌다", path)
		}
	}
}

func TestJudgeCleanRefusesLinkedDir(t *testing.T) {
	memory := newRepo(t)
	target := t.TempDir()
	os.MkdirAll(store.LocalDir(memory), 0o755)
	if err := os.Symlink(target, filepath.Join(store.LocalDir(memory), judgeDirName)); err != nil {
		t.Skip("링크를 못 만든다 :", err)
	}
	if _, code := capture(t, func() int { return run([]string{"judge", "clean", "--apply"}) }); code != exitSecurity {
		t.Fatalf("링크 폴더는 종료 4 여야 한다 : %d", code)
	}
}

func TestJudgeCleanBadOlder(t *testing.T) {
	newRepo(t)
	for _, value := range []string{"0", "-3", "abc"} {
		if _, code := capture(t, func() int { return run([]string{"judge", "clean", "--older", value}) }); code != exitUsage {
			t.Fatalf("--older %s 는 사용법 오류여야 한다 : %d", value, code)
		}
	}
}
