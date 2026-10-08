package main

// K — 바깥 LLM 판정. add --origin 의 R3 와 mem judge 를 가짜 서버로 본다.
// 진짜 서버에는 아무것도 안 보낸다.

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/retain"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// TestMain 은 이 기계의 진짜 ~/.aimemory/llm.toml 을 시험이 못 읽게 막는다 —
// 시험이 바깥 서버로 요청을 보내면 안 된다.
func TestMain(m *testing.M) {
	os.Setenv(config.LLMPathEnv, filepath.Join(os.TempDir(), "mem-test-no-llm.toml-없음"))
	os.Exit(m.Run())
}

// fakeLLM 은 letter 를 확률 0.9 로 고르는 가짜 서버다. status 가 0 이 아니면 그 코드로 답한다.
func fakeLLM(t *testing.T, letter string, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	hits := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		io.ReadAll(r.Body)
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		tops := []map[string]any{{"token": letter, "logprob": math.Log(0.9)}}
		for _, other := range []string{"A", "B", "C"} {
			if other != letter {
				tops = append(tops, map[string]any{"token": other, "logprob": math.Log(0.04)})
			}
		}
		data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
			"logprobs": map[string]any{"content": []any{map[string]any{"token": letter, "top_logprobs": tops}}}}}})
		w.Write(data)
	}))
	t.Cleanup(server.Close)
	return server, hits
}

// useLLM 은 시험 하나 동안 llm.toml 을 그 서버로 가리킨다.
func useLLM(t *testing.T, address string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "llm.toml")
	text := fmt.Sprintf("url = %q\njudge_profile = \"judge\"\ntimeout_ms = 1000\n", address)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.LLMPathEnv, path)
}

func judgeFiles(memory string) []os.DirEntry {
	files, _ := os.ReadDir(filepath.Join(store.LocalDir(memory), judgeDirName))
	return files
}

func TestAutoAddWithoutLLMSkipsR3(t *testing.T) {
	memory := autoRepo(t)
	out, code := captureBoth(t, func() int { return run(autoArgs("본문 한 줄")) })
	if code != exitOK || strings.Contains(out, "R3") {
		t.Fatalf("llm.toml 이 없으면 R3 없이 지금처럼 들어가야 한다 : %d %s", code, out)
	}
	if len(judgeFiles(memory)) != 0 {
		t.Fatal("판정 기록을 남겼다")
	}
}

func TestAutoAddR3Rejects(t *testing.T) {
	memory := autoRepo(t)
	server, hits := fakeLLM(t, "B", 0)
	useLLM(t, server.URL)
	out, code := capture(t, func() int { return run(autoArgs("본문 한 줄")) })
	if code != exitCheck || !strings.Contains(out, retain.RuleSupport) {
		t.Fatalf("「반대」면 R3 거절 · 종료 2 다 : %d %s", code, out)
	}
	if hits.Load() != 1 || len(judgeFiles(memory)) != 1 {
		t.Fatalf("한 번 묻고 기록을 하나 남겨야 한다 : hits=%d", hits.Load())
	}
	raw, _ := os.ReadFile(store.LogPath(memory))
	if !strings.Contains(string(raw), "자동 · stop · "+retain.RuleSupport) {
		t.Fatalf("log.md 에 R3 거절이 없다 :\n%s", raw)
	}
	// 같은 입력은 다시 안 묻는다.
	capture(t, func() int { return run(autoArgs("본문 한 줄")) })
	if hits.Load() != 1 {
		t.Fatalf("같은 입력을 다시 물었다 : %d", hits.Load())
	}
}

func TestAutoAddR3Supports(t *testing.T) {
	autoRepo(t)
	server, hits := fakeLLM(t, "A", 0)
	useLLM(t, server.URL)
	out, code := captureBoth(t, func() int { return run(autoArgs("본문 한 줄")) })
	if code != exitOK || hits.Load() != 1 {
		t.Fatalf("「지지」면 통과다 : %d hits=%d %s", code, hits.Load(), out)
	}
}

func TestAutoAddR3ServerDownStillPasses(t *testing.T) {
	autoRepo(t)
	server, _ := fakeLLM(t, "", http.StatusInternalServerError)
	useLLM(t, server.URL)
	out, code := captureBoth(t, func() int { return run(autoArgs("본문 한 줄")) })
	if code != exitOK || !strings.Contains(out, "R3") || !strings.Contains(out, "http-500") {
		t.Fatalf("서버가 안 되면 경고 한 줄로 건너뛰고 들어가야 한다 : %d %s", code, out)
	}
}

func TestAutoAddR3NotAskedWhenRulesReject(t *testing.T) {
	autoRepo(t)
	server, hits := fakeLLM(t, "A", 0)
	useLLM(t, server.URL)
	out, code := capture(t, func() int { return run(autoArgs("지연이 3170ms 로 늘었다")) })
	if code != exitCheck || hits.Load() != 0 {
		t.Fatalf("규칙 판에 걸리면 서버에 묻지 않는다 : %d hits=%d %s", code, hits.Load(), out)
	}
}

func TestJudgeCommand(t *testing.T) {
	memory := newRepo(t)
	out, code := capture(t, func() int {
		return run([]string{"judge", "support", "--evidence", "근거 문장", "--claim", "주장 문장"})
	})
	if code != exitOK || !strings.Contains(out, "LLM 주소 없음") {
		t.Fatalf("설정이 없으면 한 줄 · 종료 0 : %d %s", code, out)
	}
	server, hits := fakeLLM(t, "C", 0)
	useLLM(t, server.URL)
	out, code = capture(t, func() int {
		return run([]string{"judge", "support", "--evidence", "근거 문장", "--claim", "주장 문장", "--json"})
	})
	var verdict map[string]any
	if code != exitOK || json.Unmarshal([]byte(out), &verdict) != nil || verdict["letter"] != "C" {
		t.Fatalf("한 쌍 판정 : %d %s", code, out)
	}
	pairs := filepath.Join(t.TempDir(), "pairs.jsonl")
	os.WriteFile(pairs, []byte(`{"id":"p1","evidence":"근거 문장","claim":"주장 문장","want":"unrelated"}`+"\n"+
		`{"id":"p2","evidence":"다른 근거","claim":"다른 주장","want":"A"}`+"\n"), 0o644)
	out, code = captureBoth(t, func() int { return run([]string{"judge", "support", "--file", pairs}) })
	if code != exitOK || !strings.Contains(out, `"id":"p1"`) || !strings.Contains(out, "맞힘 1/2") {
		t.Fatalf("파일 판정 : %d %s", code, out)
	}
	if hits.Load() != 2 {
		t.Fatalf("p1 은 기록에서 읽어야 한다 : hits=%d", hits.Load())
	}
	if len(judgeFiles(memory)) != 2 {
		t.Fatal("판정 기록이 두 건이어야 한다")
	}
	out, code = capture(t, func() int { return run([]string{"judge", "config"}) })
	if code != exitOK || !strings.Contains(out, "judge") || !strings.Contains(out, "키 : 없음") {
		t.Fatalf("config : %d %s", code, out)
	}
	if _, code := capture(t, func() int { return run([]string{"judge", "support", "--evidence", "x"}) }); code != exitUsage {
		t.Fatal("--claim 없는 한 쌍을 받았다")
	}
	server.Close()
	_, code = capture(t, func() int {
		// 규칙 단에 안 걸리는 쌍이어야 서버까지 간다 (「새 근거」·「새 주장」은 R-무관에 걸린다).
		return run([]string{"judge", "support", "--evidence", "새 근거 문장", "--claim", "새 주장 문장"})
	})
	if code != exitCheck {
		t.Fatalf("서버가 안 닿으면 종료 2 : %d", code)
	}
}

// --stage — 고른 단만 돈다. rules 만이면 서버를 안 부르고, 결과 줄에 stage·reason 이 찍힌다.
func TestJudgeStageOption(t *testing.T) {
	newRepo(t)
	server, hits := fakeLLM(t, "A", 0)
	useLLM(t, server.URL)
	pairs := filepath.Join(t.TempDir(), "pairs.jsonl")
	os.WriteFile(pairs, []byte(
		`{"id":"n1","evidence":"새 기억을 넣을 때 중복 검사를 한다","claim":"새 기억을 넣을 때 중복 검사를 안 한다","want":"B"}`+"\n"+
			`{"id":"u1","evidence":"고양이는 햇볕 아래에서 낮잠을 즐긴다","claim":"서버 배포는 금요일 오후에 멈춘다","want":"C"}`+"\n"+
			`{"id":"s1","evidence":"색인은 SQLite FTS5 로 만든다","claim":"색인은 SQLite FTS5 로 만들고 점수 순으로 보여 준다","want":"A"}`+"\n"), 0o644)
	out, code := capture(t, func() int { return run([]string{"judge", "support", "--file", pairs, "--stage", "rules"}) })
	if code != exitOK || hits.Load() != 0 {
		t.Fatalf("--stage rules 는 서버를 안 부른다 : %d hits=%d %s", code, hits.Load(), out)
	}
	rows := map[string]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		row := map[string]any{}
		json.Unmarshal([]byte(line), &row)
		rows[row["id"].(string)] = row
	}
	if rows["n1"]["letter"] != "B" || rows["n1"]["stage"] != "rules" || rows["n1"]["reason"] != "neg:한다→안 한다" {
		t.Errorf("부정 쌍 : %v", rows["n1"])
	}
	if rows["u1"]["letter"] != "C" || !strings.HasPrefix(rows["u1"]["reason"].(string), "unrelated:") {
		t.Errorf("무관 쌍 : %v", rows["u1"])
	}
	if rows["s1"]["letter"] != "" || rows["s1"]["unsure"] != true || rows["s1"]["stage"] != "rules" {
		t.Errorf("안 걸린 쌍은 글자 빈 모른다 : %v", rows["s1"])
	}
	// 전체 사다리면 안 걸린 쌍만 서버에 묻는다.
	out, code = capture(t, func() int { return run([]string{"judge", "support", "--file", pairs}) })
	if code != exitOK || hits.Load() != 1 || !strings.Contains(out, `"stage":"semif"`) {
		t.Fatalf("사다리 전체 : %d hits=%d %s", code, hits.Load(), out)
	}
	for _, bad := range []string{"llm", "rules,,nli", "rules,laya", ""} {
		if _, code := capture(t, func() int { return run([]string{"judge", "support", "--file", pairs, "--stage", bad}) }); code != exitUsage {
			t.Errorf("--stage %q 를 받았다", bad)
		}
	}
}
