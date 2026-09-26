package main

// 2026-09-20 사용 피드백에서 나온 자리들 — 저장/거절을 마지막 줄로 · 색인 대기
// id 로 show · severity 별칭 · todo_status 기본값 · 공백 sources · tags --list.

import (
	"strings"
	"testing"
)

// 거절이면 화면 끝에 「거절됨」이 나오고 --check 를 권한다.
func TestAddSaysRejectedAtTheEnd(t *testing.T) {
	newRepo(t)
	out, code := captureBoth(t, func() int {
		return run([]string{"add", "--type", "decision", "--scope", "mem-search",
			"--tags", "index,korean", "--title", "짧", "--summary", "짧다", "--body", "한 줄"})
	})
	if code != exitCheck {
		t.Fatalf("거절이어야 한다 : %d", code)
	}
	if !strings.Contains(out, "거절됨") || !strings.Contains(out, "--check") {
		t.Fatalf("거절 한 줄이 없다 : %s", out)
	}
}

// 저장이면 「저장됨 : <id>」 가 **맨 마지막 줄**이다. 바로 승격됐으면 색인
// 대기 말이 없고, 락을 못 잡아 큐에 남았으면 「mem index 뒤에」 가 붙는다.
func TestAddSaysStoredAsLastLine(t *testing.T) {
	memory := newRepo(t)
	out, code := captureBoth(t, func() int { return run(addArgs("다섯 줄짜리 본문이다")) })
	if code != exitOK {
		t.Fatalf("통과여야 한다 : %d (%s)", code, out)
	}
	if last := lastLine(out); !strings.HasPrefix(last, "저장됨 : 2026") || strings.Contains(last, "mem index") {
		t.Fatalf("마지막 줄이 저장 알림이 아니다 : %s", last)
	}
	holdLock(t, memory)
	out, code = captureBoth(t, func() int { return run(append(addArgs("락이 잡힌 동안의 다른 본문이다"), "--new")) })
	if code != exitOK {
		t.Fatalf("통과여야 한다 : %d (%s)", code, out)
	}
	if last := lastLine(out); !strings.HasPrefix(last, "저장됨 : 2026") || !strings.Contains(last, "mem index") {
		t.Fatalf("마지막 줄이 색인 대기 알림이 아니다 : %s", last)
	}
}

// 색인 대기 id 를 show 하면 「없다」가 아니라 「아직 색인 전이다」다.
func TestShowTellsQueuedID(t *testing.T) {
	memory := newRepo(t)
	// 남이 색인 중이라 add 가 락을 못 잡고 큐에 둔 경우다.
	holdLock(t, memory)
	added, code := capture(t, func() int { return run(addArgs("색인 전에 보려는 본문이다")) })
	if code != exitOK {
		t.Fatalf("넣기가 실패했다 : %s", added)
	}
	id := strings.TrimSpace(added)
	out, code := captureBoth(t, func() int { return run([]string{"show", id}) })
	if code != exitUsage {
		t.Fatalf("종료 코드가 1 이어야 한다 : %d", code)
	}
	if !strings.Contains(out, "아직 색인 전이다") || !strings.Contains(out, id) {
		t.Fatalf("색인 대기라고 안 말한다 : %s", out)
	}
	// 정말 없는 id 는 그대로 「없다」다.
	missing, _ := captureBoth(t, func() int { return run([]string{"show", "20260101-abcdabcd"}) })
	if !strings.Contains(missing, "그런 기억이 없다") {
		t.Fatalf("없는 id 안내가 바뀌었다 : %s", missing)
	}
}

// --severity medium 은 mid 로 바꿔 받고, 바꿨다고 말한다.
func TestSeverityMediumIsAccepted(t *testing.T) {
	newRepo(t)
	argv := append([]string{}, addArgs("severity 별칭을 보는 본문이다")...)
	for at, word := range argv {
		if word == "high" {
			argv[at] = "medium"
		}
	}
	out, code := captureBoth(t, func() int { return run(argv) })
	if code != exitOK {
		t.Fatalf("medium 이 거절됐다 : %d (%s)", code, out)
	}
	if !strings.Contains(out, "severity") || !strings.Contains(out, "mid") {
		t.Fatalf("바꿨다고 안 말한다 : %s", out)
	}
}

// todo 는 --todo-status 를 안 줘도 open 으로 들어간다.
func TestTodoStatusDefaultsToOpen(t *testing.T) {
	newRepo(t)
	out, code := captureBoth(t, func() int {
		return run([]string{"add", "--type", "todo", "--scope", "mem-search",
			"--tags", "index,korean", "--sources", "file:internal/token/token.go",
			"--title", "낱말 자르기를 고친다",
			"--summary", "두 글자 한글 검색어가 0건이 되는 자리를 trigram 쪽에서 고쳐야 한다",
			"--body", "trigram 이 두 글자를 못 잡는다.\n내일 internal/token 을 고친다.\n2026-09-30 까지 끝낸다."})
	})
	if code != exitOK {
		t.Fatalf("todo_status 없이 거절됐다 : %d (%s)", code, out)
	}
	if !strings.Contains(out, "todo_status") || !strings.Contains(out, "open") {
		t.Fatalf("기본값을 뒀다고 안 말한다 : %s", out)
	}
}

// 옵션 값이 아닌 낱말은 조용히 먹지 않고 거절한다.
func TestAddRefusesStrayWords(t *testing.T) {
	newRepo(t)
	argv := append(addArgs("공백 sources 를 보는 본문이다"), "note:둘째근거")
	out, code := captureBoth(t, func() int { return run(argv) })
	if code != exitUsage {
		t.Fatalf("종료 코드가 1 이어야 한다 : %d (%s)", code, out)
	}
	if !strings.Contains(out, "쉼표로 잇는다") {
		t.Fatalf("쉼표를 안 알려준다 : %s", out)
	}
}

// tags --list 는 표준 태그와 scope 를 그대로 보여준다.
func TestTagsListShowsStandard(t *testing.T) {
	newRepo(t)
	out, code := capture(t, func() int { return run([]string{"tags", "--list"}) })
	if code != exitOK {
		t.Fatalf("종료 코드가 0 이어야 한다 : %d (%s)", code, out)
	}
	for _, want := range []string{"표준 태그", "index", "fts5", "표준 scope", "mem-search"} {
		if !strings.Contains(out, want) {
			t.Fatalf("`%s` 가 목록에 없다 : %s", want, out)
		}
	}
}

// version 은 판과 빌드 표시를 찍는다.
func TestVersionPrintsBuildMarks(t *testing.T) {
	out, code := capture(t, func() int { return run([]string{"version"}) })
	if code != exitOK {
		t.Fatalf("종료 코드가 0 이어야 한다 : %d", code)
	}
	if !strings.HasPrefix(out, "mem ") || !strings.Contains(out, devMark) {
		t.Fatalf("판·빌드 표시가 없다 : %s", out)
	}
}
