package retain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

const testSession = "abcd1234-0000-1111-2222-333344445555"

// transcriptLine 은 Claude Code 대화 기록 한 줄을 흉내 낸다.
func transcriptLine(t *testing.T, kind string, content any) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"type": kind,
		"message": map[string]any{"role": kind, "content": content}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw) + "\n"
}

func toolUse(name string, input map[string]any) map[string]any {
	return map[string]any{"type": "tool_use", "name": name, "input": input}
}

// writeTranscript 는 사용자 턴 turns 번, 고침 edits 번이 든 기록을 쓴다.
func writeTranscript(t *testing.T, path string, turns, edits int, extra ...string) {
	t.Helper()
	text := strings.Builder{}
	for at := 0; at < turns; at++ {
		text.WriteString(transcriptLine(t, "user", "사용자가 다음 일을 시킨다 번호 "+string(rune('a'+at))))
	}
	for at := 0; at < edits; at++ {
		text.WriteString(transcriptLine(t, "assistant", []any{
			toolUse("Edit", map[string]any{"file_path": "cmd/mem/main.go", "old_string": "a", "new_string": "b"})}))
	}
	for _, line := range extra {
		text.WriteString(line)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(text.String()); err != nil {
		t.Fatal(err)
	}
}

func defaults() config.RetainConfig { return config.Default("").Retain }

func TestStopNudgesOverThreshold(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "s.jsonl")
	writeTranscript(t, record, 1, 2)
	now := time.Now()
	input := StopInput{SessionID: testSession, TranscriptPath: record}
	first := Stop(dir, defaults(), input, now)
	if first.Nudge || first.Why != WhyBelow {
		t.Fatalf("문턱 밑인데 알렸다 : %+v", first)
	}
	writeTranscript(t, record, 0, 3)
	second := Stop(dir, defaults(), input, now)
	if !second.Nudge || second.Short != "abcd1234" {
		t.Fatalf("고침 5번이면 알려야 한다 : %+v", second)
	}
	third := Stop(dir, defaults(), input, now)
	if third.Nudge || third.Edits != 0 {
		t.Fatalf("알린 뒤에는 셈을 비우고 다시 쌓여야 알린다 : %+v", third)
	}
}

func TestStopSilentCases(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "s.jsonl")
	writeTranscript(t, record, 10, 10)
	now := time.Now()
	off := defaults()
	off.Nudge = false
	cases := []struct {
		name     string
		settings config.RetainConfig
		input    StopInput
		why      string
	}{
		{"active", defaults(), StopInput{SessionID: testSession, TranscriptPath: record, Active: true}, WhyActive},
		{"off", off, StopInput{SessionID: testSession, TranscriptPath: record}, WhyOff},
		{"bad id", defaults(), StopInput{SessionID: "../x", TranscriptPath: record}, WhyBadInput},
		{"not jsonl", defaults(), StopInput{SessionID: testSession, TranscriptPath: filepath.Join(dir, "a.txt")}, WhyNoRecord},
		{"relative", defaults(), StopInput{SessionID: testSession, TranscriptPath: "s.jsonl"}, WhyNoRecord},
	}
	for _, one := range cases {
		got := Stop(dir, one.settings, one.input, now)
		if got.Nudge || got.Why != one.why {
			t.Errorf("%s : %+v", one.name, got)
		}
	}
	t.Setenv(ChildEnv, "1")
	if got := Stop(dir, defaults(), StopInput{SessionID: testSession, TranscriptPath: record}, now); got.Why != WhyChild {
		t.Errorf("자식 세션은 침묵이다 : %+v", got)
	}
}

func TestStopMaxAndMemAdd(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "s.jsonl")
	input := StopInput{SessionID: testSession, TranscriptPath: record}
	now := time.Now()
	settings := defaults()
	settings.NudgeMax = 1
	writeTranscript(t, record, 0, 6)
	if got := Stop(dir, settings, input, now); !got.Nudge {
		t.Fatalf("첫 알림이 안 나갔다 : %+v", got)
	}
	writeTranscript(t, record, 0, 6)
	if got := Stop(dir, settings, input, now); got.Nudge || got.Why != WhyMaxed {
		t.Fatalf("nudge_max 를 넘었다 : %+v", got)
	}
	other := filepath.Join(dir, "o.jsonl")
	added := transcriptLine(t, "assistant", []any{toolUse("Bash", map[string]any{"command": "mem add --type howto"})})
	writeTranscript(t, other, 0, 6, added)
	got := Stop(dir, defaults(), StopInput{SessionID: "ffff0000-1", TranscriptPath: other}, now)
	if got.Nudge || got.Why != WhyAdded || got.MemAdds != 1 {
		t.Fatalf("이미 mem add 를 쳤으면 침묵이다 : %+v", got)
	}
}

func TestMarkWritesQueue(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if err := Mark(dir, testSession, "C:/x.jsonl", ReasonCompact, now); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(QueueDir(dir), "abcd1234.json"))
	if err != nil {
		t.Fatal(err)
	}
	entry := QueueEntry{}
	if err := json.Unmarshal(data, &entry); err != nil || entry.Reason != ReasonCompact {
		t.Fatalf("큐 표가 틀렸다 : %s", data)
	}
	if Mark(dir, "../../evil", "", ReasonEnd, now) == nil {
		t.Fatal("못 쓰는 세션 id 로 파일을 만들었다")
	}
}

func TestTranscriptSkipsReminders(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "s.jsonl")
	writeTranscript(t, record, 0, 0,
		transcriptLine(t, "user", "<system-reminder>숨은 글 ZZZ</system-reminder>진짜 사용자 말이다"),
		transcriptLine(t, "user", []any{map[string]any{"type": "tool_result", "content": "도구 결과 777"}}))
	tally, err := ReadWhole(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tally.Talk.String(), "ZZZ") {
		t.Fatal("system-reminder 덩이가 대화 글에 남았다")
	}
	if strings.Contains(tally.Talk.String(), "777") || !strings.Contains(tally.All.String(), "777") {
		t.Fatal("도구 결과는 R2 쪽(All)에만 있어야 한다")
	}
	if tally.Turns != 1 {
		t.Fatalf("도구 결과만 있는 user 줄은 턴이 아니다 : %d", tally.Turns)
	}
}

func TestReadTailKeepsPartialLine(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "s.jsonl")
	writeTranscript(t, record, 1, 0)
	file, _ := os.OpenFile(record, os.O_APPEND|os.O_WRONLY, 0o644)
	file.WriteString(`{"type":"user","message":{"content":"쓰는 중`)
	file.Close()
	tally, offset, err := ReadTail(record, 0)
	if err != nil || tally.Turns != 1 {
		t.Fatalf("완성된 줄만 세야 한다 : %+v %v", tally, err)
	}
	info, _ := os.Stat(record)
	if offset >= info.Size() {
		t.Fatal("쓰는 중인 줄까지 오프셋을 넘겼다")
	}
}

func TestFragments(t *testing.T) {
	got := Fragments("2026-10-05 에 cmd/mem/main.go:12 를 고쳐 p95 가 180ms 로 줄었다. 20261005-34ee8ac3 참고 https://a.b/c 1개")
	kinds := map[string]string{}
	for _, one := range got {
		kinds[one.Text] = one.Kind
	}
	want := map[string]string{"2026-10-05": KindDate, "cmd/mem/main.go:12": KindPath, "95": KindNumber,
		"180": KindNumber, "20261005-34ee8ac3": KindID, "https://a.b/c": KindURL}
	for text, kind := range want {
		if kinds[text] != kind {
			t.Errorf("%s 를 %s 로 뽑아야 한다 : %v", text, kind, kinds)
		}
	}
	if _, found := kinds["1"]; found {
		t.Error("한 자리 수는 안 뽑는다")
	}
	if _, found := kinds["10"]; found {
		t.Error("날짜 안 숫자를 따로 뽑았다")
	}
}

// gateFixture 는 관문 시험용 저장소 뿌리·기록·후보다.
type gateFixture struct {
	root    string
	context Context
	memory  *model.Memory
}

func newGateFixture(t *testing.T) gateFixture {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "cmd", "mem"), 0o755)
	os.WriteFile(filepath.Join(root, "cmd", "mem", "main.go"), []byte("package main"), 0o644)
	record := filepath.Join(root, "s.jsonl")
	writeTranscript(t, record, 0, 0,
		transcriptLine(t, "user", "훅 지연이 길어서 상태 파일을 오프셋으로 읽게 바꾸자. p95 가 180ms 였다."),
		transcriptLine(t, "assistant", []any{toolUse("Bash", map[string]any{"command": "go test ./... # 42 passed"})}))
	tally, err := ReadWhole(record)
	if err != nil {
		t.Fatal(err)
	}
	memory := &model.Memory{Type: model.TypeHowto, Title: "훅 오프셋 읽기", Scope: "aimemorytool",
		Summary: "Stop 훅은 대화 기록을 오프셋 뒤만 읽는다. p95 180ms 를 줄이려고 바꿨다",
		Body:    "cmd/mem/main.go 에서 고쳤다.\n시험 42 통과.", Sources: []string{"file:cmd/mem/main.go"}}
	return gateFixture{root: root, memory: memory, context: Context{Settings: defaults(), Root: root,
		Record: tally, Scopes: []string{"aimemorytool"}}}
}

func rulesOf(result Result) string {
	names := []string{}
	for _, one := range result.Rejects {
		names = append(names, one.Rule)
	}
	return strings.Join(names, " ")
}

func TestGatePassesGrounded(t *testing.T) {
	fixture := newGateFixture(t)
	candidate := Candidate{Memory: fixture.memory, Origin: OriginStop,
		Quotes: []string{"훅 지연이 길어서 상태 파일을 오프셋으로 읽게 바꾸자"}}
	if result := Check(candidate, fixture.context); result.Rejected() {
		t.Fatalf("근거가 다 있는데 거절했다 : %s", rulesOf(result))
	}
	retained := candidate
	retained.Origin = "retain:qwen3"
	if result := Check(retained, fixture.context); result.Rejected() {
		t.Fatalf("(나) 길도 근거가 있으면 통과다 : %s", rulesOf(result))
	}
}

func TestGateRejectsEachRule(t *testing.T) {
	cases := []struct {
		name string
		rule string
		edit func(*Candidate, *Context)
	}{
		{"R1 대화에 없는 근거", RuleQuote, func(c *Candidate, _ *Context) {
			c.Quotes = []string{"이런 말은 대화에 한 번도 나온 적이 없다"}
		}},
		{"R1 짧은 근거", RuleQuote, func(c *Candidate, _ *Context) { c.Quotes = []string{"오프셋"} }},
		{"R1 근거 넷", RuleQuote, func(c *Candidate, _ *Context) { c.Quotes = []string{"a", "b", "c", "d"} }},
		{"R1 retain 근거 없음", RuleQuote, func(c *Candidate, _ *Context) { c.Origin, c.Quotes = "retain:m", nil }},
		{"R1 retain 기록 없음", RuleQuote, func(c *Candidate, x *Context) { c.Origin, x.Record = "retain:m", nil }},
		{"R2 지어낸 수", RuleFragment, func(c *Candidate, _ *Context) { c.Memory.Body += "\n지연이 3170ms 였다." }},
		{"R2 없는 경로", RuleFragment, func(c *Candidate, _ *Context) { c.Memory.Body += "\ninternal/ghost/none.go 도 고쳤다." }},
		{"R4 닮은 기억", RuleNoMerge, func(c *Candidate, _ *Context) { c.GateRules = []string{"duplicate-soft"} }},
		{"R5 덮기", RuleNoBy, func(c *Candidate, _ *Context) { c.Supersedes = "20260101-aaaaaaaa" }},
		{"R6 세션 상한", RuleCap, func(_ *Candidate, x *Context) { x.SessionAdds = 5 }},
		{"R6 하루 상한", RuleCap, func(_ *Candidate, x *Context) { x.DayAdds = 20 }},
		{"R7 retain decision", RuleType, func(c *Candidate, _ *Context) { c.Origin, c.Memory.Type = "retain:m", model.TypeDecision }},
		{"R8 근거 없음", RuleSources, func(c *Candidate, _ *Context) { c.Memory.Sources = nil }},
		{"R8 죽은 파일", RuleSources, func(c *Candidate, _ *Context) { c.Memory.Sources = []string{"file:nope.go"} }},
		{"R8 뿌리 밖", RuleSources, func(c *Candidate, _ *Context) { c.Memory.Sources = []string{"file:../outside.go"} }},
		{"R8 retain 모르는 scope", RuleSources, func(c *Candidate, _ *Context) { c.Origin, c.Memory.Scope = "retain:m", "nowhere" }},
	}
	for _, one := range cases {
		fixture := newGateFixture(t)
		candidate := Candidate{Memory: fixture.memory, Origin: OriginStop,
			Quotes: []string{"훅 지연이 길어서 상태 파일을 오프셋으로 읽게 바꾸자"}}
		context := fixture.context
		one.edit(&candidate, &context)
		result := Check(candidate, context)
		if !strings.Contains(rulesOf(result), one.rule) {
			t.Errorf("%s : %s 에 걸려야 하는데 %q", one.name, one.rule, rulesOf(result))
		}
	}
}

func TestGateWithoutRecordForStop(t *testing.T) {
	fixture := newGateFixture(t)
	context := fixture.context
	context.Record = nil
	candidate := Candidate{Memory: fixture.memory, Origin: OriginStop}
	result := Check(candidate, context)
	if result.Rejected() {
		t.Fatalf("(가)는 기록이 없어도 저장소 대조로 지난다 : %s", rulesOf(result))
	}
	if len(result.Warnings) < 2 {
		t.Fatalf("기록 없음 · 못 본 수를 알려야 한다 : %v", result.Warnings)
	}
}

// fakeJudge 는 R3 자리를 시험한다. A1 은 늘 nil 이지만 갈래는 살아 있어야 한다.
type fakeJudge struct{ answer, ok bool }

func (f fakeJudge) Supports(string, string) (bool, bool) { return f.answer, f.ok }

func TestGateJudgeSlot(t *testing.T) {
	fixture := newGateFixture(t)
	candidate := Candidate{Memory: fixture.memory, Origin: OriginStop,
		Quotes: []string{"훅 지연이 길어서 상태 파일을 오프셋으로 읽게 바꾸자"}}
	context := fixture.context
	context.Judge = fakeJudge{answer: false, ok: true}
	if !strings.Contains(rulesOf(Check(candidate, context)), RuleSupport) {
		t.Fatal("판정자가 아니라고 하면 R3 거절이다")
	}
	context.Judge = fakeJudge{ok: false}
	if result := Check(candidate, context); result.Rejected() || len(result.Warnings) == 0 {
		t.Fatalf("판정을 못 받으면 건너뛰고 알린다 : %+v", result)
	}
}

// countingJudge 는 몇 번 불렸는지 세고, 못 받은 까닭을 말해 주는 판정자다 (llm.Judge 꼴).
type countingJudge struct {
	calls   *int
	ok      bool
	problem string
}

func (c countingJudge) Supports(string, string) (bool, bool) { *c.calls++; return true, c.ok }
func (c countingJudge) Problem() string                      { return c.problem }

// K — 규칙 판에 이미 걸렸으면 R3 는 바깥 서버에 묻지 않는다. 못 받으면 까닭을 경고에 넣는다.
func TestGateJudgeSkippedWhenRejectedAndSaysWhy(t *testing.T) {
	fixture := newGateFixture(t)
	calls := 0
	candidate := Candidate{Memory: fixture.memory, Origin: OriginStop, Supersedes: "20260101-aaaaaaaa",
		Quotes: []string{"훅 지연이 길어서 상태 파일을 오프셋으로 읽게 바꾸자"}}
	context := fixture.context
	context.Judge = countingJudge{calls: &calls, ok: true}
	if result := Check(candidate, context); !result.Rejected() || calls != 0 {
		t.Fatalf("R5 에 걸렸는데 판정자를 불렀다 : calls=%d", calls)
	}
	candidate.Supersedes = ""
	context.Judge = countingJudge{calls: &calls, ok: false, problem: "timeout"}
	result := Check(candidate, context)
	if result.Rejected() || calls != 1 || !strings.Contains(strings.Join(result.Warnings, " "), "timeout") {
		t.Fatalf("못 받으면 까닭을 알리고 건너뛴다 : %+v calls=%d", result, calls)
	}
}
