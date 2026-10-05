package hook

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/retain"
)

// 자동 쌓기 훅 셋의 이벤트 이름이다 (자동쌓기설계 2-2). 셋 다 LLM 0 이고, 색인을
// 열지 않는다 — 설정·상태 파일·대화 기록 꼬리만 읽는다.
const (
	StopEventName       = "Stop"
	PreCompactEventName = "PreCompact"
	SessionEndEventName = "SessionEnd"
)

// retainStats 는 `--json` 으로 재는 값이다.
type retainStats struct {
	Event string `json:"event"`
	retain.Decision
	Queued bool  `json:"queued,omitempty"`
	MS     int64 `json:"ms"`
}

// runRetain 은 Stop·PreCompact·SessionEnd 한 번이다. 무엇이 틀려도 종료는 0 이고,
// 할 말이 없으면 stdout 은 빈 채로 둔다.
func runRetain(input Input, flags flagSet, kind Event, stdout io.Writer, started time.Time) int {
	stats := retainStats{Event: eventNameOf(kind)}
	text := ""
	project := retainProject(input)
	if project != nil && kind == EventStop {
		decision := retain.Stop(project.Dir, project.Config.Retain, retain.StopInput{
			SessionID: input.SessionID, TranscriptPath: input.TranscriptPath,
			Active: input.StopHookActive}, time.Now())
		stats.Decision = decision
		if decision.Nudge {
			text = nudgeText(project, decision.Short)
		}
	}
	if project != nil && kind != EventStop {
		reason := retain.ReasonEnd
		if kind == EventPreCompact {
			reason = retain.ReasonCompact
		}
		err := retain.Mark(project.Dir, input.SessionID, input.TranscriptPath, reason, time.Now())
		note(err)
		stats.Queued = err == nil
	}
	stats.MS = time.Since(started).Milliseconds()
	if flags.json {
		raw, err := json.Marshal(stats)
		if err == nil {
			fmt.Fprintln(stdout, string(raw))
		}
		return 0
	}
	if text == "" {
		return 0
	}
	if flags.dryRun {
		fmt.Fprint(stdout, text)
		return 0
	}
	write(stdout, text, StopEventName)
	return 0
}

// retainProject 는 훅 입력의 cwd 로 저장소를 찾는다. 못 찾으면 nil — 침묵이다.
func retainProject(input Input) *config.Repository {
	dir, ok := startDir(input)
	if !ok {
		note(errors.New(i18n.T(i18n.HookNoCwd)))
		return nil
	}
	project, err := config.Resolve("", dir)
	if err != nil {
		note(err)
		return nil
	}
	return project
}

// nudgeText 는 알림 글이다 (자동쌓기설계 2-2). 우리 상수와 vocab scope 목록만
// 싣는다 — 대화 글·기억 글은 한 글자도 안 싣는다.
func nudgeText(project *config.Repository, short string) string {
	lines := []string{
		i18n.T(i18n.NudgeHead),
		i18n.T(i18n.NudgeTypes),
		i18n.T(i18n.NudgeSkip),
		i18n.T(i18n.NudgeHow, short),
		i18n.T(i18n.NudgeNothing),
	}
	if scopes := knownScopes(project); scopes != "" {
		lines = append(lines, i18n.T(i18n.NudgeScopes, scopes))
	}
	return strings.Join(lines, "\n") + "\n"
}
