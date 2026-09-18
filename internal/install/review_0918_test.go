package install

import (
	"path/filepath"
	"strings"
	"testing"
)

// 리뷰 A5 — `--undo` 는 우리 bin 폴더만 뺀다. 백업 글을 통째로 되쓰면 설치
// 뒤에 남이 더한 PATH 항목이 통째로 사라진다.
func TestUndoKeepsEntriesAddedAfterInstall(t *testing.T) {
	home := newHome(t)
	fake := &fakeRegistry{value: PathValue{Text: `C:\Tools`, Expand: true}}
	defer UseRegistry(fake)()
	if _, err := Install(Options{Apply: true, NoEmbed: true}); err != nil {
		t.Fatalf("install 실패 : %v", err)
	}
	// 설치한 뒤에 다른 프로그램이 제 폴더를 PATH 에 더했다.
	fake.value.Text += `;C:\NewTool`
	if _, err := Uninstall(Options{Apply: true}); err != nil {
		t.Fatalf("undo 실패 : %v", err)
	}
	if listedIn(fake.value.Text, filepath.Join(home, rootDirName, "bin")) {
		t.Fatalf("우리 폴더가 안 빠졌다 : %s", fake.value.Text)
	}
	if !strings.Contains(fake.value.Text, `C:\NewTool`) {
		t.Fatalf("설치 뒤에 더한 항목이 사라졌다 : %s", fake.value.Text)
	}
	if !strings.Contains(fake.value.Text, `C:\Tools`) {
		t.Fatalf("원래 항목이 사라졌다 : %s", fake.value.Text)
	}
}
