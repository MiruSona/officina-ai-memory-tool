package model

import (
	"strings"
	"testing"
	"time"
)

// v0.2 규격 칸 하나하나를 본다 (설계 2-2 · 규칙표 F02·F06·F14~F19).

// good 은 v0.2 규격을 다 지키는 기억이다. 시험마다 여기서 한 칸씩 무너뜨린다.
func good() Memory {
	return Memory{
		ID: "20260823-3f9a2c1b", Type: TypeDecision,
		Title:   "훅 주입 상한을 바이트로 잰다",
		Summary: strings.Repeat("가", 40),
		Tags:    []string{"hook", "security"},
		Scope:   "aimemorytool", Date: "2026-08-23",
		Author:  "claude-code/opus-5",
		Sources: []string{"url:https://code.claude.com/docs/en/hooks"},
		Spec:    SpecV2,
		Body:    "본문",
	}
}

func problemText(problems []error) string {
	parts := make([]string, 0, len(problems))
	for _, one := range problems {
		parts = append(parts, one.Error())
	}
	return strings.Join(parts, " | ")
}

func TestV2GoodMemoryPasses(t *testing.T) {
	memory := good()
	if problems := Validate(&memory); len(problems) > 0 {
		t.Fatalf("규격을 다 지킨 기억이 걸렸다 : %s", problemText(problems))
	}
}

// 필수 칸 여덟 (F02). 하나씩 비우고 다 걸리는지 본다.
func TestV2RequiredFields(t *testing.T) {
	cases := map[string]func(*Memory){
		"id":      func(m *Memory) { m.ID = "" },
		"type":    func(m *Memory) { m.Type = "" },
		"title":   func(m *Memory) { m.Title = "" },
		"summary": func(m *Memory) { m.Summary = "" },
		"tags":    func(m *Memory) { m.Tags = nil },
		"scope":   func(m *Memory) { m.Scope = "" },
		"date":    func(m *Memory) { m.Date = "" },
		"author":  func(m *Memory) { m.Author = "" },
	}
	for field, breakIt := range cases {
		memory := good()
		breakIt(&memory)
		problems := Validate(&memory)
		if !strings.Contains(problemText(problems), "`"+field+"`") {
			t.Fatalf("%s 를 비웠는데 안 걸렸다 : %s", field, problemText(problems))
		}
	}
}

// F19 — author 세 꼴.
func TestAuthorShape(t *testing.T) {
	okay := []string{"human:mirusona", "claude-code/opus-5", "hook:session-start",
		"import:aimemorytool", "gemini/2.5-pro"}
	for _, value := range okay {
		if !IsAuthor(value) {
			t.Fatalf("맞는 author 가 걸렸다 : %s", value)
		}
	}
	bad := []string{"ai", "user", "hook", "human:", "human:mi ru", "claude-code", "/1.0", "hook:세션"}
	for _, value := range bad {
		if IsAuthor(value) {
			t.Fatalf("틀린 author 가 통과했다 : %s", value)
		}
	}
}

// F16 — decision·issue·caution 은 근거가 있어야 한다.
func TestSourcesRequired(t *testing.T) {
	for _, kind := range SourcesRequiredTypes {
		memory := good()
		memory.Type = kind
		memory.Severity = SeverityMid
		memory.Sources = nil
		if !strings.Contains(problemText(Validate(&memory)), "sources") {
			t.Fatalf("%s 인데 근거 없이 통과했다", kind)
		}
	}
	memory := good()
	memory.Type = TypeHistory
	memory.Sources = nil
	if problems := Validate(&memory); len(problems) > 0 {
		t.Fatalf("history 는 근거가 없어도 된다 : %s", problemText(problems))
	}
}

// F17 — 근거 접두 다섯.
func TestSourceShape(t *testing.T) {
	okay := []string{"file:Docs/설계.md", "file:Docs/설계.md#L61-L82",
		"commit:42c18dd", "commit:" + strings.Repeat("a", 40),
		"url:https://example.com/x", "mem:20260823-3f9a2c1b", "note:옛 설계 2절"}
	for _, value := range okay {
		if !IsSource(value) {
			t.Fatalf("맞는 근거가 걸렸다 : %s", value)
		}
	}
	bad := []string{"Docs/설계.md", "file:", "commit:zzz", "url:example.com",
		"mem:없는것", "note:", "git:abc"}
	for _, value := range bad {
		if IsSource(value) {
			t.Fatalf("틀린 근거가 통과했다 : %s", value)
		}
	}
}

func TestSourcesNoteOnly(t *testing.T) {
	if !SourcesNoteOnly([]string{"note:가", "note:나"}) {
		t.Fatal("note 뿐인데 못 알아봤다")
	}
	if SourcesNoteOnly([]string{"note:가", "file:x"}) {
		t.Fatal("file 이 있는데 note 뿐이라고 했다")
	}
	if SourcesNoteOnly(nil) {
		t.Fatal("근거가 없는 것은 note 뿐인 것과 다르다")
	}
}

// F06 — 제목 길이와 요약 베끼기.
func TestTitleShape(t *testing.T) {
	short := good()
	short.Title = "짧다"
	if !strings.Contains(problemText(Validate(&short)), "title") {
		t.Fatal("6자 미만 제목이 통과했다")
	}
	long := good()
	long.Title = strings.Repeat("가", 41)
	if !strings.Contains(problemText(Validate(&long)), "title") {
		t.Fatal("40자 넘는 제목이 통과했다")
	}
	echo := good()
	echo.Summary = strings.Repeat("나", 40)
	echo.Title = strings.Repeat("나", 25)
	if len(Validate(&echo)) == 0 {
		t.Fatal("요약 앞머리를 베낀 제목이 통과했다")
	}
}

// F08 — 태그 하한 2 · 상한 5. 옛 규격은 하한이 1이다.
func TestTagCount(t *testing.T) {
	one := good()
	one.Tags = []string{"hook"}
	if len(Validate(&one)) == 0 {
		t.Fatal("태그 1개가 v0.2 에서 통과했다")
	}
	six := good()
	six.Tags = []string{"a", "b", "c", "d", "e", "f"}
	if len(Validate(&six)) == 0 {
		t.Fatal("태그 6개가 통과했다")
	}
	legacy := one
	legacy.Spec = SpecV1
	legacy.LegacySource = LegacySourceAI
	if problems := ValidateAs(&legacy, SpecV1); len(problems) > 0 {
		t.Fatalf("옛 규격은 태그 1개가 통과해야 한다 : %s", problemText(problems))
	}
}

// F14·F15 — todo_status 는 todo 에만, severity 는 issue·caution 에만.
func TestStatusAndSeverityBelongToOneType(t *testing.T) {
	stray := good()
	stray.TodoStatus = StatusOpen
	if !strings.Contains(problemText(Validate(&stray)), "todo_status") {
		t.Fatal("decision 에 붙은 todo_status 가 통과했다")
	}
	straySeverity := good()
	straySeverity.Severity = SeverityHigh
	if !strings.Contains(problemText(Validate(&straySeverity)), "severity") {
		t.Fatal("decision 에 붙은 severity 가 통과했다")
	}
	todo := good()
	todo.Type = TypeTodo
	todo.Sources = nil
	todo.TodoStatus = "열림"
	if len(Validate(&todo)) == 0 {
		t.Fatal("todo_status 값이 목록 밖인데 통과했다")
	}
}

// superseded_by 와 invalid_at 은 한 짝이다.
func TestSupersedePair(t *testing.T) {
	half := good()
	half.SupersededBy = "20260823-aaaaaaaa"
	if len(Validate(&half)) == 0 {
		t.Fatal("invalid_at 없이 superseded_by 만 있는데 통과했다")
	}
	whole := half
	whole.InvalidAt = "2026-09-01"
	if problems := Validate(&whole); len(problems) > 0 {
		t.Fatalf("둘 다 있으면 통과해야 한다 : %s", problemText(problems))
	}
}

func TestStaleAfterShape(t *testing.T) {
	memory := good()
	memory.StaleAfter = "2027-02-30"
	if len(Validate(&memory)) == 0 {
		t.Fatal("달력에 없는 stale_after 가 통과했다")
	}
	memory.StaleAfter = "2027-02-23"
	if problems := Validate(&memory); len(problems) > 0 {
		t.Fatalf("맞는 stale_after 가 걸렸다 : %s", problemText(problems))
	}
}

func TestFutureDate(t *testing.T) {
	now := time.Date(2026, 8, 23, 0, 0, 0, 0, time.Local)
	if !IsFutureDate("2026-08-24", now) {
		t.Fatal("내일을 미래로 안 봤다")
	}
	if IsFutureDate("2026-08-23", now) {
		t.Fatal("오늘을 미래로 봤다")
	}
}

// 옛 규격 호환 — `source`·`status` 로 적힌 파일을 읽고, 다시 쓸 때도 옛 이름을 지킨다.
func TestLegacyFrontMatterRoundTrip(t *testing.T) {
	raw := []byte(`---
id: 20260822-3f9a2c1b
type: todo
date: 2026-08-22
summary: ` + strings.Repeat("가", 40) + `
tags: [mem]
source: ai
scope: mem
status: open
---

본문
`)
	memory, err := Parse(raw)
	if err != nil {
		t.Fatalf("옛 규격 파일을 못 읽었다 : %v", err)
	}
	if !memory.IsLegacy() || memory.Spec != SpecV1 {
		t.Fatalf("옛 규격이라고 표시하지 않았다 : spec=%d", memory.Spec)
	}
	if memory.Author != "claude-code/unknown" {
		t.Fatalf("source: ai 를 author 로 못 옮겼다 : %q", memory.Author)
	}
	if memory.TodoStatus != StatusOpen || memory.LegacyStatus != StatusOpen {
		t.Fatalf("status 를 todo_status 로 못 옮겼다 : %q", memory.TodoStatus)
	}
	if problems := Validate(memory); len(problems) > 0 {
		t.Fatalf("옛 규격 파일이 옛 자로도 걸렸다 : %s", problemText(problems))
	}
	// v0.2 자로 재면 걸려야 한다 — migrate 가 할 일이 남았다는 뜻이다.
	if len(ValidateAs(memory, SpecV2)) == 0 {
		t.Fatal("옛 규격 파일이 v0.2 자를 그냥 통과했다")
	}
	again := string(Encode(memory))
	if !strings.Contains(again, "source: ai") || strings.Contains(again, "author:") {
		t.Fatalf("옛 파일을 다시 쓰면서 규격을 조용히 옮겼다 :\n%s", again)
	}
	if !strings.Contains(again, "status: open") || strings.Contains(again, "todo_status:") {
		t.Fatalf("옛 status 칸이 안 지켜졌다 :\n%s", again)
	}
}

func TestLegacyAuthorMap(t *testing.T) {
	cases := map[string]string{
		LegacySourceAI:     "claude-code/unknown",
		LegacySourceUser:   "human:unknown",
		LegacySourceHook:   "hook:session-start",
		LegacySourceImport: "import:unknown",
		"":                 "",
	}
	for source, want := range cases {
		if got := LegacyAuthor(source); got != want {
			t.Fatalf("%q → %q 여야 하는데 %q 였다", source, want, got)
		}
		if want != "" && !IsAuthor(want) {
			t.Fatalf("옮긴 author 가 규격을 안 맞는다 : %s", want)
		}
	}
}

// 새 규격 파일은 새 칸으로 읽고 쓴다.
func TestV2FrontMatterRoundTrip(t *testing.T) {
	memory := good()
	memory.Sources = []string{"file:Docs/설계.md#L61-L82", "url:https://example.com/x"}
	memory.StaleAfter = "2027-02-23"
	memory.Migrated = true
	text := Encode(&memory)
	back, err := Parse(text)
	if err != nil {
		t.Fatalf("다시 못 읽었다 : %v", err)
	}
	if back.Spec != SpecV2 || back.Author != memory.Author {
		t.Fatalf("author 가 왕복에서 깨졌다 : spec=%d author=%q", back.Spec, back.Author)
	}
	if len(back.Sources) != 2 || back.Sources[0] != memory.Sources[0] {
		t.Fatalf("sources 가 왕복에서 깨졌다 : %v", back.Sources)
	}
	if back.StaleAfter != memory.StaleAfter || !back.Migrated {
		t.Fatalf("stale_after·migrated 가 왕복에서 깨졌다 : %+v", back)
	}
}
