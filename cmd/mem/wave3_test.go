package main

// 3물결 파도 H 회귀 — 옵션 파서 · add 관문 연결 · set --by · migrate · tags.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 16개 명령이 다 붙어 있어야 한다 (설계 6-1).
func TestSixteenCommandsAreRegistered(t *testing.T) {
	want := []string{"install", "init", "add", "set", "search", "show", "hook", "index",
		"migrate", "gc", "lint", "review", "tags", "eval", "status"}
	for _, name := range want {
		if _, found := registry[name]; !found {
			t.Errorf("%s 가 등록이 안 됐다", name)
		}
	}
	if len(registry) != len(want) {
		t.Errorf("명령 수가 %d 다. help 를 뺀 15개여야 한다", len(registry))
	}
}

// add 는 새 옵션을 다 받고, 모르는 옵션은 여전히 거절한다.
func TestAddParsesNewOptions(t *testing.T) {
	argv := []string{"--title", "제목", "--author", "human:me", "--sources", "file:a.go",
		"--by", "20260822-11112222", "--new", "--stale-after", "2027-01-01",
		"--todo-status", "open", "--json"}
	parsed, err := parseOptions(argv, addBools, addValues)
	if err != nil {
		t.Fatalf("새 옵션을 못 읽는다 : %v", err)
	}
	if parsed.text("author") != "human:me" || !parsed.flags["new"] || !parsed.flags["json"] {
		t.Fatalf("값이 안 들어왔다 : %+v", parsed)
	}
	if _, err := parseOptions([]string{"--author2", "x"}, addBools, addValues); err == nil {
		t.Fatal("모르는 옵션을 받아 줬다")
	}
}

// 관문 세 경우 — 통과(0) · 품질 거절(2) · 보안 거절(4).
func TestAddGateThreeOutcomes(t *testing.T) {
	memory := newRepo(t)
	if _, code := capture(t, func() int { return run(addArgs("통과하는 기억")) }); code != exitOK {
		t.Fatalf("갖춘 기억은 통과해야 한다 : %d", code)
	}
	if queuedCount(t, memory) != 1 {
		t.Fatal("통과한 기억이 큐에 없다")
	}
	thin := []string{"add", "--type", "decision", "--scope", "aimemorytool", "--tags", "design",
		"--author", "human:tester", "--title", "근거 없는 결정",
		"--summary", "근거도 태그도 모자란 결정을 넣으려고 하면 관문이 막아야 한다는 것을 본다",
		"--body", "한 줄"}
	out, code := capture(t, func() int { return run(thin) })
	if code != exitCheck {
		t.Fatalf("품질 거절은 2 여야 한다 : %d (%s)", code, out)
	}
	if !strings.Contains(out, "다음에 할 것") {
		t.Fatalf("다음에 뭘 할지 안 알려준다 : %s", out)
	}
	if queuedCount(t, memory) != 1 {
		t.Fatal("거절한 기억이 큐에 들어갔다")
	}
}

// --check 는 관문만 돌리고 절대 저장하지 않는다.
func TestAddCheckRunsGateOnly(t *testing.T) {
	memory := newRepo(t)
	out, code := capture(t, func() int { return run(append(addArgs("검사만"), "--check")) })
	if code != exitOK {
		t.Fatalf("갖춘 기억은 0 이어야 한다 : %d (%s)", code, out)
	}
	if queuedCount(t, memory) != 0 {
		t.Fatal("--check 가 큐에 넣었다")
	}
}

// --json 은 판정 한 덩어리를 준다.
func TestAddJSONVerdict(t *testing.T) {
	newRepo(t)
	out, code := capture(t, func() int {
		return run(append(addArgs("판정 JSON"), "--check", "--json"))
	})
	if code != exitOK {
		t.Fatalf("종료 코드가 0 이어야 한다 : %d", code)
	}
	one := map[string]any{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &one); err != nil {
		t.Fatalf("한 줄 JSON 이 아니다 : %s", out)
	}
	if _, ok := one["kind"]; !ok {
		t.Fatalf("판정 칸이 없다 : %s", out)
	}
}

// add --by 는 옛 결정에 덮임 표시를 달고 log.md 에 한 줄 남긴다 (설계 3-4).
func TestAddBySupersedesOld(t *testing.T) {
	memory := newRepo(t)
	old := queueAndIndex(t, "옛 결정")
	newArgs := append(addArgs("새 결정이 옛것을 덮는다"), "--by", old, "--new")
	if _, code := capture(t, func() int { return run(newArgs) }); code != exitOK {
		t.Fatal("덮는 add 가 실패했다")
	}
	if _, code := capture(t, func() int { return run([]string{"index", "--quiet"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	after := readStore(t, memory, old)
	if !strings.Contains(after, "superseded_by:") || !strings.Contains(after, "invalid_at:") {
		t.Fatalf("덮임 표시가 안 달렸다 :\n%s", after)
	}
	log, err := os.ReadFile(filepath.Join(memory, "log.md"))
	if err != nil || !strings.Contains(string(log), "덮음") {
		t.Fatalf("log.md 에 덮음 줄이 없다 : %v %s", err, log)
	}
}

// set --by 는 superseded_by 와 invalid_at 을 한 짝으로 채운다.
func TestSetBySetsThePair(t *testing.T) {
	memory := newRepo(t)
	id := queueAndIndex(t, "덮일 결정")
	if _, code := capture(t, func() int {
		return run([]string{"set", id, "--by", "20260901-aa11bb22"})
	}); code != exitOK {
		t.Fatal("set --by 가 실패했다")
	}
	if _, code := capture(t, func() int { return run([]string{"index", "--quiet"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	after := readStore(t, memory, id)
	if !strings.Contains(after, "superseded_by: 20260901-aa11bb22") || !strings.Contains(after, "invalid_at:") {
		t.Fatalf("한 짝이 안 채워졌다 :\n%s", after)
	}
}

// set --stale-after · --todo-status 도 큐를 지나 파일에 닿는다.
func TestSetStaleAfterAndTodoStatus(t *testing.T) {
	memory := newRepo(t)
	id := queueAndIndex(t, "다시 볼 기억")
	if _, code := capture(t, func() int {
		return run([]string{"set", id, "--stale-after", "2027-01-01"})
	}); code != exitOK {
		t.Fatal("set --stale-after 가 실패했다")
	}
	if _, code := capture(t, func() int { return run([]string{"index", "--quiet"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	if !strings.Contains(readStore(t, memory, id), "stale_after: 2027-01-01") {
		t.Fatalf("다시 볼 날이 안 들어갔다 :\n%s", readStore(t, memory, id))
	}
}

// legacyMemory 는 v0.1 규격 파일 하나를 store 에 바로 놓는다.
func legacyMemory(t *testing.T, memory, id, tags, scope string) {
	t.Helper()
	text := "---\nid: " + id + "\ntype: decision\nsummary: " +
		"옛 규격으로 저장된 기억이 새 규격으로 잘 옮겨지는지 보는 시험용 요약이다\n" +
		"tags: [" + tags + "]\nscope: " + scope + "\ndate: 2026-08-22\nsource: ai\n---\n\n" +
		"옛 규격 기억이라 제목도 근거도 없다. 이전이 그것을 채워야 한다.\n\n" +
		"근거 : Docs/Guide/설계개요.md\n둘째 줄이다.\n셋째 줄이다.\n"
	path := filepath.Join(memory, "store", id[:4], id[4:6], id+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// migrate 는 --dry-run 이 기본이고 그때는 파일을 하나도 안 고친다 (설계 2-5).
func TestMigrateDryRunChangesNothing(t *testing.T) {
	memory := newRepo(t)
	legacyMemory(t, memory, "20260822-1111aaaa", "design, impl", "mem")
	before := readStore(t, memory, "20260822-1111aaaa")
	out, code := capture(t, func() int { return run([]string{"migrate"}) })
	if code != exitOK {
		t.Fatalf("dry-run 은 0 이어야 한다 : %d (%s)", code, out)
	}
	if !strings.Contains(out, "이전 대상 1건") {
		t.Fatalf("대상을 못 셌다 : %s", out)
	}
	if readStore(t, memory, "20260822-1111aaaa") != before {
		t.Fatal("dry-run 이 파일을 고쳤다")
	}
	if queuedCount(t, memory) != 0 {
		t.Fatal("dry-run 이 큐에 넣었다")
	}
}

// migrate --apply 는 원본을 아카이브에 남기고 큐를 지나 새 규격으로 올린다.
func TestMigrateApplyAndRestore(t *testing.T) {
	memory := newRepo(t)
	id := "20260822-2222bbbb"
	legacyMemory(t, memory, id, "design, quality", "mem")
	before := readStore(t, memory, id)
	if _, code := capture(t, func() int { return run([]string{"migrate", "--apply"}) }); code != exitOK {
		t.Fatal("migrate --apply 가 실패했다")
	}
	if _, code := capture(t, func() int { return run([]string{"index", "--quiet"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	after := readStore(t, memory, id)
	for _, want := range []string{"author: claude-code/unknown", "migrated: true", "sources:"} {
		if !strings.Contains(after, want) {
			t.Fatalf("%q 가 안 들어갔다 :\n%s", want, after)
		}
	}
	// 제목은 지어내지 않는다 (리뷰 B). 요약을 베낀 제목은 head 열에 새 신호를
	// 안 넣고 검색을 오히려 나쁘게 했다. `mem review --kind title` 이 부른다.
	if strings.Contains(after, "title:") {
		t.Fatalf("migrate 가 제목을 지어냈다 :\n%s", after)
	}
	if strings.Contains(after, "source: ai") {
		t.Fatalf("옛 칸이 남았다 :\n%s", after)
	}
	// 되돌리기 — 아카이브에 남긴 원본으로 돌아온다.
	if _, code := capture(t, func() int { return run([]string{"migrate", "--restore"}) }); code != exitOK {
		t.Fatal("migrate --restore 가 실패했다")
	}
	if readStore(t, memory, id) != before {
		t.Fatalf("되돌린 파일이 원본과 다르다 :\n%s", readStore(t, memory, id))
	}
}

// 태그가 하한을 못 채우면 그 칸만 사람 손으로 넘긴다. 억지로 안 채운다
// (설계 2-5). v0.3 부터는 **나머지 칸은 그대로 옮긴다** — 태그 하나 때문에
// author·sources 까지 버리던 것이 실기억 49건이었다 (조사 E #6).
func TestMigrateLeavesThinTagsToPeople(t *testing.T) {
	memory := newRepo(t)
	legacyMemory(t, memory, "20260822-3333cccc", "impl", "mem")
	out, code := capture(t, func() int { return run([]string{"migrate"}) })
	if code != exitOK {
		t.Fatalf("dry-run 은 0 이어야 한다 : %d", code)
	}
	if !strings.Contains(out, "칸별로만 옮길 것") || !strings.Contains(out, "20260822-3333cccc") {
		t.Fatalf("칸별 이전 표에 안 올랐다 : %s", out)
	}
	if !strings.Contains(out, "남은 칸 tags") {
		t.Fatalf("막힌 칸이 tags 라고 안 말했다 : %s", out)
	}
}

// tags --check 는 표준 밖 태그를 센다. --rename 은 --dry-run 이 기본이다.
func TestTagsCheckAndRename(t *testing.T) {
	memory := newRepo(t)
	queueAndIndex(t, "태그 시험")
	out, code := capture(t, func() int { return run([]string{"tags", "--check"}) })
	if code != exitOK {
		t.Fatalf("표준 태그만 있으면 0 이어야 한다 : %d (%s)", code, out)
	}
	out, code = capture(t, func() int { return run([]string{"tags", "--rename", "korean=eval"}) })
	if code != exitOK {
		t.Fatalf("dry-run 은 0 이어야 한다 : %d (%s)", code, out)
	}
	if !strings.Contains(out, "안 고쳤다") {
		t.Fatalf("dry-run 이라고 말해야 한다 : %s", out)
	}
	if queuedCount(t, memory) != 0 {
		t.Fatal("dry-run 이 큐에 넣었다")
	}
	if _, code := capture(t, func() int {
		return run([]string{"tags", "--rename", "korean=eval", "--apply"})
	}); code != exitOK {
		t.Fatal("tags --rename --apply 가 실패했다")
	}
	if queuedCount(t, memory) != 1 {
		t.Fatal("--apply 가 큐에 안 넣었다")
	}
}

// tags --add 는 vocab.toml 을 고친다. 기억 파일은 안 건드린다.
func TestTagsAddWritesVocab(t *testing.T) {
	memory := newRepo(t)
	if _, code := capture(t, func() int { return run([]string{"tags", "--add", "shader=design"}) }); code != exitOK {
		t.Fatal("tags --add 가 실패했다")
	}
	raw, err := os.ReadFile(filepath.Join(memory, "vocab.toml"))
	if err != nil || !strings.Contains(string(raw), "shader") {
		t.Fatalf("vocab.toml 에 안 들어갔다 : %v %s", err, raw)
	}
}

// status 하위 화면 넷이 다 돈다.
func TestStatusSubScreens(t *testing.T) {
	newRepo(t)
	queueAndIndex(t, "상태 화면")
	for _, argv := range [][]string{{"status", "--quality"}, {"status", "--db"},
		{"status", "--doctor"}, {"status", "--log"}} {
		out, _ := capture(t, func() int { return run(argv) })
		if strings.TrimSpace(out) == "" {
			t.Errorf("%v 가 아무것도 안 찍는다", argv)
		}
	}
}
