package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dropHookEvent 는 settings.json 에서 이벤트 하나를 뗀다 — 「일부만 붙음」을 만든다.
func dropHookEvent(t *testing.T, root, event string) {
	t.Helper()
	path := ClaudeSettingsPath(root)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	hooks, _ := doc["hooks"].(map[string]any)
	delete(hooks, event)
	out, _ := json.Marshal(doc)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func retainProject(t *testing.T, retain bool) string {
	t.Helper()
	root := newProject(t)
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("MEM_INSTALL_NO_PATH", "1")
	if _, err := Init(Options{Root: root, Retain: retain}); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRetainCheckAllNoneSome(t *testing.T) {
	full := retainCheck(retainProject(t, true))
	if !full.OK || !strings.Contains(full.Note, "Stop·PreCompact·SessionEnd 3/3") {
		t.Fatalf("셋 다 붙으면 켜짐 3/3 이어야 한다 : %+v", full)
	}
	none := retainCheck(retainProject(t, false))
	if !none.OK || !strings.Contains(none.Note, "mem init --retain") {
		t.Fatalf("하나도 없으면 OK 꺼짐이어야 한다 : %+v", none)
	}
	root := retainProject(t, true)
	dropHookEvent(t, root, sessionEndKey)
	some := retainCheck(root)
	if some.OK || some.Security || !strings.Contains(some.Note, "2/3") {
		t.Fatalf("일부만 붙으면 보안 아닌 실패 2/3 이어야 한다 : %+v", some)
	}
}

// 설정 파일이 아예 없으면 꺼짐(OK)이다 — 고를 수 있는 기능이다.
func TestRetainCheckNoSettings(t *testing.T) {
	check := retainCheck(t.TempDir())
	if !check.OK {
		t.Fatalf("설정이 없으면 꺼짐 OK 여야 한다 : %+v", check)
	}
}
