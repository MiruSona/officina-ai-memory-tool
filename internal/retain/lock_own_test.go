package retain

import (
	"os"
	"path/filepath"
	"testing"
)

// 오래됐다고 남이 치우고 새로 잡은 잠금을, 늦게 끝난 옛 주인이 풀며 지우면 안 된다.
func TestLockReleaseKeepsOthersLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	unlock, err := lockFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("9999 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	unlock()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("남의 잠금을 지웠다 : %v", err)
	}
	os.Remove(path)
	unlock, err = lockFile(path)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("내 잠금이 남았다 : %v", err)
	}
}
