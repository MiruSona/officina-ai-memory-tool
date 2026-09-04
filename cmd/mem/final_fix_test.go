package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// show --json 의 칸 이름은 다른 명령의 --json 과 같은 소문자 snake 다
// (최종측정 4-1 ③). 예전에는 Go 구조체 이름이 그대로 나가 `ID`·`Type` 이었다.
func TestShowJSONKeysAreLowerSnake(t *testing.T) {
	newRepo(t)
	out, _ := capture(t, func() int { return run(addArgs("첫 줄\n둘째 줄")) })
	id := strings.TrimSpace(out)
	if _, code := capture(t, func() int { return run([]string{"index", "--quiet"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	shown, code := capture(t, func() int { return run([]string{"show", id, "--json"}) })
	if code != exitOK {
		t.Fatalf("show --json 이 실패했다 : %d", code)
	}
	one := map[string]any{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(shown)), &one); err != nil {
		t.Fatalf("한 줄 JSON 이 아니다 : %s", shown)
	}
	// v0.2 는 `source` 대신 `author` 다 (설계 결정 3).
	for _, key := range []string{"id", "type", "date", "summary", "tags", "author", "scope", "body"} {
		if _, ok := one[key]; !ok {
			t.Fatalf("칸 %q 가 없다 : %v", key, keysOf(one))
		}
	}
	for key := range one {
		if key != strings.ToLower(key) {
			t.Fatalf("대문자 칸이 남았다 : %q", key)
		}
	}
	if one["id"] != id {
		t.Fatalf("id 값이 다르다 : %v", one["id"])
	}
}

func keysOf(one map[string]any) []string {
	out := make([]string, 0, len(one))
	for key := range one {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// 아래 셋은 「싼 따라잡기」 회귀다 (최종측정 2-4 ①). 기본 검색은 store/ 폴더
// mtime 이 그대로면 파일을 하나도 stat 하지 않는다.

// 폴더 mtime 이 그대로면 따라잡기를 건너뛴다. 손으로 고친 본문이 검색에 안
// 잡히는 것이 「훑지 않았다」 의 증거다 — 파일을 세지 않고도 알 수 있다.
func TestSearchSkipsScanWhenFoldersUnchanged(t *testing.T) {
	memory := newRepo(t)
	id := addOne(t, "처음 본문이다")
	// 폴더를 만든 지 2초 안이면 mtime 을 안 믿는다 (store 의 stampWindow).
	// 시험이 2초를 기다리지 않도록 폴더 시각을 과거로 돌리고 한 번 더 색인해
	// 「믿을 수 있는 자리표」 를 저장한다.
	ageStoreDirs(t, memory)
	if _, code := capture(t, func() int { return run([]string{"index", "--quiet"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	rewriteBody(t, memory, id, "손으로만 고친 낱말 두더지굴")
	out, code := capture(t, func() int { return run([]string{"search", "두더지굴"}) })
	if code != exitOK {
		t.Fatalf("search 가 실패했다 : %d", code)
	}
	if strings.Contains(out, id) {
		t.Fatalf("파일을 훑었다 — 따라잡기를 건너뛰지 않았다 :\n%s", out)
	}
}

// store 에 파일을 바로 넣으면 그 달 폴더 mtime 이 바뀌므로 따라잡는다.
func TestSearchCatchesUpWhenFolderChanged(t *testing.T) {
	memory := newRepo(t)
	first := addOne(t, "처음 본문이다")
	second := copyAs(t, memory, first, "20260801-abcd1234", "새로 들어온 낱말 두더지굴")
	out, code := capture(t, func() int { return run([]string{"search", "두더지굴"}) })
	if code != exitOK {
		t.Fatalf("search 가 실패했다 : %d", code)
	}
	if !strings.Contains(out, second) {
		t.Fatalf("새 파일을 못 따라잡았다 :\n%s", out)
	}
}

// inbox/new 에 뭔가 있으면 폴더 mtime 과 상관없이 승격한다.
func TestSearchPromotesQueuedEvenWhenFoldersUnchanged(t *testing.T) {
	newRepo(t)
	addOne(t, "처음 본문이다")
	// 요약이 앞의 것과 닮으면 승격이 합쳐 버려 id 가 달라진다. 따로 쓴다.
	other := []string{"add", "--type", "decision", "--scope", "mem-search", "--tags", "index,eval",
		"--sources", "file:internal/index/promote.go", "--author", "human:tester",
		"--title", "큐에서 바로 승격한다",
		"--summary", "큐에서 승격해야 하는 아주 다른 요약을 가진 기억 하나다 두더지굴 이야기다",
		"--body", "큐에 있는 기억도 검색이 바로 따라잡아 승격한다는 것을 두더지굴 로 확인한다." +
			"\n\n두더지굴 은 이 시험에만 쓰는 낱말이다.\n다른 기억에는 안 들어간다.\n승격이 되면 검색에 잡힌다."}
	out, _ := capture(t, func() int { return run(other) })
	queued := strings.TrimSpace(out)
	found, code := capture(t, func() int { return run([]string{"search", "두더지굴"}) })
	if code != exitOK {
		t.Fatalf("search 가 실패했다 : %d", code)
	}
	if !strings.Contains(found, queued) {
		t.Fatalf("큐에 있는 기억을 안 승격했다 : %s\n%s", queued, found)
	}
}

// ageStoreDirs 는 store/ 아래 폴더 시각을 한 시간 전으로 돌린다. 파일은
// 안 건드린다.
func ageStoreDirs(t *testing.T, memory string) {
	t.Helper()
	when := time.Now().Add(-time.Hour).Truncate(time.Second)
	root := filepath.Join(memory, "store")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return err
		}
		return os.Chtimes(path, when, when)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// addOne 은 기억 하나를 넣고 색인까지 마친 뒤 id 를 준다.
func addOne(t *testing.T, body string) string {
	t.Helper()
	out, _ := capture(t, func() int { return run(addArgs(body)) })
	id := strings.TrimSpace(out)
	if _, code := capture(t, func() int { return run([]string{"index", "--quiet"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	return id
}

func storeFile(memory, id string) string {
	return filepath.Join(memory, "store", id[:4], id[4:6], id+".md")
}

// rewriteBody 는 store 의 md 본문만 갈아 끼운다. 폴더 mtime 은 안 바뀐다.
func rewriteBody(t *testing.T, memory, id, body string) {
	t.Helper()
	path := storeFile(memory, id)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	head, _, ok := strings.Cut(string(raw), "\n---\n")
	if !ok {
		t.Fatalf("머리말을 못 갈랐다 : %s", raw)
	}
	if err := os.WriteFile(path, []byte(head+"\n---\n\n"+body+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// copyAs 는 있는 기억을 새 id 로 복사해 store 에 바로 넣는다. 달 폴더 mtime 이
// 바뀐다.
func copyAs(t *testing.T, memory, from, id, body string) string {
	t.Helper()
	raw, err := os.ReadFile(storeFile(memory, from))
	if err != nil {
		t.Fatal(err)
	}
	head, _, ok := strings.Cut(string(raw), "\n---\n")
	if !ok {
		t.Fatalf("머리말을 못 갈랐다 : %s", raw)
	}
	head = strings.Replace(head, "id: "+from, "id: "+id, 1)
	head = strings.Replace(head, "date: ", "date: ", 1)
	path := storeFile(memory, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(head+"\n---\n\n"+body+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return id
}
