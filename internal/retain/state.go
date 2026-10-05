// Package retain 은 자동 쌓기 A1 이다 — Stop 알림 · PreCompact·SessionEnd 큐 표 ·
// 자동 관문(R1·R2·R4~R8). 모델은 하나도 안 부른다 (자동쌓기설계 2-2 · 2-3).
package retain

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// 상태·큐 파일 자리. 전부 Memory/local/ 아래라 git 에 안 들어간다.
const (
	stateFileName = "retain-state.json"
	queueDirName  = "retain-queue"
	// stateKeepDays 를 넘게 안 보인 세션 줄은 상태 파일에서 뺀다. 파일이 끝없이
	// 커지면 매 Stop 훅이 그만큼 느려진다.
	stateKeepDays = 30
)

// ShortLen 은 세션 id 를 줄여 적는 길이다 (`origin_session` · `note:session`).
const ShortLen = 8

var sessionPattern = regexp.MustCompile(`^[A-Za-z0-9-]{8,128}$`)

// Session 은 세션 하나의 알림 상태다. 오프셋 뒤만 읽어서 큰 대화 기록을 매 턴
// 통째로 읽지 않는다.
type Session struct {
	TranscriptPath string `json:"transcript_path"`
	Offset         int64  `json:"offset"`
	Edits          int    `json:"edits"`
	Turns          int    `json:"turns"`
	Nudges         int    `json:"nudges"`
	LastNudge      string `json:"last_nudge,omitempty"`
	// AutoAdds 는 이 세션에서 자동 관문을 지나 들어간 기억 수다 (R6 · 큐 covered).
	AutoAdds int    `json:"auto_adds"`
	Seen     string `json:"seen"`
}

// State 는 retain-state.json 통째다. Days 는 날짜별 자동 기억 수다 (R6 per_day).
type State struct {
	Sessions map[string]*Session `json:"sessions"`
	Days     map[string]int      `json:"days"`
}

// ValidSession 은 훅 입력의 session_id 를 파일 이름·글에 써도 되는지다.
func ValidSession(id string) bool { return sessionPattern.MatchString(id) }

// Short 는 세션 id 앞 8자다. 못 쓰는 id 면 빈 글이다.
func Short(id string) string {
	if !ValidSession(id) {
		return ""
	}
	return id[:ShortLen]
}

// StatePath 는 저장소 하나의 상태 파일이다.
func StatePath(dir string) string {
	return filepath.Join(store.LocalDir(dir), stateFileName)
}

// QueueDir 는 PreCompact·SessionEnd 표가 쌓이는 폴더다.
func QueueDir(dir string) string {
	return filepath.Join(store.LocalDir(dir), queueDirName)
}

// LoadState 는 상태 파일을 읽는다. 없거나 깨졌으면 빈 상태다 — 훅은 이것 때문에
// 멈추면 안 된다.
func LoadState(dir string) State {
	state := State{Sessions: map[string]*Session{}, Days: map[string]int{}}
	data, err := os.ReadFile(StatePath(dir))
	if err != nil {
		return state
	}
	if json.Unmarshal(data, &state) != nil {
		return State{Sessions: map[string]*Session{}, Days: map[string]int{}}
	}
	if state.Sessions == nil {
		state.Sessions = map[string]*Session{}
	}
	if state.Days == nil {
		state.Days = map[string]int{}
	}
	return state
}

// SaveState 는 옆에 쓰고 rename 한다. 반쯤 쓰인 파일이 안 보인다.
// 읽고 고쳐 쓰는 판이면 잠금까지 잡는 UpdateState 를 쓴다 — 이것만 부르면 두 세션이
// 겹칠 때 한쪽 셈이 사라진다.
func SaveState(dir string, state State, now time.Time) error {
	prune(&state, now)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(StatePath(dir), data)
}

// prune 은 오래 안 보인 세션과 지난 날짜 셈을 뺀다.
func prune(state *State, now time.Time) {
	limit := now.AddDate(0, 0, -stateKeepDays)
	for id, session := range state.Sessions {
		seen, err := time.Parse(time.RFC3339, session.Seen)
		if err != nil || seen.Before(limit) {
			delete(state.Sessions, id)
		}
	}
	for day := range state.Days {
		at, err := time.ParseInLocation("2006-01-02", day, time.Local)
		if err != nil || at.Before(limit) {
			delete(state.Days, day)
		}
	}
}

// Find 는 세션 id 나 그 앞머리(8자 이상)로 세션을 찾는다. 빈 값이면 가장 최근에
// 본 세션이다. 못 찾거나 앞머리가 8자보다 짧으면 빈 id 다 — 1글자 앞머리가 아무
// 세션이나 고르면 안 된다 (리뷰 2026-10-05).
func (s State) Find(prefix string) (string, *Session) {
	bestID, best := "", (*Session)(nil)
	if prefix != "" && len(prefix) < ShortLen {
		return bestID, best
	}
	for id, session := range s.Sessions {
		if prefix != "" && (len(id) < len(prefix) || id[:len(prefix)] != prefix) {
			continue
		}
		if best == nil || session.Seen > best.Seen {
			bestID, best = id, session
		}
	}
	return bestID, best
}

// RecentSessions 는 now 에서 within 안에 본 세션 수다. `add --origin` 이 `--session`
// 없이 왔을 때 「가장 최근 세션」 을 골라도 되는지 본다 — 둘 이상이면 남의 세션
// 기록과 대조할 수 있다.
func (s State) RecentSessions(now time.Time, within time.Duration) int {
	count := 0
	for _, session := range s.Sessions {
		seen, err := time.Parse(time.RFC3339, session.Seen)
		if err == nil && now.Sub(seen) <= within {
			count++
		}
	}
	return count
}

// 잠금 손잡이. 훅 예산(Stop 은 사람이 기다린다)을 넘지 않게 총 200ms 안쪽에서
// 포기한다. 훅 한 판은 수 ms 라 lockStale 보다 오래된 잠금은 죽은 프로세스가 남긴
// 고아로 보고 넘겨 잡는다.
const (
	lockWait  = 200 * time.Millisecond
	lockStep  = 10 * time.Millisecond
	lockStale = 5 * time.Second
)

// ErrStateBusy 는 다른 프로세스가 상태 파일을 쥐고 있어 잠금을 못 잡았다는 뜻이다.
// 훅은 이것을 받으면 조용히 이번 판을 건너뛴다.
var ErrStateBusy = errors.New("retain state is locked")

// UpdateState 는 상태 파일을 잠그고 읽어 change 로 고친 뒤 쓴다. change 가 거짓을
// 내면 안 쓴다. 깨진 상태 파일은 `.bad` 로 옮겨 두고 빈 상태에서 시작한다 —
// 그냥 덮으면 무엇이 깨졌는지 볼 길이 없다.
func UpdateState(dir string, now time.Time, change func(*State) bool) error {
	path := StatePath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	unlock, err := lockFile(path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	state := loadOrQuarantine(path)
	if !change(&state) {
		return nil
	}
	return SaveState(dir, state, now)
}

// loadOrQuarantine 은 LoadState 와 같되, 깨진 파일을 `.bad` 로 옮긴다. 잠금 안에서만
// 부른다 — 잠금 밖에서 옮기면 남이 막 쓴 파일을 치울 수 있다.
func loadOrQuarantine(path string) State {
	empty := State{Sessions: map[string]*Session{}, Days: map[string]int{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return empty
	}
	state := State{}
	if json.Unmarshal(data, &state) != nil {
		os.Rename(path, path+".bad")
		return empty
	}
	if state.Sessions == nil {
		state.Sessions = map[string]*Session{}
	}
	if state.Days == nil {
		state.Days = map[string]int{}
	}
	return state
}

// lockFile 은 O_EXCL 로 잠금 파일을 만든다. 못 만들면 lockStep 마다 다시 해 보고,
// lockWait 를 넘기면 ErrStateBusy 다. 오래된 잠금은 지우고 다시 잡는다.
func lockFile(path string) (func(), error) {
	deadline := time.Now().Add(lockWait)
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			file.Close()
			return func() { os.Remove(path) }, nil
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > lockStale {
			os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, ErrStateBusy
		}
		time.Sleep(lockStep)
	}
}

// writeAtomic 은 tmp 에 쓰고 rename 한다. tmp 이름에 pid·난수를 넣어 두 프로세스가
// 같은 tmp 를 덮지 않게 한다.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary := fmt.Sprintf("%s.%d-%d.tmp", path, os.Getpid(), rand.Int63())
	if err := os.WriteFile(temporary, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		os.Remove(temporary)
		return err
	}
	return nil
}
