package main

// 사용 피드백 반영 3 (2026-09-23) — --by 합쳐짐 · tags 기본 목록 · set 모르는 옵션 ·
// 경고 본문을 끝줄 가까이.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
)

// `add --by <옛id>` 로 넣은 기억은 색인 뒤에도 add 가 알려 준 id 로 남는다.
// 전에는 제목이 닮은 옛 기억에 합쳐져 id 가 사라졌다 (사용 피드백 2026-09-20).
func TestAddByKeepsNewIDAfterIndex(t *testing.T) {
	newRepo(t)
	oldOut, code := capture(t, func() int { return run(addArgs("옛 결정 본문이다")) })
	if code != 0 {
		t.Fatalf("첫 add 가 실패했다 (%d) : %s", code, oldOut)
	}
	oldID := strings.TrimSpace(oldOut)
	mustRun(t, "index")
	newOut, code := capture(t, func() int {
		return run(append(addArgs("옛 결정을 뒤집는 새 본문이다"), "--by", oldID, "--new"))
	})
	if code != 0 {
		t.Fatalf("add --by 가 실패했다 (%d) : %s", code, newOut)
	}
	newID := strings.TrimSpace(newOut)
	mustRun(t, "index")
	shown := mustRun(t, "show", newID)
	if !strings.Contains(shown, newID) {
		t.Fatalf("add 가 알려 준 id 가 색인 뒤에 없다 : %s", shown)
	}
	old := mustRun(t, "show", oldID)
	if strings.Contains(old, "옛 결정을 뒤집는 새 본문이다") {
		t.Fatalf("덮는 기억이 옛 기억에 합쳐졌다 : %s", old)
	}
}

// 인자 없이 친 `mem tags` 는 --list 와 같다.
func TestTagsWithoutArgsLists(t *testing.T) {
	newRepo(t)
	bare := mustRun(t, "tags")
	listed := mustRun(t, "tags", "--list")
	if bare != listed {
		t.Fatalf("인자 없는 tags 가 --list 와 다르다 :\n%s\n---\n%s", bare, listed)
	}
	// --apply 만 주면 --rename 이 빠졌다고 알린다. 목록으로 떨어지지 않는다.
	out, code := captureBoth(t, func() int { return run([]string{"tags", "--apply"}) })
	if code != exitUsage || !strings.Contains(out, "--rename") {
		t.Fatalf("--apply 만 줬는데 --rename 을 안 알린다 (%d) : %s", code, out)
	}
}

// `mem set` 에 모르는 옵션을 주면 add 와 무엇이 다른지 한 줄 더 알린다.
func TestSetUnknownOptionPointsToAdd(t *testing.T) {
	newRepo(t)
	out, code := captureBoth(t, func() int {
		return run([]string{"set", "20260921-abcdabcd", "--by-new", "--type", "decision"})
	})
	if code != exitUsage {
		t.Fatalf("종료 코드가 1 이어야 한다 : %d (%s)", code, out)
	}
	for _, want := range []string{"--type", "mem add"} {
		if !strings.Contains(out, want) {
			t.Fatalf("`%s` 가 오류문에 없다 : %s", want, out)
		}
	}
}

// 경고 알림 줄 바로 밑에 규칙 이름과 까닭이 붙는다.
func TestWarnNoteListsRulesBelow(t *testing.T) {
	verdict := quality.Verdict{Kind: quality.KindWarn, Findings: []quality.Finding{
		{Rule: "duplicate-soft", Reason: "닮은 기억이 있다", Level: quality.GradeWarn}}}
	out, _ := captureBoth(t, func() int { warnNote(verdict); return 0 })
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("알림 한 줄 + 경고 한 줄이어야 한다 : %q", lines)
	}
	if !strings.Contains(lines[1], "duplicate-soft") || !strings.Contains(lines[1], "닮은 기억이 있다") {
		t.Fatalf("경고 본문이 알림 줄 밑에 없다 : %q", lines)
	}
}

// 리뷰 2026-09-23 #1 — 중복 관문과 결정 관문이 같이 걸리면 중복 관문의 덮기
// 줄(`mem set … --by-new`)이 먼저 나왔다. 「다른 주제다(--new)」가 맨 앞이다.
func TestNextStepsPutNewTopicFirst(t *testing.T) {
	verdict := quality.Verdict{Kind: quality.KindQualityReject, Findings: []quality.Finding{
		{Rule: quality.RuleDuplicateHard, Level: quality.GradeReject,
			Next: []string{"mem set 20260923-aaaaaaaa --by-new   (그 기억을 이 내용으로 덮는다)", quality.NewTopicStep}},
		{Rule: quality.RuleDecisionGate, Level: quality.GradeReject,
			Next: []string{quality.NewTopicStep, "mem add … --by 20260923-aaaaaaaa   (그 결정을 이 기억이 뒤집을 때만 덮는다)"}},
	}}
	out, _ := captureBoth(t, func() int { printNextSteps(os.Stdout, verdict); return 0 })
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 {
		t.Fatalf("머리 한 줄 + 다음 수 세 줄이어야 한다 : %q", lines)
	}
	if strings.TrimSpace(lines[1]) != strings.TrimSpace(quality.NewTopicStep) {
		t.Fatalf("첫 다음 수가 --new 가 아니다 : %q", lines)
	}
}

// 리뷰 2026-09-23 #3 — JSONL 줄의 supersedes 는 비운다 (--by 검사를 안 거친다).
func TestJSONLDropsSupersedes(t *testing.T) {
	memory := newRepo(t)
	line := jsonlLine(t, map[string]any{"type": "caution", "severity": "mid", "scope": "mem-search",
		"tags": []string{"index", "korean"}, "sources": []string{"file:internal/token/token.go"},
		"title":   "두 글자 검색어를 조심한다",
		"summary": "두 글자 한글 검색어는 trigram 에 안 잡혀 0건이 되니 색인 쪽을 먼저 본다",
		"body":    jsonlBody("첫째 줄이다."), "supersedes": "../../bad"})
	stdinOf(t, line+"\n")
	out, code := captureBoth(t, func() int { return run([]string{"add", "--jsonl"}) })
	if code != exitOK {
		t.Fatalf("묶음이 거절됐다 : %d (%s)", code, out)
	}
	entries, err := os.ReadDir(filepath.Join(memory, "inbox", "new"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("큐에 한 건이어야 한다 : %v %v", entries, err)
	}
	data, err := os.ReadFile(filepath.Join(memory, "inbox", "new", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "supersedes") {
		t.Fatalf("supersedes 가 큐에 실렸다 : %s", data)
	}
}

// 리뷰 2026-09-23 #8 — 「새 기억은 mem add」 안내는 add 에만 있는 옵션일 때만 붙는다.
func TestSetHintOnlyForAddOptions(t *testing.T) {
	newRepo(t)
	out, code := captureBoth(t, func() int {
		return run([]string{"set", "20260921-abcdabcd", "--sumary", "오타"})
	})
	if code != exitUsage || !strings.Contains(out, "--sumary") {
		t.Fatalf("오타를 모르는 옵션으로 안 막았다 (%d) : %s", code, out)
	}
	if strings.Contains(out, "mem add") {
		t.Fatalf("오타에 add 안내가 붙었다 : %s", out)
	}
	out, _ = captureBoth(t, func() int {
		return run([]string{"set", "20260921-abcdabcd", "--hold"})
	})
	if !strings.Contains(out, "mem add") {
		t.Fatalf("add 에만 있는 --hold 에 안내가 없다 : %s", out)
	}
}
