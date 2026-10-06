package fileio

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noTemp 는 폴더에 임시 파일이 안 남았는지 본다.
func noTemp(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), tempMark) {
			t.Fatalf("임시 파일이 남았다 : %s", entry.Name())
		}
	}
}

func readString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestReplaceWritesAndBacksUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	backup := filepath.Join(dir, "backup")
	os.WriteFile(path, []byte("old"), 0o644)
	if err := ReplaceFile(path, []byte("new"), []byte("old"), backup); err != nil {
		t.Fatal(err)
	}
	if got := readString(t, path); got != "new" {
		t.Fatalf("새 바이트가 아니다 : %q", got)
	}
	if got := readString(t, filepath.Join(backup, "AGENTS.md"+BackupSuffix)); got != "old" {
		t.Fatalf("백업에 옛 바이트가 없다 : %q", got)
	}
	noTemp(t, dir)
}

func TestReplaceRefusesChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	backup := filepath.Join(dir, "backup")
	os.WriteFile(path, []byte("someone else"), 0o644)
	err := ReplaceFile(path, []byte("new"), []byte("old"), backup)
	if !errors.Is(err, ErrChanged) {
		t.Fatalf("ErrChanged 여야 한다 : %v", err)
	}
	if got := readString(t, path); got != "someone else" {
		t.Fatalf("원본이 바뀌었다 : %q", got)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("백업이 생기면 안 된다 : %v", err)
	}
	noTemp(t, dir)
}

func TestReplaceMissingWithBeforeIsChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gone.txt")
	if err := ReplaceFile(path, []byte("new"), []byte("old"), ""); !errors.Is(err, ErrChanged) {
		t.Fatalf("없어진 파일은 ErrChanged 여야 한다 : %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("없던 파일을 만들면 안 된다")
	}
}

func TestReplaceCreatesNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.txt")
	backup := filepath.Join(dir, "backup")
	if err := ReplaceFile(path, []byte("hello"), nil, backup); err != nil {
		t.Fatal(err)
	}
	if got := readString(t, path); got != "hello" {
		t.Fatalf("새 파일 내용 : %q", got)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatal("새 파일에는 백업이 없어야 한다")
	}
	noTemp(t, dir)
}

func TestReplaceWithoutBackupDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	os.WriteFile(path, []byte("old"), 0o644)
	if err := ReplaceFile(path, []byte("new"), []byte("old"), ""); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || readString(t, path) != "new" {
		t.Fatalf("백업 없이 새 파일 하나만 있어야 한다 : %v", entries)
	}
}

func TestReplaceTwiceKeepsOneBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	backup := filepath.Join(dir, "backup")
	os.WriteFile(path, []byte("v1"), 0o644)
	if err := ReplaceFile(path, []byte("v2"), []byte("v1"), backup); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceFile(path, []byte("v3"), []byte("v2"), backup); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(backup)
	if len(entries) != 1 {
		t.Fatalf("백업은 한 벌이어야 한다 : %v", entries)
	}
	if got := readString(t, filepath.Join(backup, "a.txt"+BackupSuffix)); got != "v2" {
		t.Fatalf("백업은 직전 판이어야 한다 : %q", got)
	}
	noTemp(t, backup)
}

func TestReplaceRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	link := filepath.Join(dir, "link.txt")
	os.WriteFile(real, []byte("old"), 0o644)
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("링크를 못 만든다 : %v", err)
	}
	if err := ReplaceFile(link, []byte("new"), []byte("old"), ""); err == nil {
		t.Fatal("링크 대상은 거절해야 한다")
	}
	if got := readString(t, real); got != "old" {
		t.Fatalf("링크 너머 파일이 바뀌었다 : %q", got)
	}
	noTemp(t, dir)
}

func TestReplaceRefusesSymlinkBackupDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	elsewhere := filepath.Join(dir, "elsewhere")
	backup := filepath.Join(dir, "backup")
	os.WriteFile(path, []byte("old"), 0o644)
	os.Mkdir(elsewhere, 0o755)
	if err := os.Symlink(elsewhere, backup); err != nil {
		t.Skipf("링크를 못 만든다 : %v", err)
	}
	if err := ReplaceFile(path, []byte("new"), []byte("old"), backup); err == nil {
		t.Fatal("링크 백업 폴더는 거절해야 한다")
	}
	if got := readString(t, path); got != "old" {
		t.Fatalf("백업 실패인데 원본이 바뀌었다 : %q", got)
	}
}

func TestReplaceRefusesDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := ReplaceFile(dir, []byte("new"), nil, ""); err == nil {
		t.Fatal("폴더는 거절해야 한다")
	}
}
