package main

// 2026-10-19 피드백 판 — set --sources · add 사용법 오류 한꺼번에 · 자동 거절 줄과 경고 가르기
// (Docs/Todo/2026-10-10-기억점검정리피드백.md · 2026-10-11-mem-add관문피드백.md).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
	"github.com/mirusona/officina-ai-memory-tool/internal/retain"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// review gone 큐가 권하는 `mem set <id> --sources …` 가 실제로 근거를 통째 갈아 끼운다.
func TestSetSourcesReplacesWhole(t *testing.T) {
	memory := newRepo(t)
	one := basisMemory("20261019-aaaa0001", model.TypeDecision, "근거를 통째로 바꾼다")
	putMemory(t, memory, one)
	mustRun(t, "index", "--full")
	mustRun(t, "set", one.ID, "--sources", "file:cmd/mem/cmd_set.go,url:https://example.com/a")
	queued := queueFiles(t, filepath.Join(memory, "inbox", "new"))
	if len(queued) != 1 || !strings.Contains(readFile(t, queued[0]), `"sources"`) {
		t.Fatalf("patch 에 sources 가 없다 : %v", queued)
	}
	mustRun(t, "index")
	text := readFile(t, storeFile(memory, one.ID))
	if !strings.Contains(text, "file:cmd/mem/cmd_set.go") || !strings.Contains(text, "url:https://example.com/a") {
		t.Fatalf("새 근거가 안 들어갔다 :\n%s", text)
	}
	if strings.Contains(text, "file:internal/index/schema.go") {
		t.Fatalf("옛 근거가 남았다 — 통째 교체가 아니다 :\n%s", text)
	}
}

// 별칭 `--source` 도 같은 칸이다. 피드백에서 사람이 단수로 친 일이 있다.
func TestSetSourceAlias(t *testing.T) {
	memory := newRepo(t)
	one := basisMemory("20261019-aaaa0002", model.TypeDecision, "단수 옵션으로 근거를 바꾼다")
	putMemory(t, memory, one)
	mustRun(t, "set", one.ID, "--source", "commit:abc1234")
	queued := queueFiles(t, filepath.Join(memory, "inbox", "new"))
	if len(queued) != 1 || !strings.Contains(readFile(t, queued[0]), "commit:abc1234") {
		t.Fatalf("--source 가 sources patch 로 안 갔다 : %v", queued)
	}
}

// 꼴이 틀린 근거·빈 근거는 큐에 넣기 전에 종료 2 로 거절한다.
func TestSetSourcesBadShapeIsTwo(t *testing.T) {
	memory := newRepo(t)
	one := basisMemory("20261019-aaaa0003", model.TypeDecision, "틀린 근거는 안 받는다")
	putMemory(t, memory, one)
	for _, given := range []string{"어디선가 봤다", ""} {
		out, code := captureBoth(t, func() int { return run([]string{"set", one.ID, "--sources", given}) })
		if code != exitCheck {
			t.Fatalf("%q 는 종료 2 여야 한다 : %d %s", given, code, out)
		}
	}
	if queued := queueFiles(t, filepath.Join(memory, "inbox", "new")); len(queued) != 0 {
		t.Fatalf("거절했는데 큐에 들어갔다 : %v", queued)
	}
}

// 새 근거에 비밀값이 섞이면 큐에 넣기 전에 종료 4 로 막고, 값은 화면에 안 찍는다.
func TestSetSourcesSecretIsFour(t *testing.T) {
	memory := newRepo(t)
	one := basisMemory("20261019-aaaa0004", model.TypeDecision, "비밀값 든 근거는 안 받는다")
	putMemory(t, memory, one)
	given := "url:https://example.com/?token=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123"
	out, code := captureBoth(t, func() int { return run([]string{"set", one.ID, "--sources", given}) })
	if code != exitSecurity || !strings.Contains(out, "sources") {
		t.Fatalf("비밀값 든 근거는 종료 4 여야 한다 : %d %s", code, out)
	}
	if strings.Contains(out, "ghp_") {
		t.Fatalf("비밀값을 화면에 찍었다 : %s", out)
	}
	if queued := queueFiles(t, filepath.Join(memory, "inbox", "new")); len(queued) != 0 {
		t.Fatalf("거절했는데 큐에 들어갔다 : %v", queued)
	}
}

// 남은 낱말과 본문 없음이 한 화면에 같이 나오고, 본문 없음이 먼저다 (10-11 피드백).
func TestAddUsageProblemsAtOnce(t *testing.T) {
	newRepo(t)
	argv := []string{"add", "--type", "issue", "--severity", "high", "--scope", "mem-search",
		"--tags", "index,korean", "--sources", "file:a.go", "file:b.go",
		"--summary", "두 글자 한글 검색어에 trigram 이 조용히 0건을 돌려줘 검색이 죽었다"}
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	before := os.Stdin
	os.Stdin = null
	out, code := captureBoth(t, func() int { return run(argv) })
	os.Stdin = before
	if code != exitUsage {
		t.Fatalf("사용법 오류는 종료 1 이다 : %d %s", code, out)
	}
	noBody := strings.Index(out, "본문을 못 받았다")
	stray := strings.Index(out, "옵션 값이 아닌 낱말")
	if noBody < 0 || stray < 0 {
		t.Fatalf("두 사유가 한 화면에 없다 :\n%s", out)
	}
	if noBody > stray {
		t.Fatalf("본문 없음이 맨 앞이어야 한다 :\n%s", out)
	}
	if !strings.Contains(out, "거절됨") {
		t.Fatalf("끝줄 「거절됨」 이 없다 :\n%s", out)
	}
}

// 자동 거절 줄에는 거절 등급 규칙만, 경고는 따로 한 줄이다 (10-11 피드백 secret-entropy).
func TestAutoRejectLineHoldsRejectsOnly(t *testing.T) {
	memory := newRepo(t)
	verdict := quality.Verdict{Kind: quality.KindQualityReject, Findings: []quality.Finding{
		{Rule: quality.RuleBodyThin, Level: quality.GradeReject, Reason: "본문이 얇다"},
		{Rule: quality.RuleSecretEntropy, Level: quality.GradeWarn, Reason: "비밀값 같다"},
	}}
	auto := autoAdd{Origin: "stop", Result: retain.Result{Rejects: []retain.Finding{{Rule: retain.RuleFragment, Reason: "지어낸 수"}}}}
	opened := store.Open(memory, false)
	out, code := capture(t, func() int { return rejectAuto(opened, verdict, auto, "후보 제목") })
	if code != exitCheck {
		t.Fatalf("품질 거절은 종료 2 다 : %d", code)
	}
	var rejectLine, warnLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "걸려 저장하지 않았다") {
			rejectLine = line
		}
		if strings.HasPrefix(line, "경고 : ") {
			warnLine = line
		}
	}
	if !strings.Contains(rejectLine, quality.RuleBodyThin) || !strings.Contains(rejectLine, retain.RuleFragment) {
		t.Fatalf("거절 줄에 거절 규칙이 없다 : %q", rejectLine)
	}
	if strings.Contains(rejectLine, quality.RuleSecretEntropy) {
		t.Fatalf("거절 줄에 경고 규칙이 섞였다 : %q", rejectLine)
	}
	if !strings.Contains(warnLine, quality.RuleSecretEntropy) {
		t.Fatalf("경고 줄이 따로 없다 :\n%s", out)
	}
	raw, _ := os.ReadFile(store.LogPath(memory))
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, quality.RuleBodyThin) && strings.Contains(line, quality.RuleSecretEntropy) {
			t.Fatalf("log.md 거절 줄에 경고 규칙이 섞였다 : %q", line)
		}
	}
}

// --json 은 칸을 더하기만 한다 — 옛 칸(findings·auto)은 그대로, 거절·경고 규칙 칸이 새로 붙는다.
func TestAutoJSONAddsRuleFields(t *testing.T) {
	verdict := quality.Verdict{Kind: quality.KindQualityReject, Findings: []quality.Finding{
		{Rule: quality.RuleBodyThin, Level: quality.GradeReject},
		{Rule: quality.RuleSecretEntropy, Level: quality.GradeWarn},
	}}
	auto := autoAdd{Origin: "stop", Result: retain.Result{Rejects: []retain.Finding{{Rule: retain.RuleFragment}}}}
	out, _ := capture(t, func() int { return printAutoJSON(verdict, auto) })
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("JSON 이 아니다 : %v %s", err, out)
	}
	for _, field := range []string{"kind", "findings", "origin", "auto", "reject_rules", "warn_rules"} {
		if _, ok := parsed[field]; !ok {
			t.Fatalf("%s 칸이 없다 : %s", field, out)
		}
	}
	rejects, _ := json.Marshal(parsed["reject_rules"])
	if strings.Contains(string(rejects), quality.RuleSecretEntropy) || !strings.Contains(string(rejects), retain.RuleFragment) {
		t.Fatalf("reject_rules 가 틀렸다 : %s", rejects)
	}
}
