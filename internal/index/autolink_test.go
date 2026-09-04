package index

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// addThree 는 태그가 같은 기억 셋을 큐에 넣은 저장소다.
func addThree(t *testing.T) *store.Store {
	t.Helper()
	opened := newStore(t)
	// 요약이 닮으면 승격이 셋을 한 파일로 합친다. 일부러 다 다르게 쓴다.
	summaries := []string{
		"두 글자 한글 검색어에 trigram 이 조용히 0건을 돌려줘 검색이 죽었다는 것을 알아냈다",
		"훅 예산을 넘긴 회차가 큐를 그대로 남겨 다음 판이 두 배로 밀린 자국을 찾았다",
		"색인을 통째로 다시 만들 때 자동 합치기를 미루면 넣는 시간이 절반으로 줄었다",
	}
	for at, body := range []string{"첫째 본문", "둘째 본문", "셋째 본문"} {
		if _, err := opened.WriteAdd(addRequest(summaries[at], body)); err != nil {
			t.Fatal(err)
		}
	}
	return opened
}

// autoLinkRows 는 파생 표에 든 이웃 수다.
func autoLinkRows(t *testing.T, dir string) int {
	t.Helper()
	opened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	count := 0
	if err := opened.sql.QueryRow("SELECT COUNT(*) FROM auto_links").Scan(&count); err != nil {
		t.Fatalf("auto_links 를 못 읽었다 : %v", err)
	}
	return count
}

// storeFingerprint 는 store/ 아래 md 파일의 이름·내용을 한 줄로 만든 것이다.
func storeFingerprint(t *testing.T, dir string) string {
	t.Helper()
	lines := []string{}
	root := filepath.Join(dir, "store")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		lines = append(lines, path+" "+hex.EncodeToString(sum[:]))
		return nil
	})
	if err != nil {
		t.Fatalf("store 를 못 훑었다 : %v", err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// 태그가 겹치는 기억 셋이 들어오면 이웃 표가 채워진다 (결정 42).
func TestAutoLinkFillsNeighbours(t *testing.T) {
	opened := addThree(t)
	result := runIndex(t, opened)
	if result.Added != 3 {
		t.Fatalf("셋이 들어와야 한다 : %+v", result)
	}
	if result.AutoLinked == 0 {
		t.Fatal("이웃 링크를 하나도 안 찾았다")
	}
	if rows := autoLinkRows(t, opened.Dir); rows != result.AutoLinked {
		t.Fatalf("표에 든 수가 다르다 : %d vs %d", rows, result.AutoLinked)
	}
}

// 자동 링크는 **머리말을 한 바이트도 안 고친다** (결정 42 뒷정리). 고치면
// file_hash 가 바뀌어 색인이 매번 저장소 전체를 다시 읽고, git 이 시끄럽고,
// gc 면제표가 기계 링크를 사람 링크로 읽는다.
func TestAutoLinkNeverTouchesStore(t *testing.T) {
	opened := addThree(t)
	runIndex(t, opened)
	before := storeFingerprint(t, opened.Dir)
	before2 := autoLinkRows(t, opened.Dir)
	second := runIndex(t, opened)
	if got := storeFingerprint(t, opened.Dir); got != before {
		t.Fatalf("색인이 store/ 를 고쳤다 :\n%s\n---\n%s", before, got)
	}
	if second.Indexed != 0 {
		t.Fatalf("두 번째 회차가 파일을 다시 읽었다 : %+v", second)
	}
	// 아무것도 안 바뀐 회차는 표를 다시 만들지도 않는다 (헛수고 안 함).
	if second.AutoLinked != 0 {
		t.Fatalf("바뀐 것이 없는데 표를 다시 만들었다 : %d", second.AutoLinked)
	}
	if rows := autoLinkRows(t, opened.Dir); rows != before2 {
		t.Fatalf("표가 그대로가 아니다 : %d vs %d", rows, before2)
	}
}
