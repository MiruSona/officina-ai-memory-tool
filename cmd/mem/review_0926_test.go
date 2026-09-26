package main

// 리뷰 2026-09-26 — add 즉시 승격의 남은 갈래 (남이 먼저 승격 · 덮임 표시 대기
// · jsonl 부분 저장 · 색인을 새로 세울 판 · --json 승격 칸 · bad 까닭 중화).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// queueLikeAdd 는 add 와 같은 요청을 관문 없이 큐에만 넣는다 — 「add 가 큐에
// 쓴 뒤 락을 기다리는 사이」 를 만든다.
func queueLikeAdd(t *testing.T, body string) (*options, store.AddRequest, string) {
	t.Helper()
	parsed, err := parseOptions(addArgs(body)[1:], addBools, addValues)
	if err != nil {
		t.Fatal(err)
	}
	request := requestOf(parsed, parsed.text("body"))
	_, opened, err := openStore(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if err := opened.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	name, err := opened.WriteAdd(request)
	if err != nil {
		t.Fatal(err)
	}
	return parsed, request, name
}

// 남(`mem index`)이 add 의 큐 파일을 먼저 쌍둥이로 돌렸다. add 가 뒤늦게 락을
// 잡으면 「색인 대기 … id 가 바뀐다」 가 아니라 살아남은 쌍둥이 id 를 찍는다.
func TestAddAfterForeignPromoteGivesRealID(t *testing.T) {
	memory := newRepo(t)
	out, code := captureBoth(t, func() int { return run(addArgs("먼저 들어간 본문")) })
	if code != exitOK {
		t.Fatalf("첫 add 가 실패했다 (%d) : %s", code, out)
	}
	firstID := storedID(t, out)
	parsed, request, name := queueLikeAdd(t, "먼저 들어간 본문")
	queued := model.QueueID(name, request.Body, request.Date)
	if queued == firstID {
		t.Fatal("시험 전제가 틀렸다")
	}
	if _, code := capture(t, func() int { return run([]string{"index"}) }); code != exitOK {
		t.Fatalf("index 가 실패했다 (%d)", code)
	}
	repository, opened, err := openStore(parsed)
	if err != nil {
		t.Fatal(err)
	}
	stored := &addStored{Name: name, Queued: queued, Promoted: promoteNow(repository, opened, []string{name})}
	stdout, code := capture(t, func() int { return stored.report(parsed, opened, request) })
	if code != exitOK || strings.TrimSpace(stdout) != firstID {
		t.Fatalf("stdout 이 쌍둥이 id 여야 한다 (%d) : %q (쌍둥이 %s · 큐 %s)", code, stdout, firstID, queued)
	}
	both, _ := captureBoth(t, func() int { return stored.report(parsed, opened, request) })
	if last := lastLine(both); last != i18n.T(i18n.AddStoredTwin, firstID) {
		t.Fatalf("끝줄이 쌍둥이 알림이어야 한다 : %s", last)
	}
	if strings.Contains(both, "색인 대기") {
		t.Fatalf("이미 승격된 것을 색인 대기라고 했다 : %s", both)
	}
	if _, err := os.Stat(storeFile(memory, queued)); err == nil {
		t.Fatal("큐 id 의 파일이 생겼다")
	}
}

// 남이 먼저 먹었는데 실제 id 를 모르면(영수증 없음) stdout 을 비우고 따로 된
// 끝줄을 찍는다. 기억은 들어갔으니 0 이다.
func TestAddGoneSaysAlreadyPromoted(t *testing.T) {
	memory := newRepo(t)
	opened := store.Open(memory, false)
	parsed := &options{flags: map[string]bool{}, values: map[string]string{}}
	stored := &addStored{Name: "x.json", Queued: "20260926-aaaaaaaa", Promoted: &index.PromoteResult{
		Outcomes: map[string]index.Outcome{"x.json": {State: index.OutcomeGone, Foreign: true}}}}
	stdout, code := capture(t, func() int { return stored.report(parsed, opened, store.AddRequest{}) })
	if code != exitOK || strings.TrimSpace(stdout) != "" {
		t.Fatalf("stdout 이 비고 0 이어야 한다 (%d) : %q", code, stdout)
	}
	both, _ := captureBoth(t, func() int { return stored.report(parsed, opened, store.AddRequest{}) })
	if last := lastLine(both); last != i18n.T(i18n.AddGone) {
		t.Fatalf("끝줄이 「이미 승격됨」 이어야 한다 : %s", last)
	}
}

// `--by` 의 덮임 표시가 아직 안 먹혔으면(left · 결과 없음) 「덮었다」 고 말하지
// 않고 「덮임 표시는 색인 대기」 를 알린다. add 는 들어갔으니 끝줄은 저장됨·0.
func TestAddByPatchPendingIsNotClaimed(t *testing.T) {
	memory := newRepo(t)
	opened := store.Open(memory, false)
	old, id := "20260901-aaaaaaaa", "20260926-bbbbbbbb"
	parsed := &options{flags: map[string]bool{}, values: map[string]string{"by": old}}
	for _, patch := range []index.Outcome{{State: index.OutcomeLeft}, {}} {
		out, code := captureBoth(t, func() int {
			return reportStored(parsed, opened, store.AddRequest{}, index.Outcome{State: index.OutcomeNew, ID: id},
				patch, nil)
		})
		if code != exitOK {
			t.Fatalf("add 는 들어갔으니 0 이어야 한다 (%d) : %s", code, out)
		}
		if !strings.Contains(out, i18n.T(i18n.AddByPending, old, id)) {
			t.Fatalf("덮임 표시 대기를 알려야 한다 (%+v) : %s", patch, out)
		}
		if strings.Contains(out, i18n.T(i18n.SetSuperseded, old, id)) {
			t.Fatalf("안 먹힌 표시를 덮었다고 했다 (%+v) : %s", patch, out)
		}
		if last := lastLine(out); last != i18n.T(i18n.AddStored, id) {
			t.Fatalf("끝줄이 저장됨이어야 한다 : %s", last)
		}
	}
	// 먹혔으면 전처럼 덮었다고 한다.
	out, _ := captureBoth(t, func() int {
		return reportStored(parsed, opened, store.AddRequest{}, index.Outcome{State: index.OutcomeNew, ID: id},
			index.Outcome{State: index.OutcomeDone}, nil)
	})
	if !strings.Contains(out, i18n.T(i18n.SetSuperseded, old, id)) {
		t.Fatalf("먹힌 표시는 덮었다고 해야 한다 : %s", out)
	}
}

// jsonl — 관문을 다 지난 뒤 승격에서 한 줄만 bad 면 그 줄만 빠진다. stdout 은
// 들어간 줄의 id 뿐이고, 빠진 줄 번호와 까닭이 찍히고, 종료 2(비밀정보면 4)다.
func TestJSONLPartialBadKeepsOthers(t *testing.T) {
	memory := newRepo(t)
	opened := store.Open(memory, false)
	parsed := &options{flags: map[string]bool{}, values: map[string]string{}}
	for _, secret := range []bool{false, true} {
		promoted := &index.PromoteResult{Outcomes: map[string]index.Outcome{
			"a.json": {State: index.OutcomeNew, ID: "20260926-aaaaaaaa"},
			"b.json": {State: index.OutcomeBad, Reason: "규격 위반 : tags", Secret: secret},
			"c.json": {State: index.OutcomeAppended, ID: "20260926-cccccccc"},
		}}
		tally := jsonlTally{}
		stdout, code := capture(t, func() int {
			for at, name := range []string{"a.json", "b.json", "c.json"} {
				tally.report(parsed, opened, store.AddRequest{Title: name}, name, at+1, promoted)
			}
			return tally.finish()
		})
		want := exitCheck
		if secret {
			want = exitSecurity
		}
		if code != want {
			t.Fatalf("종료 코드가 %d 여야 한다 : %d", want, code)
		}
		if ids := strings.Fields(stdout); len(ids) != 2 || ids[0] != "20260926-aaaaaaaa" || ids[1] != "20260926-cccccccc" {
			t.Fatalf("들어간 두 줄의 id 만 찍어야 한다 : %q", stdout)
		}
		if tally.Stored != 2 || tally.Bad != 1 {
			t.Fatalf("셈이 틀렸다 : %+v", tally)
		}
	}
	// 화면 — 빠진 줄 번호와 까닭, 끝줄은 bad 셈.
	promoted := &index.PromoteResult{Outcomes: map[string]index.Outcome{
		"a.json": {State: index.OutcomeNew, ID: "20260926-aaaaaaaa"},
		"b.json": {State: index.OutcomeBad, Reason: "규격 위반 : tags"}}}
	tally := jsonlTally{}
	out, _ := captureBoth(t, func() int {
		tally.report(parsed, opened, store.AddRequest{}, "a.json", 1, promoted)
		tally.report(parsed, opened, store.AddRequest{}, "b.json", 2, promoted)
		return tally.finish()
	})
	if !strings.Contains(out, i18n.T(i18n.JSONLBadLine, 2, i18n.T(i18n.AddStoredBad, "규격 위반 : tags"))) {
		t.Fatalf("빠진 줄 번호와 까닭이 없다 : %s", out)
	}
	if last := lastLine(out); last != i18n.T(i18n.JSONLStoredBad, 1) {
		t.Fatalf("끝줄이 bad 셈이어야 한다 : %s", last)
	}
}

// 색인을 새로 세울 판이라 미뤘으면 큐 id 와 「색인은 다음 mem index」 끝줄이다.
func TestAddDeferredSaysNextIndex(t *testing.T) {
	memory := newRepo(t)
	opened := store.Open(memory, false)
	parsed := &options{flags: map[string]bool{}, values: map[string]string{}}
	stored := &addStored{Name: "x.json", Queued: "20260926-aaaaaaaa",
		Promoted: &index.PromoteResult{Deferred: true, Outcomes: map[string]index.Outcome{}}}
	out, code := captureBoth(t, func() int { return stored.report(parsed, opened, store.AddRequest{}) })
	if code != exitOK || lastLine(out) != i18n.T(i18n.AddQueuedFresh, "20260926-aaaaaaaa") {
		t.Fatalf("미룬 add 의 끝줄이 틀렸다 (%d) : %s", code, out)
	}
	if state, id := stored.promoteState(); state != index.OutcomeLeft || id != "20260926-aaaaaaaa" {
		t.Fatalf("--json 승격 칸은 left 와 큐 id 여야 한다 : %s %s", state, id)
	}
}

// index.db 를 지운 저장소(새로 받은 것과 같다)에서 add — store 전체 재색인을
// 떠안지 않고 큐에 둔다. 다음 `mem index` 뒤에는 들어간다.
func TestAddAfterIndexRemovedDefers(t *testing.T) {
	memory := newRepo(t)
	if out, code := captureBoth(t, func() int { return run(addArgs("먼저 있던 본문")) }); code != exitOK {
		t.Fatalf("첫 add 가 실패했다 (%d) : %s", code, out)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		os.Remove(index.DBPath(memory) + suffix)
	}
	out, code := captureBoth(t, func() int {
		return run(append(addArgs("색인을 지운 뒤 넣는 딴 본문"), "--new"))
	})
	if code != exitOK {
		t.Fatalf("add 가 실패했다 (%d) : %s", code, out)
	}
	// 관문은 색인을 새로 세우지 않고 읽기만 한다. 즉시 승격은 빈 색인에 대고
	// 승격하지 않고 큐에 둔다 — 「락 못 잡음」 과 다른 끝줄이다.
	last := lastLine(out)
	if last != i18n.T(i18n.AddQueuedFresh, storedID(t, out)) {
		t.Fatalf("색인을 새로 세울 판의 끝줄이어야 한다 : %s", last)
	}
	if queuedCount(t, memory) != 1 {
		t.Fatal("미뤘으면 큐에 있어야 한다")
	}
	if _, code := capture(t, func() int { return run([]string{"index"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	if queuedCount(t, memory) != 0 {
		t.Fatal("끝내 큐에 남았다")
	}
}

// `add --json` — 판정 JSON 에 승격 칸(promote · id)이 실리고 둘째 줄은 전처럼 id 다.
func TestAddJSONCarriesPromote(t *testing.T) {
	memory := newRepo(t)
	stdout, code := capture(t, func() int { return run(append(addArgs("제이슨으로 넣는 본문"), "--json")) })
	if code != exitOK {
		t.Fatalf("add --json 이 실패했다 (%d) : %s", code, stdout)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("JSON 한 줄과 id 한 줄이어야 한다 : %q", stdout)
	}
	verdict := map[string]any{}
	if err := json.Unmarshal([]byte(lines[0]), &verdict); err != nil {
		t.Fatalf("첫 줄이 JSON 이 아니다 : %v", err)
	}
	if verdict["promote"] != index.OutcomeNew || verdict["id"] != lines[1] || verdict["kind"] == nil {
		t.Fatalf("승격 칸이 틀렸다 : %v (둘째 줄 %s)", verdict, lines[1])
	}
	if _, err := os.Stat(storeFile(memory, lines[1])); err != nil {
		t.Fatalf("그 id 의 파일이 없다 : %v", err)
	}
}

// `--clear-bad` 도 `--bad` 처럼 이름·까닭을 중화해 찍는다. 까닭 파일은 한도까지만 읽는다.
func TestClearBadNeutralizesAndBadReasonIsCapped(t *testing.T) {
	memory := newRepo(t)
	writeBad(t, memory, "1.1.1.json", `{"op":"patch"}`, "까닭 \x1b[31m빨강\x1b[0m 끝")
	out, code := capture(t, func() int { return run([]string{"index", "--clear-bad"}) })
	if code != exitOK || strings.Contains(out, "\x1b") {
		t.Fatalf("제어 문자가 그대로 나갔다 (%d) : %q", code, out)
	}
	opened := store.Open(memory, false)
	writeBad(t, memory, "2.1.1.json", `{"op":"patch"}`, strings.Repeat("가", 1<<16))
	if got := opened.BadReason("2.1.1.json"); len(got) > 1<<16 {
		t.Fatalf("까닭을 한도 넘게 읽었다 : %d 바이트", len(got))
	}
	if got := opened.BadReason("../mem.toml"); got != "" {
		t.Fatalf("경로를 넘어 읽었다 : %q", got)
	}
	if err := os.WriteFile(filepath.Join(memory, "local", "bad-reasons", "x.err"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if opened.BadReason("x") != "ok" {
		t.Fatal("보통 까닭 파일을 못 읽는다")
	}
}
