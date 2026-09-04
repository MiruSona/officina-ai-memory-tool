package hook

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
)

// TestHookWiresNormalize 는 훅의 따라잡기 색인도 `mem.toml [canon]` 정규화기를
// 타는지 본다. 안 꽂으면 훅이 만든 fts_norm 행만 대표말 치환이 빠져 같은 낱말이
// 다른 글자가 된다 (2A 넘김).
func TestHookWiresNormalize(t *testing.T) {
	restore := index.Normalize
	t.Cleanup(func() { index.SetNormalize(restore) })
	index.SetNormalize(func(text string) string { return text })

	dir := newRepo(t)
	settings := config.Default("시험")
	settings.Canon = map[string]string{"인덱싱": "색인"}
	path := filepath.Join(dir, config.DirName, config.FileName)
	if err := os.WriteFile(path, config.Encode(settings), 0o644); err != nil {
		t.Fatal(err)
	}
	addMemory(t, dir, request("history", "openapi", longEnough("인덱싱 판을 올렸다")))
	indexAll(t, dir)
	index.SetNormalize(func(text string) string { return text })

	runHook(t, []string{"session-start"}, startInput(dir))
	if got := index.Normalize("인덱싱"); got != "색인" {
		t.Fatalf("훅이 정규화기를 안 꽂았다 : %q", got)
	}
}
