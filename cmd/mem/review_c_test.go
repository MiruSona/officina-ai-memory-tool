package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 7단계 리뷰 C — tags --add-scope 가 vocab.toml 을 고치고, 그 순간부터 목록
// 밖 scope 는 거절이다 (실데이터 시험 C5·A2).
func TestTagsAddScope(t *testing.T) {
	memory := newRepo(t)
	out, code := capture(t, func() int { return run([]string{"tags", "--add-scope", "mygame"}) })
	if code != exitOK {
		t.Fatalf("tags --add-scope 가 실패했다 : %d (%s)", code, out)
	}
	raw, err := os.ReadFile(filepath.Join(memory, "vocab.toml"))
	if err != nil || !strings.Contains(string(raw), "mygame") {
		t.Fatalf("vocab.toml 에 안 들어갔다 : %v %s", err, raw)
	}
}

// tags --check 는 표준 밖 scope 도 세고, 늘리는 명령을 그대로 찍어 준다.
func TestTagsCheckShowsScopeAndSteps(t *testing.T) {
	newRepo(t)
	queueAndIndex(t, "scope 시험")
	writeVocab(t, "[tag]\n\"index\" = []\n\"korean\" = []\n\n[scope]\n\"딴것\" = []\n")
	out, code := capture(t, func() int { return run([]string{"tags", "--check"}) })
	if code != exitCheck {
		t.Fatalf("표준 밖 scope 가 있으면 2 여야 한다 : %d (%s)", code, out)
	}
	if !strings.Contains(out, "표준 밖 scope") {
		t.Fatalf("표준 밖 scope 를 안 센다 : %s", out)
	}
	if !strings.Contains(out, "mem tags --add-scope mem-search") {
		t.Fatalf("늘리는 명령을 안 알려준다 : %s", out)
	}
}

// 거절도 log.md 에 남는다 — S05(add-reject-rate)의 재료다 (설계 3-5).
func TestRejectIsLogged(t *testing.T) {
	memory := newRepo(t)
	if _, code := capture(t, func() int {
		return run([]string{"add", "--type", "decision", "--scope", "mem-search",
			"--tags", "index,korean", "--title", "짧", "--summary", "짧다", "--body", "한 줄"})
	}); code != exitCheck {
		t.Fatal("거절이어야 한다")
	}
	raw, err := os.ReadFile(filepath.Join(memory, "log.md"))
	if err != nil || !strings.Contains(string(raw), "거절") {
		t.Fatalf("거절이 기록에 안 남았다 : %v %s", err, raw)
	}
}

// S06 은 거절 등급만 센다. add 가 통과시킨 경고를 결함으로 세면 구조적으로
// 10% 아래로 못 내려간다 (실데이터 시험 4-4 · G6 ④).
func TestQualityDefectCountsRejectOnly(t *testing.T) {
	newRepo(t)
	// 근거가 note: 뿐이라 경고 하나가 붙지만 add 는 통과시킨다.
	if _, code := capture(t, func() int {
		return run(append(addArgs("경고만 붙는 기억"), "--sources", "note:회의에서 정했다"))
	}); code != exitOK {
		t.Fatal("경고는 저장을 막지 않는다")
	}
	if _, code := capture(t, func() int { return run([]string{"index"}) }); code != exitOK {
		t.Fatal("색인이 실패했다")
	}
	out, _ := capture(t, func() int { return run([]string{"status", "--quality"}) })
	line := rowOf(out, "quality-defect-rate")
	if !strings.Contains(line, "0.00") {
		t.Fatalf("경고를 결함으로 세고 있다 : %s", line)
	}
	if warn := rowOf(out, "warn-per-memory"); strings.Contains(warn, "0.00") {
		t.Fatalf("경고를 아예 안 세면 그것대로 잘못이다 : %s", warn)
	}
}

func rowOf(out, name string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, name) {
			return line
		}
	}
	return ""
}

func writeVocab(t *testing.T, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join("Memory", "vocab.toml"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
