package main

// mem retain 의 갈래 길 — 재시도 · 서버 안 닿음 · 판정 끊김 · dry-run · --session · prune ·
// 자식 add 의 판정 사다리(rules,nli) · 큐 대체 길 · auto report 의 A2 처리 셈.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/retain"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

func TestRetainRetriesBrokenJSONOnce(t *testing.T) {
	memory := retainRepo(t)
	generate, hits := fakeGenerate(t, "이건 JSON 이 아니다", "```json\n{\"candidates\":[],\"nothing\":true}\n```")
	useRetainLLM(t, generate.URL, fakeNLI(t, 0.97, 0).URL)
	out, code := captureBoth(t, func() int { return run([]string{"retain"}) })
	entry := queueEntryOf(t, memory)
	if code != exitOK || hits.Load() != 2 || entry.Result != retain.ResultNothing {
		t.Fatalf("깨진 JSON 은 한 번 다시 묻고 울타리를 벗겨 읽는다 : %d hits=%d %+v\n%s", code, hits.Load(), entry, out)
	}
	lines := retainLogLines(t, memory)
	if len(lines) != 1 || lines[0].ParseFail != 0 || len(lines[0].GenMS) != 2 {
		t.Fatalf("재시도 판의 retain-log 가 다르다 : %+v", lines)
	}
}

func TestRetainCountsParseFailAfterRetry(t *testing.T) {
	memory := retainRepo(t)
	generate, hits := fakeGenerate(t, "깨짐", "또 깨짐")
	useRetainLLM(t, generate.URL, fakeNLI(t, 0.97, 0).URL)
	captureBoth(t, func() int { return run([]string{"retain"}) })
	lines := retainLogLines(t, memory)
	if hits.Load() != 2 || len(lines) != 1 || lines[0].ParseFail != 1 {
		t.Fatalf("두 번 깨지면 parse_fail 하나다 : hits=%d %+v", hits.Load(), lines)
	}
	if entry := queueEntryOf(t, memory); entry.Result != retain.ResultNothing {
		t.Fatalf("후보가 없으면 nothing 이다 : %+v", entry)
	}
}

func TestRetainServerDownLeavesQueue(t *testing.T) {
	memory := retainRepo(t)
	generate, hits := fakeGenerate(t, goodCandidates())
	down := fakeNLI(t, 0.97, 0)
	down.Close()
	useRetainLLM(t, generate.URL, down.URL)
	out, code := captureBoth(t, func() int { return run([]string{"retain"}) })
	if code != exitOK || hits.Load() != 0 || !strings.Contains(out, "큐 그대로") {
		t.Fatalf("서버가 안 닿으면 한 줄 + 종료 0 이다 : %d hits=%d %s", code, hits.Load(), out)
	}
	if entry := queueEntryOf(t, memory); entry.DoneAt != "" {
		t.Fatalf("큐를 건드렸다 : %+v", entry)
	}
}

func TestRetainWithoutGenerateURLSkips(t *testing.T) {
	retainRepo(t)
	useRetainLLM(t, "", fakeNLI(t, 0.97, 0).URL)
	out, code := captureBoth(t, func() int { return run([]string{"retain"}) })
	if code != exitOK || !strings.Contains(out, "생성 주소 없음") {
		t.Fatalf("생성 주소가 없으면 건너뛴다 : %d %s", code, out)
	}
}

func TestRetainStopsWhenJudgeDrops(t *testing.T) {
	memory := retainRepo(t)
	generate, _ := fakeGenerate(t, goodCandidates())
	useRetainLLM(t, generate.URL, fakeNLI(t, 0.97, 500).URL)
	out, code := captureBoth(t, func() int { return run([]string{"retain"}) })
	if entry := queueEntryOf(t, memory); code != exitOK || entry.DoneAt != "" {
		t.Fatalf("판정이 끊기면 done_at 없이 둔다 : %d %+v\n%s", code, entry, out)
	}
	lines := retainLogLines(t, memory)
	if len(lines) != 1 || lines[0].Error == "" {
		t.Fatalf("멈춘 까닭이 retain-log 에 없다 : %+v", lines)
	}
}

func TestRetainDryRunSavesNothing(t *testing.T) {
	memory := retainRepo(t)
	generate, _ := fakeGenerate(t, goodCandidates())
	useRetainLLM(t, generate.URL, fakeNLI(t, 0.97, 0).URL)
	out, code := captureBoth(t, func() int { return run([]string{"retain", "--dry-run"}) })
	if entry := queueEntryOf(t, memory); code != exitOK || entry.DoneAt != "" {
		t.Fatalf("dry-run 은 큐를 표시하지 않는다 : %d %+v\n%s", code, entry, out)
	}
	if files, _ := filepath.Glob(filepath.Join(memory, "store", "*", "*", "*.md")); len(files) != 0 {
		t.Fatalf("dry-run 인데 저장했다 : %v", files)
	}
	lines := retainLogLines(t, memory)
	if len(lines) != 1 || !lines[0].Dry || len(lines[0].Items) != 1 || lines[0].Items[0].Verdict != retainVerdictPass {
		t.Fatalf("dry-run 줄에 후보 판정이 없다 : %+v", lines)
	}
}

func TestRetainSessionOptionRedoesDone(t *testing.T) {
	memory := retainRepo(t)
	generate, hits := fakeGenerate(t, `{"candidates":[],"nothing":true}`)
	useRetainLLM(t, generate.URL, fakeNLI(t, 0.97, 0).URL)
	captureBoth(t, func() int { return run([]string{"retain"}) })
	captureBoth(t, func() int { return run([]string{"retain"}) })
	if hits.Load() != 1 {
		t.Fatalf("done_at 이 있는 항목을 다시 골랐다 : %d", hits.Load())
	}
	captureBoth(t, func() int { return run([]string{"retain", "--session", retainShort}) })
	if hits.Load() != 2 || queueEntryOf(t, memory).Result != retain.ResultNothing {
		t.Fatalf("--session 은 done_at 이어도 돈다 : %d", hits.Load())
	}
}

func TestRetainPrunePreviewThenApply(t *testing.T) {
	memory := retainRepo(t)
	gone := filepath.Join(retain.QueueDir(memory), "deadbeef.json")
	writeJSONFile(t, retain.QueueDir(memory), "deadbeef.json", retain.QueueEntry{SessionID: "deadbeef-0000",
		TranscriptPath: filepath.Join(memory, "없는.jsonl"), At: time.Now().Format(time.RFC3339)})
	out, code := captureBoth(t, func() int { return run([]string{"retain", "--prune"}) })
	if _, err := os.Stat(gone); code != exitOK || err != nil || !strings.Contains(out, "deadbeef") {
		t.Fatalf("미리보기는 보여만 준다 : %d %v %s", code, err, out)
	}
	out, code = captureBoth(t, func() int { return run([]string{"retain", "--prune", "--apply"}) })
	if _, err := os.Stat(gone); code != exitOK || err == nil {
		t.Fatalf("--apply 가 안 지웠다 : %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(retain.QueueDir(memory), retainShort+".json")); err != nil {
		t.Fatal("살아 있는 항목까지 지웠다")
	}
	raw, _ := os.ReadFile(store.LogPath(memory))
	if !strings.Contains(string(raw), "큐 정리 · 1건") {
		t.Fatalf("log.md 에 큐 정리 줄이 없다 :\n%s", raw)
	}
	if _, code := captureBoth(t, func() int { return run([]string{"retain", "--apply"}) }); code != exitUsage {
		t.Fatal("--apply 는 --prune 과만 쓴다")
	}
}

// retain:* 자식 add 는 NLI 판정이 있어야 한다 — 생성 주소만 있고 nli_url 이 없으면 물을 단이 없어 거절이다.
func TestAutoAddRetainWithoutNLIRejects(t *testing.T) {
	autoRepo(t)
	server, hits := fakeLLM(t, "A", 0)
	useRetainLLM(t, server.URL, "")
	args := append(addArgs("본문 한 줄"), "--origin", "retain:strata", "--session", autoSession[:8], "--quotes", autoQuote)
	out, code := capture(t, func() int { return run(args) })
	if code != exitCheck || hits.Load() != 0 || !strings.Contains(out, retain.SupportSkipped) {
		t.Fatalf("retain 은 NLI 가 없으면 skipped 로 거절한다 : %d hits=%d %s", code, hits.Load(), out)
	}
}

// 세션 상태에 없는 세션도 큐 항목의 transcript_path 로 기록을 찾아 R1 을 지난다.
func TestAutoAddRetainFindsQueueTranscript(t *testing.T) {
	retainRepo(t)
	useRetainLLM(t, "", fakeNLI(t, 0.97, 0).URL)
	args := []string{"add", "--type", "caution", "--severity", "mid", "--scope", "aimemorytool",
		"--tags", "search,korean", "--sources", "note:transcript " + retainShort + "#1", "--author", "mem-retain/1",
		"--summary", "두 글자 한글 검색어는 trigram 색인에서 0건이 된다", "--title", "한글 두 글자 검색 주의",
		"--body", "두 글자 한글 검색어는 0건이 된다.\n검색 결과가 빈다.\n짧은 검색어는 따로 다룬다.",
		"--origin", "retain:strata", "--session", retainShort, "--quotes", retainQuote}
	out, code := captureBoth(t, func() int { return run(args) })
	if code != exitOK {
		t.Fatalf("큐 대체 길로 기록을 찾아야 한다 : %d %s", code, out)
	}
}

func TestAutoReportCountsRetainDone(t *testing.T) {
	memory := newRepo(t)
	at := time.Now().Format(time.RFC3339)
	queue := retain.QueueDir(memory)
	writeJSONFile(t, queue, "a.json", retain.QueueEntry{Reason: "end", At: at})
	writeJSONFile(t, queue, "b.json", retain.QueueEntry{Reason: "end", At: at, DoneAt: at, Result: retain.ResultNothing})
	queued, missed, retained, _ := reportMissed(memory, autoFilter{Since: "2000-01-01", Until: "2999-12-31"})
	if queued != 2 || missed != 1 || retained != 1 {
		t.Fatalf("done_at 항목은 놓침이 아니라 A2 처리로 센다 : %d %d %d", queued, missed, retained)
	}
}

// dry-run 후보 줄에 최종 scope 가, 추정으로 바꿨으면 생성의 원래 scope 도 실린다.
func TestRetainLogCarriesScopeAndGuess(t *testing.T) {
	memory := retainRepo(t)
	entry := queueEntryOf(t, memory)
	entry.Cwd = filepath.Join(filepath.Dir(memory), "AIMemoryTool")
	writeJSONFile(t, retain.QueueDir(memory), retainShort+".json", entry)
	guessed := strings.Replace(goodCandidates(), `"scope":"aimemorytool"`, `"scope":"nowhere"`, 1)
	generate, _ := fakeGenerate(t, guessed)
	useRetainLLM(t, generate.URL, fakeNLI(t, 0.97, 0).URL)
	captureBoth(t, func() int { return run([]string{"retain", "--dry-run"}) })
	lines := retainLogLines(t, memory)
	if len(lines) != 1 || len(lines[0].Items) != 1 {
		t.Fatalf("후보 줄이 하나여야 한다 : %+v", lines)
	}
	item := lines[0].Items[0]
	if item.Scope != "aimemorytool" || item.ScopeFrom == nil || *item.ScopeFrom != "nowhere" {
		t.Fatalf("최종 scope 와 원래 scope 가 다 실려야 한다 : %+v", item)
	}
}

// 방금 바뀐 기록의 세션은 건너뛰고, --json 요약에 그 수를 싣는다.
func TestRetainJSONCountsSkippedLive(t *testing.T) {
	memory := retainRepo(t)
	now := time.Now()
	os.Chtimes(queueEntryOf(t, memory).TranscriptPath, now, now)
	generate, hits := fakeGenerate(t, goodCandidates())
	useRetainLLM(t, generate.URL, fakeNLI(t, 0.97, 0).URL)
	out, code := captureBoth(t, func() int { return run([]string{"retain", "--json"}) })
	if code != exitOK || hits.Load() != 0 || !strings.Contains(out, `"skipped_live":1`) {
		t.Fatalf("살아 있는 세션은 건너뛰고 skipped_live 로 센다 : %d hits=%d\n%s", code, hits.Load(), out)
	}
	out, _ = captureBoth(t, func() int { return run([]string{"retain"}) })
	if !strings.Contains(out, "살아 있어 건너뜀 1") {
		t.Fatalf("사람용 끝줄에도 건너뛴 수가 있어야 한다 :\n%s", out)
	}
}

// guessScope 는 A1 훅과 같은 hook.ScopeFor 를 쓰고, 이름이 정확히 같은(정규화 뒤) vocab scope 만 준다.
func TestRetainGuessScopeExactOnly(t *testing.T) {
	memory := newRepo(t)
	repository, err := config.Open(memory)
	if err != nil {
		t.Fatal(err)
	}
	repository.Config.Scope = map[string]string{"ToolHome": "aimemorytool"}
	worker := &retainWorker{repository: repository, vocab: vocabOf(repository)}
	cases := map[string]string{
		"D:/x/AIMemoryTool":         "aimemorytool", // 폴더 이름 소문자
		"D:/x/ToolHome":             "aimemorytool", // mem.toml [scope] 표가 이긴다
		"D:/x/AIMemoryToolTest":     "",             // 부분 문자열은 안 맞춘다
		"D:/x/officina-judge-model": "",
		"":                          "",
	}
	for dir, want := range cases {
		if got := worker.guessScope(dir); got != want {
			t.Fatalf("%q → %q (기대 %q)", dir, got, want)
		}
	}
	if got := worker.guessScope("D:/x/Nowhere", "D:/x/AIMemoryTool"); got != "aimemorytool" {
		t.Fatalf("앞이 안 맞으면 다음 폴더를 본다 : %q", got)
	}
}
