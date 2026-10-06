package index

import (
	"os"
	"testing"
)

// OpenAsIs 는 「안 건드린다」 쪽 열기다. 0바이트 index.db 에 스키마를 통째로 쓰면
// 안 되고, 「색인 없음·깨짐」 으로 돌려줘야 한다 (리뷰 10-06 #2).
func TestOpenAsIsLeavesEmptyFileAlone(t *testing.T) {
	dir := t.TempDir()
	path := DBPath(dir)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	database, err := OpenAsIs(dir)
	if database != nil {
		database.Close()
	}
	if !IsUnusable(err) {
		t.Fatalf("빈 색인은 「색인 없음」 이어야 한다 : %v", err)
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Size() != 0 {
		t.Fatalf("빈 색인 파일에 썼다 : %d 바이트", info.Size())
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); err == nil {
			t.Fatalf("딸린 파일 %s 를 만들었다", suffix)
		}
	}
}
