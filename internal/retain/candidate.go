package retain

import (
	"encoding/json"
	"errors"
	"strings"
)

// 생성 후보를 버린 까닭. retain-log 의 rejected 칸에 이 이름으로 센다 (A2 설계 4·7절).
const (
	DropParse = "parse"
	DropType  = "type"
	DropTags  = "tags"
	DropScope = "scope"
	DropCap   = "cap"
)

// genTypes 는 생성이 낼 수 있는 종류 넷이다. decision 은 R7 이 막으므로 처음부터 안 받는다.
var genTypes = map[string]bool{"caution": true, "todo": true, "howto": true, "fact": true}

// supersedeLine 은 덮을 기억을 R5 가 허용하는 꼴로 본문 끝에 적는 줄이다.
const supersedeLine = "덮을 후보 : mem:"

// GenCandidate 는 생성 서버가 낸 기억 후보 하나다 (A2 설계 4절 출력 꼴).
// 관문에 거는 Candidate 와는 다른 것이다 — 이것을 자식 `mem add` 인자로 편다.
type GenCandidate struct {
	Type       string   `json:"type"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Body       string   `json:"body"`
	Tags       []string `json:"tags"`
	Scope      string   `json:"scope"`
	Quotes     []string `json:"quotes"`
	Files      []string `json:"files"`
	Supersedes string   `json:"supersedes"`
}

type genOutput struct {
	Candidates []json.RawMessage `json:"candidates"`
	Nothing    bool              `json:"nothing"`
}

var errEmptyAnswer = errors.New("empty generate answer")

// ParseCandidates 는 생성 답을 읽어 다듬는다. JSON 이 통째로 깨졌으면 err 다 — 부르는
// 쪽이 한 번 다시 묻는다. 후보 하나가 틀리면 그것만 버리고 dropped 에 까닭별로 센다.
// vocab 목록이 비면 그 거르기는 끈다. maxPer 가 0 이하면 상한이 없다.
func ParseCandidates(raw string, vocabScopes, vocabTags []string, maxPer int) ([]GenCandidate, bool, map[string]int, error) {
	text := stripFence(raw)
	if text == "" {
		return nil, false, nil, errEmptyAnswer
	}
	output := genOutput{}
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		return nil, false, nil, err
	}
	dropped := map[string]int{}
	cands := []GenCandidate{}
	for _, item := range output.Candidates {
		one, why := tidyCandidate(item, vocabScopes, vocabTags)
		if why != "" {
			dropped[why]++
			continue
		}
		if maxPer > 0 && len(cands) >= maxPer {
			dropped[DropCap]++
			continue
		}
		cands = append(cands, one)
	}
	return cands, output.Nothing, dropped, nil
}

// tidyCandidate 는 후보 하나를 읽고 거른다. 버릴 때는 까닭 이름을 준다.
func tidyCandidate(item json.RawMessage, vocabScopes, vocabTags []string) (GenCandidate, string) {
	one := GenCandidate{}
	if json.Unmarshal(item, &one) != nil {
		return one, DropParse
	}
	one.Type = strings.ToLower(strings.TrimSpace(one.Type))
	one.Title, one.Summary = strings.TrimSpace(one.Title), strings.TrimSpace(one.Summary)
	one.Body, one.Scope = strings.TrimSpace(one.Body), strings.TrimSpace(one.Scope)
	if one.Type == "" || one.Title == "" || one.Summary == "" || one.Body == "" || one.Scope == "" {
		return one, DropParse
	}
	if len(one.Quotes) < 1 || len(one.Quotes) > quoteMax {
		return one, DropParse
	}
	if !genTypes[one.Type] {
		return one, DropType
	}
	if len(vocabScopes) > 0 && !contains(vocabScopes, one.Scope) {
		return one, DropScope
	}
	one.Tags = standardTags(one.Tags, vocabTags)
	if len(one.Tags) == 0 {
		return one, DropTags
	}
	if id := strings.TrimSpace(one.Supersedes); id != "" {
		one.Body += "\n" + supersedeLine + id
		one.Supersedes = ""
	}
	return one, ""
}

// standardTags 는 표준 태그만 남기고 겹친 것을 뺀다. 표준 목록이 비면 그대로 둔다.
func standardTags(tags, vocab []string) []string {
	kept := []string{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || contains(kept, tag) {
			continue
		}
		if len(vocab) > 0 && !contains(vocab, tag) {
			continue
		}
		kept = append(kept, tag)
	}
	return kept
}

// stripFence 는 앞뒤 ``` 울타리(언어 이름 포함)를 벗긴다.
func stripFence(raw string) string {
	text := strings.TrimSpace(raw)
	if !strings.HasPrefix(text, "```") {
		return text
	}
	newline := strings.IndexByte(text, '\n')
	if newline < 0 {
		return ""
	}
	text = strings.TrimSpace(text[newline+1:])
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}
