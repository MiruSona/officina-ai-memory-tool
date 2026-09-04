package main

// 7단계 코드리뷰 갈래 A (보안·연동) 가 더한 시험이다. 하나가 결함 하나를
// 재현하고, 고친 뒤에도 다시 안 생기게 막는다.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
)

// TestHelpCoversEveryOption 은 도움말이 실제 옵션 배열보다 뒤처지는 것을 막는다
// (보안연동 시험 L-3). `mem help install` 이 --bundle 을 안 적던 것이 이 시험이
// 없어서였다.
func TestHelpCoversEveryOption(t *testing.T) {
	// 도움말에 안 적어도 되는 것 : 안 쓰거나 다른 이름으로만 적는 별칭이다.
	skip := map[string]map[string]bool{
		"add": {"status": true, "source": true, "link": true},
		"set": {"status": true, "link": true},
	}
	for name, item := range registry {
		text, found := i18n.CommandHelp(name)
		if !found {
			t.Fatalf("%s 에 도움말이 없다", name)
		}
		for _, option := range append(append([]string{}, item.bools...), item.values...) {
			if skip[name][option] {
				continue
			}
			if !strings.Contains(text, "--"+option) {
				t.Errorf("mem help %s 가 --%s 를 안 적는다", name, option)
			}
		}
	}
}

// TestHelpRejectsUnknownOption 은 `mem help --nope` 가 exit 0 이던 것을 막는다
// (L-1).
func TestHelpRejectsUnknownOption(t *testing.T) {
	if code := run([]string{"help", "--nope-zzz"}); code != exitUsage {
		t.Fatalf("help --nope-zzz 종료 코드 %d, 1 이라야 한다", code)
	}
	if code := run([]string{"help", "add"}); code != exitOK {
		t.Fatalf("help add 종료 코드 %d, 0 이라야 한다", code)
	}
}

// TestSetTakesLinkAlias 는 add 에만 있던 --link 를 set 도 받는지 본다 (L-4).
func TestSetTakesLinkAlias(t *testing.T) {
	parsed, err := parseOptions([]string{"--link", "20260823-aaaaaaaa"}, setBools, setValues)
	if err != nil {
		t.Fatalf("set 이 --link 를 모른다 : %v", err)
	}
	if parsed.text("link") == "" {
		t.Fatal("--link 값이 안 들어왔다")
	}
}

// TestGitignoreHasShadowExe 는 init 이 그림자 exe 를 .gitignore 에 넣는지 본다
// (M-4).
func TestGitignoreHasShadowExe(t *testing.T) {
	found := false
	for _, line := range i18n.InstallIgnoreLines {
		if line == "mem.exe" {
			found = true
		}
	}
	if !found {
		t.Fatal(".gitignore 줄에 mem.exe 가 없다")
	}
}

// TestTildePath 는 status --embed 가 집 폴더를 ~ 로 줄이는지 본다 (L-6).
func TestTildePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("집 폴더를 못 찾는다")
	}
	full := filepath.Join(home, ".aimemory", "bin", "onnxruntime.dll")
	if got := tildePath(full); got != "~/.aimemory/bin/onnxruntime.dll" {
		t.Fatalf("tildePath = %q", got)
	}
	if got := tildePath(filepath.Join("C:", "elsewhere", "x.dll")); strings.HasPrefix(got, "~") {
		t.Fatalf("집 밖 경로를 줄였다 : %q", got)
	}
}

// TestMigrateScansSources 는 `sources` 칸에 든 열쇠를 migrate 가 잡는지 본다
// (보안연동 시험 M-1). 고치기 전에는 body·summary·title 만 훑어 그대로 다시 썼다.
func TestMigrateScansSources(t *testing.T) {
	memory := newRepo(t)
	putLegacySourceSecret(t, memory, "20260822-8888bbbb")
	out, code := capture(t, func() int { return run([]string{"migrate", "--apply"}) })
	if code != exitSecurity {
		t.Fatalf("sources 의 비밀정보에도 4 여야 한다 : %d (%s)", code, out)
	}
	if strings.Contains(out, "AKIA") || strings.Contains(out, "ghp_") {
		t.Fatalf("값을 찍었다 : %s", out)
	}
}

// putLegacySourceSecret 은 sources 칸에만 열쇠가 든 옛 규격 기억이다.
func putLegacySourceSecret(t *testing.T, memory, id string) {
	t.Helper()
	text := "---\n" +
		"id: " + id + "\n" +
		"type: history\n" +
		"summary: 근거 칸에 열쇠를 적어 둔 옛 기억이다. 이전이 이것을 그대로 다시 쓰면 안 된다\n" +
		"tags: [design, impl]\nscope: mem\ndate: 2026-08-22\nsource: ai\n" +
		"sources: [\"url:https://example.com/?token=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123\"]\n---\n\n" +
		"옛 규격 기억이다. 본문에는 아무 열쇠도 없다.\n둘째 줄이다.\n셋째 줄이다.\n"
	path := filepath.Join(memory, "store", id[:4], id[4:6], id+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestIndexExitsFourOnSecret 은 손으로 고쳐 넣은 비밀정보 때문에 색인에서 뺀
// 파일이 있으면 종료 코드가 4(보안 차단)인지 본다 (M-2). 고치기 전에는 2 였다.
func TestIndexExitsFourOnSecret(t *testing.T) {
	memory := newRepo(t)
	putRaw(t, memory, "20260105-dddd1111", "",
		"멀쩡한 본문이다.\n둘째 줄이다.\n셋째 줄이다.\n")
	putRaw(t, memory, "20260105-dddd2222", "",
		"열쇠를 AKIAIOSFODNN7EXAMPLE 로 적어 뒀다.\n둘째 줄이다.\n셋째 줄이다.\n")
	out, code := capture(t, func() int { return run([]string{"index", "--full"}) })
	if code != exitSecurity {
		t.Fatalf("비밀정보로 막힌 파일이 있으면 4 여야 한다 : %d (%s)", code, out)
	}
	if strings.Contains(out, "AKIA") {
		t.Fatalf("값을 찍었다 : %s", out)
	}
}

// TestIndexExitsTwoOnBadShape 은 규격 위반(2)과 비밀정보(4)를 가르는지 본다.
func TestIndexExitsTwoOnBadShape(t *testing.T) {
	memory := newRepo(t)
	dir := filepath.Join(memory, "store", "2026", "01")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "20260105-eeee1111.md"),
		[]byte("---\nid: 20260105-eeee1111\n---\n\n머리말이 모자란 파일이다.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, code := capture(t, func() int { return run([]string{"index", "--full"}) })
	if code != exitCheck {
		t.Fatalf("규격 위반은 2 여야 한다 : %d", code)
	}
}

// TestAddScopeFailsUnderLiveLock 은 산 락 아래에서 `tags --add-scope` 가
// 종료 코드 6 인지 본다 (보안연동 시험 L-2). 고치기 전에는 락을 안 잡고
// vocab.toml 을 그대로 덮어썼다 — 두 프로세스가 같이 쓰면 앞의 태그가 사라진다.
func TestAddScopeFailsUnderLiveLock(t *testing.T) {
	memory := newRepo(t)
	release, got, err := index.TryLock(memory)
	if err != nil || !got {
		t.Fatalf("시험이 락을 못 잡았다 : %v %v", got, err)
	}
	defer release()
	before, err := os.ReadFile(filepath.Join(memory, "vocab.toml"))
	if err != nil {
		t.Fatal(err)
	}
	_, code := capture(t, func() int { return run([]string{"tags", "--add-scope", "renderer"}) })
	if code != exitLocked {
		t.Fatalf("산 락 아래에서는 6 이어야 한다 : %d", code)
	}
	after, err := os.ReadFile(filepath.Join(memory, "vocab.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("락을 못 잡았는데 vocab.toml 을 고쳤다")
	}
}

// 락이 없으면 예전처럼 그냥 된다.
func TestAddScopeWorksWithoutLock(t *testing.T) {
	newRepo(t)
	out, code := capture(t, func() int { return run([]string{"tags", "--add-scope", "renderer"}) })
	if code != exitOK {
		t.Fatalf("락이 없으면 0 이어야 한다 : %d (%s)", code, out)
	}
}
