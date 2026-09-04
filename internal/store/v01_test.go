package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// 조사E #9 — 가운데 폴더가 링크면 문자열 검사만으로는 밖으로 나간다.
func TestAbsRefusesSymlinkedFolder(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "store", "2026")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := linkDir(outside, link); err != nil {
		t.Skipf("이 기계에서는 링크를 못 만든다 : %v", err)
	}
	store := Open(root, false)
	if _, err := store.Abs("store/2026/08/20260822-3f9a2c1b.md"); err == nil {
		t.Fatal("링크로 밖을 가리키는 경로를 막아야 한다")
	}
	if _, err := store.Abs("store/2027/08/20260822-3f9a2c1b.md"); err != nil {
		t.Fatalf("링크가 아닌 경로는 통과해야 한다 : %v", err)
	}
}

// linkDir 는 폴더 링크를 만든다. 윈도우에서 심볼릭 링크는 권한이 있어야 하지만
// 정션(mklink /J)은 권한 없이도 만들어지고 EvalSymlinks 가 똑같이 푼다.
func linkDir(target, link string) error {
	err := os.Symlink(target, link)
	if err == nil || runtime.GOOS != "windows" {
		return err
	}
	return exec.Command("cmd", "/c", "mklink", "/J", link, target).Run()
}
