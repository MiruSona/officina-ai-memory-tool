package lint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
)

// TestSourceMissingFindsDeadPath 는 근거 표류 갈고리가 없는 경로를 잡는지 본다.
// 이 갈고리가 비어 있으면 `mem review` 의 STALE 큐에 D02 가 한 건도 안 온다
// (결정 36 ③ · 2D 넘김).
func TestSourceMissingFindsDeadPath(t *testing.T) {
	root := t.TempDir()
	storeDir := filepath.Join(root, "Memory")
	if err := os.MkdirAll(filepath.Join(storeDir, "store"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "있는파일.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ask := SourceMissing(storeDir)
	if ask == nil {
		t.Fatal("갈고리가 nil 이다")
	}
	memory := &model.Memory{ID: "20260105-aaaa0001"}
	if rule, _ := ask(memory, "file:있는파일.go"); rule != "" {
		t.Fatalf("있는 파일을 없다고 했다 : %s", rule)
	}
	rule, reason := ask(memory, "file:없는폴더/없는파일.go:12")
	if rule != quality.RuleDeadPath {
		t.Fatalf("없는 경로를 못 잡았다 : %q", rule)
	}
	if reason == "" {
		t.Fatal("까닭을 안 말했다")
	}
	// 우리가 판정할 수 없는 것은 아무 말도 안 한다.
	if rule, _ := ask(memory, "note:그냥 메모"); rule != "" {
		t.Fatalf("메모를 근거 표류로 봤다 : %s", rule)
	}
}
