package main

// mem retain 본체 (A2 retain 설계 1·2·7절). 가짜 생성 서버 · 가짜 NLI · 실제 자식 `mem add`
// (이 시험 바이너리를 TestMain 이 명령으로 돌린다) 로 끝까지 돈다.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/retain"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

const retainSessionID = "abcd1234-0000-1111-2222-333344445555"
const retainShort = "abcd1234"
const retainQuote = "두 글자 한글 검색어에 trigram 이 조용히 0건을 돌려준다"

// retainRepo 는 대화 기록과 큐 항목 하나를 갖춘 저장소다. 세션 상태 파일에는 안 적는다 —
// 자식 관문이 큐의 transcript_path 로 기록을 찾는 대체 길을 같이 잰다.
func retainRepo(t *testing.T) string {
	t.Helper()
	memory := newRepo(t)
	record := filepath.Join(filepath.Dir(memory), "session.jsonl")
	user, _ := json.Marshal(map[string]any{"type": "user",
		"message": map[string]any{"role": "user", "content": retainQuote + " — 고쳐 달라."}})
	reply, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant",
		"content": []any{map[string]any{"type": "text", "text": "알겠다. 색인 쪽을 본다."}}}})
	os.WriteFile(record, []byte(string(user)+"\n"+string(reply)+"\n"), 0o644)
	writeJSONFile(t, retain.QueueDir(memory), retainShort+".json", retain.QueueEntry{SessionID: retainSessionID,
		TranscriptPath: record, Reason: retain.ReasonEnd, At: time.Now().Add(-time.Hour).Format(time.RFC3339)})
	return memory
}

// goodCandidates 는 관문을 다 지날 후보 하나짜리 생성 답이다.
func goodCandidates() string {
	one := map[string]any{"type": "caution", "title": "한글 두 글자 검색 주의",
		"summary": "두 글자 한글 검색어는 trigram 색인에서 0건이 된다",
		"body":    "두 글자 한글 검색어는 trigram 색인에서 0건이 된다.\n증상은 검색 결과가 비는 것이다.\n짧은 검색어는 따로 다뤄야 한다.",
		"tags":    []string{"search", "korean"}, "scope": "aimemorytool", "quotes": []string{retainQuote}}
	data, _ := json.Marshal(map[string]any{"candidates": []any{one}, "nothing": false})
	return string(data)
}

// fakeGenerate 는 답을 차례로 내는 가짜 Strata 다. 답이 모자라면 마지막 답을 되풀이한다.
func fakeGenerate(t *testing.T, answers ...string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	hits := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"data":[]}`)
			return
		}
		at := int(hits.Add(1)) - 1
		if at >= len(answers) {
			at = len(answers) - 1
		}
		data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": answers[at]}}},
			"usage": map[string]any{"prompt_tokens": 120, "completion_tokens": 30}})
		w.Write(data)
	}))
	t.Cleanup(server.Close)
	return server, hits
}

// fakeNLI 는 /health 에 답하고 /judge 에 지지 a · 반대 1-a 로 답한다. status 가 0 이 아니면 /judge 만 그 코드다.
func fakeNLI(t *testing.T, support float64, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		if r.URL.Path == "/health" {
			io.WriteString(w, `{"ok":true}`)
			return
		}
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		fmt.Fprintf(w, `{"a":%.3f,"b":%.3f,"c":0,"model":"fake0001","ms":1.0,"label_set":"nli3"}`, support, 1-support)
	}))
	t.Cleanup(server.Close)
	return server
}

// useRetainLLM 은 시험 하나 동안 llm.toml 을 두 가짜 서버로 가리킨다. 빈 주소는 안 적는다.
func useRetainLLM(t *testing.T, generate, nli string) {
	t.Helper()
	text := ""
	if generate != "" {
		text += fmt.Sprintf("generate_url = %q\n", generate)
	}
	if nli != "" {
		text += fmt.Sprintf("nli_url = %q\n", nli)
	}
	path := filepath.Join(t.TempDir(), "llm.toml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEM_LLM_CONFIG", path)
}

func queueEntryOf(t *testing.T, memory string) retain.QueueEntry {
	t.Helper()
	entry := retain.QueueEntry{}
	data, err := os.ReadFile(filepath.Join(retain.QueueDir(memory), retainShort+".json"))
	if err != nil || json.Unmarshal(data, &entry) != nil {
		t.Fatalf("큐 항목을 못 읽었다 : %v", err)
	}
	return entry
}

// retainLogLines 는 오늘 retain-log 의 줄들이다.
func retainLogLines(t *testing.T, memory string) []retainLogLine {
	t.Helper()
	path := filepath.Join(store.LocalDir(memory), retainLogDirName, time.Now().Format("2006-01-02")+".jsonl")
	raw, _ := os.ReadFile(path)
	lines := []retainLogLine{}
	for _, text := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		one := retainLogLine{}
		if text != "" && json.Unmarshal([]byte(text), &one) == nil {
			lines = append(lines, one)
		}
	}
	return lines
}

func TestRetainSavesSupportedCandidate(t *testing.T) {
	memory := retainRepo(t)
	generate, _ := fakeGenerate(t, goodCandidates())
	useRetainLLM(t, generate.URL, fakeNLI(t, 0.97, 0).URL)
	out, code := captureBoth(t, func() int { return run([]string{"retain"}) })
	if code != exitOK {
		t.Fatalf("retain 이 실패했다 : %d %s", code, out)
	}
	entry := queueEntryOf(t, memory)
	if entry.DoneAt == "" || entry.Result != retain.ResultSaved || len(entry.Saved) != 1 {
		t.Fatalf("큐에 저장 표시가 없다 : %+v\n%s", entry, out)
	}
	raw := readMemoryFile(t, memory, entry.Saved[0])
	for _, want := range []string{"origin: retain:strata", "origin_session: " + retainShort,
		"note:transcript " + retainShort + "#1", "author: mem-retain/1"} {
		if !strings.Contains(raw, want) {
			t.Errorf("기억 파일에 %q 가 없다 :\n%s", want, raw)
		}
	}
	lines := retainLogLines(t, memory)
	if len(lines) != 1 || lines[0].Saved != 1 || lines[0].Candidates != 1 || lines[0].Dry || len(lines[0].PromptTokens) != 1 {
		t.Fatalf("retain-log 줄이 다르다 : %+v", lines)
	}
}

func TestRetainRejectsContradictedCandidate(t *testing.T) {
	memory := retainRepo(t)
	generate, _ := fakeGenerate(t, goodCandidates())
	useRetainLLM(t, generate.URL, fakeNLI(t, 0.02, 0).URL)
	out, code := captureBoth(t, func() int { return run([]string{"retain"}) })
	entry := queueEntryOf(t, memory)
	if code != exitOK || entry.Result != retain.ResultRejected || len(entry.Saved) != 0 {
		t.Fatalf("지지 못 받은 후보는 거절이다 : %d %+v\n%s", code, entry, out)
	}
	lines := retainLogLines(t, memory)
	if len(lines) != 1 || lines[0].Rejected[retain.RuleSupport] != 1 {
		t.Fatalf("R3 거절이 retain-log 에 안 셌다 : %+v", lines)
	}
}
