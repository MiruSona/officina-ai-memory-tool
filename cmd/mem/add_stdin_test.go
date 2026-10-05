package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// `--body` 도 `--stdin` 도 없이 부른 add 가 표준입력을 기다리며 멈추지 않는다
// (반복 06 피드백 4번). 에이전트는 입력을 못 넣어 시간 한도까지 묶인다.

// openStdin 은 표준입력을 안 닫힌 파이프로 갈아 끼우고 쓰는 쪽을 돌려준다.
func openStdin(t *testing.T) *os.File {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	before := os.Stdin
	os.Stdin = reader
	t.Cleanup(func() {
		os.Stdin = before
		writer.Close()
	})
	return writer
}

func shortStdinWait(t *testing.T) {
	t.Helper()
	before := stdinWait
	stdinWait = 50 * time.Millisecond
	t.Cleanup(func() { stdinWait = before })
}

func bodyFor(t *testing.T, argv ...string) (string, error) {
	t.Helper()
	parsed, err := parseOptions(argv, addBools, addValues)
	if err != nil {
		t.Fatal(err)
	}
	return bodyOf(parsed)
}

func TestBodyOfPipeWithData(t *testing.T) {
	shortStdinWait(t)
	stdinOf(t, "파이프로 넘긴 본문\n")
	body, err := bodyFor(t)
	if err != nil || body != "파이프로 넘긴 본문" {
		t.Fatalf("파이프 본문을 못 받았다 : %q · %v", body, err)
	}
}

func TestBodyOfOpenPipeTimesOut(t *testing.T) {
	shortStdinWait(t)
	openStdin(t)
	started := time.Now()
	_, err := bodyFor(t)
	if err == nil || !strings.Contains(err.Error(), "--stdin") {
		t.Fatalf("열린 빈 파이프인데 「--body 나 --stdin」 거절이 아니다 : %v", err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatalf("시한보다 훨씬 오래 기다렸다 : %v", time.Since(started))
	}
}

// .NET Process 의 StandardInput 은 BOM 만 먼저 보내고 안 닫는다. 그래도 시한에 걸린다.
func TestBodyOfOpenPipeWithOnlyBOMTimesOut(t *testing.T) {
	shortStdinWait(t)
	writer := openStdin(t)
	writer.WriteString(string(rune(0xFEFF)))
	started := time.Now()
	_, err := bodyFor(t)
	if err == nil || !strings.Contains(err.Error(), "--stdin") {
		t.Fatalf("BOM 만 온 열린 파이프인데 거절이 아니다 : %v", err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatalf("시한보다 훨씬 오래 기다렸다 : %v", time.Since(started))
	}
}

// 본문이 시한 안에 오기 시작하면 나머지는 시한을 넘겨도 끝까지 받는다.
func TestBodyOfSlowRestIsKept(t *testing.T) {
	shortStdinWait(t)
	writer := openStdin(t)
	writer.WriteString("앞부분")
	go func() {
		time.Sleep(200 * time.Millisecond)
		writer.WriteString(" 뒷부분\n")
		writer.Close()
	}()
	body, err := bodyFor(t)
	if err != nil || body != "앞부분 뒷부분" {
		t.Fatalf("늦게 온 뒷부분을 놓쳤다 : %q · %v", body, err)
	}
}

func TestBodyOfClosedEmptyPipeRefuses(t *testing.T) {
	// PowerShell 5.1 의 `$null |` 는 BOM 한 글자와 줄바꿈을 보낸다. 그것도 빈 본문이다.
	for _, text := range []string{"", string(rune(0xFEFF)) + "\r\n"} {
		shortStdinWait(t)
		stdinOf(t, text)
		if _, err := bodyFor(t); err == nil || !strings.Contains(err.Error(), "--stdin") {
			t.Fatalf("빈 파이프(%q)인데 거절이 아니다 : %v", text, err)
		}
	}
}

// `--stdin` 을 명시하면 시한을 넘겨 온 본문도 끝까지 기다려 받는다.
func TestBodyOfExplicitStdinWaits(t *testing.T) {
	shortStdinWait(t)
	writer := openStdin(t)
	go func() {
		time.Sleep(200 * time.Millisecond)
		writer.WriteString("늦게 온 본문")
		writer.Close()
	}()
	body, err := bodyFor(t, "--stdin")
	if err != nil || body != "늦게 온 본문" {
		t.Fatalf("--stdin 인데 끝까지 안 기다렸다 : %q · %v", body, err)
	}
}

// `--stdin` 길도 맨 앞 BOM 하나만 벗긴다 (코드리뷰 10-05 낮음 6). `mem set --stdin` 도
// 같은 readStdin 을 탄다. 본문 안쪽 글자는 그대로다.
func TestReadStdinDropsLeadingBOMOnly(t *testing.T) {
	bom := string(rune(0xFEFF))
	cases := map[string]string{
		bom + "본문\r\n":    "본문",
		"가" + bom + "나":   "가" + bom + "나",
		bom + bom + "두 개": bom + "두 개",
		"BOM 없는 본문\n":     "BOM 없는 본문",
	}
	for input, want := range cases {
		stdinOf(t, input)
		got, err := bodyFor(t, "--stdin")
		if err != nil || got != want {
			t.Fatalf("%q → %q 여야 하는데 %q · %v", input, want, got, err)
		}
	}
}

func TestBodyOfFlagSkipsStdin(t *testing.T) {
	shortStdinWait(t)
	openStdin(t)
	body, err := bodyFor(t, "--body", "옵션 본문")
	if err != nil || body != "옵션 본문" {
		t.Fatalf("--body 를 못 썼다 : %q · %v", body, err)
	}
}
