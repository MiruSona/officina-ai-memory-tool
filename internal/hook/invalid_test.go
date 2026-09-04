package hook

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// putMemory 는 머리말을 직접 정한 기억 하나를 store 에 바로 쓴다. inbox 를
// 거치면 invalid_at·superseded_by 를 못 넣는다.
func putMemory(t *testing.T, dir string, item *model.Memory) {
	t.Helper()
	if problems := model.Validate(item); len(problems) > 0 {
		t.Fatalf("시험 기억이 규격을 어겼다 : %v", problems[0])
	}
	opened := store.Open(filepath.Join(dir, config.DirName), false)
	if err := opened.WriteMemory(item); err != nil {
		t.Fatal(err)
	}
}

func plain(id, kind, summary string) *model.Memory {
	return &model.Memory{ID: id, Type: kind, Date: "2026-08-01", Summary: longEnough(summary),
		Tags: []string{"mem", "hook"}, LegacySource: model.LegacySourceAI, Scope: "mem",
		Body: "본문은 여기에 둔다."}
}

// 무효 기억은 훅 어느 절에도 안 실린다 — 검색과 같은 규칙이다.
func TestHookSkipsInvalid(t *testing.T) {
	dir := newRepo(t)
	gone := time.Now().AddDate(0, 0, -3).Format("2006-01-02")

	deadPin := plain("20260801-11110000", model.TypeDecision, "고정인데 새 결정이 덮어 버린 옛 결정이다")
	deadPin.Pinned = true
	deadPin.SupersededBy = "20260801-99990000"
	putMemory(t, dir, deadPin)

	deadTodo := plain("20260801-22220000", model.TypeTodo, "덮여 버린 할 일이라 세션에 실리면 안 된다")
	deadTodo.TodoStatus = model.StatusOpen
	deadTodo.SupersededBy = "20260801-99990000"
	putMemory(t, dir, deadTodo)

	deadDecision := plain("20260801-33330000", model.TypeDecision, "기한이 지나 버린 결정이라 세션에 실리면 안 된다")
	deadDecision.InvalidAt = gone
	putMemory(t, dir, deadDecision)

	deadCaution := plain("20260801-44440000", model.TypeCaution, "덮여 버린 주의라서 세션에 실리면 안 된다")
	deadCaution.Severity = model.SeverityHigh
	deadCaution.SupersededBy = "20260801-99990000"
	putMemory(t, dir, deadCaution)

	live := plain("20260801-99990000", model.TypeDecision, "지금 살아 있는 결정 하나는 반드시 실려야 한다")
	putMemory(t, dir, live)

	indexAll(t, dir)
	text := contextOf(t, runHook(t, []string{EventName}, startInput(dir)))
	if !strings.Contains(text, live.ID) {
		t.Fatalf("살아 있는 결정이 안 실렸다 :\n%s", text)
	}
	for _, dead := range []string{deadPin.ID, deadTodo.ID, deadDecision.ID, deadCaution.ID} {
		if strings.Contains(text, dead) {
			t.Fatalf("무효 기억 %s 가 훅 절에 실렸다 :\n%s", dead, text)
		}
	}
}
