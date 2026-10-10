package lint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
)

// D05 시험 (점검·정리 설계 9절). 임시 트리에 `ToolA/internal/x.go` 를 두고 scope 는
// `toola` 다 — scope 폴더 기준이 실제로 쓰이는지 보려는 것이다.

// putToolA 는 프로젝트 뿌리 아래에 scope 폴더 하나를 만든다.
func putToolA(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "ToolA", "internal")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestBodyAskJudgesPaths 는 실물 대조 규칙 넷을 하나씩 본다.
func TestBodyAskJudgesPaths(t *testing.T) {
	root := t.TempDir()
	storeDir := filepath.Join(root, "Memory")
	if err := os.MkdirAll(filepath.Join(storeDir, "store"), 0o755); err != nil {
		t.Fatal(err)
	}
	putToolA(t, root)
	_, ask := Missing(storeDir)
	memory := &model.Memory{ID: "20260820-b0d50009", Scope: "toola"}
	cases := []struct {
		path   string
		linked bool
		want   string
	}{
		{"internal/gone.go", false, quality.RuleDeadBodyPath},       // 첫 마디는 scope 폴더에 있는데 파일이 없다
		{"ToolA/internal/gone.go", false, quality.RuleDeadBodyPath}, // 뿌리 기준도 같다
		{"internal/gone.go", true, quality.RuleDeadPath},            // 링크면 D02
		{"internal/x.go", false, ""},                                // scope 폴더로 풀린다
		{"ToolA/internal/x.go", false, ""},                          // 뿌리로 풀린다
		{"ToolA/internal/x.go:12", false, ""},                       // 줄 번호는 뗀다
		{"Assets/Scripts/Player.cs", false, ""},                     // 첫 마디가 없다 → 남의 저장소
		{"ToolA/internal/install", false, ""},                       // 확장자가 없다
		{"../ToolA/gone.go", true, ""},                              // 기억 파일 자리를 모른다
		{"https://example.com/a.go", false, ""},                     // 바깥 주소
	}
	for _, item := range cases {
		rule, reason := ask(memory, item.path, item.linked)
		if rule != item.want {
			t.Errorf("%q (linked=%v) : %q 를 바랐는데 %q", item.path, item.linked, item.want, rule)
		}
		if rule != "" && reason == "" {
			t.Errorf("%q : 까닭을 안 말했다", item.path)
		}
	}
	// scope 가 다르면 scope 폴더 기준이 없다 → 첫 마디 `internal` 이 어디에도 없어 남의 저장소다.
	other := &model.Memory{ID: "20260820-b0d50010", Scope: "toolb"}
	if rule, _ := ask(other, "internal/gone.go", false); rule != "" {
		t.Errorf("scope 폴더가 없는 기억에서 첫 마디 없는 경로를 걸었다 : %s", rule)
	}
	// 저장소 폴더 기준으로 풀리는 경로.
	if err := os.WriteFile(filepath.Join(storeDir, "store", "a.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, fresh := Missing(storeDir)
	if rule, _ := fresh(memory, "store/a.md", false); rule != "" {
		t.Errorf("저장소 폴더 기준으로 있는 경로를 걸었다 : %s", rule)
	}
}

// TestBodyAskSkipsWhenTooMany 는 경로 집합이 다 안 찼으면 아무 말도 안 하는지 본다.
func TestBodyAskSkipsWhenTooMany(t *testing.T) {
	half := &missing{root: t.TempDir(), known: map[string]bool{}, full: false}
	if rule, _ := half.bodyAsk(&model.Memory{}, "internal/gone.go", false); rule != "" {
		t.Fatalf("반쯤 훑은 목록으로 없다고 했다 : %s", rule)
	}
}

// bodyHookWired 는 quality 가 본문 경로를 뽑아 BodyMissing 을 부르는지 본다. 갈래 1
// (staleBodyPaths)이 아직 안 붙었으면 통합 시험은 잴 것이 없다.
func bodyHookWired() bool {
	called := false
	memory := &model.Memory{ID: "20260820-b0d50011", Type: "history", Scope: "toola", Date: "2026-08-20",
		Body: "고친 자리는 internal/gone.go 이다.\n"}
	quality.CheckRepo([]*model.Memory{memory}, quality.RepoOptions{
		BodyMissing: func(*model.Memory, string, bool) (string, string) { called = true; return "", "" },
	})
	return called
}

// TestLintDeadBodyPath 는 lint 한 판에서 D05 가 fixture 셋 중 없는 경로에만 걸리는지,
// 본문 링크의 D02 가 두 번 안 나오는지 본다.
func TestLintDeadBodyPath(t *testing.T) {
	if !bodyHookWired() {
		t.Skip("quality 의 staleBodyPaths 가 아직 BodyMissing 을 안 부른다 (갈래 1 몫) — 붙은 뒤 다시 돈다")
	}
	opened := newRepo(t, "body-path-dead.md", "body-path-alive.md", "body-path-foreign.md")
	putToolA(t, filepath.Dir(opened.Dir))
	writeMemoryFile(t, opened, "20260820-b0d50004",
		goodMemory("20260820-b0d50004", "", "없는 문서를 링크한다.\n[여기](ToolA/없는문서.md) 를 보라는데 그런 것은 없다.\n한 줄 더 적어 본문 하한을 채운다.\n"))
	report := runOn(t, opened, false)
	dead := problemsOf(report, quality.RuleDeadBodyPath)
	if len(dead) != 1 || filepath.Base(dead[0].Path) != "20260820-b0d50001.md" {
		t.Fatalf("D05 가 없는 경로 하나에만 걸려야 한다 : %+v", dead)
	}
	if links := problemsOf(report, quality.RuleDeadPath); len(links) != 1 {
		t.Fatalf("본문 링크 D02 가 한 번만 나와야 한다 : %+v", links)
	}
}

func problemsOf(report *Report, rule string) []Problem {
	found := []Problem{}
	for _, problem := range report.Problems {
		if problem.Rule == rule {
			found = append(found, problem)
		}
	}
	return found
}
