package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/retain"
)

// 코드리뷰 2026-10-05 지적을 고친 자리의 시험이다 (가 = 자동쌓기·LLM, 나 = 검색·모음).

// 가-1 — JSONL 줄의 origin·origin_session·basis_hash·rev·rule 은 비운다.
// 손 묶음이 자동 기억·카드인 척하면 auto undo·consolidate 가 그것을 건드린다.
func TestJSONLDropsMachineFields(t *testing.T) {
	memory := newRepo(t)
	line := jsonlLine(t, map[string]any{"type": "caution", "severity": "mid", "scope": "mem-search",
		"tags": []string{"index", "korean"}, "sources": []string{"file:internal/token/token.go"},
		"title":   "두 글자 검색어를 조심한다",
		"summary": "두 글자 한글 검색어는 trigram 에 안 잡혀 0건이 되니 색인 쪽을 먼저 본다",
		"body":    jsonlBody("첫째 줄이다."), "origin": "card", "origin_session": "c0ffee00",
		"basis_hash": "deadbeef", "rev": 3, "rule": "chain"})
	holdLock(t, memory)
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
	for _, key := range []string{"\"origin\"", "origin_session", "basis_hash", "\"rev\"", "\"rule\""} {
		if strings.Contains(string(data), key) {
			t.Fatalf("%s 가 큐에 실렸다 : %s", key, data)
		}
	}
}

// 가-4 — 세션 앞자리가 8자보다 짧으면 사용법 오류다 (여러 세션에 걸린다).
func TestShortSessionPrefixIsUsageError(t *testing.T) {
	autoRepo(t)
	short := append(addArgs("본문 한 줄"), "--origin", "stop", "--session", "c0ff", "--quotes", autoQuote)
	if out, code := captureBoth(t, func() int { return run(short) }); code != exitUsage {
		t.Fatalf("짧은 --session 으로 add 를 받았다 : %d %s", code, out)
	}
	for _, verb := range []string{"undo", "redo"} {
		argv := []string{"auto", verb, "--session", "c0ffee"}
		if out, code := captureBoth(t, func() int { return run(argv) }); code != exitUsage {
			t.Fatalf("짧은 --session 으로 auto %s 를 받았다 : %d %s", verb, code, out)
		}
	}
}

// 가-5 — --session 없이 --origin 을 주고 최근 30분 안에 세션이 둘 이상이면 거절한다.
func TestOriginWithoutSessionRefusesWhenAmbiguous(t *testing.T) {
	memory := autoRepo(t)
	state := retain.LoadState(memory)
	state.Sessions["0ddba11-2222-3333"] = &retain.Session{TranscriptPath: state.Sessions[autoSession].TranscriptPath,
		Seen: time.Now().Format(time.RFC3339)}
	if err := retain.SaveState(memory, state, time.Now()); err != nil {
		t.Fatal(err)
	}
	argv := append(addArgs("본문 한 줄"), "--origin", "stop", "--quotes", autoQuote)
	out, code := captureBoth(t, func() int { return run(argv) })
	if code != exitUsage || !strings.Contains(out, "--session") {
		t.Fatalf("세션이 둘인데 고르지 않고 받았다 : %d %s", code, out)
	}
}

// 가-3 — redo 의 --origin·--until 은 진짜로 거른다. 안 맞는 거름이면 아무것도 안 살린다.
func TestAutoRedoHonoursFilter(t *testing.T) {
	memory := autoRepo(t)
	out, code := capture(t, func() int { return run(autoArgs("본문 한 줄")) })
	if code != exitOK {
		t.Fatalf("자동 add 실패 : %s", out)
	}
	id := strings.TrimSpace(out)
	mustRun(t, "auto", "undo", "--origin", "stop", "--apply")
	mustRun(t, "auto", "redo", "--origin", "card", "--apply")
	if !strings.Contains(readMemoryFile(t, memory, id), "review: true") {
		t.Fatal("--origin card 인데 stop 기억을 살렸다")
	}
	mustRun(t, "auto", "redo", "--until", "2000-01-01", "--apply")
	if !strings.Contains(readMemoryFile(t, memory, id), "review: true") {
		t.Fatal("--until 이 기억 날짜보다 앞인데 살렸다")
	}
	mustRun(t, "auto", "redo", "--origin", "stop", "--apply")
	if strings.Contains(readMemoryFile(t, memory, id), "review: true") {
		t.Fatal("맞는 거름인데 안 살렸다")
	}
}

// 나-1 — consolidate --apply 는 손으로 쓴 모음 기억(origin 없음)을 안 건드린다.
// 나-6 — 카드 머리말에 무리 규칙(rule)이 적힌다.
func TestConsolidateLeavesHandObservation(t *testing.T) {
	memory, all := b1Repo(t)
	hand := basisMemory("20260901-0b500001", model.TypeObservation, "공유기 흐름을 손으로 모았다")
	hand.Sources = []string{model.SourceMem + all[0].ID, model.SourceMem + all[1].ID, model.SourceMem + all[2].ID}
	hand.Body = "1. 처음엔 L 이다 [mem:" + all[0].ID + "]\n2. 다음엔 M 이다 [mem:" + all[1].ID +
		"]\n3. 지금은 N 이다 [mem:" + all[2].ID + "]"
	path := putMemory(t, memory, hand)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mustRun(t, "index")
	mustRun(t, "consolidate", "--apply")
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("손 모음 기억을 고쳐 썼다 :\n%s", after)
	}
	found := mustRun(t, "search", "공유기", "--limit", "10")
	cards := 0
	for _, line := range strings.Split(found, "\n") {
		// 나-4 — basis_hash 없는 옛 손 모음은 근거가 살아 있으면 [낡음] 이 아니다.
		if strings.Contains(line, hand.ID) && strings.Contains(line, "[낡음]") {
			t.Fatalf("해시 없는 손 모음이 낡았다 : %s", line)
		}
		if strings.Contains(line, "| observation |") && !strings.Contains(line, hand.ID) {
			cards++
			id := strings.TrimSpace(strings.Split(line, "|")[2])
			if !strings.Contains(readMemoryFile(t, memory, id), "rule: chain") {
				t.Fatalf("카드 머리말에 rule 이 없다 : %s", id)
			}
		}
	}
	if cards != 1 {
		t.Fatalf("새 카드 한 장이 따로 써져야 한다 :\n%s", found)
	}
}

// 나-2 — 근거가 보류되면 그 카드는 기본 검색에서 빠지고 근거 줄에도 안 나온다.
func TestHeldBasisHidesCard(t *testing.T) {
	memory, all := b1Repo(t)
	mustRun(t, "consolidate", "--apply")
	held := *all[2]
	held.Review = true
	putMemory(t, memory, &held)
	mustRun(t, "index")
	found := mustRun(t, "search", "공유기", "--limit", "10")
	if strings.Contains(found, "observation") || strings.Contains(found, "↳ "+held.ID) {
		t.Fatalf("보류 근거를 단 카드나 그 근거 줄이 떴다 :\n%s", found)
	}
}

// 나-4 — 손 add 로 쓴 모음 기억도 basis_hash 를 받아 쓰자마자 낡지 않는다.
// 나-5 — 모음 기억을 근거로 삼은 모음 기억은 거절한다.
func TestAddObservationGetsHashAndRefusesObservationBasis(t *testing.T) {
	memory, all := b1Repo(t)
	argv := []string{"add", "--type", "observation", "--scope", "mem-search", "--tags", "index,search",
		"--author", "human:tester", "--sources", "mem:" + all[2].ID + ",mem:" + all[3].ID,
		"--title", "공유기와 회선을 같이 본다",
		"--summary", "공유기 모델 N 결정과 옛 회선 속도 사실을 한데 모아 둔 손 모음 기억이다",
		"--body", "1. 공유기는 N 이다 [mem:" + all[2].ID + "]\n2. 회선 속도는 옛 값이다 [mem:" + all[3].ID + "]"}
	out, code := captureBoth(t, func() int { return run(argv) })
	if code != exitOK {
		t.Fatalf("손 모음 add 가 거절됐다 : %d %s", code, out)
	}
	id := ""
	for _, word := range strings.Fields(out) {
		if model.IsID(word) {
			id = word
			break
		}
	}
	if id == "" {
		t.Fatalf("id 를 못 찾았다 : %s", out)
	}
	if !strings.Contains(readMemoryFile(t, memory, id), "basis_hash:") {
		t.Fatalf("손 모음에 basis_hash 가 없다 :\n%s", readMemoryFile(t, memory, id))
	}
	if found := mustRun(t, "search", "공유기", "--limit", "10"); strings.Contains(found, "[낡음]") {
		t.Fatalf("막 쓴 손 모음이 낡았다 :\n%s", found)
	}
	again := append([]string(nil), argv...)
	for at := range again {
		if again[at] == "--sources" {
			again[at+1] = "mem:" + id + ",mem:" + all[3].ID
		}
		if again[at] == "--body" {
			again[at+1] = "1. 모음을 다시 모았다 [mem:" + id + "]\n2. 회선 속도는 옛 값이다 [mem:" + all[3].ID + "]"
		}
	}
	out, code = captureBoth(t, func() int { return run(append(again, "--new")) })
	if code == exitOK || !strings.Contains(out, "모음 기억이다") {
		t.Fatalf("모음 기억을 근거로 삼은 모음을 받았다 : %d %s", code, out)
	}
}
