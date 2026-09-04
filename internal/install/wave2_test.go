package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// settingsOf 는 .claude/settings.json 을 평범한 나무로 읽는다.
func settingsOf(t *testing.T, root string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(ClaudeSettingsPath(root))
	if err != nil {
		t.Fatal(err)
	}
	tree := map[string]any{}
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("JSON 이 아니다 : %v\n%s", err, raw)
	}
	return tree
}

func allowOf(t *testing.T, root string) []string {
	t.Helper()
	tree := settingsOf(t, root)
	permissions, ok := tree["permissions"].(map[string]any)
	if !ok {
		return nil
	}
	list, ok := permissions["allow"].([]any)
	if !ok {
		return nil
	}
	out := []string{}
	for _, item := range list {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

// 설계 5-2 — init 이 명령별 좁은 allow 규칙을 넣는다.
func TestInitAddsAllowRules(t *testing.T) {
	root := newProject(t)
	mustInit(t, Options{Root: root})
	have := allowOf(t, root)
	for _, rule := range AllowRules {
		if !contains(have, rule) {
			t.Fatalf("%s 가 없다 : %v", rule, have)
		}
	}
	for _, never := range []string{"Bash(mem gc:*)", "Bash(mem install:*)", "Bash(mem *)"} {
		if contains(have, never) {
			t.Fatalf("%s 는 넣으면 안 된다", never)
		}
	}
}

// 있는 키는 그대로 두고 빠진 것만 채운다. 두 번 돌려도 안 는다.
func TestAllowKeepsWhatWasThereAndIsIdempotent(t *testing.T) {
	root := newProject(t)
	before := `{
  "permissions": { "allow": ["Bash(git status)", "Bash(mem search:*)"], "deny": ["Bash(rm:*)"] },
  "model": "opus"
}`
	writeSettings(t, root, before)
	mustInit(t, Options{Root: root})
	have := allowOf(t, root)
	if !contains(have, "Bash(git status)") {
		t.Fatalf("사람이 적은 규칙이 사라졌다 : %v", have)
	}
	if count := countOf(have, "Bash(mem search:*)"); count != 1 {
		t.Fatalf("이미 있는 규칙이 %d번 들었다", count)
	}
	if settingsOf(t, root)["model"] != "opus" {
		t.Fatal("다른 키가 사라졌다")
	}
	mustInit(t, Options{Root: root})
	if again := allowOf(t, root); len(again) != len(have) {
		t.Fatalf("두 번째 init 이 규칙을 늘렸다 : %d → %d", len(have), len(again))
	}
}

// init --undo 는 우리 규칙만 빼고 남의 것은 남긴다 (설계 6-1).
func TestUndoRemovesOurAllowRulesOnly(t *testing.T) {
	root := newProject(t)
	writeSettings(t, root, `{"permissions": {"allow": ["Bash(git status)"]}}`)
	mustInit(t, Options{Root: root})
	if _, err := Undo(Options{Root: root}); err != nil {
		t.Fatal(err)
	}
	have := allowOf(t, root)
	if !contains(have, "Bash(git status)") {
		t.Fatalf("남의 규칙을 지웠다 : %v", have)
	}
	for _, rule := range AllowRules {
		if contains(have, rule) {
			t.Fatalf("우리 규칙 %s 가 남았다", rule)
		}
	}
	if _, err := os.Stat(filepath.Join(root, config.DirName)); err != nil {
		t.Fatal("Memory/ 를 지웠다")
	}
}

// --dry-run 은 파일을 안 만든다.
func TestDryRunWritesNothing(t *testing.T) {
	root := newProject(t)
	if _, err := Init(Options{Root: root, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		ClaudeSettingsPath(root),
		filepath.Join(root, config.DirName, config.VocabFileName),
		filepath.Join(root, config.DirName, i18n.InstallLogName),
	} {
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("--dry-run 인데 %s 를 만들었다", path)
		}
	}
}

// 설계 6-1 — init 이 vocab.toml 을 만들고, 두 번 돌려도 안 바뀐다.
func TestInitWritesVocab(t *testing.T) {
	root := newProject(t)
	mustInit(t, Options{Root: root})
	path := filepath.Join(root, config.DirName, config.VocabFileName)
	first := readFile(t, path)
	if !strings.Contains(first, "[tag]") || !strings.Contains(first, "[scope]") {
		t.Fatalf("vocab.toml 이 비었다 :\n%s", first)
	}
	mustInit(t, Options{Root: root})
	if again := readFile(t, path); again != first {
		t.Fatal("두 번째 init 이 vocab.toml 을 고쳤다")
	}
}

// 사람이 적은 태그는 그대로 두고 빠진 절만 채운다.
func TestVocabFillsMissingKeysOnly(t *testing.T) {
	root := newProject(t)
	dir := filepath.Join(root, config.DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, config.VocabFileName)
	if err := os.WriteFile(path, []byte("[tag]\n\"우리것\" = []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustInit(t, Options{Root: root})
	text := readFile(t, path)
	if !strings.Contains(text, "우리것") {
		t.Fatalf("사람이 적은 태그가 사라졌다 :\n%s", text)
	}
	if !strings.Contains(text, "[scope.alias]") {
		t.Fatalf("빠진 절을 안 채웠다 :\n%s", text)
	}
}

// 설계 결정 27 — log.md 자리를 만들고 있으면 안 덮는다.
func TestInitWritesLogOnce(t *testing.T) {
	root := newProject(t)
	mustInit(t, Options{Root: root})
	path := filepath.Join(root, config.DirName, i18n.InstallLogName)
	if err := os.WriteFile(path, []byte("# 내가 고친 기록\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustInit(t, Options{Root: root})
	if readFile(t, path) != "# 내가 고친 기록\n" {
		t.Fatal("있는 log.md 를 덮었다")
	}
}

// 설계 5-5 — 옛 문안 블록은 새 다섯 줄로 갈아 끼운다.
func TestRulesBlockIsReplaced(t *testing.T) {
	root := newProject(t)
	rules := filepath.Join(root, "AGENTS.md")
	oldBlock := i18n.InstallBlockOpen + "\n" + i18n.InstallRulesHeading + "\n\n- 옛 문안 한 줄.\n" +
		i18n.InstallBlockClose + "\n"
	if err := os.WriteFile(rules, []byte("# 규칙\n\n"+oldBlock), 0o644); err != nil {
		t.Fatal(err)
	}
	mustInit(t, Options{Root: root})
	text := readFile(t, rules)
	if strings.Contains(text, "옛 문안 한 줄") {
		t.Fatalf("옛 블록이 남았다 :\n%s", text)
	}
	if !strings.Contains(text, "개인 취향·말투는 Claude 내장 기억에 둔다") {
		t.Fatalf("새 문안이 안 들어갔다 :\n%s", text)
	}
	if strings.Count(text, i18n.InstallBlockOpen) != 1 {
		t.Fatal("블록이 두 번 붙었다")
	}
	if !strings.HasPrefix(text, "# 규칙") {
		t.Fatal("사람이 쓴 글이 사라졌다")
	}
	mustInit(t, Options{Root: root})
	if strings.Count(readFile(t, rules), i18n.InstallBlockOpen) != 1 {
		t.Fatal("두 번째 init 이 또 붙였다")
	}
}

// 설계 6-1 — status --doctor 가 쓸 점검을 다 본다
// (그림자 exe · 의미 검색까지 — 보안시험 M-3 · v0.3 결정 16).
// 훅은 붙이는 표(hookSpecs)마다 한 줄이라 자리 번호로 안 찾는다.
func TestDoctorSeesHookAndAllow(t *testing.T) {
	root := newProject(t)
	before := Doctor(root)
	if len(before) != len(hookSpecs)+7 {
		t.Fatalf("점검 수가 틀리다 : %d", len(before))
	}
	for at := range hookSpecs {
		if before[at].OK {
			t.Fatalf("init 전인데 훅이 괜찮다고 한다 : %+v", before[at])
		}
	}
	if checkNamed(before, i18n.T(i18n.DoctorAllow)).OK {
		t.Fatal("init 전인데 allow 가 괜찮다고 한다")
	}
	mustInit(t, Options{Root: root})
	after := Doctor(root)
	for at, spec := range hookSpecs {
		if !after[at].OK {
			t.Fatalf("init 뒤인데 %s 훅이 안 잡힌다 : %+v", spec.event, after[at])
		}
	}
	if !checkNamed(after, i18n.T(i18n.DoctorAllow)).OK {
		t.Fatal("init 뒤인데 allow 가 안 잡힌다")
	}
	for _, check := range after {
		if check.What == "" {
			t.Fatal("이름 없는 점검이 있다")
		}
	}
}

// SubagentStart 만 지운 settings 는 doctor 가 문제로 잡는다 (리뷰 #3).
func TestDoctorSeesMissingSubagentHook(t *testing.T) {
	root := newProject(t)
	mustInit(t, Options{Root: root})
	path := ClaudeSettingsPath(root)
	root2, before, _, err := loadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	setEventList(root2, subagentKey, nil)
	if err := writeSettingsAtomic(path, root2, before); err != nil {
		t.Fatal(err)
	}
	found := checkNamed(Doctor(root), i18n.T(i18n.DoctorHookOne, subagentKey))
	if found.What == "" {
		t.Fatal("SubagentStart 점검이 아예 없다")
	}
	if found.OK {
		t.Fatalf("훅이 없는데 괜찮다고 한다 : %+v", found)
	}
	if checkNamed(Doctor(root), i18n.T(i18n.DoctorHookOne, sessionKey)).OK != true {
		t.Fatal("SessionStart 는 그대로인데 문제라고 한다")
	}
}

// checkNamed 는 이름으로 점검 한 줄을 찾는다. 없으면 빈 것이다.
func checkNamed(checks []Check, what string) Check {
	for _, check := range checks {
		if check.What == what {
			return check
		}
	}
	return Check{}
}

// 락 잔해는 점검이 알아본다.
func TestDoctorSeesStaleLock(t *testing.T) {
	root := newProject(t)
	mustInit(t, Options{Root: root})
	stale := filepath.Join(root, config.DirName, "index.lock.stale.1.1")
	if err := os.WriteFile(stale, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	found := checkNamed(Doctor(root), i18n.T(i18n.DoctorLock))
	if !strings.Contains(found.Note, "잔해") {
		t.Fatalf("락 잔해를 못 봤다 : %+v", found)
	}
}

func writeSettings(t *testing.T, root, text string) {
	t.Helper()
	path := ClaudeSettingsPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func contains(list []string, want string) bool {
	return countOf(list, want) > 0
}

func countOf(list []string, want string) int {
	count := 0
	for _, item := range list {
		if item == want {
			count++
		}
	}
	return count
}
