package retain

import (
	"bytes"
	"encoding/json"
	"strings"
)

// 발화 역할. 대화 기록 JSONL 의 type 값 그대로다.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Utterance 는 대화 기록의 말 한 덩이다. 같은 역할이 잇달면 한 덩이로 붙인다.
type Utterance struct {
	Role string
	Text string
}

// ReadUtterances 는 대화 기록 끝 64MB 에서 사용자·AI 글만 차례대로 꺼낸다 (A2 설계 3절).
// 도구 입력·결과와 하네스 덩이(system-reminder)·isMeta·isCompactSummary 줄은 뺀다.
func ReadUtterances(path string) ([]Utterance, error) {
	data, err := readEnd(path)
	if err != nil {
		return nil, err
	}
	list := []Utterance{}
	for _, raw := range bytes.Split(data, []byte{'\n'}) {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			continue
		}
		one := line{}
		if json.Unmarshal(raw, &one) != nil || one.IsMeta || one.IsCompactSummary {
			continue
		}
		if one.Type != RoleUser && one.Type != RoleAssistant {
			continue
		}
		text := talkOf(one.Message.Content)
		if text == "" {
			continue
		}
		last := len(list) - 1
		if last >= 0 && list[last].Role == one.Type {
			list[last].Text += "\n" + text
			continue
		}
		list = append(list, Utterance{Role: one.Type, Text: text})
	}
	return list, nil
}

// talkOf 는 한 줄의 content 에서 글 블록만 모아 다듬는다. 도구 블록은 버린다.
func talkOf(content json.RawMessage) string {
	text := ""
	if json.Unmarshal(content, &text) == nil {
		return strings.TrimSpace(cleanTalk(text))
	}
	blocks := []block{}
	if json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	parts := []string{}
	for _, item := range blocks {
		if item.Type != "text" {
			continue
		}
		clean := strings.TrimSpace(cleanTalk(item.Text))
		if clean != "" {
			parts = append(parts, clean)
		}
	}
	return strings.Join(parts, "\n")
}
