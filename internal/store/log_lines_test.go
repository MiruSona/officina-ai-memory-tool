package store

import (
	"os"
	"testing"
	"time"
)

// A8 — gc 가 접은 줄을 모아 한 번에 쓰는 AppendLogLines 시험.

var a8Day = time.Date(2026, 10, 7, 9, 0, 0, 0, time.Local)

// readRaw 는 log.md 를 손대지 않고 그대로 읽는다. 없으면 빈 글이다.
func readRaw(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(LogPath(dir))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

// seedLog 는 log.md 를 주어진 글로 미리 채운다. 빈 글이면 파일을 안 만든다.
func seedLog(t *testing.T, dir, text string) {
	t.Helper()
	if text == "" {
		return
	}
	if err := os.WriteFile(LogPath(dir), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 빈 lines 면 파일을 만들지도, 덧붙이지도 않는다.
func TestAppendLogLinesEmptyWritesNothing(t *testing.T) {
	dir := t.TempDir()
	if err := AppendLogLines(dir, a8Day, LogFolded, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(LogPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("빈 lines 인데 log.md 가 생겼다 : %v", err)
	}
	seedLog(t, dir, "# 기록\n")
	if err := AppendLogLines(dir, a8Day, LogFolded, []string{}); err != nil {
		t.Fatal(err)
	}
	if got := readRaw(t, dir); got != "# 기록\n" {
		t.Fatalf("빈 lines 인데 글이 바뀌었다 : %q", got)
	}
}

// 한 번에 쓴 결과는 AppendLog 를 줄마다 부른 결과와 바이트까지 같다. 시작 글이
// 비었을 때 · 줄끝이 없을 때 · 날짜 절이 이미 있을 때를 다 본다.
func TestAppendLogLinesMatchesOneByOne(t *testing.T) {
	lines := []string{"가 (gc, warm 200일)", "나\r\n둘째\t칸", "  다  "}
	starts := []string{
		"",
		"# 기록",
		"# 기록\n\n## 2026-10-06\n- 들어옴 옛 줄\n",
		"# 기록\n\n## 2026-10-07\n- 들어옴 오늘 줄\n",
		bomPrefix + "# 기록\r\n\r\n## 2026-10-07\r\n- 들어옴 CRLF 줄\r\n",
	}
	for _, start := range starts {
		one, batch := t.TempDir(), t.TempDir()
		seedLog(t, one, start)
		seedLog(t, batch, start)
		for _, line := range lines {
			if err := AppendLog(one, a8Day, LogFolded, line); err != nil {
				t.Fatal(err)
			}
		}
		if err := AppendLogLines(batch, a8Day, LogFolded, lines); err != nil {
			t.Fatal(err)
		}
		if a, b := readRaw(t, one), readRaw(t, batch); a != b {
			t.Fatalf("시작 %q 에서 결과가 다르다\n줄마다 : %q\n한 번에 : %q", start, a, b)
		}
	}
}

// AppendLog 한 줄 결과는 고치기 전 판과 바이트까지 같다 (기대값은 옛 판 규칙
// 그대로 손으로 적었다). 줄바꿈·탭은 공백으로 눌리고 앞뒤 공백은 잘린다.
func TestAppendLogSingleLineBytes(t *testing.T) {
	cases := []struct{ start, want string }{
		{"", "## 2026-10-07\n- 접힘 가 나\n"},
		{"# 기록", "# 기록\n\n## 2026-10-07\n- 접힘 가 나\n"},
		{"# 기록\n", "# 기록\n\n## 2026-10-07\n- 접힘 가 나\n"},
		{"## 2026-10-07\n- 들어옴 x\n", "## 2026-10-07\n- 들어옴 x\n- 접힘 가 나\n"},
	}
	for _, c := range cases {
		dir := t.TempDir()
		seedLog(t, dir, c.start)
		if err := AppendLog(dir, a8Day, LogFolded, " 가\n나 "); err != nil {
			t.Fatal(err)
		}
		if got := readRaw(t, dir); got != c.want {
			t.Fatalf("시작 %q\n받음 : %q\n기대 : %q", c.start, got, c.want)
		}
	}
}

// 날짜 절이 이미 있으면 다시 붙이지 않는다.
func TestAppendLogLinesKeepsExistingHeading(t *testing.T) {
	dir := t.TempDir()
	seedLog(t, dir, "## 2026-10-07\n- 들어옴 x\n")
	if err := AppendLogLines(dir, a8Day, LogFolded, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	want := "## 2026-10-07\n- 들어옴 x\n- 접힘 a\n- 접힘 b\n"
	if got := readRaw(t, dir); got != want {
		t.Fatalf("받음 : %q\n기대 : %q", got, want)
	}
}
