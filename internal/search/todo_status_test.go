package search

import (
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 할 일은 표의 종류 칸에 상태가 붙는다 — 끝난 것을 show 없이 가려 본다 (반복 06 피드백 2번).
func TestKindOfShowsTodoStatus(t *testing.T) {
	cases := []struct {
		hit  Hit
		want string
	}{
		{Hit{Type: model.TypeTodo, TodoStatus: model.StatusOpen}, "todo open"},
		{Hit{Type: model.TypeTodo, TodoStatus: model.StatusDone}, "todo done"},
		{Hit{Type: model.TypeIssue, Severity: model.SeverityHigh}, "issue high"},
		{Hit{Type: model.TypeDecision}, "decision"},
	}
	for _, test := range cases {
		if got := kindOf(test.hit); got != test.want {
			t.Fatalf("kindOf : %q, 바란 것 %q", got, test.want)
		}
	}
}
