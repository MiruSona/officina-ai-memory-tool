package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// 코드리뷰 10-05 에서 나온 init 흠들이다.

// `[retain] # 자동` 처럼 머리에 주석이 붙어도 이미 있는 절이다. 하나 더 붙이면
// 뒤 절이 사람이 고친 값을 기본값으로 덮는다.
func TestRetainHeaderWithComment(t *testing.T) {
	for _, header := range []string{"[retain] # 자동", "[ retain ]", "[retain]\r"} {
		if !hasRetainHeader("schema = 2\n" + header + "\nnudge = false\n") {
			t.Fatalf("%q 를 [retain] 으로 못 본다", header)
		}
	}
	if hasRetainHeader("# [retain] 은 아직 안 켰다\n") {
		t.Fatal("주석 속 [retain] 을 머리로 봤다")
	}
}

// 옛 이름 `[repo] pin_max` 만 있는 mem.toml 에 init 해도 33 이 그대로다.
func TestInitKeepsOldPinName(t *testing.T) {
	root := newProject(t)
	path := writeMemoryFile(t, root, config.FileName, "schema = 2\nname = \"x\"\n[repo]\npin_max = 33\n")
	mustInit(t, Options{Root: root, NoHook: true})
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Repo.PinMax != 33 {
		t.Fatalf("pin_max 33 이 %d 로 바뀌었다", loaded.Repo.PinMax)
	}
}

// [search] 머리에 주석이 붙은 mem.toml 에 init 해도 rrf_k 99 가 그대로고, 두 번째는 안 고친다.
func TestInitKeepsValueUnderCommentedHeader(t *testing.T) {
	root := newProject(t)
	base := string(config.Encode(config.Default("시험")))
	base = strings.Replace(base, "rrf_k = 10\n", "rrf_k = 99\n", 1)
	base = strings.Replace(base, "mix_keep = ", "# mix_keep 지움\nmix_keep_gone = ", 1)
	text := strings.Replace(base, "[search]\n", "[search] # 검색 손잡이\n", 1)
	path := writeMemoryFile(t, root, config.FileName, text)
	mustInit(t, Options{Root: root, NoHook: true})
	first := readFile(t, path)
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Search.RRFK != 99 {
		t.Fatalf("rrf_k 99 가 %v 로 덮였다", loaded.Search.RRFK)
	}
	mustInit(t, Options{Root: root, NoHook: true})
	if readFile(t, path) != first {
		t.Fatal("두 번째 init 이 또 고쳤다")
	}
}

// AGENTS.md 의 BOM 은 init 뒤에도 남는다.
func TestRulesKeepBOM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(path, []byte("\ufeff# 규칙\n\n본문\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureRules(path, false); err != nil {
		t.Fatal(err)
	}
	text := readFile(t, path)
	if !strings.HasPrefix(text, "\ufeff# 규칙\n\n본문\n") {
		t.Fatalf("BOM 이나 원래 글이 사라졌다 : %q", text)
	}
	if strings.Count(text, "\ufeff") != 1 {
		t.Fatalf("BOM 이 둘이 됐다 : %q", text)
	}
}

// 옛 블록만 CRLF 로 든 AGENTS.md 를 갈아 끼우면 결과도 CRLF 다.
func TestRulesOldBlockOnlyKeepsCRLF(t *testing.T) {
	old := i18n.InstallBlockOpen + "\r\n옛 문안\r\n" + i18n.InstallBlockClose + "\r\n"
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureRules(path, false); err != nil {
		t.Fatal(err)
	}
	text := readFile(t, path)
	if !strings.Contains(toLF(text), i18n.InstallRulesBlock) {
		t.Fatalf("새 블록이 안 들었다 : %q", text)
	}
	if strings.Count(text, "\n") != strings.Count(text, "\r\n") {
		t.Fatalf("CRLF 파일이 LF 가 됐다 : %q", text)
	}
}
