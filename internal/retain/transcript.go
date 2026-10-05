package retain

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// 대화 기록 읽기 상한. 훅은 오프셋 뒤 꼬리만, 관문은 통째로 읽는다. 훅 꼬리는
// 처음 보는 큰 세션(이어 열기 등)에서도 p95 200ms 안에 들게 2MB 로 자른다.
const (
	tailLimit  = 2 << 20
	wholeLimit = 64 << 20
)

// editTools 는 「파일을 고친 도구 호출」로 세는 이름이다 (알림 문턱 nudge_edits).
var editTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}

// reminderPattern 은 훅·하네스가 끼운 덩이다. 우리 기억 블록이 다시 근거로
// 쓰이는 고리를 막으려고 대화 글에서 뺀다 (자동쌓기설계 2-3 보내기 전 처리).
var reminderPattern = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)

// memAddPattern 은 셸 도구 명령 안의 `mem add` 다. 경로로 부른 exe
// (`.\…\mem.exe add` · `& "C:\…\mem.exe" add`)도 센다 — 앞 글자에 `\ / " '` 를 받고
// exe 뒤 닫는 따옴표를 받는다 (리뷰 2026-10-05).
var memAddPattern = regexp.MustCompile(`(^|[\s;&|(\\/"'])mem(\.exe)?["']?\s+add\b`)

// commandEnd 는 셸 명령 한 토막의 끝이다. `--check` 를 그 토막 안에서만 찾는다.
var commandEnd = regexp.MustCompile(`[;|&\r\n]`)

// checkFlag 는 미리보기 옵션이다. `mem add --check` 는 넣은 것이 아니다.
var checkFlag = regexp.MustCompile(`(^|\s)--check(\s|=|$)`)

// hasMemAdd 는 명령 안에 미리보기가 아닌 `mem add` 가 하나라도 있는지다.
func hasMemAdd(command string) bool {
	for _, at := range memAddPattern.FindAllStringIndex(command, -1) {
		rest := command[at[1]:]
		if end := commandEnd.FindStringIndex(rest); end != nil {
			rest = rest[:end[0]]
		}
		if !checkFlag.MatchString(rest) {
			return true
		}
	}
	return false
}

var errNotTranscript = errors.New("not a transcript file")

// Tally 는 대화 기록 한 토막에서 센 것과 모은 글이다.
type Tally struct {
	Edits   int
	Turns   int
	MemAdds int
	// Talk 은 사용자·AI 글만이다 (R1). All 은 도구 입력·결과까지 넣은 것이다 (R2).
	Talk strings.Builder
	All  strings.Builder
}

// line 은 대화 기록 JSONL 한 줄에서 우리가 보는 칸만이다.
type line struct {
	Type    string `json:"type"`
	IsMeta  bool   `json:"isMeta"`
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type block struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Content json.RawMessage `json:"content"`
}

// openTranscript 는 대화 기록 파일만 연다. 훅 입력이 엉뚱한 파일을 가리켜도
// 링크·특수 파일·`.jsonl` 아닌 것은 안 읽는다.
func openTranscript(path string) (*os.File, os.FileInfo, error) {
	if path == "" || !filepath.IsAbs(path) || !strings.EqualFold(filepath.Ext(path), ".jsonl") {
		return nil, nil, errNotTranscript
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errNotTranscript
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return file, info, nil
}

// ReadTail 은 offset 뒤 완성된 줄만 센다. 새 오프셋은 마지막 줄바꿈 바로 뒤다 —
// 쓰는 중인 마지막 줄은 다음 번에 다시 읽는다.
func ReadTail(path string, offset int64) (*Tally, int64, error) {
	file, info, err := openTranscript(path)
	if err != nil {
		return nil, offset, err
	}
	defer file.Close()
	size := info.Size()
	if offset > size || offset < 0 {
		// 파일이 줄었다 (다른 세션 기록으로 바뀜 등). 처음부터 다시 센다.
		offset = 0
	}
	if size-offset > tailLimit {
		offset = size - tailLimit
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	data, err := io.ReadAll(io.LimitReader(file, tailLimit))
	if err != nil {
		return nil, offset, err
	}
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return &Tally{}, offset, nil
	}
	tally := &Tally{}
	tally.add(data[:end+1])
	return tally, offset + int64(end) + 1, nil
}

// wholeRoom 은 ReadWhole 이 읽는 꼬리 길이다. 시험이 줄여 쓴다.
var wholeRoom int64 = wholeLimit

// ReadWhole 은 관문이 근거를 대조하려고 대화 기록 전체를 읽는다. 상한보다 크면
// **끝** 상한만큼 읽는다 — 근거는 대개 최근 글에 있다. 줄 중간에서 시작한 첫 토막은
// 버린다 (리뷰 2026-10-05).
func ReadWhole(path string) (*Tally, error) {
	file, info, err := openTranscript(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	start := int64(0)
	if info.Size() > wholeRoom {
		start = info.Size() - wholeRoom
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, wholeRoom))
	if err != nil {
		return nil, err
	}
	if start > 0 {
		cut := bytes.IndexByte(data, '\n')
		if cut < 0 {
			data = nil
		} else {
			data = data[cut+1:]
		}
	}
	tally := &Tally{}
	tally.add(data)
	return tally, nil
}

// add 는 줄마다 갈라 센다. 못 읽는 줄은 건너뛴다 — 모르는 꼴은 침묵이다.
func (t *Tally) add(data []byte) {
	for _, raw := range bytes.Split(data, []byte{'\n'}) {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			continue
		}
		one := line{}
		if json.Unmarshal(raw, &one) != nil {
			continue
		}
		if one.Type != "user" && one.Type != "assistant" {
			continue
		}
		t.addLine(one)
	}
}

func (t *Tally) addLine(one line) {
	text := ""
	if json.Unmarshal(one.Message.Content, &text) == nil {
		if one.Type == "user" && !one.IsMeta && strings.TrimSpace(cleanTalk(text)) != "" {
			t.Turns++
		}
		t.addTalk(text)
		return
	}
	blocks := []block{}
	if json.Unmarshal(one.Message.Content, &blocks) != nil {
		return
	}
	userText := false
	for _, item := range blocks {
		switch item.Type {
		case "text":
			userText = userText || strings.TrimSpace(cleanTalk(item.Text)) != ""
			t.addTalk(item.Text)
		case "tool_use":
			t.addToolUse(item)
		case "tool_result":
			t.All.WriteString(contentText(item.Content) + "\n")
		}
	}
	if one.Type == "user" && !one.IsMeta && userText {
		t.Turns++
	}
}

func (t *Tally) addTalk(text string) {
	clean := cleanTalk(text)
	t.Talk.WriteString(clean + "\n")
	t.All.WriteString(clean + "\n")
}

func (t *Tally) addToolUse(item block) {
	if editTools[item.Name] {
		t.Edits++
	}
	input := map[string]any{}
	if json.Unmarshal(item.Input, &input) != nil {
		return
	}
	if command, ok := input["command"].(string); ok && hasMemAdd(command) {
		t.MemAdds++
	}
	for _, value := range input {
		t.All.WriteString(stringsOf(value) + "\n")
	}
}

// cleanTalk 은 하네스 덩이를 뺀 글이다.
func cleanTalk(text string) string {
	return reminderPattern.ReplaceAllString(text, " ")
}

// contentText 는 tool_result 의 content 를 글로 편다 (글 하나이거나 블록 목록).
func contentText(raw json.RawMessage) string {
	text := ""
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	blocks := []block{}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	parts := []string{}
	for _, item := range blocks {
		if item.Type == "text" {
			parts = append(parts, item.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// stringsOf 는 도구 입력 값 안의 글을 다 꺼내 잇는다.
func stringsOf(value any) string {
	switch item := value.(type) {
	case string:
		return item
	case []any:
		parts := []string{}
		for _, one := range item {
			parts = append(parts, stringsOf(one))
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		parts := []string{}
		for _, one := range item {
			parts = append(parts, stringsOf(one))
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// Fold 는 공백·줄바꿈을 한 칸으로 접고 소문자·`/` 로 맞춘다. 근거 대조는 이
// 꼴끼리만 견준다.
func Fold(text string) string {
	text = strings.ReplaceAll(text, `\`, "/")
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}
