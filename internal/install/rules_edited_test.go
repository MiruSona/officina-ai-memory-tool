package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// 2026-10-19 사용자 결정 — init 은 사람이 고친 mem 블록을 덮지 않는다.

// humanLines 는 10-10 게임 저장소에서 사람이 블록 안에 더한 두 줄의 꼴이다.
const humanLines = "- 이 저장소의 scope 는 `game` 하나만 쓴다.\n- 빌드 기록은 `history` 로만 남긴다.\n"

// 10-10 사례 : 옛 문안에 사람이 두 줄을 더한 블록. 예전에는 새 문안으로 덮여
// 두 줄이 사라졌다. 이제는 그대로 두고 안내 줄을 낸다.
func TestRulesEditedBlockIsKept(t *testing.T) {
	root := newProject(t)
	rules := filepath.Join(root, "AGENTS.md")
	old := i18n.InstallOldRulesBlocks[len(i18n.InstallOldRulesBlocks)-1]
	edited := strings.Replace(old, i18n.InstallBlockClose, strings.TrimSuffix(humanLines, "\n")+"\n"+i18n.InstallBlockClose, 1)
	original := "# 규칙\n\n" + edited
	if err := os.WriteFile(rules, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	for round := 1; round <= 2; round++ {
		report := mustInit(t, Options{Root: root})
		if got := readFile(t, rules); got != original {
			t.Fatalf("%d번째 init 이 손으로 고친 블록을 덮었다 :\n%s", round, got)
		}
		step := findStep(t, report, "AGENTS.md")
		if step.Changed || step.Todo != i18n.T(i18n.InitTodoKeepEdited) {
			t.Fatalf("표 줄이 「안 덮음」 이 아니다 : %+v", step)
		}
		if !hasNote(report, i18n.T(i18n.InitRulesEditedNote, "AGENTS.md")) {
			t.Fatalf("안내 줄이 없다 : %v", report.Notes)
		}
	}
}

// 지금 문안에 줄을 더해도 사람이 고친 것이다. --dry-run 에도 안내가 나온다.
func TestRulesEditedCurrentBlockDryRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	edited := strings.Replace(i18n.InstallRulesBlock, i18n.InstallBlockClose, humanLines+i18n.InstallBlockClose, 1)
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	step, err := ensureRules(path, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if step.Changed || step.Note == "" {
		t.Fatalf("손으로 고친 블록을 고치려 한다 : %+v", step)
	}
}

// 알려진 옛 문안은 판마다(표식 없는 MVP 판까지) 새 문안으로 갈아 끼운다.
func TestRulesEveryOldTemplateIsReplaced(t *testing.T) {
	for index, old := range i18n.InstallOldRulesBlocks {
		path := filepath.Join(t.TempDir(), "AGENTS.md")
		if err := os.WriteFile(path, []byte("# 규칙\n\n"+old), 0o644); err != nil {
			t.Fatal(err)
		}
		step, err := ensureRules(path, "", false)
		if err != nil {
			t.Fatal(err)
		}
		text := readFile(t, path)
		if !step.Changed || step.Note != "" || !strings.Contains(text, i18n.InstallRulesBlock) {
			t.Fatalf("%d번째 옛 문안을 안 갈아 끼웠다 : %+v\n%s", index, step, text)
		}
		if strings.Count(text, i18n.InstallRulesHeading) != 1 || !strings.HasPrefix(text, "# 규칙") {
			t.Fatalf("%d번째 : 블록이 겹치거나 원래 글이 사라졌다 :\n%s", index, text)
		}
	}
}

// 줄 끝 공백·CRLF 만 다른 것은 사람이 고친 게 아니다.
func TestClassifyIgnoresLineEndSpaces(t *testing.T) {
	old := i18n.InstallOldRulesBlocks[len(i18n.InstallOldRulesBlocks)-1]
	spaced := strings.ReplaceAll(old, "\n", "  \r\n")
	if kind := classifyRulesBlock("# 규칙\r\n\r\n" + spaced); kind != blockOld {
		t.Fatalf("옛 문안으로 못 알아봤다 : %d", kind)
	}
	current := strings.ReplaceAll(i18n.InstallRulesBlock, "\n", " \n")
	if kind := classifyRulesBlock(current); kind != blockCurrent {
		t.Fatalf("지금 문안으로 못 알아봤다 : %d", kind)
	}
	if kind := classifyRulesBlock("# 규칙\n"); kind != blockNone {
		t.Fatalf("블록이 없는데 있다고 한다 : %d", kind)
	}
}

func findStep(t *testing.T, report *Report, what string) Step {
	t.Helper()
	for _, step := range report.Steps {
		if step.What == what {
			return step
		}
	}
	t.Fatalf("%s 줄이 표에 없다", what)
	return Step{}
}

func hasNote(report *Report, want string) bool {
	for _, note := range report.Notes {
		if note == want {
			return true
		}
	}
	return false
}
