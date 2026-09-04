package store

import (
	"os"
	"path/filepath"
	"testing"
)

// TestListMemoriesSkipsReservedIndex 는 `index.md` 가 목록에 안 뜨는지 본다.
// 예약 이름이라 기억이 아니다 (Guide 기억파일규격 1절 · 1C 넘김).
func TestListMemoriesSkipsReservedIndex(t *testing.T) {
	opened := Open(filepath.Join(t.TempDir(), "Memory"), false)
	if err := opened.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(opened.StoreDir(), "2026", "01")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"20260105-aaaa0001.md", "index.md", "INDEX.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("---\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := opened.ListMemories()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("예약 이름을 안 걸렀다 : %+v", files)
	}
	if filepath.Base(files[0].Path) != "20260105-aaaa0001.md" {
		t.Fatalf("엉뚱한 파일이 남았다 : %s", files[0].Path)
	}
}
