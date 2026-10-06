package main

// 2026-10-06 작은 버그 판이 더한 시험이다. 하나가 결함 하나를 재현한다.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// A7 — 같은 회차에 `--links` 를 큐에 넣고 index 전에 `--link` 를 더하면 앞의
// 목록이 사라지던 것. `--link` 는 아직 색인 안 된 큐의 links 위에 더해야 한다.
func TestSetLinkSeesQueuedLinks(t *testing.T) {
	memory := newRepo(t)
	first := putLinked(t, memory, "20260105-cccc1111", []string{"20260105-cccc4444"})
	putLinked(t, memory, "20260105-cccc2222", nil)
	putLinked(t, memory, "20260105-cccc3333", nil)
	putLinked(t, memory, "20260105-cccc4444", nil)
	if _, code := capture(t, func() int { return run([]string{"index", "--full"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	if _, code := capture(t, func() int {
		return run([]string{"set", first, "--links", "20260105-cccc2222"})
	}); code != exitOK {
		t.Fatal("set --links 가 실패했다")
	}
	// index 전에 바로 더한다 — 큐의 --links 가 아직 파일에 없다.
	if _, code := capture(t, func() int {
		return run([]string{"set", first, "--link", "20260105-cccc3333"})
	}); code != exitOK {
		t.Fatal("set --link 가 실패했다")
	}
	if _, code := capture(t, func() int { return run([]string{"index"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	text := readFile(t, filepath.Join(memory, "store", "2026", "01", first+".md"))
	if !strings.Contains(text, "20260105-cccc2222") || !strings.Contains(text, "20260105-cccc3333") {
		t.Fatalf("큐의 --links 가 사라졌다 : %s", text)
	}
	if strings.Contains(text, "20260105-cccc4444") {
		t.Fatalf("--links 의 통째 교체가 풀렸다 : %s", text)
	}
}

// 손으로 고친 큐의 id 아닌 글은 새 `--link` patch 에 다시 실리면 안 된다 (리뷰 10-06 #3).
func TestSetLinkDropsNonIDFromQueue(t *testing.T) {
	memory := newRepo(t)
	first := putLinked(t, memory, "20260105-eeee1111", nil)
	putLinked(t, memory, "20260105-eeee2222", nil)
	putLinked(t, memory, "20260105-eeee3333", nil)
	mustRun(t, "index", "--full")
	mustRun(t, "set", first, "--links", "20260105-eeee2222")
	inbox := filepath.Join(memory, "inbox", "new")
	queued := queueFiles(t, inbox)
	if len(queued) != 1 {
		t.Fatalf("큐가 한 건이어야 한다 : %v", queued)
	}
	raw := readFile(t, queued[0])
	tampered := strings.Replace(raw, `"20260105-eeee2222"`, `"20260105-eeee2222","엉뚱한 글"`, 1)
	if tampered == raw {
		t.Fatalf("시험 준비 실패 — 큐에서 links 를 못 찾았다 : %s", raw)
	}
	if err := os.WriteFile(queued[0], []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "set", first, "--link", "20260105-eeee3333")
	for _, path := range queueFiles(t, inbox) {
		text := readFile(t, path)
		if strings.Contains(text, "20260105-eeee3333") && strings.Contains(text, "엉뚱한 글") {
			t.Fatalf("id 아닌 글이 새 patch 에 실렸다 : %s", text)
		}
	}
}

func queueFiles(t *testing.T, dir string) []string {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// `--no-index` 인데 색인 판이 다르면 색인을 지우고 다시 만들던 것. 지금은 손대지
// 않고 종료 코드 2 로 거절한다 — eval·search 가 같은 길을 탄다.
func TestNoIndexRefusesStaleSchema(t *testing.T) {
	for _, command := range [][]string{{"eval", "--no-index"}, {"search", "본문", "--no-index"}} {
		memory := newRepo(t)
		if _, code := capture(t, func() int { return run(addArgs("본문 한 줄")) }); code != exitOK {
			t.Fatal("add 가 실패했다")
		}
		if _, code := capture(t, func() int { return run([]string{"index"}) }); code != exitOK {
			t.Fatal("index 가 실패했다")
		}
		setUserVersion(t, memory, 6)
		out, code := capture(t, func() int { return run(command) })
		if code != exitCheck {
			t.Fatalf("%v : 판이 다르면 2 여야 한다 : %d (%s)", command, code, out)
		}
		if version := userVersion(t, memory); version != 6 {
			t.Fatalf("%v : --no-index 인데 색인을 다시 만들었다 (판 %d)", command, version)
		}
	}
}

// 색인 판이 exe 보다 새것이면 「색인 없음」(5) 이 아니라 「낡은 exe」(2) 다 —
// `--no-index` 유무와 상관없이 같고, 색인은 안 건드린다 (리뷰 10-06 #1).
func TestTooNewIndexIsCheckFailure(t *testing.T) {
	commands := [][]string{{"eval", "--no-index"}, {"search", "본문", "--no-index"}, {"search", "본문"}}
	for _, command := range commands {
		t.Run(strings.Join(command, " "), func(t *testing.T) { tooNewCase(t, command) })
	}
}

func tooNewCase(t *testing.T, command []string) {
	memory := newRepo(t)
	if _, code := capture(t, func() int { return run(addArgs("본문 한 줄")) }); code != exitOK {
		t.Fatal("add 가 실패했다")
	}
	if _, code := capture(t, func() int { return run([]string{"index"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	newer := index.SchemaVersion + 5
	setUserVersion(t, memory, newer)
	before := readFile(t, index.DBPath(memory))
	out, code := captureBoth(t, func() int { return run(command) })
	if code != exitCheck {
		t.Fatalf("%v : 새 판 색인은 2 여야 한다 : %d (%s)", command, code, out)
	}
	if !strings.Contains(out, fmt.Sprintf("DB 판 %d", newer)) {
		t.Fatalf("%v : 낡은 exe 안내가 없다 : %q", command, out)
	}
	if version := userVersion(t, memory); version != newer {
		t.Fatalf("%v : 새 판 색인을 건드렸다 (판 %d)", command, version)
	}
	after := readFile(t, index.DBPath(memory))
	// 판이 맞을 때와 같은 여는 PRAGMA(auto_vacuum)가 머리말 셈값만 고친다 — 다시
	// 만들지는 않는다. `--no-index` 는 그것마저 안 쓴다.
	if len(after) != len(before) || (strings.Contains(strings.Join(command, " "), "--no-index") && after != before) {
		t.Fatalf("%v : 새 판 색인 파일이 바뀌었다 (%d → %d 바이트)", command, len(before), len(after))
	}
}

// --no-index 가 없으면 예전처럼 다시 만들고 그대로 돈다.
func TestStaleSchemaRebuildsWithoutNoIndex(t *testing.T) {
	memory := newRepo(t)
	if _, code := capture(t, func() int { return run(addArgs("본문 한 줄")) }); code != exitOK {
		t.Fatal("add 가 실패했다")
	}
	if _, code := capture(t, func() int { return run([]string{"index"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	setUserVersion(t, memory, 6)
	if out, code := capture(t, func() int { return run([]string{"search", "본문"}) }); code != exitOK {
		t.Fatalf("search 가 실패했다 : %d (%s)", code, out)
	}
	if version := userVersion(t, memory); version != index.SchemaVersion {
		t.Fatalf("다시 만들지 않았다 (판 %d)", version)
	}
}

func setUserVersion(t *testing.T, memory string, version int) {
	t.Helper()
	database, err := index.Open(memory)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.SQL().Exec(fmt.Sprintf("PRAGMA user_version=%d", version)); err != nil {
		t.Fatal(err)
	}
}

func userVersion(t *testing.T, memory string) int {
	t.Helper()
	handle, err := sql.Open("sqlite", index.DBPath(memory))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	version := 0
	if err := handle.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

// 자동 gc 가 모음 카드의 근거를 접어 카드가 [낡음] 이 되던 것 (자동쌓기A1후속 6절 ·
// 2026-10-06 재현). 사슬 맨 앞(덮이고 무효 날짜가 지난 것)이 첫 후보였다.
func TestAutoGCKeepsCardBasis(t *testing.T) {
	memory, all := b1Repo(t)
	out := mustRun(t, "consolidate", "--apply", "--json")
	report := consolidateReport{}
	if err := json.Unmarshal([]byte(out[strings.Index(out, "{"):]), &report); err != nil {
		t.Fatalf("JSON 이 아니다 : %v\n%s", err, out)
	}
	if len(report.Written) != 1 {
		t.Fatalf("카드 한 장을 써야 한다 : %+v", report)
	}
	// 근거 밖 옛 기억 하나 — gc 가 정말 도는지 보는 대조군이다.
	control := basisMemory("20260822-b1000009", model.TypeHistory, "옛 회선을 걷어냈다")
	control.Date = "2026-08-22"
	putMemory(t, memory, control)
	mustRun(t, "index")
	tomlPath := filepath.Join(memory, "mem.toml")
	settings := readFile(t, tomlPath)
	if strings.Contains(settings, "[gc]") {
		t.Fatalf("시험 준비 실패 — gc 절이 이미 있다 :\n%s", settings)
	}
	settings += "\n[gc]\nwarm_count = 1\nwarm_days = 1\ncold_count = 1\ncold_days = 1\n"
	if err := os.WriteFile(tomlPath, []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}
	gcOut := mustRun(t, "gc", "--json")
	mustRun(t, "index")
	if !strings.Contains(gcOut, control.ID) {
		t.Fatalf("시험이 헛돈다 — 근거 밖 기억도 안 접혔다 :\n%s", gcOut)
	}
	if strings.Contains(gcOut, all[0].ID) {
		t.Fatalf("카드 근거를 접었다 :\n%s", gcOut)
	}
	if found := mustRun(t, "search", "공유기", "--limit", "10"); strings.Contains(found, "[낡음]") {
		t.Fatalf("gc 뒤 카드가 낡았다 :\n%s", found)
	}
}

// 큐에 --link 가 두 번 쌓여도 둘 다 남는다 (같은 뿌리).
func TestSetLinkTwiceBeforeIndex(t *testing.T) {
	memory := newRepo(t)
	first := putLinked(t, memory, "20260105-dddd1111", nil)
	putLinked(t, memory, "20260105-dddd2222", nil)
	putLinked(t, memory, "20260105-dddd3333", nil)
	if _, code := capture(t, func() int { return run([]string{"index", "--full"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	for _, link := range []string{"20260105-dddd2222", "20260105-dddd3333"} {
		if _, code := capture(t, func() int { return run([]string{"set", first, "--link", link}) }); code != exitOK {
			t.Fatal("set --link 가 실패했다")
		}
	}
	if _, code := capture(t, func() int { return run([]string{"index"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	text := readFile(t, filepath.Join(memory, "store", "2026", "01", first+".md"))
	if !strings.Contains(text, "20260105-dddd2222") || !strings.Contains(text, "20260105-dddd3333") {
		t.Fatalf("앞의 --link 가 사라졌다 : %s", text)
	}
}
