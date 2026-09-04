package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadTestdata(t *testing.T) {
	config, err := Load(filepath.Join("..", "..", "testdata", "config", FileName))
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if config.Schema != 2 || config.Name != "게임A" {
		t.Fatalf("unexpected head keys: %+v", config)
	}
	if config.Tokenizer != Tokenizer {
		t.Fatalf("unexpected tokenizer: %s", config.Tokenizer)
	}
	if len(config.Secret.Patterns) != 3 || config.Secret.Patterns[2] != "a,b" {
		t.Fatalf("comma inside a quoted item must survive: %v", config.Secret.Patterns)
	}
	if config.GC.WarmCount != 1500 || config.GC.WarmDays != 45 || config.GC.ColdDays != 200 {
		t.Fatalf("unexpected gc values: %+v", config.GC)
	}
	if config.GC.ColdCount != Default("").GC.ColdCount {
		t.Fatal("a missing key must fall back to the default")
	}
	if config.Budget.Startup != 900 || config.Budget.Resume != 300 || config.Budget.Clear != 1000 {
		t.Fatalf("unexpected budget: %+v", config.Budget)
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	want := Default("게임A")
	text := string(Encode(want))
	if strings.Contains(text, "\r") || strings.HasPrefix(text, "\ufeff") {
		t.Fatal("mem.toml must be LF without a BOM")
	}
	got, err := Parse(text)
	if err != nil {
		t.Fatalf("reparse failed: %v\n%s", err, text)
	}
	if got.Name != want.Name || got.Schema != want.Schema || got.Tokenizer != want.Tokenizer {
		t.Fatalf("head keys changed: %+v", got)
	}
	if len(got.Secret.Patterns) != len(want.Secret.Patterns) {
		t.Fatalf("patterns changed: %v", got.Secret.Patterns)
	}
	for i, pattern := range want.Secret.Patterns {
		if got.Secret.Patterns[i] != pattern {
			t.Fatalf("pattern %d changed: %q vs %q", i, got.Secret.Patterns[i], pattern)
		}
	}
	if got.GC != want.GC || got.Budget != want.Budget {
		t.Fatalf("numbers changed: %+v %+v", got.GC, got.Budget)
	}
}

// TestParseSkipsBadLines covers review #7: one line we cannot read must not
// throw the whole repository away. It is skipped with a warning and every other
// setting still arrives.
func TestParseSkipsBadLines(t *testing.T) {
	for _, text := range []string{"schema\n", "[gc\n", "warm_days = 90days\n", "patterns = [\"a\"\n"} {
		warned := []string{}
		before := Warn
		Warn = func(line string) { warned = append(warned, line) }
		got, err := Parse("name = \"keep\"\n" + text)
		Warn = before
		if err != nil {
			t.Errorf("%q 한 줄 때문에 저장소 전체가 죽었다 : %v", text, err)
			continue
		}
		if len(warned) == 0 {
			t.Errorf("%q 를 조용히 넘겼다", text)
		}
		if got.Name != "keep" || got.GC.WarmCount != Default("").GC.WarmCount {
			t.Errorf("%q 때문에 다른 값까지 잃었다 : %+v", text, got)
		}
	}
}

// TestParseReadsAwkwardValues covers review #7 too: a non standard escape, a
// literal string and a list spread over several lines all have to arrive.
func TestParseReadsAwkwardValues(t *testing.T) {
	text := "name = \"proj\"\n[secret]\npatterns = [\n  \"\\d{6}\",\n  'sk-[a-z]+',\n]\n"
	warned := []string{}
	before := Warn
	Warn = func(line string) { warned = append(warned, line) }
	got, err := Parse(text)
	Warn = before
	if err != nil {
		t.Fatalf("읽다가 죽었다 : %v", err)
	}
	if len(warned) != 0 {
		t.Fatalf("멀쩡한 줄을 건너뛰었다 : %v", warned)
	}
	want := []string{`\d{6}`, `sk-[a-z]+`}
	if len(got.Secret.Patterns) != len(want) {
		t.Fatalf("여러 줄 배열을 못 읽었다 : %+v", got.Secret.Patterns)
	}
	for position, pattern := range want {
		if got.Secret.Patterns[position] != pattern {
			t.Fatalf("%d 번째 패턴이 %q 여야 하는데 %q 다", position, pattern, got.Secret.Patterns[position])
		}
	}
}

func writeRepo(t *testing.T, dir string) {
	t.Helper()
	memoryDir := filepath.Join(dir, DirName)
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, FileName), Encode(Default("t")), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindWalksUp(t *testing.T) {
	root := t.TempDir()
	writeRepo(t, root)
	deep := filepath.Join(root, "src", "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	found, ok := Find(deep)
	if !ok || found != filepath.Join(root, DirName) {
		t.Fatalf("expected the repository at the top, got %q (%v)", found, ok)
	}
}

func TestFindStopsAtGitRoot(t *testing.T) {
	outer := t.TempDir()
	writeRepo(t, outer)
	inner := filepath.Join(outer, "tool")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	// A submodule has .git as a file, so both shapes must stop the walk.
	if err := os.WriteFile(filepath.Join(inner, ".git"), []byte("gitdir: ../.git/modules/tool\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if found, ok := Find(inner); ok {
		t.Fatalf("walk must stop at the git root, got %q", found)
	}
}

func TestResolveWithoutRepository(t *testing.T) {
	empty := t.TempDir()
	_, err := Resolve(empty, empty)
	missing := &NoRepositoryError{}
	if err == nil || err.Error() != missing.Error() {
		t.Fatalf("expected a missing repository error, got %v", err)
	}
	if missing.ExitCode() != 3 {
		t.Fatal("missing repository must map to exit code 3")
	}
}

func TestResolveFindsForcedRepo(t *testing.T) {
	root := t.TempDir()
	writeRepo(t, root)
	project, err := Resolve(root, root)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if project == nil || project.Dir != filepath.Join(root, DirName) {
		t.Fatalf("unexpected project repository: %+v", project)
	}
	direct, err := Resolve(filepath.Join(root, DirName), root)
	if err != nil || direct == nil {
		t.Fatalf("--repo pointing at the Memory folder must work: %v", err)
	}
}

// TestScopeAndPinRoundTrip is 설계검토 #32: what init writes must read back the
// same, or the settings quietly go back to the defaults on the next run.
func TestScopeAndPinRoundTrip(t *testing.T) {
	want := Default("시험")
	want.Pin.Max = 3
	want.Scope = map[string]string{"AIMemoryTool": "aimemorytool", "DiagramTool": "diagramtool"}
	back, err := Parse(string(Encode(want)))
	if err != nil {
		t.Fatal(err)
	}
	if back.Pin.Max != 3 {
		t.Fatalf("[pin] max came back as %d", back.Pin.Max)
	}
	for folder, scope := range want.Scope {
		if back.Scope[folder] != scope {
			t.Fatalf("[scope] %q came back as %q", folder, back.Scope[folder])
		}
	}
}

// TestArchiveDaysDefaultsOff is 설계검토 #8b: the delete step is the one that
// cannot be undone, so it stays off until someone turns it on.
func TestArchiveDaysDefaultsOff(t *testing.T) {
	if days := Default("시험").GC.ArchiveDays; days != 0 {
		t.Fatalf("archive_days default is %d, want 0", days)
	}
	settings, err := Parse("[gc]\narchive_days = 400\n")
	if err != nil || settings.GC.ArchiveDays != 400 {
		t.Fatalf("archive_days could not be turned on: %d %v", settings.GC.ArchiveDays, err)
	}
}
