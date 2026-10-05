package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
)

// init 이 빠진 것을 채울 때 mem.toml · vocab.toml 의 손 주석을 안 지운다
// (mem issue 20261005-ebe02fad).

func writeMemoryFile(t *testing.T, root, name, text string) string {
	t.Helper()
	dir := filepath.Join(root, config.DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInitKeepsConfigComments(t *testing.T) {
	root := newProject(t)
	base := string(config.Encode(config.Default("시험")))
	text := strings.Replace(base, "[hook]\n", "[hook]\n# 손으로 단 주석\n", 1)
	text = strings.Replace(text, "max_bytes = ", "# max_bytes 는 지웠다\nmax_byte_gone = ", 1)
	text = "\ufeff" + strings.ReplaceAll(text, "\n", "\r\n")
	path := writeMemoryFile(t, root, config.FileName, text)
	mustInit(t, Options{Root: root, NoHook: true})
	got := readFile(t, path)
	if !strings.HasPrefix(got, "\ufeff") {
		t.Fatal("BOM 이 사라졌다")
	}
	for _, line := range []string{"# 손으로 단 주석\r\n", "# max_bytes 는 지웠다\r\n"} {
		if !strings.Contains(got, line) {
			t.Fatalf("손 주석 %q 이 사라졌다", line)
		}
	}
	if left := config.MissingKeys(strings.TrimPrefix(got, "\ufeff")); len(left) != 0 {
		t.Fatalf("빠진 키가 남았다 : %v", left)
	}
	mustInit(t, Options{Root: root, NoHook: true})
	if again := readFile(t, path); again != got {
		t.Fatal("두 번째 init 이 또 고쳤다")
	}
}

func TestInitKeepsVocabComments(t *testing.T) {
	root := newProject(t)
	text := "# 우리 팀 낱말 — 손 주석\n[tag]\n\"search\" = [\"ranking\"]  # 줄 끝 주석\n\n[scope]\n\"mem\" = []\n"
	path := writeMemoryFile(t, root, config.VocabFileName, text)
	mustInit(t, Options{Root: root, NoHook: true})
	got := readFile(t, path)
	if !strings.HasPrefix(got, text) {
		t.Fatalf("있던 줄이 바뀌었다 :\n%s", got)
	}
	if left := config.MissingVocabKeys(got); len(left) != 0 {
		t.Fatalf("빠진 절이 남았다 : %v", left)
	}
	loaded, err := config.ParseVocab(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.TagDeny) == 0 {
		t.Fatal("빈 [tag.deny] 에 씨앗을 안 줬다")
	}
}
