package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// queueAndIndex 는 기억 한 건을 넣고 색인까지 돌린 뒤 id 를 준다.
func queueAndIndex(t *testing.T, body string) string {
	t.Helper()
	out, code := capture(t, func() int { return run(addArgs(body)) })
	if code != 0 {
		t.Fatalf("add 가 실패했다 : %s", out)
	}
	id := strings.TrimSpace(out)
	if _, code := capture(t, func() int { return run([]string{"index", "--quiet"}) }); code != 0 {
		t.Fatal("index 가 실패했다")
	}
	return id
}

// mem add 로 넣은 id 가 승격 뒤의 파일 이름과 같아야 한다 (설계 4-1).
func TestIndexPromotesWithTheSameID(t *testing.T) {
	memory := newRepo(t)
	id := queueAndIndex(t, "본문 한 줄")
	path := storeFile(memory, id)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("add 가 찍은 id 자리에 파일이 없다 : %v", err)
	}
	if left, err := os.ReadDir(filepath.Join(memory, "inbox", "new")); err != nil || len(left) != 0 {
		t.Fatalf("큐가 안 비었다 : %v %v", left, err)
	}
}

// index 는 두 번째부터 안 바뀐 파일을 건너뛴다.
func TestIndexSkipsUnchangedOnSecondRun(t *testing.T) {
	newRepo(t)
	queueAndIndex(t, "본문")
	out, code := capture(t, func() int { return run([]string{"index"}) })
	if code != 0 {
		t.Fatalf("index 가 실패했다 : %s", out)
	}
	if !strings.Contains(out, "그대로 1") {
		t.Fatalf("건너뛴 것을 안 알린다 : %s", out)
	}
}

// mem set 은 store/ 를 직접 안 고치고 큐에만 쓴다. 반영은 index 가 한다.
func TestSetQueuesPatchAndIndexApplies(t *testing.T) {
	memory := newRepo(t)
	id := queueAndIndex(t, "첫 본문")
	before := readStore(t, memory, id)
	if strings.Contains(before, "pinned: true") {
		t.Fatal("아직 고정이면 안 된다")
	}
	if _, code := capture(t, func() int {
		return run([]string{"set", id, "--pin", "--by", "20260901-aa11bb22", "--importance", "5"})
	}); code != 0 {
		t.Fatal("set 이 실패했다")
	}
	if readStore(t, memory, id) != before {
		t.Fatal("set 은 store/ 를 직접 고치면 안 된다")
	}
	if _, code := capture(t, func() int { return run([]string{"index", "--quiet"}) }); code != 0 {
		t.Fatal("index 가 실패했다")
	}
	after := readStore(t, memory, id)
	for _, want := range []string{"pinned: true", "superseded_by: 20260901-aa11bb22", "importance: 5"} {
		if !strings.Contains(after, want) {
			t.Fatalf("%q 가 안 들어갔다 :\n%s", want, after)
		}
	}
}

// mem set --body 는 본문을 통째로 갈아 끼운다.
func TestSetReplacesBody(t *testing.T) {
	memory := newRepo(t)
	id := queueAndIndex(t, "옛 본문")
	if _, code := capture(t, func() int { return run([]string{"set", id, "--body", "새 본문"}) }); code != 0 {
		t.Fatal("set 이 실패했다")
	}
	if _, code := capture(t, func() int { return run([]string{"index", "--quiet"}) }); code != 0 {
		t.Fatal("index 가 실패했다")
	}
	after := readStore(t, memory, id)
	if !strings.Contains(after, "새 본문") || strings.Contains(after, "옛 본문") {
		t.Fatalf("본문이 안 바뀌었다 :\n%s", after)
	}
}

func TestSetRefusesNothingAndBadValues(t *testing.T) {
	newRepo(t)
	id := queueAndIndex(t, "본문")
	if code := run([]string{"set", id}); code != 1 {
		t.Fatalf("고칠 게 없으면 1 이라야 한다 : %d", code)
	}
	if code := run([]string{"set", id, "--importance", "9"}); code != 1 {
		t.Fatalf("범위 밖 importance 는 1 이라야 한다 : %d", code)
	}
	if code := run([]string{"set", "그런거없음", "--pin"}); code != 1 {
		t.Fatalf("id 꼴이 아니면 1 이라야 한다 : %d", code)
	}
}

// show 는 색인을 거쳐 찾고, 조회수를 남기는 유일한 명령이다 (설계 18-3).
func TestShowGoesThroughIndexAndCountsHit(t *testing.T) {
	memory := newRepo(t)
	id := queueAndIndex(t, "본문 한 줄")
	out, code := capture(t, func() int { return run([]string{"show", id}) })
	if code != 0 || !strings.Contains(out, "본문 한 줄") {
		t.Fatalf("show 가 본문을 못 냈다 : %d %q", code, out)
	}
	hits, err := os.ReadFile(filepath.Join(memory, "local", "hits.jsonl"))
	if err != nil {
		t.Fatalf("조회수가 안 남았다 : %v", err)
	}
	if !strings.Contains(string(hits), `"cmd":"show"`) || !strings.Contains(string(hits), id) {
		t.Fatalf("조회 기록이 이상하다 : %s", hits)
	}
}

// status 는 색인 건수와 마지막 색인 시각을 같이 보여준다 (설계 9-5).
func TestStatusShowsIndexLine(t *testing.T) {
	newRepo(t)
	before, _ := capture(t, func() int { return run([]string{"status"}) })
	if !strings.Contains(before, "아직 없다") {
		t.Fatalf("색인이 없을 때를 안 알린다 : %s", before)
	}
	queueAndIndex(t, "본문")
	after, _ := capture(t, func() int { return run([]string{"status"}) })
	if !strings.Contains(after, "기억   : 1건") {
		t.Fatalf("색인 줄이 없다 : %s", after)
	}
}

// 머리말이 깨진 파일은 status 가 "색인 안 된 파일" 로 보여주고 index 는 2 로 끝난다.
func TestUnindexedFileIsReported(t *testing.T) {
	memory := newRepo(t)
	queueAndIndex(t, "본문")
	broken := storeFile(memory, "20260822-3f9a2c1b")
	if err := os.MkdirAll(filepath.Dir(broken), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken, []byte("머리말이 없다\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code := capture(t, func() int { return run([]string{"index", "--quiet"}) }); code != exitCheck {
		t.Fatalf("색인 안 된 파일이 있으면 2 라야 한다 : %d", code)
	}
	out, _ := capture(t, func() int { return run([]string{"status"}) })
	if !strings.Contains(out, "색인 안 된 파일 1건") || !strings.Contains(out, "20260822-3f9a2c1b.md") {
		t.Fatalf("status 가 안 알린다 : %s", out)
	}
}

// 등록 구조 — 도움말 있는 명령은 전부 registry 에서 나온다.
func TestRegistryHoldsTheCommands(t *testing.T) {
	// help 는 레지스트리가 아니라 run 이 바로 받는다 (설계 8-1 의 13번).
	for _, name := range i18n.HelpTopics() {
		if name == "help" {
			continue
		}
		if _, found := registry[name]; !found {
			t.Errorf("%s 가 등록이 안 됐다", name)
		}
	}
}

func readStore(t *testing.T, memory, id string) string {
	t.Helper()
	data, err := os.ReadFile(storeFile(memory, id))
	if err != nil {
		t.Fatalf("기억 파일을 못 읽었다 : %v", err)
	}
	return string(data)
}
