package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// newProject 는 가짜 홈과 빈 프로젝트 폴더를 만든다. 진짜 홈·PATH·.claude 는
// 절대 안 건드린다.
func newProject(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("PATH", os.Getenv("PATH"))
	return t.TempDir()
}

func mustInit(t *testing.T, options Options) *Report {
	t.Helper()
	report, err := Init(options)
	if err != nil {
		t.Fatalf("init 실패 : %v", err)
	}
	return report
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s 를 못 읽었다 : %v", path, err)
	}
	return string(data)
}

// 빈 폴더에 두 번 init : 두 번째는 아무것도 안 바꾼다.
func TestInitTwiceChangesNothing(t *testing.T) {
	root := newProject(t)
	first := mustInit(t, Options{Root: root})
	if first.Changed() == 0 {
		t.Fatal("첫 init 이 아무것도 안 했다")
	}
	for _, name := range []string{
		filepath.Join(config.DirName, "store"),
		filepath.Join(config.DirName, "inbox", "tmp"),
		filepath.Join(config.DirName, "inbox", "new"),
		filepath.Join(config.DirName, "inbox", "bad"),
		filepath.Join(config.DirName, "local"),
		filepath.Join(config.DirName, "archive"),
		filepath.Join(config.DirName, "golden"),
		filepath.Join(config.DirName, config.FileName),
		filepath.Join(config.DirName, i18n.InstallUsageName),
		".gitignore", ".gitattributes", "AGENTS.md",
		filepath.Join(".claude", "settings.json"),
	} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("%s 가 안 생겼다", name)
		}
	}
	before := snapshot(t, root)
	second := mustInit(t, Options{Root: root})
	if second.Changed() != 0 {
		t.Errorf("두 번째 init 이 %d 가지를 바꿨다", second.Changed())
	}
	if after := snapshot(t, root); after != before {
		t.Error("두 번째 init 이 파일 내용을 바꿨다")
	}
}

// snapshot 은 폴더 전체를 한 글로 만든다. 두 번째 실행이 바꾼 게 있으면 달라진다.
func snapshot(t *testing.T, root string) string {
	t.Helper()
	out := strings.Builder{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out.WriteString(rel + "\n" + string(data) + "\n")
		return nil
	})
	if err != nil {
		t.Fatalf("훑기 실패 : %v", err)
	}
	return out.String()
}

// --dry-run 은 아무 파일도 안 만든다.
func TestInitDryRunTouchesNothing(t *testing.T) {
	root := newProject(t)
	report := mustInit(t, Options{Root: root, DryRun: true})
	if report.Changed() == 0 {
		t.Error("dry-run 이 할 일을 하나도 안 세었다")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("dry-run 이 %d 개를 만들었다", len(entries))
	}
	table := strings.Join(report.Lines(), "\n")
	for _, want := range []string{i18n.T(i18n.InitStepStore), config.FileName,
		i18n.InstallUsageName, ".gitignore", "settings.json", "AGENTS.md"} {
		if !strings.Contains(table, want) {
			t.Errorf("dry-run 표에 %s 줄이 없다\n%s", want, table)
		}
	}
}

// 남의 훅이 든 settings.json 은 그 훅을 그대로 남긴다.
func TestSettingsKeepsOtherHooks(t *testing.T) {
	root := newProject(t)
	path := ClaudeSettingsPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{
  "permissions": { "allow": ["Bash(ls)"] },
  "hooks": {
    "SessionStart": [
      { "matcher": "startup", "hooks": [ { "type": "command", "command": "other-tool", "args": ["go"] } ] }
    ]
  }
}
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	mustInit(t, Options{Root: root})
	text := readFile(t, path)
	for _, want := range []string{"other-tool", "permissions", "Bash(ls)", `"command": "mem"`,
		`"timeout": 10`, "startup|resume|clear|compact|fork"} {
		if !strings.Contains(text, want) {
			t.Errorf("settings.json 에 %s 가 없다\n%s", want, text)
		}
	}
	// 백업은 쓰기가 끝나면 치운다. .claude/ 는 git 이 추적하는 자리라
	// 사람이 넣어 둔 로컬 설정이 통째로 커밋되면 안 된다 (리뷰 A #8).
	if _, err := os.Stat(path + backupSuffix); err == nil {
		t.Error("백업 파일이 남았다")
	}
	// 키 순서를 지켰는지 : permissions 가 hooks 보다 앞이어야 한다.
	if strings.Index(text, "permissions") > strings.Index(text, `"hooks"`) {
		t.Error("키 순서가 뒤집혔다")
	}
}

// --undo 뒤에도 남의 훅은 남고 Memory/ 는 그대로다.
func TestUndoKeepsOtherHookAndMemory(t *testing.T) {
	root := newProject(t)
	path := ClaudeSettingsPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{"hooks":{"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"other-tool","args":["go"]}]}]}}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	mustInit(t, Options{Root: root})
	report, err := Undo(Options{Root: root})
	if err != nil {
		t.Fatalf("undo 실패 : %v", err)
	}
	if report.Changed() != 2 {
		t.Errorf("undo 가 %d 가지를 뗐다 (훅 + 규칙 = 2 여야 한다)", report.Changed())
	}
	text := readFile(t, path)
	if !strings.Contains(text, "other-tool") {
		t.Error("남의 훅이 사라졌다")
	}
	if strings.Contains(text, `"mem"`) {
		t.Errorf("mem 훅이 안 빠졌다\n%s", text)
	}
	rules := readFile(t, filepath.Join(root, "AGENTS.md"))
	if strings.Contains(rules, i18n.InstallRulesHeading) {
		t.Error("규칙 블록이 안 빠졌다")
	}
	if _, err := os.Stat(filepath.Join(root, config.DirName, config.FileName)); err != nil {
		t.Error("undo 가 Memory/ 를 지웠다")
	}
	// 두 번째 undo 는 뗄 것이 없다.
	again, err := Undo(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if again.Changed() != 0 {
		t.Errorf("두 번째 undo 가 %d 가지를 건드렸다", again.Changed())
	}
}

// 사용법.md 는 있으면 안 덮는다.
func TestUsageDocIsNeverOverwritten(t *testing.T) {
	root := newProject(t)
	dir := filepath.Join(root, config.DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mine := "# 우리 팀이 고친 사용법\n"
	path := filepath.Join(dir, i18n.InstallUsageName)
	if err := os.WriteFile(path, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	mustInit(t, Options{Root: root})
	if got := readFile(t, path); got != mine {
		t.Errorf("사용법.md 를 덮어썼다 : %q", got)
	}
}

// mem.toml 은 빠진 키만 채우고 사람이 고친 값은 그대로 둔다.
func TestConfigFillsOnlyMissingKeys(t *testing.T) {
	root := newProject(t)
	dir := filepath.Join(root, config.DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, config.FileName)
	old := "schema = 1\nname = \"우리것\"\n\n[pin]\nmax = 3\n"
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	mustInit(t, Options{Root: root})
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "우리것" {
		t.Errorf("name 이 바뀌었다 : %s", loaded.Name)
	}
	if loaded.Pin.Max != 3 {
		t.Errorf("사람이 고친 pin.max 가 바뀌었다 : %d", loaded.Pin.Max)
	}
	if left := config.MissingKeys(readFile(t, path)); len(left) != 0 {
		t.Errorf("아직 빠진 키가 있다 : %v", left)
	}
}

// 규칙 블록은 두 번 붙지 않는다. AGENTS.md 가 있으면 CLAUDE.md 는 안 만든다.
func TestRulesBlockAppendsOnce(t *testing.T) {
	root := newProject(t)
	agents := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(agents, []byte("# 팀 규칙\n\n- 하나\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustInit(t, Options{Root: root})
	mustInit(t, Options{Root: root})
	text := readFile(t, agents)
	if count := strings.Count(text, i18n.InstallRulesHeading); count != 1 {
		t.Errorf("규칙 블록이 %d 번 붙었다", count)
	}
	if !strings.Contains(text, "# 팀 규칙") {
		t.Error("원래 내용이 사라졌다")
	}
	for _, want := range []string{"mem search", "mem add", i18n.InstallUsagePath} {
		if !strings.Contains(text, want) {
			t.Errorf("규칙 블록에 %s 가 없다", want)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); err == nil {
		t.Error("AGENTS.md 가 있는데 CLAUDE.md 를 만들었다")
	}
}

// AGENTS.md 가 없고 CLAUDE.md 만 있으면 거기에 붙인다.
func TestRulesGoIntoClaudeWhenNoAgents(t *testing.T) {
	root := newProject(t)
	claude := filepath.Join(root, "CLAUDE.md")
	if err := os.WriteFile(claude, []byte("# 규칙\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustInit(t, Options{Root: root})
	if !strings.Contains(readFile(t, claude), i18n.InstallRulesHeading) {
		t.Error("CLAUDE.md 에 규칙 블록이 없다")
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); err == nil {
		t.Error("CLAUDE.md 가 있는데 AGENTS.md 를 만들었다")
	}
}

// .gitignore 와 .gitattributes 는 원래 내용을 남기고 빠진 줄만 붙인다.
func TestGitFilesKeepWhatWasThere(t *testing.T) {
	root := newProject(t)
	ignore := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(ignore, []byte("bin/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustInit(t, Options{Root: root})
	text := readFile(t, ignore)
	if !strings.Contains(text, "bin/") {
		t.Error("원래 .gitignore 줄이 사라졌다")
	}
	for _, want := range i18n.InstallIgnoreLines {
		if !strings.Contains(text, want) {
			t.Errorf(".gitignore 에 %s 가 없다", want)
		}
	}
	attributes := readFile(t, filepath.Join(root, ".gitattributes"))
	for _, want := range i18n.InstallAttributeLines {
		if !strings.Contains(attributes, want) {
			t.Errorf(".gitattributes 에 %q 가 없다", want)
		}
	}
	mustInit(t, Options{Root: root})
	if count := strings.Count(readFile(t, ignore), i18n.InstallIgnoreOpen); count != 1 {
		t.Errorf(".gitignore 블록이 %d 번 붙었다", count)
	}
	if count := strings.Count(readFile(t, filepath.Join(root, ".gitattributes")), "*.jsonl.gz"); count != 1 {
		t.Errorf(".gitattributes 줄이 %d 번 붙었다", count)
	}
}

// --gemini 는 .gemini/settings.json 에 같은 훅을 밀리초 timeout 으로 쓴다.
func TestGeminiSettings(t *testing.T) {
	root := newProject(t)
	mustInit(t, Options{Root: root, Gemini: true})
	text := readFile(t, GeminiSettingsPath(root))
	for _, want := range []string{`"SessionStart"`, `"command": "mem"`,
		`"hook"`, `"session-start"`, `"timeout": 60000`} {
		if !strings.Contains(text, want) {
			t.Errorf(".gemini/settings.json 에 %s 가 없다\n%s", want, text)
		}
	}
	if strings.Contains(readFile(t, ClaudeSettingsPath(root)), "60000") {
		t.Error("Claude 쪽 timeout 이 밀리초로 적혔다")
	}
	// 두 번째 실행은 아무것도 안 바꾼다.
	again := mustInit(t, Options{Root: root, Gemini: true})
	if again.Changed() != 0 {
		t.Errorf("두 번째 --gemini init 이 %d 가지를 바꿨다", again.Changed())
	}
}

// --no-hook 은 settings.json 을 아예 안 만든다.
func TestNoHookSkipsSettings(t *testing.T) {
	root := newProject(t)
	mustInit(t, Options{Root: root, NoHook: true})
	if _, err := os.Stat(ClaudeSettingsPath(root)); err == nil {
		t.Error("--no-hook 인데 settings.json 을 만들었다")
	}
}

// 모르는 모양의 settings.json 은 손대지 않고 「손으로」 로 넘긴다.
func TestOddSettingsIsLeftAlone(t *testing.T) {
	root := newProject(t)
	path := ClaudeSettingsPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	odd := `{"hooks": "이건 배열이 아니다"}`
	if err := os.WriteFile(path, []byte(odd), 0o644); err != nil {
		t.Fatal(err)
	}
	report := mustInit(t, Options{Root: root})
	if got := readFile(t, path); got != odd {
		t.Errorf("모르는 모양인데 파일을 고쳤다 : %s", got)
	}
	if !strings.Contains(strings.Join(report.Lines(), "\n"), i18n.T(i18n.InitStateOddShape)) {
		t.Error("모르는 모양이라고 알리지 않았다")
	}
}
