package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/retain"
)

const retainSession = "beef0001-aaaa-bbbb-cccc-ddddeeeeffff"

// editsTranscript 는 파일 고침 n 번이 든 대화 기록이다.
func editsTranscript(t *testing.T, n int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	text := strings.Builder{}
	for at := 0; at < n; at++ {
		text.WriteString(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Edit","input":{"file_path":"a.go"}}]}}` + "\n")
	}
	if err := os.WriteFile(path, []byte(text.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func stopInput(t *testing.T, dir, transcript string, active bool) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"hook_event_name": "Stop", "cwd": dir,
		"session_id": retainSession, "transcript_path": transcript, "stop_hook_active": active})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestStopHookOutputShape(t *testing.T) {
	dir := newRepo(t)
	transcript := editsTranscript(t, 6)
	out := bytes.Buffer{}
	if code := Run([]string{"stop"}, strings.NewReader(stopInput(t, dir, transcript, false)), &out); code != 0 {
		t.Fatalf("종료 코드 %d", code)
	}
	answer := struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}{}
	if err := json.Unmarshal(out.Bytes(), &answer); err != nil {
		t.Fatalf("훅 JSON 이 아니다 : %q", out.String())
	}
	if answer.HookSpecificOutput.HookEventName != StopEventName {
		t.Fatalf("이벤트 이름 : %q", answer.HookSpecificOutput.HookEventName)
	}
	text := answer.HookSpecificOutput.AdditionalContext
	if !strings.Contains(text, "--origin stop --session beef0001") {
		t.Fatalf("세션 8자가 든 알림이어야 한다 : %s", text)
	}
	if lines := strings.Count(strings.TrimSpace(text), "\n") + 1; lines > 6 {
		t.Fatalf("알림은 6줄 이하다 : %d줄", lines)
	}
	again := bytes.Buffer{}
	Run([]string{"stop"}, strings.NewReader(stopInput(t, dir, transcript, false)), &again)
	if again.Len() != 0 {
		t.Fatalf("새로 고친 것이 없으면 다시 알리지 않는다 : %q", again.String())
	}
}

func TestStopHookSilentWhenActive(t *testing.T) {
	dir := newRepo(t)
	out := bytes.Buffer{}
	code := Run([]string{"stop"}, strings.NewReader(stopInput(t, dir, editsTranscript(t, 9), true)), &out)
	if code != 0 || out.Len() != 0 {
		t.Fatalf("stop_hook_active 면 침묵 · 0 이다 : %d %q", code, out.String())
	}
}

// 훅은 무엇이 틀려도 0 이다 — 저장소 없음 · 깨진 JSON · 기록 없음 · 상태 파일 못 씀.
func TestStopHookFailuresExitZero(t *testing.T) {
	dir := newRepo(t)
	blocked := filepath.Join(dir, config.DirName, "local")
	os.RemoveAll(blocked)
	if err := os.WriteFile(blocked, []byte("파일이라 폴더를 못 만든다"), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := []string{
		"{깨진",
		stopInput(t, t.TempDir(), editsTranscript(t, 9), false),
		stopInput(t, dir, filepath.Join(dir, "없다.jsonl"), false),
		stopInput(t, dir, editsTranscript(t, 9), false),
	}
	for _, input := range inputs {
		out := bytes.Buffer{}
		if code := Run([]string{"stop"}, strings.NewReader(input), &out); code != 0 || out.Len() != 0 {
			t.Errorf("0 · 빈 출력이어야 한다 : %d %q (%s)", code, out.String(), input)
		}
	}
	for _, event := range []string{"pre-compact", "session-end"} {
		out := bytes.Buffer{}
		if code := Run([]string{event}, strings.NewReader(stopInput(t, dir, "", false)), &out); code != 0 || out.Len() != 0 {
			t.Errorf("%s : %d %q", event, code, out.String())
		}
	}
}

func TestPreCompactAndSessionEndQueue(t *testing.T) {
	dir := newRepo(t)
	for _, event := range []string{"PreCompact", "SessionEnd"} {
		input := strings.Replace(stopInput(t, dir, "C:/t.jsonl", false), `"Stop"`, `"`+event+`"`, 1)
		out := bytes.Buffer{}
		if code := Run([]string{"x"}, strings.NewReader(input), &out); code != 0 || out.Len() != 0 {
			t.Fatalf("%s : %d %q", event, code, out.String())
		}
	}
	data, err := os.ReadFile(filepath.Join(retain.QueueDir(filepath.Join(dir, config.DirName)), "beef0001.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"reason": "end"`) {
		t.Fatalf("나중 표(end)가 앞 표를 덮어야 한다 : %s", data)
	}
}

func TestStopHookJSONStats(t *testing.T) {
	dir := newRepo(t)
	out := bytes.Buffer{}
	Run([]string{"stop", "--json"}, strings.NewReader(stopInput(t, dir, editsTranscript(t, 1), false)), &out)
	stats := map[string]any{}
	if err := json.Unmarshal(out.Bytes(), &stats); err != nil {
		t.Fatalf("잰 값 JSON 이 아니다 : %q", out.String())
	}
	if stats["why"] != retain.WhyBelow || stats["event"] != StopEventName {
		t.Fatalf("까닭이 틀렸다 : %v", stats)
	}
}
