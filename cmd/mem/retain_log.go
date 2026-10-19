package main

// retain-log — `mem retain` 한 판의 세션별 기록이다 (A2 retain 설계 7절). Memory/local 아래라
// git 에 안 들어간다. 첫 실측에서 토큰 수·시간·관문 효과를 이 줄로 잰다.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// retainLogDirName 은 retain-log 폴더 이름이다.
const retainLogDirName = "retain-log"

// retainVerdictPass 는 dry-run 후보가 관문을 다 지났다는 표시다. 걸리면 규칙 이름을 쉼표로 잇는다.
const retainVerdictPass = "pass"

// retainLogLine 은 세션 하나의 기록이다. 칸 이름은 설계 7절 그대로다.
type retainLogLine struct {
	At            string          `json:"at"`
	Session       string          `json:"session"`
	Dry           bool            `json:"dry"`
	Bytes         int64           `json:"bytes"`
	TalkChars     int             `json:"talk_chars"`
	Chunks        int             `json:"chunks"`
	DroppedChunks int             `json:"dropped_chunks"`
	PromptTokens  []int           `json:"prompt_tokens"`
	Candidates    int             `json:"candidates"`
	ParseFail     int             `json:"parse_fail"`
	Saved         int             `json:"saved"`
	SavedIDs      []string        `json:"saved_ids"`
	Rejected      map[string]int  `json:"rejected"`
	GenMS         []int64         `json:"gen_ms"`
	TotalMS       int64           `json:"total_ms"`
	Error         string          `json:"error"`
	DroppedFiles  int             `json:"dropped_files"`
	Items         []retainLogItem `json:"items,omitempty"`
}

// retainLogItem 은 dry-run 후보 하나와 관문 판정이다 (관문 효과 자의 자료). Sources 는 자식에 넘긴 최종 근거다.
// Summary 는 R3 가 인용과 견주는 글이고, Reasons 는 거절 규칙마다 자식이 준 까닭 글이다 (사람이 거절을 다시 판정할 때 본다).
type retainLogItem struct {
	Title   string   `json:"title"`
	Type    string   `json:"type"`
	Summary string   `json:"summary"`
	Quotes  []string `json:"quotes"`
	Sources []string `json:"sources"`
	Verdict string   `json:"verdict"`
	Reasons []string `json:"reasons,omitempty"`
}

func newRetainLogLine(short string, dry bool, now time.Time) retainLogLine {
	return retainLogLine{At: now.Format(time.RFC3339), Session: short, Dry: dry, PromptTokens: []int{},
		SavedIDs: []string{}, Rejected: map[string]int{}, GenMS: []int64{}}
}

// appendRetainLog 는 그날 파일 끝에 한 줄을 붙인다.
func appendRetainLog(dir string, line retainLogLine, now time.Time) error {
	folder := filepath.Join(store.LocalDir(dir), retainLogDirName)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(line)
	if err != nil {
		return err
	}
	path := filepath.Join(folder, now.Format("2006-01-02")+".jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = file.Write(append(data, '\n'))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}
