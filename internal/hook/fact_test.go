package hook

import (
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 종류 표의 `hook` 칸이 절을 만든다 — fact 는 「환경」 절에 실린다.
func TestFactSectionAppears(t *testing.T) {
	dir := stuffed(t)
	addMany(t, dir, model.TypeFact, "mem", 6, false)
	indexAll(t, dir)
	text := contextOf(t, runHook(t, []string{"session-start"}, startInput(dir)))
	if !strings.Contains(text, "## "+model.HookFact+" (") {
		t.Fatalf("환경 절이 안 생겼다 :\n%s", text)
	}
	// 절마다 최근 5줄 그대로다.
	if strings.Contains(text, "## "+model.HookFact+" (6") {
		t.Fatalf("환경 절이 절당 줄 수를 넘었다 :\n%s", text)
	}
	// 절 차례는 고정 · 최근 결정 · 열린 이슈·할 일 · 환경 · 알림 이다.
	if at, before := strings.Index(text, "## "+model.HookFact), strings.Index(text, "## "+model.HookOpen); at < before {
		t.Fatalf("환경 절이 열린 이슈·할 일 앞에 왔다 :\n%s", text)
	}
	// 절이 하나 늘어도 바이트 상한을 안 넘는다.
	if max := config.Default("시험").Hook.MaxBytes; len(text) > max {
		t.Fatalf("블록이 %d바이트다 (상한 %d)", len(text), max)
	}
}

// 0건인 절은 아예 안 만든다 — 다른 절과 같은 규칙이다.
func TestFactSectionHiddenWhenEmpty(t *testing.T) {
	dir := filled(t)
	text := contextOf(t, runHook(t, []string{"session-start"}, startInput(dir)))
	if strings.Contains(text, "## "+model.HookFact) {
		t.Fatalf("fact 가 한 건도 없는데 환경 절이 생겼다 :\n%s", text)
	}
}
