package main

// 자동 쌓기 A1 — add --origin 과 자동 관문, mem auto list/undo/redo,
// review --reject, init --retain (자동쌓기설계 2절).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/retain"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

const autoSession = "c0ffee00-1111-2222-3333-444455556666"
const autoQuote = "두 글자 한글 검색어에 trigram 이 조용히 0건을 돌려준다"

// autoRepo 는 근거 파일·대화 기록·세션 상태까지 갖춘 시험 저장소다.
func autoRepo(t *testing.T) string {
	t.Helper()
	memory := newRepo(t)
	root := filepath.Dir(memory)
	source := filepath.Join(root, "internal", "token")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(source, "token.go"), []byte("package token"), 0o644)
	record := filepath.Join(root, "session.jsonl")
	line, _ := json.Marshal(map[string]any{"type": "user",
		"message": map[string]any{"role": "user", "content": autoQuote + " — 고쳐 달라."}})
	os.WriteFile(record, append(line, '\n'), 0o644)
	state := retain.LoadState(memory)
	state.Sessions[autoSession] = &retain.Session{TranscriptPath: record, Seen: time.Now().Format(time.RFC3339)}
	if err := retain.SaveState(memory, state, time.Now()); err != nil {
		t.Fatal(err)
	}
	return memory
}

func autoArgs(body string) []string {
	return append(addArgs(body), "--origin", "stop", "--session", autoSession[:8], "--quotes", autoQuote)
}

func TestAutoAddMarksOrigin(t *testing.T) {
	memory := autoRepo(t)
	out, code := capture(t, func() int { return run(autoArgs("본문 한 줄")) })
	if code != exitOK {
		t.Fatalf("근거가 다 있는데 거절했다 : %s", out)
	}
	id := strings.TrimSpace(out)
	raw := readMemoryFile(t, memory, id)
	for _, want := range []string{"origin: stop", "origin_session: c0ffee00", "note:session c0ffee00"} {
		if !strings.Contains(raw, want) {
			t.Errorf("기억 파일에 %q 가 없다 :\n%s", want, raw)
		}
	}
	state := retain.LoadState(memory)
	if state.Sessions[autoSession].AutoAdds != 1 {
		t.Fatal("R6 셈이 안 올랐다")
	}
}

func TestAutoAddRejectsAndLogs(t *testing.T) {
	memory := autoRepo(t)
	out, code := capture(t, func() int { return run(autoArgs("지연이 3170ms 로 늘었다")) })
	if code != exitCheck || !strings.Contains(out, retain.RuleFragment) {
		t.Fatalf("지어낸 수는 R2 거절 · 종료 2 다 : %d %s", code, out)
	}
	raw, _ := os.ReadFile(store.LogPath(memory))
	if !strings.Contains(string(raw), "자동 · stop · "+retain.RuleFragment) {
		t.Fatalf("log.md 에 자동 거절이 없다 :\n%s", raw)
	}
	_, code = capture(t, func() int {
		return run(append(addArgs("본문"), "--quotes", autoQuote))
	})
	if code != exitUsage {
		t.Fatal("--quotes 는 --origin 없이 못 쓴다")
	}
	_, code = capture(t, func() int { return run(append(addArgs("본문"), "--origin", "robot")) })
	if code != exitUsage {
		t.Fatal("모르는 origin 을 받았다")
	}
}

func TestAutoAddCannotForceDuplicate(t *testing.T) {
	autoRepo(t)
	if _, code := capture(t, func() int { return run(addArgs("본문 한 줄")) }); code != exitOK {
		t.Fatal("사람 add 가 실패했다")
	}
	out, code := capture(t, func() int { return run(append(autoArgs("본문 한 줄"), "--new")) })
	if code == exitOK || !strings.Contains(out, retain.RuleNoMerge) {
		t.Fatalf("자동 기억은 --new 로도 닮은 기억을 못 민다 : %d %s", code, out)
	}
}

func TestAutoUndoRedoRoundTrip(t *testing.T) {
	memory := autoRepo(t)
	out, code := capture(t, func() int { return run(autoArgs("본문 한 줄")) })
	if code != exitOK {
		t.Fatalf("자동 add 실패 : %s", out)
	}
	id := strings.TrimSpace(out)
	listed, _ := capture(t, func() int { return run([]string{"auto", "list"}) })
	if !strings.Contains(listed, id) {
		t.Fatalf("list 에 없다 :\n%s", listed)
	}
	if _, code := capture(t, func() int { return run([]string{"auto", "undo"}) }); code != exitUsage {
		t.Fatal("거름 없는 undo 를 받았다")
	}
	capture(t, func() int { return run([]string{"auto", "undo", "--origin", "stop"}) })
	if found, _ := capture(t, func() int { return run([]string{"search", "trigram"}) }); !strings.Contains(found, id) {
		t.Fatal("미리보기인데 기억이 빠졌다")
	}
	if _, code := capture(t, func() int { return run([]string{"auto", "undo", "--origin", "stop", "--apply"}) }); code != exitOK {
		t.Fatal("undo 실패")
	}
	if found, _ := capture(t, func() int { return run([]string{"search", "trigram"}) }); strings.Contains(found, id) {
		t.Fatalf("undo 뒤에도 검색에 뜬다 :\n%s", found)
	}
	if !strings.Contains(readMemoryFile(t, memory, id), "review: true") {
		t.Fatal("파일은 남고 보류로 돌아야 한다")
	}
	if _, code := capture(t, func() int { return run([]string{"auto", "redo", "--apply"}) }); code != exitOK {
		t.Fatal("redo 실패")
	}
	if found, _ := capture(t, func() int { return run([]string{"search", "trigram"}) }); !strings.Contains(found, id) {
		t.Fatalf("redo 뒤에 검색에 안 뜬다 :\n%s", found)
	}
	again, _ := capture(t, func() int { return run([]string{"auto", "redo", "--apply"}) })
	if !strings.Contains(again, "없다") {
		t.Fatalf("이미 되살린 기록을 또 쓴다 :\n%s", again)
	}
	raw, _ := os.ReadFile(store.LogPath(memory))
	if !strings.Contains(string(raw), store.LogAutoUndo) || !strings.Contains(string(raw), store.LogAutoRedo) {
		t.Fatalf("log.md 에 되돌림·되살림이 없다 :\n%s", raw)
	}
}

func TestReviewRejectFoldsHeld(t *testing.T) {
	newRepo(t)
	out, code := capture(t, func() int { return run(append(addArgs("본문 한 줄"), "--hold")) })
	if code != exitOK {
		t.Fatalf("add --hold 실패 : %s", out)
	}
	id := strings.TrimSpace(out)
	capture(t, func() int { return run([]string{"index"}) })
	if _, code := capture(t, func() int { return run([]string{"review", "--reject", id, "--promote", id}) }); code != exitUsage {
		t.Fatal("--promote 와 --reject 를 같이 받았다")
	}
	if _, code := capture(t, func() int { return run([]string{"review", "--reject", id}) }); code != exitOK {
		t.Fatal("--reject 실패")
	}
	held, _ := capture(t, func() int { return run([]string{"review", "--kind", "held"}) })
	if strings.Contains(held, id) {
		t.Fatalf("버린 기억이 승격 대기에 남았다 :\n%s", held)
	}
	plain, code := capture(t, func() int { return run(append(addArgs("다른 본문 하나 둘"), "--new")) })
	if code != exitOK {
		t.Fatalf("보통 add 실패 : %s", plain)
	}
	capture(t, func() int { return run([]string{"index"}) })
	if _, code := capture(t, func() int { return run([]string{"review", "--reject", strings.TrimSpace(plain)}) }); code != exitUsage {
		t.Fatal("보류 아닌 기억을 버렸다")
	}
}

func TestInitRetainAddsAndUndoRemoves(t *testing.T) {
	root := t.TempDir()
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("MEM_INSTALL_NO_PATH", "1")
	if _, code := capture(t, func() int { return run([]string{"init", "--repo", root, "--retain"}) }); code != exitOK {
		t.Fatal("init --retain 실패")
	}
	settings, err := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"Stop"`, `"PreCompact"`, `"SessionEnd"`, `"stop"`, `"pre-compact"`, `"session-end"`} {
		if !strings.Contains(string(settings), want) {
			t.Errorf("settings.json 에 %s 가 없다", want)
		}
	}
	toml, _ := os.ReadFile(filepath.Join(root, "Memory", "mem.toml"))
	if strings.Count(string(toml), "[retain]") != 1 {
		t.Fatalf("[retain] 칸이 하나여야 한다 :\n%s", toml)
	}
	capture(t, func() int { return run([]string{"init", "--repo", root, "--retain"}) })
	toml, _ = os.ReadFile(filepath.Join(root, "Memory", "mem.toml"))
	if strings.Count(string(toml), "[retain]") != 1 {
		t.Fatal("두 번 돌리면 [retain] 이 겹친다")
	}
	if _, code := capture(t, func() int { return run([]string{"init", "--repo", root, "--retain", "--undo"}) }); code != exitOK {
		t.Fatal("init --retain --undo 실패")
	}
	settings, _ = os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if strings.Contains(string(settings), `"stop"`) || !strings.Contains(string(settings), `"session-start"`) {
		t.Fatalf("--retain --undo 는 자동 쌓기 훅만 걷는다 : %s", settings)
	}
	capture(t, func() int { return run([]string{"init", "--repo", root, "--retain"}) })
	if _, code := capture(t, func() int { return run([]string{"init", "--repo", root, "--undo"}) }); code != exitOK {
		t.Fatal("init --undo 실패")
	}
	settings, _ = os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if strings.Contains(string(settings), `"session-end"`) || strings.Contains(string(settings), `"stop"`) {
		t.Fatalf("--undo 가 자동 쌓기 훅을 못 걷었다 :\n%s", settings)
	}
}

func TestInitWithoutRetainAddsNoStop(t *testing.T) {
	root := t.TempDir()
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("MEM_INSTALL_NO_PATH", "1")
	capture(t, func() int { return run([]string{"init", "--repo", root}) })
	settings, _ := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if strings.Contains(string(settings), `"Stop"`) {
		t.Fatal("--retain 없이 Stop 훅을 붙였다")
	}
}
