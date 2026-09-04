package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hookBlock 은 settings.json 에서 한 이벤트 키가 든 자리를 대충 잘라 본다.
// 우리 args 두 짝이 그 이벤트 안에 있는지 세는 데 쓴다.
func countAction(text, action string) int {
	return strings.Count(text, `"`+action+`"`)
}

// I1 — 새 설치가 두 이벤트를 넣고 SubagentStart 에는 matcher 키가 없다.
func TestInitInstallsBothHooks(t *testing.T) {
	root := newProject(t)
	mustInit(t, Options{Root: root})
	text := readFile(t, ClaudeSettingsPath(root))
	for _, want := range []string{`"SessionStart"`, `"SubagentStart"`,
		`"session-start"`, `"subagent-start"`, "startup|resume|clear|compact|fork"} {
		if !strings.Contains(text, want) {
			t.Errorf("settings.json 에 %s 가 없다\n%s", want, text)
		}
	}
	// matcher 는 SessionStart 쪽 하나뿐이다. 서브에이전트는 전 종류에 건다.
	if count := strings.Count(text, `"matcher"`); count != 1 {
		t.Errorf("matcher 가 %d 개다 (SessionStart 하나여야 한다)\n%s", count, text)
	}
}

// I3 — 두 번 돌려도 파일이 안 바뀐다.
func TestInitIsIdempotentWithBothHooks(t *testing.T) {
	root := newProject(t)
	mustInit(t, Options{Root: root})
	first := readFile(t, ClaudeSettingsPath(root))
	again := mustInit(t, Options{Root: root})
	if again.Changed() != 0 {
		t.Errorf("두 번째 init 이 %d 가지를 바꿨다", again.Changed())
	}
	if second := readFile(t, ClaudeSettingsPath(root)); second != first {
		t.Errorf("두 번째 init 이 파일을 고쳤다\n%s", second)
	}
	if countAction(first, "subagent-start") != 1 {
		t.Errorf("서브에이전트 훅이 %d 번 붙었다", countAction(first, "subagent-start"))
	}
}

// I2 — SessionStart 만 있던 파일에 하나만 더해지고 남의 훅·다른 키·줄끝이 그대로다.
func TestInitAddsOnlySubagentToOldFile(t *testing.T) {
	root := newProject(t)
	path := ClaudeSettingsPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "\ufeff{\r\n  \"permissions\": { \"allow\": [\"Bash(ls)\"] },\r\n" +
		"  \"hooks\": {\r\n    \"SessionStart\": [\r\n" +
		"      { \"matcher\": \"startup\", \"hooks\": [ { \"type\": \"command\", " +
		"\"command\": \"mem\", \"args\": [\"hook\", \"session-start\"] } ] },\r\n" +
		"      { \"matcher\": \"startup\", \"hooks\": [ { \"type\": \"command\", " +
		"\"command\": \"other-tool\", \"args\": [\"go\"] } ] }\r\n    ]\r\n  }\r\n}\r\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	mustInit(t, Options{Root: root})
	text := readFile(t, path)
	if countAction(text, "session-start") != 1 {
		t.Errorf("세션 훅이 %d 개가 됐다\n%s", countAction(text, "session-start"), text)
	}
	if countAction(text, "subagent-start") != 1 {
		t.Errorf("서브에이전트 훅이 %d 개다\n%s", countAction(text, "subagent-start"), text)
	}
	if !strings.Contains(text, "other-tool") {
		t.Errorf("남의 훅이 사라졌다\n%s", text)
	}
	if strings.Index(text, "permissions") > strings.Index(text, `"hooks"`) {
		t.Error("키 차례가 뒤집혔다")
	}
	if !strings.HasPrefix(text, "\ufeff") || !strings.Contains(text, "\r\n") {
		t.Error("BOM 이나 줄끝이 바뀌었다")
	}
}

// I4 — --undo 가 우리 훅을 다 뺀다. 손으로 걸어 둔 subagent-stop 도 같이 걷힌다.
func TestUndoRemovesEveryMemHook(t *testing.T) {
	root := newProject(t)
	path := ClaudeSettingsPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{"hooks":{"SubagentStop":[{"hooks":[` +
		`{"type":"command","command":"mem","args":["hook","subagent-stop"]},` +
		`{"type":"command","command":"other-tool","args":["go"]}]}]}}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	mustInit(t, Options{Root: root})
	if _, err := Undo(Options{Root: root}); err != nil {
		t.Fatalf("undo 실패 : %v", err)
	}
	text := readFile(t, path)
	for _, gone := range []string{"session-start", "subagent-start", "subagent-stop"} {
		if strings.Contains(text, gone) {
			t.Errorf("%s 훅이 안 빠졌다\n%s", gone, text)
		}
	}
	if !strings.Contains(text, "other-tool") {
		t.Errorf("남의 훅이 사라졌다\n%s", text)
	}
}

// I5 — --no-subagent-hook 은 SessionStart 만 넣는다.
func TestNoSubagentHookFlag(t *testing.T) {
	root := newProject(t)
	mustInit(t, Options{Root: root, NoSubagentHook: true})
	text := readFile(t, ClaudeSettingsPath(root))
	if !strings.Contains(text, "session-start") {
		t.Errorf("세션 훅이 없다\n%s", text)
	}
	if strings.Contains(text, "SubagentStart") {
		t.Errorf("--no-subagent-hook 인데 서브에이전트 훅을 넣었다\n%s", text)
	}
}

// I6 — hooks.SubagentStart 가 배열이 아니면 손대지 않고 「손으로」 로 넘긴다.
func TestOddSubagentShapeIsLeftAlone(t *testing.T) {
	root := newProject(t)
	path := ClaudeSettingsPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	odd := `{"hooks":{"SubagentStart":"이건 배열이 아니다"}}`
	if err := os.WriteFile(path, []byte(odd), 0o644); err != nil {
		t.Fatal(err)
	}
	mustInit(t, Options{Root: root})
	if text := readFile(t, path); text != odd {
		t.Errorf("모르는 모양을 고쳤다\n%s", text)
	}
}

// I7 — Gemini settings 에는 SessionStart 만 들어간다. 그 CLI 에는 SubagentStart 가 없다.
func TestGeminiGetsSessionHookOnly(t *testing.T) {
	root := newProject(t)
	mustInit(t, Options{Root: root, Gemini: true})
	text := readFile(t, GeminiSettingsPath(root))
	if !strings.Contains(text, "session-start") {
		t.Errorf(".gemini 에 세션 훅이 없다\n%s", text)
	}
	if strings.Contains(text, "SubagentStart") {
		t.Errorf(".gemini 에 서브에이전트 훅이 들어갔다\n%s", text)
	}
}

// I8 — 두 훅과 allow 규칙을 한 번에 쓴다. 백업 파일이 남지 않는다.
func TestSettingsWrittenOnce(t *testing.T) {
	root := newProject(t)
	writes := 0
	original := settingsWriter
	settingsWriter = func(path string, root *jsonObject, before []byte) error {
		writes++
		return original(path, root, before)
	}
	defer func() { settingsWriter = original }()
	mustInit(t, Options{Root: root})
	if writes != 1 {
		t.Errorf("settings.json 을 %d 번 썼다 (한 번이어야 한다)", writes)
	}
	if _, err := os.Stat(ClaudeSettingsPath(root) + backupSuffix); err == nil {
		t.Error("백업 파일이 남았다")
	}
}
