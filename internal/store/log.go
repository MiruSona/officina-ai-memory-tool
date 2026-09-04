package store

// Memory/log.md 는 사람이 훑는 시간순 기록이다 (설계 3-5). 검색 색인에는 안
// 들어가고, 도구가 한 줄씩 덧붙이기만 한다.
//
// 파도 G 가 review 쪽에 같은 일을 하는 함수를 두면 그쪽으로 옮기고 여기를
// 지운다 — 지금은 add·set·migrate 가 쓸 자리가 필요해서 여기에 둔다.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// bomPrefix 는 UTF-8 BOM 이다. 사람이 메모장으로 고치면 붙는다.
const bomPrefix = "\uFEFF"

// LogFileName 은 기록 파일 이름이다.
const LogFileName = "log.md"

// LogPath 는 저장소 하나의 기록 파일 자리다.
func LogPath(dir string) string { return filepath.Join(dir, LogFileName) }

// 기록 한 줄의 머리말. 무슨 일이 있었는지를 두 글자로 가른다.
const (
	LogAdded    = "들어옴"
	LogSuper    = "덮음"
	LogFolded   = "접힘"
	LogRejected = "거절"
	LogMigrated = "이전"
	LogRenamed  = "태그바꿈"
	// LogPromoted 는 사람이 자동 생성 기억을 승격한 것이다 (결정 6).
	LogPromoted = "승격"
)

// AppendLog 는 오늘 날짜 절 밑에 한 줄을 덧붙인다. 날짜 절이 없으면 만든다.
// 실패해도 부르는 쪽이 하던 일을 멈추지 않는다 — 기록은 곁다리다.
func AppendLog(dir string, at time.Time, kind, line string) error {
	path := LogPath(dir)
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := string(old)
	heading := "## " + at.Format("2006-01-02")
	out := strings.Builder{}
	out.WriteString(text)
	if text != "" && !strings.HasSuffix(text, "\n") {
		out.WriteString("\n")
	}
	if !strings.Contains(text, heading+"\n") {
		if text != "" {
			out.WriteString("\n")
		}
		out.WriteString(heading + "\n")
	}
	out.WriteString(fmt.Sprintf("- %s %s\n", kind, oneLine(line)))
	return appendFileRetry(path, out.String())
}

// oneLine 은 줄바꿈을 공백으로 눌러 한 항목이 반드시 한 줄이게 한다.
func oneLine(text string) string {
	return strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(text))
}

// appendFileRetry 는 통째로 다시 쓴다. log.md 는 한 줄씩 자라는 작은 파일이고,
// 통째 쓰기가 붙이기보다 반쯤 쓰인 줄을 안 남긴다.
func appendFileRetry(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileRetry(path, []byte(text))
}

// ReadLog 는 기록 파일을 읽는다. 없으면 빈 글이다.
func ReadLog(dir string) (string, error) {
	data, err := os.ReadFile(LogPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimPrefix(string(data), bomPrefix), nil
}
