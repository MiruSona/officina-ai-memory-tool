package index

// `--status` 거르기는 todo_status 칸을 본다. 옛 이름 status 를 쓰면 SQL 오류다
// (반복 06 피드백 1번).

import (
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

func TestListRowsFiltersByTodoStatus(t *testing.T) {
	opened := newStore(t)
	todos := []struct {
		id     string
		status string
		body   string
	}{
		{"20260823-e1e11111", model.StatusOpen, "열린 할 일 본문이다."},
		{"20260823-e2e22222", model.StatusDone, "끝난 할 일 본문이다."},
		{"20260823-e3e33333", model.StatusDoing, "하는 중인 할 일 본문이다."},
	}
	for _, item := range todos {
		memory := sampleMemory(item.id)
		memory.Type = model.TypeTodo
		memory.TodoStatus = item.status
		memory.Body = item.body
		writeMemory(t, opened.Dir, memory)
	}
	plain := sampleMemory("20260823-e4e44444")
	plain.Body = "일반 결정 본문이다."
	writeMemory(t, opened.Dir, plain)
	runIndex(t, opened)

	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	cases := []struct {
		status string
		want   int
	}{
		{model.StatusOpen, 1},
		{model.StatusDone, 1},
		{model.StatusDoing, 1},
		{"없는값", 0},
		{"", 4},
	}
	now := time.Now()
	for _, test := range cases {
		rows, err := database.ListRows(Filter{Status: test.status}, 10, now)
		if err != nil {
			t.Fatalf("--status %q 가 오류를 냈다 : %v", test.status, err)
		}
		if len(rows) != test.want {
			t.Fatalf("--status %q : %d건, 바란 것 %d건", test.status, len(rows), test.want)
		}
	}
}
