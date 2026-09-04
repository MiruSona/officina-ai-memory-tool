package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "memory", name))
	if err != nil {
		t.Fatalf("testdata read failed: %v", err)
	}
	return data
}

func TestParseTodo(t *testing.T) {
	memory, err := Parse(readTestdata(t, "20260822-3f9a2c1b.md"))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if memory.ID != "20260822-3f9a2c1b" || memory.Type != TypeTodo || memory.TodoStatus != StatusOpen {
		t.Fatalf("unexpected front matter: %+v", memory)
	}
	if len(memory.Tags) != 2 || memory.Tags[0] != "mem" {
		t.Fatalf("unexpected tags: %v", memory.Tags)
	}
	if strings.Contains(memory.Body, "---") || memory.Body == "" {
		t.Fatalf("unexpected body: %q", memory.Body)
	}
	if problems := Validate(memory); len(problems) != 0 {
		t.Fatalf("valid file reported problems: %v", problems)
	}
}

func TestRoundTrip(t *testing.T) {
	for _, name := range []string{"20260822-3f9a2c1b.md", "20260822-9e02d7f1.md"} {
		first, err := Parse(readTestdata(t, name))
		if err != nil {
			t.Fatalf("%s parse failed: %v", name, err)
		}
		encoded := Encode(first)
		second, err := Parse(encoded)
		if err != nil {
			t.Fatalf("%s reparse failed: %v", name, err)
		}
		if string(Encode(second)) != string(encoded) {
			t.Fatalf("%s round trip changed the file", name)
		}
		if second.Body != first.Body || second.Summary != first.Summary {
			t.Fatalf("%s round trip lost content", name)
		}
	}
}

func TestEncodeKeyOrderAndBytes(t *testing.T) {
	memory := Memory{
		ID: "20260822-3f9a2c1b", Type: TypeTodo, Date: "2026-08-22",
		Summary: strings.Repeat("가", 40), Tags: []string{"mem", "gc"},
		Author: "human:mirusona", Scope: "mem", TodoStatus: StatusOpen, Pinned: true,
		Title: "gc 임계값을 올린다", Spec: SpecV2, Body: "본문",
	}
	text := string(Encode(&memory))
	if strings.Contains(text, "\r") || strings.HasPrefix(text, "\ufeff") {
		t.Fatal("encoder must write LF and no BOM")
	}
	want := []string{"id:", "type:", "title:", "summary:", "tags:", "scope:", "date:", "author:", "pinned:", "todo_status:"}
	position := -1
	for _, key := range want {
		found := strings.Index(text, key)
		if found <= position {
			t.Fatalf("key %s is out of order in:\n%s", key, text)
		}
		position = found
	}
	if !strings.Contains(text, "tags: [mem, gc]") {
		t.Fatalf("tags should stay on one line:\n%s", text)
	}
}

func TestValidateProblems(t *testing.T) {
	cases := map[string]Memory{
		"scope missing":   {ID: "20260822-3f9a2c1b", Type: TypeHistory, Date: "2026-08-22", Summary: strings.Repeat("가", 40), Tags: []string{"mem"}, LegacySource: LegacySourceAI},
		"scope shape":     {ID: "20260822-3f9a2c1b", Type: TypeHistory, Date: "2026-08-22", Summary: strings.Repeat("가", 40), Tags: []string{"mem"}, LegacySource: LegacySourceAI, Scope: "Mem Search"},
		"summary short":   {ID: "20260822-3f9a2c1b", Type: TypeHistory, Date: "2026-08-22", Summary: "짧다", Tags: []string{"mem"}, LegacySource: LegacySourceAI, Scope: "mem"},
		"summary long":    {ID: "20260822-3f9a2c1b", Type: TypeHistory, Date: "2026-08-22", Summary: strings.Repeat("가", 121), Tags: []string{"mem"}, LegacySource: LegacySourceAI, Scope: "mem"},
		"tags too many":   {ID: "20260822-3f9a2c1b", Type: TypeHistory, Date: "2026-08-22", Summary: strings.Repeat("가", 40), Tags: []string{"a", "b", "c", "d", "e", "f"}, LegacySource: LegacySourceAI, Scope: "mem"},
		"tag shape":       {ID: "20260822-3f9a2c1b", Type: TypeHistory, Date: "2026-08-22", Summary: strings.Repeat("가", 40), Tags: []string{"한글"}, LegacySource: LegacySourceAI, Scope: "mem"},
		"todo status":     {ID: "20260822-3f9a2c1b", Type: TypeTodo, Date: "2026-08-22", Summary: strings.Repeat("가", 40), Tags: []string{"mem"}, LegacySource: LegacySourceAI, Scope: "mem"},
		"issue severity":  {ID: "20260822-3f9a2c1b", Type: TypeIssue, Date: "2026-08-22", Summary: strings.Repeat("가", 40), Tags: []string{"mem"}, LegacySource: LegacySourceAI, Scope: "mem"},
		"bad type":        {ID: "20260822-3f9a2c1b", Type: "note", Date: "2026-08-22", Summary: strings.Repeat("가", 40), Tags: []string{"mem"}, LegacySource: LegacySourceAI, Scope: "mem"},
		"bad date":        {ID: "20260822-3f9a2c1b", Type: TypeHistory, Date: "2026/08/22", Summary: strings.Repeat("가", 40), Tags: []string{"mem"}, LegacySource: LegacySourceAI, Scope: "mem"},
		"importance high": {ID: "20260822-3f9a2c1b", Type: TypeHistory, Date: "2026-08-22", Summary: strings.Repeat("가", 40), Tags: []string{"mem"}, LegacySource: LegacySourceAI, Scope: "mem", Importance: 6},
	}
	for name, memory := range cases {
		if problems := Validate(&memory); len(problems) == 0 {
			t.Errorf("%s: expected a problem, got none", name)
		}
	}
}

func TestValidateImportanceZeroAndInRangePass(t *testing.T) {
	base := Memory{ID: "20260822-3f9a2c1b", Type: TypeHistory, Date: "2026-08-22",
		Summary: strings.Repeat("가", 40), Tags: []string{"mem"}, LegacySource: LegacySourceAI, Scope: "mem"}
	for _, importance := range []int{0, 1, 5} {
		memory := base
		memory.Importance = importance
		if problems := Validate(&memory); len(problems) != 0 {
			t.Errorf("importance %d: unexpected problems %v", importance, problems)
		}
	}
	memory := base
	memory.Importance = -1
	if problems := Validate(&memory); len(problems) == 0 {
		t.Error("importance -1: expected a problem, got none")
	}
}

func TestValidateFileWithMissingFields(t *testing.T) {
	memory, err := Parse(readTestdata(t, "bad-no-scope.md"))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if problems := Validate(memory); len(problems) != 2 {
		t.Fatalf("expected scope and severity problems, got %v", problems)
	}
}

func TestParseRejectsMissingFrontMatter(t *testing.T) {
	if _, err := Parse([]byte("no front matter here\n")); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := Parse([]byte("---\nid: x\n")); err == nil {
		t.Fatal("expected an unclosed front matter error")
	}
}

func TestNewIDAndPath(t *testing.T) {
	at := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	first := NewID("본문", at)
	if len(first) != 17 || first[:9] != "20260822-" {
		t.Fatalf("unexpected id: %s", first)
	}
	if !idPattern.MatchString(first) {
		t.Fatalf("id does not match its own pattern: %s", first)
	}
	if second := NewID("본문", at.Add(time.Nanosecond)); second == first {
		t.Fatal("ids must differ when the time differs")
	}
	if got := StorePath(first); got != "store/2026/08/"+first+".md" {
		t.Fatalf("unexpected path: %s", got)
	}
}

func TestDisplayTitleFallsBackToSummary(t *testing.T) {
	memory := Memory{Summary: strings.Repeat("가", 50)}
	if len([]rune(memory.DisplayTitle())) != 40 {
		t.Fatal("fallback title must be the first 40 runes")
	}
	memory.Title = "제목"
	if memory.DisplayTitle() != "제목" {
		t.Fatal("explicit title must win")
	}
}

func TestStorePathRejectsEscape(t *testing.T) {
	bad := []string{`\..\..\AGENTS`, "../../AGENTS", "20260822-XYZ", "20260822-3F9A2C1B", "", "store/x"}
	for _, id := range bad {
		if IsID(id) {
			t.Fatalf("must not be an id: %q", id)
		}
		if got := StorePath(id); got != "" {
			t.Fatalf("path must be empty for %q, got %q", id, got)
		}
	}
	if !IsID("20260822-3f9a2c1b") {
		t.Fatal("a well formed id must pass")
	}
}
