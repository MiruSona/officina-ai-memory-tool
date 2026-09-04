package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 리뷰 A #9 — 남의 settings.json 모양(BOM·CRLF)은 그대로 둔다. 훅 한 줄 붙인
// 것이 파일 전체 diff 로 뜨면 안 된다.
func TestSettingsKeepsBOMAndCRLF(t *testing.T) {
	root := newProject(t)
	path := ClaudeSettingsPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	before := "\ufeff{\r\n  \"permissions\": {\r\n    \"allow\": [\"Bash(ls)\"]\r\n  }\r\n}\r\n"
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	mustInit(t, Options{Root: root})
	after := readFile(t, path)
	if !strings.HasPrefix(after, "\ufeff") {
		t.Error("BOM 이 사라졌다")
	}
	if !strings.Contains(after, "\r\n") {
		t.Error("줄끝이 LF 로 바뀌었다")
	}
	if strings.Contains(strings.ReplaceAll(after, "\r\n", "\n"), "\r") {
		t.Error("줄끝이 섞였다")
	}
	if !strings.Contains(after, `"command": "mem"`) {
		t.Error("훅이 안 붙었다")
	}
}

// 리뷰 A #8 — init 이 만든 .gitignore 블록이 우리가 남기는 부스러기를 덮는다.
func TestIgnoreCoversOurLeftovers(t *testing.T) {
	root := newProject(t)
	mustInit(t, Options{Root: root})
	text := readFile(t, filepath.Join(root, ".gitignore"))
	for _, want := range []string{".claude/*.mem-bak", "Memory/index.lock.stale.*"} {
		if !strings.Contains(text, want) {
			t.Errorf(".gitignore 에 %s 가 없다", want)
		}
	}
}

// 리뷰 A #18 — 사용자 PATH 의 앞뒤 `;` 는 남의 것이다. 안 건드린다.
func TestPathKeepsUserSeparators(t *testing.T) {
	if got := withFront(";C:\\a;C:\\b;", "C:\\mem"); got != "C:\\mem;C:\\a;C:\\b;" {
		t.Fatalf("남의 값을 손댔다 : %s", got)
	}
	if got := withFront("C:\\a", "C:\\mem"); got != "C:\\mem;C:\\a" {
		t.Fatalf("붙이는 꼴이 틀렸다 : %s", got)
	}
	if got := withFront("", "C:\\mem"); got != "C:\\mem" {
		t.Fatalf("빈 PATH 를 잘못 다뤘다 : %s", got)
	}
}
