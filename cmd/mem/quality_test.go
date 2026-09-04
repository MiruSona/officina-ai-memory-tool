package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// statusLines 는 status 가 반드시 내는 줄 머리다 (설계 9-5).
var statusLines = []string{"저장소 :", "기억   :", "색인   :", "설치   :", "설정   :", "사용   :"}

// TestStatusShowsEveryLine 은 한 화면에 여섯 줄이 다 나오는지 본다.
func TestStatusShowsEveryLine(t *testing.T) {
	newRepo(t)
	if _, code := capture(t, func() int { return run(addArgs("본문 한 줄")) }); code != 0 {
		t.Fatalf("add 가 실패했다 : %d", code)
	}
	if _, code := capture(t, func() int { return run([]string{"index", "--quiet"}) }); code != 0 {
		t.Fatalf("index 가 실패했다 : %d", code)
	}
	out, code := capture(t, func() int { return run([]string{"status"}) })
	for _, head := range statusLines {
		if !strings.Contains(out, head) {
			t.Fatalf("「%s」 줄이 없다 :\n%s", head, out)
		}
	}
	if !strings.Contains(out, "기억   : 1건") {
		t.Fatalf("건수가 안 맞는다 :\n%s", out)
	}
	// 이 시험 저장소에는 훅도 PATH 도 없으니 색인 건강은 나쁘다고 나와야 한다.
	if code != exitCheck {
		t.Fatalf("훅·PATH 가 없는데 종료 코드가 %d 다", code)
	}
}

// TestStatusJSON 은 --json 이 한 줄 JSON 인지 본다.
func TestStatusJSON(t *testing.T) {
	newRepo(t)
	capture(t, func() int { return run([]string{"index", "--quiet"}) })
	out, _ := capture(t, func() int { return run([]string{"status", "--json"}) })
	if strings.Count(strings.TrimSpace(out), "\n") != 0 {
		t.Fatalf("한 줄이 아니다 :\n%s", out)
	}
	data := statusData{}
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		t.Fatalf("JSON 이 아니다 : %v\n%s", err, out)
	}
	if !data.HasIndex {
		t.Fatal("색인을 만들었는데 has_index 가 거짓이다")
	}
}

// TestStatusCountsUnindexed 는 머리말이 깨진 파일을 사람에게 보여주는지 본다
// (설계 9-5 · 조사E #13).
func TestStatusCountsUnindexed(t *testing.T) {
	memory := newRepo(t)
	capture(t, func() int { return run(addArgs("본문 한 줄")) })
	capture(t, func() int { return run([]string{"index", "--quiet"}) })
	broken := storeFile(memory, "20260822-deadbeef")
	if err := os.MkdirAll(filepath.Dir(broken), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken, []byte("---\nid: 깨짐\n본문만 있다\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := capture(t, func() int { return run([]string{"status"}) })
	if !strings.Contains(out, "색인 안 된 파일 1건") {
		t.Fatalf("색인 안 된 파일 줄이 없다 :\n%s", out)
	}
	if code != exitCheck {
		t.Fatalf("색인 안 된 파일이 있는데 종료 코드가 %d 다", code)
	}
}

// TestLintCommandExit 는 오류가 있으면 2, 없으면 0 인지 본다 (설계 8-1).
func TestLintCommandExit(t *testing.T) {
	memory := newRepo(t)
	capture(t, func() int { return run(addArgs("본문 한 줄")) })
	capture(t, func() int { return run([]string{"index", "--quiet"}) })
	out, code := capture(t, func() int { return run([]string{"lint"}) })
	if code != exitOK {
		t.Fatalf("깨끗한 저장소인데 종료 코드가 %d 다 :\n%s", code, out)
	}
	broken := storeFile(memory, "20260822-deadbeef")
	if err := os.MkdirAll(filepath.Dir(broken), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken, []byte("---\nid: 깨짐\n본문만 있다\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code = capture(t, func() int { return run([]string{"lint"}) })
	if code != exitCheck {
		t.Fatalf("오류가 있는데 종료 코드가 %d 다 :\n%s", code, out)
	}
	if !strings.Contains(out, "front-matter") {
		t.Fatalf("규칙 이름이 표에 없다 :\n%s", out)
	}
}

// TestGCCommandAlwaysZero 는 gc 가 늘 0 으로 끝나는지 본다 (설계 8-1).
func TestGCCommandAlwaysZero(t *testing.T) {
	newRepo(t)
	capture(t, func() int { return run([]string{"index", "--quiet"}) })
	out, code := capture(t, func() int { return run([]string{"gc", "--dry-run"}) })
	if code != exitOK {
		t.Fatalf("gc 종료 코드가 %d 다 :\n%s", code, out)
	}
	if !strings.Contains(out, "정리할 기억이 없다") {
		t.Fatalf("빈 저장소에서 할 말이 다르다 :\n%s", out)
	}
}
