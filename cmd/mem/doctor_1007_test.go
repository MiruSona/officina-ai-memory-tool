package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
)

// doctorRepo 는 init 을 마친 시험 저장소다. retain 이면 자동 쌓기 훅 셋도 붙인다.
func doctorRepo(t *testing.T, retain bool) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("MEM_INSTALL_NO_PATH", "1")
	argv := []string{"init", "--repo", root}
	if retain {
		argv = append(argv, "--retain")
	}
	if _, code := capture(t, func() int { return run(argv) }); code != exitOK {
		t.Fatal("init 실패")
	}
	return root
}

func doctorLine(out, what string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, what) {
			return line
		}
	}
	return ""
}

func TestDoctorRetainLine(t *testing.T) {
	t.Setenv(config.LLMPathEnv, filepath.Join(t.TempDir(), "llm.toml"))
	full := doctorRepo(t, true)
	out, _ := capture(t, func() int { return run([]string{"status", "--doctor", "--repo", full}) })
	if line := doctorLine(out, "자동 쌓기 훅"); !strings.HasPrefix(strings.TrimSpace(line), mark(true)) ||
		!strings.Contains(line, "3/3") {
		t.Fatalf("셋 다 붙음 줄이 틀리다 : %q\n%s", line, out)
	}
	none := doctorRepo(t, false)
	out, _ = capture(t, func() int { return run([]string{"status", "--doctor", "--repo", none}) })
	if line := doctorLine(out, "자동 쌓기 훅"); !strings.HasPrefix(strings.TrimSpace(line), mark(true)) ||
		!strings.Contains(line, "꺼짐") {
		t.Fatalf("꺼짐 줄이 틀리다 : %q\n%s", line, out)
	}
	if line := doctorLine(out, "llm 판정(R3)"); !strings.Contains(line, "꺼짐") || !strings.Contains(line, "llm.toml 없음") {
		t.Fatalf("llm 꺼짐 줄이 틀리다 : %q", line)
	}
	some := doctorRepo(t, true)
	path := filepath.Join(some, ".claude", "settings.json")
	raw, _ := os.ReadFile(path)
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	delete(doc["hooks"].(map[string]any), "SessionEnd")
	edited, _ := json.Marshal(doc)
	if err := os.WriteFile(path, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := capture(t, func() int { return run([]string{"status", "--doctor", "--repo", some}) })
	if line := doctorLine(out, "자동 쌓기 훅"); !strings.Contains(line, "2/3") {
		t.Fatalf("일부만 붙음 줄이 틀리다 : %q\n%s", line, out)
	}
	if code == exitOK {
		t.Fatalf("일부만 붙으면 실패 종료여야 한다 : %d", code)
	}
}

// 키 값 · URL 의 user:pass@ · 경로 · ?key= 가 doctor 출력에 새지 않는다.
func TestDoctorLLMLineHidesSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llm.toml")
	text := "url = \"http://alice:hunter2@127.0.0.1:8080/v1/secretpath?key=QUERYSECRET\"\n" +
		"key = \"sk-SECRETKEYVALUE1234567890\"\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.LLMPathEnv, path)
	root := doctorRepo(t, false)
	errOut := ""
	out, _ := captureBoth(t, func() int { return run([]string{"status", "--doctor", "--repo", root}) })
	line := doctorLine(out, "llm 판정(R3)")
	if !strings.Contains(line, "http://127.0.0.1:8080") || !strings.Contains(line, "키 있음") {
		t.Fatalf("llm 켜짐 줄이 틀리다 : %q\n%s", line, out)
	}
	for _, secret := range []string{"SECRETKEYVALUE", "hunter2", "alice", "secretpath", "QUERYSECRET"} {
		if strings.Contains(out+errOut, secret) {
			t.Fatalf("%s 가 출력에 샜다 :\n%s\n%s", secret, out, errOut)
		}
	}
}
