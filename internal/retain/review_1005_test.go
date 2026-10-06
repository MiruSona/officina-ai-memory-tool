package retain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// 리뷰 2026-10-05 (자동 쌓기) 지적을 막는 시험이다.

// 가-2 : 여러 Stop 이 한 저장소에서 겹쳐도 세션 줄을 잃지 않는다.
func TestStopConcurrentKeepsEverySession(t *testing.T) {
	dir := t.TempDir()
	const workers, rounds = 8, 5
	// 200ms 훅 예산은 8명이 한 잠금에 줄 서면 기계가 바쁠 때 쉽게 넘는다 (실측 :
	// 부하 중 100판에 56판이 「너무 자주 못 잡음」, 세션 잃음은 0판). 여기서 보려는
	// 것은 잠금이 셈을 지키는가이지 예산이 아니라서 기다림만 늘린다. 짧게 포기하는
	// 것은 TestUpdateStateGivesUpQuickly 가 본다.
	saved := lockWait
	lockWait = 2 * time.Second
	defer func() { lockWait = saved }()
	now := time.Now()
	failed := 0
	lock := sync.Mutex{}
	group := sync.WaitGroup{}
	for worker := range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for round := range rounds {
				id := fmt.Sprintf("sess%04d-%04d-0000-0000", worker, round)
				record := filepath.Join(dir, id+".jsonl")
				writeTranscript(t, record, 1, 1)
				if Stop(dir, defaults(), StopInput{SessionID: id, TranscriptPath: record}, now).Why == WhyStateFail {
					lock.Lock()
					failed++
					lock.Unlock()
				}
			}
		}()
	}
	group.Wait()
	state := LoadState(dir)
	// 잠금을 못 잡은 판(lockWait 넘게 기다림)은 조용히 건너뛰니 그만큼은 빠져도 된다.
	if len(state.Sessions)+failed != workers*rounds {
		t.Fatalf("세션 줄을 잃었다 : 남은 %d + 건너뜀 %d != %d", len(state.Sessions), failed, workers*rounds)
	}
	if failed > workers*rounds/2 {
		t.Fatalf("잠금을 너무 자주 못 잡았다 : %d", failed)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(StatePath(dir)), "*.tmp"))
	if _, err := os.Stat(StatePath(dir) + ".lock"); err == nil || len(leftovers) > 0 {
		t.Fatalf("잠금·tmp 파일이 남았다 : %v", leftovers)
	}
}

// 가-2 : 깨진 상태 파일은 `.bad` 로 옮기고, 오래된 잠금은 넘겨 잡는다.
func TestUpdateStateQuarantinesAndStealsStaleLock(t *testing.T) {
	dir := t.TempDir()
	path := StatePath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	lock := path + ".lock"
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	err := UpdateState(dir, time.Now(), func(state *State) bool {
		state.Days["2026-10-05"] = 1
		return true
	})
	if err != nil {
		t.Fatalf("고아 잠금을 넘겨 잡아야 한다 : %v", err)
	}
	if data, err := os.ReadFile(path + ".bad"); err != nil || string(data) != "{broken" {
		t.Fatalf("깨진 파일을 .bad 로 옮겨야 한다 : %q %v", data, err)
	}
	if LoadState(dir).Days["2026-10-05"] != 1 {
		t.Fatal("새 상태를 써야 한다")
	}
}

// 가-2 : 살아 있는 잠금이면 짧게 기다리다 포기한다(훅 예산).
func TestUpdateStateGivesUpQuickly(t *testing.T) {
	dir := t.TempDir()
	path := StatePath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".lock", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err := UpdateState(dir, time.Now(), func(*State) bool { return true })
	if err != ErrStateBusy {
		t.Fatalf("잠금을 못 잡으면 ErrStateBusy : %v", err)
	}
	if spent := time.Since(started); spent > 400*time.Millisecond {
		t.Fatalf("너무 오래 기다렸다 : %v", spent)
	}
}

// 가-4 : 8자보다 짧은 앞머리는 아무 세션도 안 고른다.
func TestFindRejectsShortPrefix(t *testing.T) {
	state := State{Sessions: map[string]*Session{testSession: {Seen: time.Now().Format(time.RFC3339)}}}
	if id, _ := state.Find("a"); id != "" {
		t.Fatalf("1글자 앞머리로 세션을 골랐다 : %s", id)
	}
	if id, _ := state.Find("abcd1234"); id != testSession {
		t.Fatalf("8자 앞머리는 찾아야 한다 : %q", id)
	}
	if id, _ := state.Find(""); id != testSession {
		t.Fatalf("빈 값은 가장 최근 세션 : %q", id)
	}
}

// 가-5 : 최근 창 안의 세션 수.
func TestRecentSessions(t *testing.T) {
	now := time.Now()
	state := State{Sessions: map[string]*Session{
		"aaaaaaaa-1": {Seen: now.Add(-time.Minute).Format(time.RFC3339)},
		"bbbbbbbb-1": {Seen: now.Add(-10 * time.Minute).Format(time.RFC3339)},
		"cccccccc-1": {Seen: now.Add(-2 * time.Hour).Format(time.RFC3339)},
	}}
	if got := state.RecentSessions(now, 30*time.Minute); got != 2 {
		t.Fatalf("30분 안 세션은 2개 : %d", got)
	}
}

// 가-7 : 상한보다 큰 기록은 끝을 읽는다.
func TestReadWholeReadsTheEnd(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "big.jsonl")
	text := strings.Builder{}
	for at := range 200 {
		text.WriteString(transcriptLine(t, "user", fmt.Sprintf("앞쪽 채움 글 %03d 번째 줄", at)))
	}
	text.WriteString(transcriptLine(t, "user", "맨 끝의 근거 문장"))
	if err := os.WriteFile(record, []byte(text.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	saved := wholeRoom
	wholeRoom = 2000
	defer func() { wholeRoom = saved }()
	tally, err := ReadWhole(record)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tally.Talk.String(), "맨 끝의 근거 문장") {
		t.Fatal("끝 글이 빠졌다")
	}
	if strings.Contains(tally.Talk.String(), "000 번째") {
		t.Fatal("앞 글을 읽었다 — 끝을 읽어야 한다")
	}
}

// 가-8 : 낱말 사이 빗금은 경로가 아니다.
func TestFragmentsSkipWordSlashes(t *testing.T) {
	for _, text := range []string{"and/or", "R1/R2 규칙", "Stop/PreCompact 훅"} {
		for _, one := range Fragments(text) {
			if one.Kind == KindPath {
				t.Errorf("%q 에서 경로를 뽑았다 : %s", text, one.Text)
			}
		}
	}
	want := map[string]bool{"cmd/mem/main.go": true, "internal/retain": false, "a/b/c": true,
		"Docs/x.md:12": true, `C:\Tools\mem.exe`: true}
	for text, path := range want {
		got := false
		for _, one := range Fragments(text) {
			got = got || one.Kind == KindPath
		}
		if got != path {
			t.Errorf("%q 경로 판정 %v, 기대 %v", text, got, path)
		}
	}
}

// 가-10 : 경로로 부른 exe 도 세고, --check 는 안 센다.
func TestHasMemAdd(t *testing.T) {
	cases := map[string]bool{
		`mem add --type caution`:                        true,
		`.\AIMemoryTool\bin\mem.exe add --type caution`: true,
		`& "C:\Tools\mem.exe" add --type caution`:       true,
		`cd x; mem add --check --type caution`:          false,
		`mem add --check --type x; mem add --type x`:    true,
		`memo add x`:     false,
		`mem search add`: false,
		`mem add --type caution --summary "--checklist"`: true,
	}
	for command, want := range cases {
		if got := hasMemAdd(command); got != want {
			t.Errorf("%q : %v, 기대 %v", command, got, want)
		}
	}
}
