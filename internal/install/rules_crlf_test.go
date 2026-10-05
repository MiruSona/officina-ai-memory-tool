package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// AGENTS.md 가 CRLF 로 체크아웃돼도 init 은 멱등이다. 예전에는 LF 블록과
// 비교가 실패해 옛 블록으로 보고 잘라 붙이면서 블록 앞 빈 줄이 init 마다 늘었다.

func TestRulesBlockIdempotentAcrossLineEndings(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"LF", "# 규칙\n\n첫 줄\n둘째 줄\n"},
		{"CRLF", "# 규칙\r\n\r\n첫 줄\r\n둘째 줄\r\n"},
		{"섞임", "# 규칙\r\n\r\n첫 줄\n둘째 줄\r\n셋째 줄\n"},
	}
	for _, test := range cases {
		path := filepath.Join(t.TempDir(), "AGENTS.md")
		if err := os.WriteFile(path, []byte(test.text), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ensureRules(path, false); err != nil {
			t.Fatal(err)
		}
		once := readFile(t, path)
		for round := 0; round < 2; round++ {
			step, err := ensureRules(path, false)
			if err != nil {
				t.Fatal(err)
			}
			if step.Changed {
				t.Fatalf("%s : %d번째 init 이 또 고쳤다 (%s)", test.name, round+2, step.Now)
			}
		}
		if again := readFile(t, path); again != once {
			t.Fatalf("%s : 세 번 돌린 결과가 한 번 돌린 것과 다르다\n%q\n%q", test.name, once, again)
		}
		if !strings.HasPrefix(once, strings.TrimRight(test.text, "\r\n")) {
			t.Fatalf("%s : 원래 글이 바뀌었다 : %q", test.name, once)
		}
		if strings.Contains(once, "\n\n\n") || strings.Contains(once, "\r\n\r\n\r\n") {
			t.Fatalf("%s : 블록 앞 빈 줄이 둘 이상이다 : %q", test.name, once)
		}
	}
}

// 체크아웃으로 블록까지 CRLF 가 된 파일은 내용이 같으면 「그대로」 다.
func TestRulesBlockCRLFCheckoutKeeps(t *testing.T) {
	block := strings.ReplaceAll(i18n.InstallRulesBlock, "\n", "\r\n")
	text := "# 규칙\r\n\r\n첫 줄\r\n\r\n" + block
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	step, err := ensureRules(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if step.Changed || step.Todo != i18n.T(i18n.InitTodoKeep) {
		t.Fatalf("CRLF 블록인데 「그대로」 가 아니다 : %+v", step)
	}
	if readFile(t, path) != text {
		t.Fatal("「그대로」 인데 파일이 바뀌었다")
	}
}

// CRLF 파일에 붙이는 블록도 CRLF 다 — 한 파일에 줄 끝이 섞이지 않게.
func TestRulesBlockFollowsCRLF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(path, []byte("# 규칙\r\n첫 줄\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureRules(path, false); err != nil {
		t.Fatal(err)
	}
	text := readFile(t, path)
	if strings.Count(text, "\n") != strings.Count(text, "\r\n") {
		t.Fatalf("CRLF 파일에 LF 줄이 섞였다 : %q", text)
	}
}
