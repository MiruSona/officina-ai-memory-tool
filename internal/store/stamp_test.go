package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func stampRepo(t *testing.T) (*Store, string) {
	t.Helper()
	opened := Open(t.TempDir(), false)
	if err := opened.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	month := filepath.Join(opened.StoreDir(), "2026", "08")
	if err := os.MkdirAll(month, 0o755); err != nil {
		t.Fatal(err)
	}
	return opened, month
}

// 방금 만든 폴더는 자리표를 안 낸다. 윈도 시계 눈금 안에서는 「안 바뀌었다」 를
// 믿을 수 없어서, 모르겠다고 말하고 부르는 쪽이 전부 훑게 둔다.
func TestStoreDirsStampIsBlankWhileFresh(t *testing.T) {
	opened, _ := stampRepo(t)
	stamp, err := opened.StoreDirsStamp()
	if err != nil {
		t.Fatal(err)
	}
	if stamp != "" {
		t.Fatalf("방금 만든 폴더인데 자리표를 냈다 : %s", stamp)
	}
}

// 자리표는 폴더 mtime 만 본다 — 파일 내용을 고쳐도 그대로다. 「싼 따라잡기」 가
// 기대는 성질이 이것이다.
func TestStoreDirsStampFollowsDirMTime(t *testing.T) {
	opened, month := stampRepo(t)
	path := filepath.Join(month, "20260801-aaaa1111.md")
	if err := os.WriteFile(path, []byte("처음"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	backdate(t, opened, month, old)
	first := stampNow(t, opened)
	if first == "" {
		t.Fatal("오래된 폴더인데 자리표가 비었다")
	}
	if err := os.WriteFile(path, []byte("내용만 길게 고쳐 쓴다"), 0o644); err != nil {
		t.Fatal(err)
	}
	backdate(t, opened, month, old)
	if stampNow(t, opened) != first {
		t.Fatal("내용만 고쳤는데 자리표가 바뀌었다")
	}
	backdate(t, opened, month, old.Add(-time.Hour))
	if stampNow(t, opened) == first {
		t.Fatal("폴더 mtime 이 바뀌었는데 자리표가 그대로다")
	}
}

// store 폴더가 없으면 자리표를 못 낸다 — 부르는 쪽은 안전한 쪽으로 떨어진다.
func TestStoreDirsStampNeedsStore(t *testing.T) {
	opened := Open(t.TempDir(), false)
	if _, err := opened.StoreDirsStamp(); err == nil {
		t.Fatal("store 가 없는데 자리표가 나왔다")
	}
}

// backdate 는 store 와 그 아래 폴더의 시각을 과거로 돌린다. 시험이 시계 눈금을
// 안 기다리게 하는 자리다.
func backdate(t *testing.T, opened *Store, month string, when time.Time) {
	t.Helper()
	for _, dir := range []string{opened.StoreDir(), filepath.Dir(month), month} {
		if err := os.Chtimes(dir, when, when); err != nil {
			t.Fatal(err)
		}
	}
}

func stampNow(t *testing.T, opened *Store) string {
	t.Helper()
	stamp, err := opened.StoreDirsStamp()
	if err != nil {
		t.Fatal(err)
	}
	return stamp
}
