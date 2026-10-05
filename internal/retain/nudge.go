package retain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
)

// ChildEnv 가 1 인 프로세스는 우리가 띄운 자식 세션이라 알림을 안 낸다
// (자동쌓기설계 2-2 조건 5).
const ChildEnv = "MEM_RETAIN_CHILD"

// StopInput 은 Stop 훅 입력에서 쓰는 칸이다.
type StopInput struct {
	SessionID      string
	TranscriptPath string
	Active         bool
}

// Decision 은 Stop 한 번의 판단이다. 알림을 낼지와 그 까닭을 같이 준다 —
// 까닭은 `--json` 으로 재는 사람만 본다.
type Decision struct {
	Nudge   bool   `json:"nudge"`
	Short   string `json:"session,omitempty"`
	Why     string `json:"why"`
	Edits   int    `json:"edits"`
	Turns   int    `json:"turns"`
	Nudges  int    `json:"nudges"`
	MemAdds int    `json:"mem_adds"`
}

// 침묵 까닭. 글자가 아니라 이름이라 i18n 표에 안 둔다.
const (
	WhyActive    = "stop_hook_active"
	WhyOff       = "nudge_off"
	WhyChild     = "child"
	WhyBadInput  = "bad_input"
	WhyNoRecord  = "no_transcript"
	WhyAdded     = "already_added"
	WhyBelow     = "below_threshold"
	WhyMaxed     = "nudge_max"
	WhyNudged    = "nudged"
	WhyStateFail = "state_write_failed"
)

// Stop 은 알림 조건 다섯을 본다 (자동쌓기설계 2-2). dir 은 Memory 폴더다.
// 상태 파일을 못 쓰면 알림을 안 낸다 — 못 세면 같은 알림이 매 턴 되풀이된다.
func Stop(dir string, settings config.RetainConfig, input StopInput, now time.Time) Decision {
	switch {
	case input.Active:
		return Decision{Why: WhyActive}
	case !settings.Nudge:
		return Decision{Why: WhyOff}
	case os.Getenv(ChildEnv) == "1":
		return Decision{Why: WhyChild}
	case !ValidSession(input.SessionID):
		return Decision{Why: WhyBadInput}
	}
	decision := Decision{}
	// 읽기→고치기→쓰기를 잠금 하나로 감싼다. 두 세션의 Stop 이 겹치면 한쪽 셈이
	// 사라지고, 같은 알림이 되풀이되거나 상한(nudge_max)이 0 으로 돌아간다.
	err := UpdateState(dir, now, func(state *State) bool {
		session := state.Sessions[input.SessionID]
		if session == nil || session.TranscriptPath != input.TranscriptPath {
			session = &Session{TranscriptPath: input.TranscriptPath}
			state.Sessions[input.SessionID] = session
		}
		tally, offset, err := ReadTail(input.TranscriptPath, session.Offset)
		if err != nil {
			decision = Decision{Why: WhyNoRecord}
			return false
		}
		session.Offset = offset
		session.Edits += tally.Edits
		session.Turns += tally.Turns
		session.Seen = now.Format(time.RFC3339)
		decision = Decision{Short: Short(input.SessionID), MemAdds: tally.MemAdds}
		decision.Why = judge(session, settings, tally.MemAdds)
		if decision.Why == WhyNudged {
			session.Nudges++
			session.LastNudge = session.Seen
			session.Edits, session.Turns = 0, 0
			decision.Nudge = true
		}
		decision.Edits, decision.Turns, decision.Nudges = session.Edits, session.Turns, session.Nudges
		return true
	})
	if err != nil {
		return Decision{Why: WhyStateFail}
	}
	return decision
}

// judge 는 조건 2~4 다. AI 가 알림 없이도 이미 `mem add` 를 쳤으면 일한 셈을
// 비운다 — 그 뒤로 다시 문턱만큼 일해야 알린다.
func judge(session *Session, settings config.RetainConfig, memAdds int) string {
	if memAdds > 0 {
		session.Edits, session.Turns = 0, 0
		return WhyAdded
	}
	if session.Nudges >= settings.NudgeMax {
		return WhyMaxed
	}
	editsOver := settings.NudgeEdits > 0 && session.Edits >= settings.NudgeEdits
	turnsOver := settings.NudgeTurns > 0 && session.Turns >= settings.NudgeTurns
	if !editsOver && !turnsOver {
		return WhyBelow
	}
	return WhyNudged
}

// 큐 표의 까닭 둘.
const (
	ReasonCompact = "compact"
	ReasonEnd     = "end"
)

// QueueEntry 는 알림을 못 받았을 수 있는 세션 표 하나다. A2(`mem retain`)가 읽는다.
type QueueEntry struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Reason         string `json:"reason"`
	At             string `json:"at"`
	Offset         int64  `json:"offset"`
	// Covered 는 그 세션에서 자동 관문을 지난 기억이 이미 있다는 뜻이다.
	Covered bool `json:"covered"`
}

// Mark 는 큐 표 하나를 쓴다. 대화 기록을 안 읽어 SessionEnd 1.5초 예산에 맞는다.
// 같은 세션은 같은 파일이라 compact 뒤 end 가 오면 덮어쓴다.
func Mark(dir, sessionID, transcriptPath, reason string, now time.Time) error {
	short := Short(sessionID)
	if short == "" {
		return errNotTranscript
	}
	state := LoadState(dir)
	entry := QueueEntry{SessionID: sessionID, TranscriptPath: transcriptPath, Reason: reason,
		At: now.Format(time.RFC3339)}
	if session := state.Sessions[sessionID]; session != nil {
		entry.Offset = session.Offset
		entry.Covered = session.AutoAdds > 0
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(QueueDir(dir), short+".json"), data)
}
