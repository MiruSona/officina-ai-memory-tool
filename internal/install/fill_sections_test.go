package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/fileio"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// 키가 없는 빈 절 머리만 빠져도 init 이 「채움」으로 세고 채운다.

// configStep 은 표에서 mem.toml 소단계를 찾는다.
func configStep(t *testing.T, report *Report) Step {
	t.Helper()
	for _, step := range report.Steps {
		if step.What == config.DirName+"/"+config.FileName {
			return step
		}
	}
	t.Fatal("mem.toml 소단계가 표에 없다")
	return Step{}
}

func TestInitFillsMissingEmptySection(t *testing.T) {
	root := newProject(t)
	base := string(config.Encode(config.Default("시험")))
	text := strings.Replace(base, "[canon]\n", "", 1)
	if text == base {
		t.Fatal("기본 글에 [canon] 머리가 없다 — 시험 전제가 틀렸다")
	}
	if len(config.MissingKeys(text)) != 0 {
		t.Fatal("키는 다 있어야 한다 — 시험 전제가 틀렸다")
	}
	path := writeMemoryFile(t, root, config.FileName, text)

	preview := configStep(t, mustInit(t, Options{Root: root, NoHook: true, DryRun: true}))
	if !preview.Changed || preview.Now != i18n.T(i18n.InitStateKeysSectionsMissing, 0, 1) {
		t.Fatalf("미리보기가 빠진 절을 안 셌다 : %+v", preview)
	}
	if readFile(t, path) != text {
		t.Fatal("미리보기가 파일을 고쳤다")
	}

	step := configStep(t, mustInit(t, Options{Root: root, NoHook: true}))
	if !step.Changed || step.Todo != i18n.T(i18n.InitTodoFillKeys) || step.Now != preview.Now {
		t.Fatalf("「채움」으로 안 떴다 : %+v", step)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "[canon]") {
		t.Fatalf("[canon] 이 안 생겼다 :\n%s", got)
	}
	if left := config.MissingSections(got); len(left) != 0 {
		t.Fatalf("빠진 절이 남았다 : %v", left)
	}

	again := configStep(t, mustInit(t, Options{Root: root, NoHook: true}))
	if again.Changed || again.Now != i18n.T(i18n.InitStatePresent) {
		t.Fatalf("두 번째 init 이 「그대로」가 아니다 : %+v", again)
	}
	if readFile(t, path) != got {
		t.Fatal("두 번째 init 이 또 고쳤다")
	}
}

func TestInitKeepsCompleteConfigBytes(t *testing.T) {
	root := newProject(t)
	text := strings.ReplaceAll(string(config.Encode(config.Default("시험"))), "\n", "\r\n")
	path := writeMemoryFile(t, root, config.FileName, text)
	step := configStep(t, mustInit(t, Options{Root: root, NoHook: true}))
	if step.Changed {
		t.Fatalf("다 있는 파일을 고친다고 센다 : %+v", step)
	}
	if readFile(t, path) != text {
		t.Fatal("다 있는 파일의 바이트가 바뀌었다")
	}
}

func TestReplaceFileRacedMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mem.toml")
	if err := os.WriteFile(path, []byte("남이 고친 글\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := replaceFile(path, []byte("새 글\n"), []byte("아까 읽은 글\n"), filepath.Join(dir, "backup"))
	if !errors.Is(err, fileio.ErrChanged) {
		t.Fatalf("ErrChanged 를 못 가려낸다 : %v", err)
	}
	if err.Error() != i18n.T(i18n.InitFileRaced, path) {
		t.Fatalf("한국어 문구가 아니다 : %q", err.Error())
	}
	if readFile(t, path) != "남이 고친 글\n" {
		t.Fatal("바뀐 파일을 덮었다")
	}
}
