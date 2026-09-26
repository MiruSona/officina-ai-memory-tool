package main

// 설계 2026-09-23 — ① add 가 제 큐 파일만 그 자리에서 승격한다 (2-6 시험)
// ② `mem index --bad` 보기 · 훅 알림 (3-5 시험).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// holdLock 은 남이 색인 중인 척 쓰기 락을 쥔다. add 의 기다림은 짧게 줄인다 —
// 1초를 다 기다리면 시험이 느려질 뿐 재는 것은 같다. 돌려주는 함수로 일찍 놓을
// 수 있고, 안 놓으면 시험 끝에 놓는다.
func holdLock(t *testing.T, memory string) func() {
	t.Helper()
	release, taken, err := index.TryLock(memory)
	if err != nil || !taken {
		t.Fatalf("락을 못 잡았다 : %v", err)
	}
	saved := index.PromoteWait
	index.PromoteWait = 60 * time.Millisecond
	done := false
	stop := func() {
		if done {
			return
		}
		done = true
		release()
		index.PromoteWait = saved
	}
	t.Cleanup(stop)
	return stop
}

// storedLine 은 끝줄 「저장됨 : <id>」 에서 id 를 꺼낸다. stdout 에는 관문 경고가
// id 앞에 섞일 수 있어 첫 줄을 믿지 않는다.
var storedLine = regexp.MustCompile(`저장됨 : (\d{8}-[0-9a-f]{8})`)

func storedID(t *testing.T, out string) string {
	t.Helper()
	found := storedLine.FindStringSubmatch(lastLine(out))
	if found == nil {
		t.Fatalf("끝줄에 저장 id 가 없다 : %s", out)
	}
	return found[1]
}

// lastLine 은 화면의 끝줄이다.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	return lines[len(lines)-1]
}

// add 직후 `show <id>` — 파일이 StorePath(id) 에 있고 본문이 나온다. index 없이.
func TestAddThenShowRightAway(t *testing.T) {
	memory := newRepo(t)
	out, code := captureBoth(t, func() int { return run(addArgs("바로 보려는 본문이다")) })
	if code != exitOK {
		t.Fatalf("add 가 실패했다 (%d) : %s", code, out)
	}
	id := storedID(t, out)
	if last := lastLine(out); last != i18n.T(i18n.AddStored, id) {
		t.Fatalf("끝줄이 「저장됨 : <id>」 가 아니다 : %s", last)
	}
	if _, err := os.Stat(storeFile(memory, id)); err != nil {
		t.Fatalf("store 파일이 없다 : %v", err)
	}
	if queuedCount(t, memory) != 0 {
		t.Fatal("큐에 남았다")
	}
	shown, code := capture(t, func() int { return run([]string{"show", id}) })
	if code != exitOK || !strings.Contains(shown, "바로 보려는 본문이다") {
		t.Fatalf("show 가 본문을 못 냈다 (%d) : %s", code, shown)
	}
}

// 같은 본문 두 번 — 두 번째 stdout 이 첫 번째 id 다. 이제 add 가 바로 색인해서
// 한 건씩 치면 관문(same-body)이 두 번째를 먼저 거절한다. 관문이 못 보는 길은
// 한 묶음 안의 같은 줄이라 그것으로 잰다 (승격 판정은 index 패키지 시험이 따로 본다).
func TestAddSameBodyTwiceGivesFirstID(t *testing.T) {
	memory := newRepo(t)
	line := jsonlLine(t, map[string]any{"type": "caution", "severity": "mid", "scope": "mem-search",
		"tags": []string{"index", "korean"}, "sources": []string{"file:internal/token/token.go"},
		"title":   "같은 본문을 두 번 넣는다",
		"summary": "같은 본문을 한 묶음에 두 번 넣으면 두 번째는 첫 번째 id 를 가리키는지 본다",
		"body":    jsonlBody("같은 줄이다.")})
	stdinOf(t, line+"\n"+line+"\n")
	out, code := capture(t, func() int { return run([]string{"add", "--jsonl"}) })
	if code != exitOK {
		t.Fatalf("묶음이 실패했다 (%d) : %s", code, out)
	}
	ids := strings.Fields(out)
	if len(ids) != 2 || ids[0] != ids[1] {
		t.Fatalf("두 번째가 첫 번째 id 여야 한다 : %q", out)
	}
	if _, err := os.Stat(storeFile(memory, ids[0])); err != nil {
		t.Fatalf("파일이 없다 : %v", err)
	}
	// 한 건짜리 add 가 쌍둥이를 받았을 때의 끝줄.
	shown, _ := captureBoth(t, func() int {
		return reportStored(&options{flags: map[string]bool{}, values: map[string]string{}}, store.Open(memory, false),
			store.AddRequest{}, index.Outcome{State: index.OutcomeDuplicate, ID: ids[0]}, index.Outcome{}, nil)
	})
	if last := lastLine(shown); last != i18n.T(i18n.AddStoredTwin, ids[0]) {
		t.Fatalf("끝줄이 중복 알림이 아니다 : %s", last)
	}
}

// 제목이 닮은 기억 — stdout 이 닮은 기억 id 이고 그 파일에 한 절이 붙었다.
func TestAddSimilarTitleAppends(t *testing.T) {
	memory := newRepo(t)
	first := strings.TrimSpace(mustRun(t, addArgs("첫 번째로 넣은 본문이다")...))
	out, code := captureBoth(t, func() int { return run(append(addArgs("두 번째로 붙을 본문이다"), "--new")) })
	if code != exitOK {
		t.Fatalf("두 번째 add 가 실패했다 (%d) : %s", code, out)
	}
	second := storedID(t, out)
	if second != first {
		t.Fatalf("닮은 기억 id 를 줘야 한다 : %s != %s\n%s", second, first, out)
	}
	if last := lastLine(out); last != i18n.T(i18n.AddStoredAppended, first) {
		t.Fatalf("끝줄이 붙임 알림이 아니다 : %s", last)
	}
	data, err := os.ReadFile(storeFile(memory, first))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "두 번째로 붙을 본문이다") {
		t.Fatalf("닮은 기억에 한 절이 안 붙었다 :\n%s", data)
	}
}

// 남이 락을 쥔 채 add — 큐 id · 「색인 대기」 · show 는 「아직 색인 전」 (지금 동작).
func TestAddUnderHeldLockQueues(t *testing.T) {
	memory := newRepo(t)
	holdLock(t, memory)
	out, code := captureBoth(t, func() int { return run(addArgs("락이 잡힌 동안 넣는 본문이다")) })
	if code != exitOK {
		t.Fatalf("add 가 실패했다 (%d) : %s", code, out)
	}
	id := storedID(t, out)
	if last := lastLine(out); last != i18n.T(i18n.AddQueuedNote, id) {
		t.Fatalf("끝줄이 색인 대기 알림이 아니다 : %s", last)
	}
	if queuedCount(t, memory) != 1 {
		t.Fatal("큐에 한 건이 있어야 한다")
	}
	shown, code := captureBoth(t, func() int { return run([]string{"show", id}) })
	if code != exitUsage || !strings.Contains(shown, "아직 색인 전이다") {
		t.Fatalf("색인 대기라고 안 말한다 (%d) : %s", code, shown)
	}
}

// `add --by X` — X 의 superseded_by 가 **파일이 있는** id 를 가리킨다. index 를
// 따로 안 돌려도 된다 (09-23 의 회차 갈림 한계가 재현 안 된다).
func TestAddByPointsToExistingFile(t *testing.T) {
	memory := newRepo(t)
	oldID := strings.TrimSpace(mustRun(t, addArgs("옛 결정 본문이다")...))
	out, code := captureBoth(t, func() int {
		return run(append(addArgs("옛 결정을 뒤집는 새 본문이다"), "--by", oldID, "--new"))
	})
	if code != exitOK {
		t.Fatalf("add --by 가 실패했다 (%d) : %s", code, out)
	}
	newID := storedID(t, out)
	if queuedCount(t, memory) != 0 {
		t.Fatal("덮임 표시가 큐에 남았다")
	}
	old, err := os.ReadFile(storeFile(memory, oldID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(old), "superseded_by: "+newID) {
		t.Fatalf("옛 기억이 새 id 를 안 가리킨다 (%s) :\n%s", newID, old)
	}
	if _, err := os.Stat(storeFile(memory, newID)); err != nil {
		t.Fatalf("가리키는 id 의 파일이 없다 : %v", err)
	}
	if last := lastLine(out); last != i18n.T(i18n.AddStored, newID) {
		t.Fatalf("끝줄이 저장 알림이 아니다 : %s", last)
	}
}

// `--by` 가 없는 옛 id 를 가리키면 add 는 들어가고 덮기만 실패했다고 끝줄에 쓴다.
func TestAddByMissingOldSaysByFailed(t *testing.T) {
	memory := newRepo(t)
	out, code := captureBoth(t, func() int {
		return run(append(addArgs("없는 옛 기억을 덮으려는 본문이다"), "--by", "20260101-abcdabcd", "--new"))
	})
	if code != exitUsage {
		t.Fatalf("덮기 실패는 1 이어야 한다 (%d) : %s", code, out)
	}
	newID := storedID(t, out)
	if _, err := os.Stat(storeFile(memory, newID)); err != nil {
		t.Fatalf("add 는 들어가야 한다 : %v", err)
	}
	if last := lastLine(out); last != i18n.T(i18n.AddQueuedNoBy, newID, "20260101-abcdabcd") {
		t.Fatalf("끝줄이 덮기 실패 알림이 아니다 : %s", last)
	}
}

// `--hold` 도 새 파일로 바로 남는다 (보류 칸이 붙은 채).
func TestAddHoldIsStoredRightAway(t *testing.T) {
	memory := newRepo(t)
	out, code := captureBoth(t, func() int { return run(append(addArgs("보류로 넣는 본문이다"), "--hold")) })
	if code != exitOK {
		t.Fatalf("add --hold 가 실패했다 (%d) : %s", code, out)
	}
	id := storedID(t, out)
	data, err := os.ReadFile(storeFile(memory, id))
	if err != nil {
		t.Fatalf("보류 기억 파일이 없다 : %v", err)
	}
	if !strings.Contains(string(data), "review: true") {
		t.Fatalf("보류 칸이 없다 :\n%s", data)
	}
}

// `--jsonl` 3줄 — 세 id 모두 파일이 있다.
func TestAddJSONLThreeStored(t *testing.T) {
	memory := newRepo(t)
	lines := []string{}
	for _, word := range []string{"첫째", "둘째", "셋째"} {
		lines = append(lines, jsonlLine(t, map[string]any{"type": "caution", "severity": "mid",
			"scope": "mem-search", "tags": []string{"index", "korean"},
			"sources": []string{"file:internal/token/token.go"},
			"title":   word + " 묶음 기억을 조심한다",
			"summary": word + " 줄로 들어온 묶음 기억이 바로 파일로 남는지 보는 요약이다 서른 자",
			"body":    jsonlBody(word + " 줄이다.")}))
	}
	stdinOf(t, strings.Join(lines, "\n")+"\n")
	out, code := capture(t, func() int { return run([]string{"add", "--jsonl", "--new"}) })
	if code != exitOK {
		t.Fatalf("묶음이 실패했다 (%d) : %s", code, out)
	}
	ids := strings.Fields(out)
	if len(ids) != 3 {
		t.Fatalf("id 가 세 줄이어야 한다 : %q", out)
	}
	for _, id := range ids {
		if _, err := os.Stat(storeFile(memory, id)); err != nil {
			t.Fatalf("%s 의 파일이 없다 : %v", id, err)
		}
	}
	if queuedCount(t, memory) != 0 {
		t.Fatal("큐에 남았다")
	}
}

// bad 결과의 화면 — stdout 은 비고, 끝줄이 「저장 안 됨」, 비밀정보면 4.
func TestReportBadCodes(t *testing.T) {
	out, code := captureBoth(t, func() int {
		return reportBad(index.Outcome{State: index.OutcomeBad, Reason: "`type` 이 틀렸다"})
	})
	if code != exitCheck || !strings.Contains(out, "저장 안 됨") || !strings.Contains(out, "mem index --bad") {
		t.Fatalf("규격 위반 bad 는 2 와 「저장 안 됨」 이어야 한다 (%d) : %s", code, out)
	}
	stdout, code := capture(t, func() int {
		return reportBad(index.Outcome{State: index.OutcomeBad, Reason: "비밀정보", Secret: true})
	})
	if code != exitSecurity || strings.TrimSpace(stdout) != "" {
		t.Fatalf("비밀정보 bad 는 4 이고 stdout 이 비어야 한다 (%d) : %q", code, stdout)
	}
}

// writeBad 는 inbox/bad 에 파일 하나를 (까닭과 함께) 넣는다.
func writeBad(t *testing.T, memory, name, body, reason string) {
	t.Helper()
	opened := store.Open(memory, false)
	if err := os.MkdirAll(opened.InboxBadDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opened.InboxBadDir(), name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if reason != "" {
		if err := opened.WriteBadReason(name, reason); err != nil {
			t.Fatal(err)
		}
	}
}

// `mem index --bad` — 종류 넷이 제 푸는 길을 찍고, 까닭 파일 없는 옛 항목도
// 찍히고, 보고 난 뒤 파일 수·내용이 그대로다.
func TestIndexBadShowsOnly(t *testing.T) {
	memory := newRepo(t)
	add, _ := json.Marshal(map[string]any{"op": "add", "type": "decision", "summary": "요약",
		"tags": []string{"a"}, "author": "human:x", "scope": "s", "body": "비밀 값 SECRETVALUE"})
	writeBad(t, memory, "1.1.1.json", string(add), "규격 위반 : tags")
	writeBad(t, memory, "2.1.1.json", `{"op":"patch","id":"20260920-aaaaaaaa","set":{"pinned":true}}`,
		"그런 기억이 없다 : 20260920-aaaaaaaa")
	writeBad(t, memory, "3.1.1.json", `{"op":"body","id":"20260920-bbbbbbbb","body":"새 본문"}`, "")
	writeBad(t, memory, "4.1.1.json", `{깨진 json`, "")
	before := badSnapshot(t, memory)
	out, code := capture(t, func() int { return run([]string{"index", "--bad"}) })
	if code != exitOK {
		t.Fatalf("--bad 가 실패했다 (%d) : %s", code, out)
	}
	fixAdd, fixDrop := i18n.T(i18n.IndexBadFixAdd), i18n.T(i18n.IndexBadFixDrop)
	wants := map[string][]string{
		"1.1.1.json": {"add", "규격 위반 : tags", fixAdd},
		"2.1.1.json": {"patch", "그런 기억이 없다", fixDrop},
		"3.1.1.json": {"body", i18n.T(i18n.IndexBadNoReason), fixDrop},
		"4.1.1.json": {i18n.T(i18n.IndexBadBroken), i18n.T(i18n.IndexBadNoReason), fixDrop},
	}
	for name, parts := range wants {
		row := lineWith(out, name)
		for _, part := range parts {
			if !strings.Contains(row, part) {
				t.Fatalf("%s 줄에 `%s` 가 없다 : %q\n%s", name, part, row, out)
			}
		}
	}
	if !strings.Contains(out, i18n.T(i18n.IndexBadHead, 4)) || !strings.Contains(out, "mem index --clear-bad") {
		t.Fatalf("머리·치우는 길이 없다 : %s", out)
	}
	if strings.Contains(out, "SECRETVALUE") {
		t.Fatalf("본문 값이 찍혔다 : %s", out)
	}
	if after := badSnapshot(t, memory); after != before {
		t.Fatalf("--bad 가 파일을 바꿨다 :\n%s\n---\n%s", before, after)
	}
}

// 비었으면 비었다고 말한다.
func TestIndexBadEmpty(t *testing.T) {
	newRepo(t)
	out, code := capture(t, func() int { return run([]string{"index", "--bad"}) })
	if code != exitOK || !strings.Contains(out, i18n.T(i18n.IndexBadNone)) {
		t.Fatalf("빈 bad 안내가 없다 (%d) : %s", code, out)
	}
}

// 훅 알림은 `--bad` 를 가리키고 파일 이름은 안 싣는다.
func TestHookBadNoticePointsToCommand(t *testing.T) {
	line := i18n.T(i18n.HookNoticeBad, 3)
	if !strings.Contains(line, "mem index --bad") || !strings.Contains(line, "--clear-bad") {
		t.Fatalf("알림이 --bad 를 안 가리킨다 : %s", line)
	}
	if strings.Contains(line, ".json") {
		t.Fatalf("알림에 파일 이름이 들어갔다 : %s", line)
	}
}

func lineWith(out, needle string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

// badSnapshot 은 inbox/bad 와 까닭 폴더의 이름·내용을 한 글로 모은다.
func badSnapshot(t *testing.T, memory string) string {
	t.Helper()
	opened := store.Open(memory, false)
	out := strings.Builder{}
	for _, dir := range []string{opened.InboxBadDir(), filepath.Join(opened.LocalDir(), "bad-reasons")} {
		entries, err := os.ReadDir(dir)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		for _, entry := range entries {
			data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			out.WriteString(entry.Name() + "=" + string(data) + "\n")
		}
	}
	return out.String()
}
