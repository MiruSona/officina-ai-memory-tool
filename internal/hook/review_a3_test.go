package hook

// 7단계 코드리뷰 A — 훅 입력의 cwd 가 비었을 때 (스트레스 시험 V14) 와
// 모르는 옵션 (보안연동 시험 L-5).

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// cwd 가 빈 훅 JSON 은 「어느 저장소인지 모른다」는 뜻이다. 지금 폴더로
// 떨어지면 남의 저장소를 열고 색인까지 고친다.
func TestEmptyCwdOpensNothing(t *testing.T) {
	dir := filled(t)
	// 지금 폴더를 그 저장소 안으로 옮겨 둔다 — 고치기 전에는 여기로 떨어졌다.
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(before) })
	t.Setenv("CLAUDE_PROJECT_DIR", "")

	out := bytes.Buffer{}
	if code := Run([]string{}, strings.NewReader(`{"hook_event_name":"SessionStart","source":"startup","cwd":""}`), &out); code != 0 {
		t.Fatalf("훅은 늘 0 이어야 한다 : %d", code)
	}
	if out.Len() != 0 {
		t.Fatalf("아무 저장소도 열면 안 되는데 출력이 있다 : %q", out.String())
	}

	// cwd 를 제대로 주면 평소대로 블록이 나온다 (막기만 하고 끝나면 안 된다).
	out.Reset()
	if code := Run([]string{}, strings.NewReader(startInput(dir)), &out); code != 0 {
		t.Fatalf("훅은 늘 0 이어야 한다 : %d", code)
	}
	if out.Len() == 0 {
		t.Fatal("cwd 를 줬는데도 빈 출력이다")
	}
}

// 훅 JSON 이 아예 안 들어온 자리(사람이 손으로 치는 것)는 예전처럼 지금
// 폴더를 본다. V14 고침이 그 길까지 막으면 안 된다.
func TestNoJSONStillUsesWorkingDir(t *testing.T) {
	dir := filled(t)
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(before) })
	t.Setenv("CLAUDE_PROJECT_DIR", "")

	out := bytes.Buffer{}
	if code := Run([]string{"session-start"}, strings.NewReader(""), &out); code != 0 {
		t.Fatalf("훅은 늘 0 이어야 한다 : %d", code)
	}
	if out.Len() == 0 {
		t.Fatal("입력 없는 훅이 지금 폴더 저장소를 못 열었다")
	}
}

// 모르는 옵션은 무시하되 stderr 로 알린다. stdout 은 그대로 훅 JSON 이다.
func TestUnknownOptionIsNotedNotSwallowed(t *testing.T) {
	dir := filled(t)
	notes := captureStderr(t)
	out := bytes.Buffer{}
	if code := Run([]string{"--include-held"}, strings.NewReader(startInput(dir)), &out); code != 0 {
		t.Fatalf("훅은 늘 0 이어야 한다 : %d", code)
	}
	if out.Len() == 0 {
		t.Fatal("모르는 옵션 때문에 블록이 사라졌다")
	}
	if !strings.Contains(notes(), "--include-held") {
		t.Fatal("모르는 옵션을 조용히 먹었다")
	}
}

// --repo 는 훅이 안 받는다. 값까지 삼켜야 그 값이 이벤트 이름으로 안 읽힌다.
func TestRepoOptionIsIgnoredWithItsValue(t *testing.T) {
	dir := filled(t)
	notes := captureStderr(t)
	out := bytes.Buffer{}
	if code := Run([]string{"--repo", `C:\nowhere`, "session-start"},
		strings.NewReader(startInput(dir)), &out); code != 0 {
		t.Fatalf("훅은 늘 0 이어야 한다 : %d (%s)", code, notes())
	}
	if out.Len() == 0 {
		t.Fatalf("--repo 값이 이벤트 이름으로 읽혔다 : %s", notes())
	}
	if !strings.Contains(notes(), "--repo") {
		t.Fatal("--repo 를 안 쓴다는 것을 안 알렸다")
	}
}

// captureStderr 는 stderr 를 파이프로 돌려 읽어 준다.
func captureStderr(t *testing.T) func() string {
	t.Helper()
	before := os.Stderr
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = write
	done := make(chan string, 1)
	go func() {
		buffer := bytes.Buffer{}
		buffer.ReadFrom(read)
		done <- buffer.String()
	}()
	text := ""
	got := false
	t.Cleanup(func() {
		if !got {
			os.Stderr = before
			write.Close()
			<-done
		}
	})
	return func() string {
		if got {
			return text
		}
		got = true
		os.Stderr = before
		write.Close()
		text = <-done
		return text
	}
}
