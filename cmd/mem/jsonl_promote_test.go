package main

// `add --jsonl` 의 즉시 승격 길 (설계 2026-09-23 2-3) — 실제 `run` 으로 잰다.
// 결과 화면 셈(jsonlTally)만 보는 시험은 review_0926_test.go 에 있다.

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// batchLine 은 제목·요약·본문이 서로 안 닮은 묶음 한 줄이다. 닮으면 승격이
// 앞 줄에 붙여 버려 줄 수와 파일 수가 어긋난다.
func batchLine(t *testing.T, title, summary, body string) string {
	t.Helper()
	return jsonlLine(t, map[string]any{"type": "caution", "severity": "mid", "scope": "mem-search",
		"tags": []string{"index", "korean"}, "sources": []string{"file:internal/token/token.go"},
		"title": title, "summary": summary, "body": jsonlBody(body)})
}

var (
	lineApple = []string{"사과 상자를 먼저 연다", "사과 상자는 창고 맨 앞에 있어 먼저 열어야 다른 상자를 꺼낼 수 있다", "사과 줄이다."}
	lineRiver = []string{"강물 수위를 매일 잰다", "장마철에는 강물 수위가 하루에도 크게 바뀌어 아침마다 눈금을 적는다", "강물 줄이다."}
)

// 락을 잡으면 줄마다 실제 id 가 stdout 에, 끝줄은 「N 건을 저장했다」, log.md 에도 그 id 다.
func TestJSONLStoredEndLineAndLog(t *testing.T) {
	memory := newRepo(t)
	stdinOf(t, batchLine(t, lineApple[0], lineApple[1], lineApple[2])+"\n"+
		batchLine(t, lineRiver[0], lineRiver[1], lineRiver[2])+"\n")
	out, code := captureBoth(t, func() int { return run([]string{"add", "--jsonl", "--new"}) })
	if code != exitOK {
		t.Fatalf("묶음이 실패했다 (%d) : %s", code, out)
	}
	if last := lastLine(out); last != i18n.T(i18n.JSONLStored, 2) {
		t.Fatalf("끝줄이 저장 셈이어야 한다 : %s", last)
	}
	ids := idsIn(out)
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("서로 다른 id 두 줄이어야 한다 : %q", out)
	}
	log, err := os.ReadFile(store.LogPath(memory))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if _, err := os.Stat(storeFile(memory, id)); err != nil {
			t.Fatalf("%s 의 파일이 없다 : %v", id, err)
		}
		if !strings.Contains(string(log), id) {
			t.Fatalf("log.md 에 %s 가 없다 :\n%s", id, log)
		}
	}
}

// 남이 락을 쥐고 있으면 묶음 전체가 큐에 남고 큐 id 를 찍는다. 다음 index 뒤
// 그 id 가 그대로 파일이 된다 (쌍둥이·붙임이 없으면 큐 id 가 곧 실제 id).
func TestJSONLUnderHeldLockQueuesAll(t *testing.T) {
	memory := newRepo(t)
	stop := holdLock(t, memory)
	stdinOf(t, batchLine(t, lineApple[0], lineApple[1], lineApple[2])+"\n"+
		batchLine(t, lineRiver[0], lineRiver[1], lineRiver[2])+"\n")
	out, code := captureBoth(t, func() int { return run([]string{"add", "--jsonl", "--new"}) })
	if code != exitOK {
		t.Fatalf("묶음이 실패했다 (%d) : %s", code, out)
	}
	if last := lastLine(out); last != i18n.T(i18n.JSONLDone, 2) {
		t.Fatalf("끝줄이 큐 셈이어야 한다 : %s", last)
	}
	ids := idsIn(out)
	if len(ids) != 2 || queuedCount(t, memory) != 2 {
		t.Fatalf("큐 id 두 줄 · 큐 두 건이어야 한다 : %q", out)
	}
	for _, id := range ids {
		if _, err := os.Stat(storeFile(memory, id)); err == nil {
			t.Fatalf("락 없이 store 를 만졌다 : %s", id)
		}
	}
	stop()
	if _, code := capture(t, func() int { return run([]string{"index"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	for _, id := range ids {
		if _, err := os.Stat(storeFile(memory, id)); err != nil {
			t.Fatalf("index 뒤 큐 id %s 의 파일이 없다 : %v", id, err)
		}
	}
}

// index.db 를 지운 판의 묶음은 미뤄 큐에 둔다. 다음 index 는 store/ 를 먼저 색인해
// 이미 있는 닮은 기억에 그 줄을 붙인다 — 겹친 파일이 안 생긴다 (설계 5절).
// 글자까지 같은 줄은 관문(same-body)이 먼저 거절해 닮은 줄로 잰다.
func TestJSONLAfterIndexRemovedDefersThenSeesTwin(t *testing.T) {
	memory := newRepo(t)
	stdinOf(t, batchLine(t, lineApple[0], lineApple[1], lineApple[2])+"\n")
	out, code := captureBoth(t, func() int { return run([]string{"add", "--jsonl", "--new"}) })
	if code != exitOK {
		t.Fatalf("첫 묶음이 실패했다 (%d) : %s", code, out)
	}
	appleID := idsIn(out)[0]
	for _, suffix := range []string{"", "-wal", "-shm"} {
		os.Remove(index.DBPath(memory) + suffix)
	}
	stdinOf(t, batchLine(t, lineApple[0], lineApple[1], "사과 둘째 줄이다.")+"\n"+
		batchLine(t, lineRiver[0], lineRiver[1], lineRiver[2])+"\n")
	out, code = captureBoth(t, func() int { return run([]string{"add", "--jsonl", "--new"}) })
	if code != exitOK {
		t.Fatalf("묶음이 실패했다 (%d) : %s", code, out)
	}
	if last := lastLine(out); last != i18n.T(i18n.JSONLDone, 2) || queuedCount(t, memory) != 2 {
		t.Fatalf("미뤘으면 두 건이 큐에 있어야 한다 : %s", out)
	}
	if _, code := capture(t, func() int { return run([]string{"index"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	if queuedCount(t, memory) != 0 {
		t.Fatal("index 뒤 큐에 남았다")
	}
	if got := storeMDCount(t, memory); got != 2 {
		t.Fatalf("사과 하나 · 강물 하나 — md 는 둘이어야 한다 : %d", got)
	}
	data, err := os.ReadFile(storeFile(memory, appleID))
	if err != nil || !strings.Contains(string(data), "사과 둘째 줄이다.") {
		t.Fatalf("닮은 줄이 사과 기억에 안 붙었다 : %v", err)
	}
}

// idsIn 은 화면에서 id 꼴 줄만 모은다 (stdout 과 stderr 가 섞인 글).
func idsIn(out string) []string {
	ids := []string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if storedLine.MatchString("저장됨 : "+line) && len(line) == 17 {
			ids = append(ids, line)
		}
	}
	return ids
}

func storeMDCount(t *testing.T, memory string) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(filepath.Join(memory, "store"), func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.HasSuffix(path, ".md") {
			count++
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}
