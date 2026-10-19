package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// keys 칸의 열쇠는 관문이 안 훑어 inbox/new 에 들어갔다가 inbox/bad 에 값째 남았다.
// 큐에 넣기 전에 종료 4 로 막고, 값은 화면에 안 찍는다.
func TestAddRefusesSecretBeforeQueue(t *testing.T) {
	memory := newRepo(t)
	argvs := [][]string{
		addArgs("열쇠는 token=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123 이다"),
		append(addArgs("멀쩡한 본문이다"), "--keys", "AKIAIOSFODNN7EXAMPLE"),
	}
	for _, argv := range argvs {
		out, code := captureBoth(t, func() int { return run(argv) })
		if code != exitSecurity {
			t.Fatalf("종료 4 여야 한다 : %d %s", code, out)
		}
		if strings.Contains(out, "ghp_") || strings.Contains(out, "AKIA") {
			t.Fatalf("값을 찍었다 : %s", out)
		}
	}
	for _, dir := range []string{"new", "bad"} {
		if queued := queueFiles(t, filepath.Join(memory, "inbox", dir)); len(queued) != 0 {
			t.Fatalf("inbox/%s 에 파일이 생겼다 : %v", dir, queued)
		}
	}
}

// --jsonl 길도 줄마다 같은 관문(decodeJSONL 안 refuseSecret)을 지난다 — keys 의 열쇠는 종료 4, 큐 비움, 값 안 찍음.
func TestAddJSONLRefusesSecretKeys(t *testing.T) {
	memory := newRepo(t)
	line := jsonlLine(t, map[string]any{"type": "caution", "severity": "mid", "scope": "mem-search",
		"tags": []string{"index", "korean"}, "sources": []string{"file:internal/token/token.go"},
		"keys":    []string{"AKIAIOSFODNN7EXAMPLE"},
		"title":   "열쇠 칸에 비밀을 넣는다",
		"summary": "JSONL 한 줄의 keys 칸에 비밀 열쇠가 있으면 큐에 넣기 전에 막는지 본다",
		"body":    jsonlBody("멀쩡한 본문이다.")})
	stdinOf(t, line+"\n")
	out, code := captureBoth(t, func() int { return run([]string{"add", "--jsonl"}) })
	if code != exitSecurity {
		t.Fatalf("종료 4 여야 한다 : %d %s", code, out)
	}
	if strings.Contains(out, "AKIA") {
		t.Fatalf("값을 찍었다 : %s", out)
	}
	for _, dir := range []string{"new", "bad"} {
		if queued := queueFiles(t, filepath.Join(memory, "inbox", dir)); len(queued) != 0 {
			t.Fatalf("inbox/%s 에 파일이 생겼다 : %v", dir, queued)
		}
	}
}
