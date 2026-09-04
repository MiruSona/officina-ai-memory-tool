package store

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// #14 A rename that fails must not leave the half queued file in inbox/tmp.
func TestFailedRenameCleansTmp(t *testing.T) {
	memoryStore := Open(filepath.Join(t.TempDir(), "Memory"), false)
	if err := memoryStore.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	// A folder where the queued file must land: the rename cannot succeed.
	old := queueName
	queueName = func() string { return "막힌자리.json" }
	defer func() { queueName = old }()
	if err := os.MkdirAll(filepath.Join(memoryStore.InboxNewDir(), "막힌자리.json"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := memoryStore.WriteAdd(AddRequest{Type: "history", Summary: "요약", Tags: []string{"mem"},
		Source: "ai", Scope: "mem", Body: "본문"}); err == nil {
		t.Fatal("the rename must fail")
	}
	left, err := os.ReadDir(memoryStore.InboxTmpDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("inbox/tmp must be left clean, found %d files", len(left))
	}
}

// #1 Windows also says ERROR_ACCESS_DENIED when another process holds a file,
// and that is worth a retry like the other two.
func TestAccessDeniedIsRetried(t *testing.T) {
	for _, errno := range []syscall.Errno{5, 32, 33} {
		if !isHeldByOther(&os.PathError{Op: "rename", Err: errno}) {
			t.Errorf("errno %d must count as held by somebody else", errno)
		}
	}
	if isHeldByOther(&os.PathError{Op: "open", Err: syscall.Errno(2)}) {
		t.Error("a missing file must not be retried")
	}
	if isHeldByOther(nil) {
		t.Error("no error is no retry")
	}
}
