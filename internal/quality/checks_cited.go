package quality

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// C16 `cited-not-superseded` — 새 기억이 `mem:` 으로 옛 것을 가리키고 「덮는다」라고
// 적었는데 옛 것에 superseded_by 가 없다. 2026-10-10 피드백의 오탐 14/14 를 막으려고
// 바뀜 말을 **문장 단위**로 본다 (설계 「구현 뒤 달라진 것」 절).

// negatedChanges 는 바뀜 말이 든 부정 꼴이다. 「덮지 않고 잇는다」는 덮은 것이 아니다.
var negatedChanges = []string{"덮어쓰지 않", "덮지 않", "덮지 말", "안 덮", "바꾸지 않", "바꾸지 말", "안 바꾼",
	"고치지 않", "고치지 말", "안 고친", "뒤집지 않", "안 뒤집", "대신하지 않", "폐기하지 않", "취소하지 않"}

// memIDShape 는 글 안에 적은 기억 id 다.
var memIDShape = regexp.MustCompile(`[0-9]{8}-[0-9a-z]{8}`)

// withoutNegatedChange 는 부정 꼴을 지운 글이다. 바뀜 말 찾기 전에 거친다.
func withoutNegatedChange(text string) string {
	for _, shape := range negatedChanges {
		text = strings.ReplaceAll(text, shape, " ")
	}
	return text
}

// changeLine 은 바뀜 말이 든 문장 하나다. ids 는 그 문장에 적힌 기억 id 다.
type changeLine struct {
	word string
	ids  []string
}

// changeSentences 는 제목·요약·본문에서 바뀜 말이 든 문장을 고른다.
// 문장은 줄바꿈과 마침표·물음표·느낌표로 자른다.
func changeSentences(m *model.Memory) []changeLine {
	text := withoutNegatedChange(m.Title + "\n" + m.Summary + "\n" + m.Body)
	pieces := strings.FieldsFunc(text, func(letter rune) bool {
		return strings.ContainsRune("\n.?!。", letter)
	})
	out := []changeLine{}
	for _, piece := range pieces {
		for _, word := range changeWordsC16 {
			if strings.Contains(piece, word) {
				out = append(out, changeLine{word: word, ids: memIDShape.FindAllString(piece, -1)})
				break
			}
		}
	}
	return out
}

// changeWordsC16 은 C16 이 보는 바뀜 말이다. changeWords 순서에 기대지 않게 따로 둔다.
var changeWordsC16 = []string{"덮", "대신", "바꾼", "고친", "뒤집"}

// pickChange 는 옛 기억 old 를 두고 쓸 바뀜 말이다. 못 쓰면 빈 글이다.
//
// 문장이 old 를 글자 그대로 가리키면 종류가 달라도 쓴다 (caution ← decision 진짜 사례).
// 다른 id 만 가리킨 문장은 그 id 이야기라 안 쓴다. id 없는 문장은 같은 종류일 때만 쓴다.
func pickChange(lines []changeLine, oldID string, sameType bool) string {
	for _, line := range lines {
		if slices.Contains(line.ids, oldID) {
			return line.word
		}
	}
	if !sameType {
		return ""
	}
	for _, line := range lines {
		if len(line.ids) == 0 {
			return line.word
		}
	}
	return ""
}

// citedNotSuperseded 는 C16 이다. m 이 「새 기억」 쪽이고, 걸림은 m 이 `mem:` 으로
// 가리킨 **옛 기억**에 단다. 짝 하나에 한 번이다.
//
// 같은 scope · 옛 것이 아직 살아 있음 · 둘 다 history 는 아님 · pickChange 가 낱말을
// 고름. 모음 기억은 원본을 `mem:` 으로 모으는 것이 일이라 새 쪽으로 안 친다.
// history 끼리는 판을 이어 쓴 것이라(3판 → 4판) 덮음이 아니다.
func citedNotSuperseded(m *model.Memory, byID map[string]*model.Memory, now time.Time, add addFunc) {
	if m.Type == model.TypeObservation {
		return
	}
	var lines []changeLine
	said := map[string]bool{}
	for _, source := range m.Sources {
		if model.SourceKind(source) != "mem" {
			continue
		}
		old := byID[strings.TrimSpace(strings.TrimPrefix(source, model.SourceMem))]
		if old == nil || old.ID == m.ID || said[old.ID] {
			continue
		}
		if old.Scope != m.Scope || !Live(old, now) {
			continue
		}
		if old.Type == model.TypeHistory && m.Type == model.TypeHistory {
			continue
		}
		if lines == nil {
			if lines = changeSentences(m); len(lines) == 0 {
				return
			}
		}
		word := pickChange(lines, old.ID, old.Type == m.Type)
		if word == "" {
			continue
		}
		said[old.ID] = true
		add(old, RuleCitedNotSuperseded,
			fmt.Sprintf("새 기억 `%s` 가 이 기억을 근거(mem:)로 달고 「%s」 라고 적었는데, 이 기억에 superseded_by 가 없다. 덮였는지 본다",
				clip(m.ID), word),
			m.ID)
	}
}
