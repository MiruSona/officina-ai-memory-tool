package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 리뷰 A·C 가 짚은 자리마다 회귀 하나. 값이 새는지가 아니라 **막히는지**를 본다.

const fakeKey = "sk-ant-api03ABCDEFGHIJKLMNOPQRSTUVWXYZ0123"

func queuedCount(t *testing.T, memory string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(memory, "inbox", "new"))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

// stdinOf 는 표준입력을 준 글월로 갈아 끼운다.
func stdinOf(t *testing.T, text string) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteString(text); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	before := os.Stdin
	os.Stdin = reader
	t.Cleanup(func() { os.Stdin = before })
}

// 리뷰 A #2 · C #3 — add 는 비밀정보를 그 자리에서 막는다.
// v0.2 는 보안 차단에 종료 코드 4 를 준다 (설계 6-1).
func TestAddRefusesSecret(t *testing.T) {
	memory := newRepo(t)
	out, code := capture(t, func() int { return run(addArgs("키는 " + fakeKey + " 이다")) })
	if code != exitSecurity {
		t.Fatalf("보안 차단은 4 여야 한다 : %d (%s)", code, out)
	}
	if strings.Contains(out, fakeKey) {
		t.Fatal("맞은 값을 화면에 찍으면 안 된다")
	}
	if queuedCount(t, memory) != 0 {
		t.Fatal("막힌 기억이 큐에 들어갔다")
	}
}

// 리뷰 A #1 — set --summary 도 같은 차단선을 지난다.
func TestSetRefusesSecret(t *testing.T) {
	memory := newRepo(t)
	_, code := capture(t, func() int {
		return run([]string{"set", "20260823-8d27dbfc", "--summary",
			"토큰은 ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123 라서 못 쓴다는 긴 요약이다 여기까지"})
	})
	if code != exitSecurity {
		t.Fatalf("보안 차단은 4 여야 한다 : %d", code)
	}
	if queuedCount(t, memory) != 0 {
		t.Fatal("막힌 고치기가 큐에 들어갔다")
	}
}

// 리뷰 A #1 — set --body 도 마찬가지다.
func TestSetBodyRefusesSecret(t *testing.T) {
	memory := newRepo(t)
	_, code := capture(t, func() int {
		return run([]string{"set", "20260823-8d27dbfc", "--body", "첫 줄\n키 " + fakeKey})
	})
	if code != exitSecurity {
		t.Fatalf("보안 차단은 4 여야 한다 : %d", code)
	}
	if queuedCount(t, memory) != 0 {
		t.Fatal("막힌 본문이 큐에 들어갔다")
	}
}

// 리뷰 C #5 — set 은 값을 그 자리에서 검사한다. 나중에 조용히 실패하지 않는다.
// 품질 관문(model.Validate) 이 거절한 값은 add 와 같은 종료 코드 2, id 꼴처럼
// usage 오류인 것은 1 이다 (진행상황.md 145줄).
func TestSetRefusesBadValues(t *testing.T) {
	memory := newRepo(t)
	usageCases := [][]string{
		{"set", "20260823-8d27dbfc", "--by", "99999999-zzzzzzzz"},
	}
	for _, argv := range usageCases {
		_, code := capture(t, func() int { return run(argv) })
		if code != exitUsage {
			t.Errorf("%v : 종료 코드가 1 이어야 한다 (%d)", argv[2:], code)
		}
	}
	checkCases := [][]string{
		{"set", "20260823-8d27dbfc", "--scope", "한글범위"},
		{"set", "20260823-8d27dbfc", "--status", "열림"},
		{"set", "20260823-8d27dbfc", "--severity", "아주높음"},
		{"set", "20260823-8d27dbfc", "--invalid-at", "2026/08/23"},
		{"set", "20260823-8d27dbfc", "--summary", "짧다"},
		{"set", "20260823-8d27dbfc", "--tags", "a,b,c,d,e,f"},
	}
	for _, argv := range checkCases {
		_, code := capture(t, func() int { return run(argv) })
		if code != exitCheck {
			t.Errorf("%v : 종료 코드가 2 여야 한다 (%d)", argv[2:], code)
		}
	}
	if queuedCount(t, memory) != 0 {
		t.Fatal("거절한 것이 큐에 들어갔다")
	}
}

// 리뷰 C #1 — --check 는 미리보기다. 무슨 일이 있어도 저장하지 않는다.
func TestAddCheckNeverStores(t *testing.T) {
	memory := newRepo(t)
	out, code := capture(t, func() int {
		return run(append(addArgs("저장되면 안 된다"), "--check"))
	})
	if code != exitOK {
		t.Fatalf("색인이 없으면 0 이어야 한다 : %d (%s)", code, out)
	}
	if queuedCount(t, memory) != 0 {
		t.Fatal("--check 가 큐에 넣었다")
	}
	if !strings.Contains(out, "넣지 않았다") {
		t.Fatalf("--check 라서 안 넣었다고 말해야 한다 : %s", out)
	}
}

// 리뷰 C #1 — 색인이 있으면 닮은 것을 보여주고, 그래도 저장은 안 한다.
func TestAddCheckShowsSimilar(t *testing.T) {
	memory := newRepo(t)
	if _, code := capture(t, func() int { return run(addArgs("첫 본문")) }); code != exitOK {
		t.Fatal("add 가 실패했다")
	}
	if _, code := capture(t, func() int { return run([]string{"index", "--quiet"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	out, code := capture(t, func() int {
		return run(append(addArgs("아주 다른 본문이다"), "--check"))
	})
	if code != exitCheck {
		t.Fatalf("닮은 것을 찾으면 2 여야 한다 : %d (%s)", code, out)
	}
	if !strings.Contains(out, "닮은 기억") {
		t.Fatalf("닮은 기억 표가 없다 : %s", out)
	}
	if queuedCount(t, memory) != 0 {
		t.Fatal("--check 가 큐에 넣었다")
	}
}

// 설계 8-1 — --jsonl 은 한 줄에 한 건이다. 한 줄이라도 나쁘면 하나도 안 넣는다.
func TestAddJSONLAllOrNothing(t *testing.T) {
	memory := newRepo(t)
	good := map[string]any{"type": "history", "scope": "aimemorytool", "tags": []string{"doc", "test"},
		"title": "한 줄 JSON 으로 넣기", "author": "human:tester",
		"summary": "한 줄 JSON 으로 들어온 기억이 제대로 큐에 들어가는지 보는 요약이다",
		"body":    jsonlBody("한 줄 JSON 이 큐에 그대로 들어간다.")}
	bad := map[string]any{"type": "history", "scope": "aimemorytool", "tags": []string{"doc", "test"},
		"title": "비밀정보가 든 줄", "author": "human:tester",
		"summary": "비밀정보가 든 줄은 묶음 전체를 막아야 한다는 것을 보는 긴 요약이다",
		"body":    jsonlBody("키 " + fakeKey)}
	first, _ := json.Marshal(good)
	second, _ := json.Marshal(bad)
	stdinOf(t, string(first)+"\n"+string(second)+"\n")
	_, code := capture(t, func() int { return run([]string{"add", "--jsonl"}) })
	if code != exitSecurity {
		t.Fatalf("비밀정보가 있으면 4 여야 한다 : %d", code)
	}
	if queuedCount(t, memory) != 0 {
		t.Fatalf("한 줄이라도 나쁘면 하나도 안 넣어야 한다 : %d", queuedCount(t, memory))
	}
	stdinOf(t, string(first)+"\n"+string(first)+"\n")
	if _, code := capture(t, func() int { return run([]string{"add", "--jsonl"}) }); code != exitOK {
		t.Fatalf("멀쩡한 묶음은 0 이어야 한다 : %d", code)
	}
	if queuedCount(t, memory) != 2 {
		t.Fatalf("두 건이 큐에 있어야 한다 : %d", queuedCount(t, memory))
	}
}

// 리뷰 A #3 — 전역 저장소가 없으면 조용히 프로젝트로 안 떨어진다.
// 7단계 리뷰 — 전역 저장소를 없앴다(설계 6-3). `--global` 은 모르는 옵션이라
// 종료 1 이고, 말없이 프로젝트 저장소에 넣지 않는다.
func TestGlobalFlagIsGone(t *testing.T) {
	memory := newRepo(t)
	_, code := capture(t, func() int { return run(append(addArgs("전역에 넣으려 했다"), "--global")) })
	if code != exitUsage {
		t.Fatalf("종료 코드가 1 이어야 한다 : %d", code)
	}
	if queuedCount(t, memory) != 0 {
		t.Fatal("프로젝트 저장소에 들어갔다")
	}
}

// 리뷰 C #9 — show 의 --json · --head · --from-archive 가 실제로 돈다.
func TestShowJSONAndHead(t *testing.T) {
	newRepo(t)
	out, _ := capture(t, func() int { return run(addArgs("첫 줄\n둘째 줄\n셋째 줄")) })
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
	head, code := capture(t, func() int { return run([]string{"show", id, "--head", "1"}) })
	if code != exitOK || strings.Contains(head, "둘째 줄") {
		t.Fatalf("--head 1 이 본문을 안 잘랐다 : %s", head)
	}
	if _, code := capture(t, func() int { return run([]string{"show", id, "--head", "0"}) }); code != exitUsage {
		t.Fatal("--head 0 은 거절해야 한다")
	}
}

// 리뷰 A #6 — 훅은 어떤 인자를 받아도 stdout 에 JSON 아닌 것을 안 찍는다.
func TestHookHelpKeepsStdoutEmpty(t *testing.T) {
	newRepo(t)
	for _, argv := range [][]string{{"hook", "-h"}, {"hook", "--help"}, {"hook", "help"}} {
		out, code := capture(t, func() int { return run(argv) })
		if code != exitOK {
			t.Errorf("%v : 훅은 늘 0 이어야 한다 (%d)", argv, code)
		}
		if strings.TrimSpace(out) != "" {
			t.Errorf("%v : stdout 이 비어 있어야 한다 : %q", argv, out)
		}
	}
}

// 리뷰 C #14 — inbox/bad 를 치우는 자리가 있다.
func TestIndexClearBad(t *testing.T) {
	memory := newRepo(t)
	bad := filepath.Join(memory, "inbox", "bad")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "1.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := capture(t, func() int { return run([]string{"index", "--clear-bad"}) })
	if code != exitOK {
		t.Fatalf("--clear-bad 가 실패했다 : %s", out)
	}
	left, _ := os.ReadDir(bad)
	if len(left) != 0 {
		t.Fatalf("치우지 않았다 : %d", len(left))
	}
}

// 리뷰 C #17 — --이름=값 꼴은 참·거짓 옵션에도 통한다.
func TestBoolOptionTakesValue(t *testing.T) {
	newRepo(t)
	if _, code := capture(t, func() int { return run(append(addArgs("본문"), "--pin=true")) }); code != exitOK {
		t.Fatalf("--pin=true 를 못 읽었다 : %d", code)
	}
	if _, code := capture(t, func() int { return run(append(addArgs("본문 둘"), "--pin=어쩌구")) }); code != exitUsage {
		t.Fatal("모르는 참·거짓 값은 거절해야 한다")
	}
}

// 리뷰 C #12 — 갓 만든 빈 저장소는 「고장」 이 아니다.
func TestFreshRepoIsNotBroken(t *testing.T) {
	data := statusData{Total: 0, Queued: 0, HasIndex: false, InPath: true, HookOn: true}
	if !healthy(&data) {
		t.Fatal("빈 저장소를 고장으로 봤다")
	}
	data.Total = 1
	if healthy(&data) {
		t.Fatal("기억이 있는데 색인이 없으면 문제다")
	}
}

// jsonlBody 는 본문 규격(첫 줄 결론 · 3줄 이상)을 갖춘 시험용 본문이다.
func jsonlBody(head string) string {
	return "한 줄 JSON 묶음이 전부 들어가거나 하나도 안 들어가는지 본다." + "\n\n" + head +
		"\n두 줄만으로는 본문 규격을 못 채운다." + "\n그래서 한 줄을 더 적는다."
}
