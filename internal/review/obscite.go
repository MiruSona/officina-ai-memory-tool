package review

// obs-cite-nli — 모음 기억(observation) 번호 줄 ↔ 그 줄이 인용한 기억 (적용 설계 5절 · B13).
//
// consolidate 가 만든 줄은 인용 기억의 「제목 — 요약」을 그대로 베끼므로 물으면 늘 지지다.
// 그래서 베낀 줄은 안 묻고, 사람이나 모델이 손으로 고친 줄만 묻는다. 확신 반대면 basis 큐에
// 경고를 하나 올린다 — 거절은 아니다. quality 의 B13(lint · LLM 0)은 그대로 둔다.

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/llm"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
)

// RuleObsCiteNLI 는 모음 기억 줄이 인용 기억과 어긋날 수 있다는 경고다 (basis 큐).
const RuleObsCiteNLI = "obs-cite-nli"

// obsLinesPerCard 는 카드 하나에서 묻는 번호 줄 상한이다.
const obsLinesPerCard = 10

var (
	// obsLineHead 는 줄 머리 「1. 」 과 「[예전] 」·「[지금] 」 표지다.
	obsLineHead = regexp.MustCompile(`^\d+\.\s+(?:\[(?:예전|지금)\]\s*)?`)
	obsLineDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\s*`)
	obsCite     = regexp.MustCompile(`\[mem:(\d{8}-[0-9a-f]{8})\]`)
)

// obsCiteAsks 는 살아 있는 모음 기억마다 번호 줄을 읽어, 인용 기억의 「제목 — 요약」과 다른 줄만
// 묻는다 (근거 = 인용 기억 요약, 주장 = 줄 글). 카드 하나에 obsLinesPerCard 줄까지 본다.
func (r *Report) obsCiteAsks(found *quality.RepoReport, byID map[string]*model.Memory,
	scopes map[string]bool, options Options) []nliAsk {
	asks := []nliAsk{}
	for _, id := range sortedKeys(byID) {
		card := byID[id]
		if card.Type != model.TypeObservation || !alive(card, options) || !inScopes(scopes, card) {
			continue
		}
		number := 0
		for _, line := range strings.Split(card.Body, "\n") {
			line = strings.TrimSpace(line)
			if !obsLineHead.MatchString(line) {
				continue
			}
			number++
			if number > obsLinesPerCard {
				break
			}
			asks = append(asks, obsLineAsks(found, card, line, number, byID, options)...)
		}
	}
	return asks
}

// obsLineAsks 는 번호 줄 하나의 물음이다. 인용이 여럿이면 인용마다 묻는다.
func obsLineAsks(found *quality.RepoReport, card *model.Memory, line string, number int,
	byID map[string]*model.Memory, options Options) []nliAsk {
	text := obsLineText(line)
	asks := []nliAsk{}
	for _, match := range obsCite.FindAllStringSubmatch(line, -1) {
		cited := byID[match[1]]
		// 덮였거나 무효인(살아 있지 않은) 인용 기억은 묻지 않는다 — 그쪽은 C15·basis 가 본다.
		if cited == nil || !alive(cited, options) || text == "" || text == obsCopiedText(cited) {
			continue
		}
		evidence := quality.StripTags(cited.Summary)
		if strings.TrimSpace(evidence) == "" {
			continue
		}
		citedID := cited.ID
		asks = append(asks, nliAsk{key: [2]string{card.ID, citedID}, scope: card.Scope,
			evidence: evidence, claim: text, apply: func(verdict llm.Verdict) {
				if verdict.Letter != llm.LetterContradict || verdict.Unsure {
					return
				}
				found.ByID[card.ID] = append(found.ByID[card.ID], quality.Finding{Rule: RuleObsCiteNLI,
					Level: quality.GradeWarn, ID: card.ID, Related: []string{citedID},
					Reason: fmt.Sprintf("%d번 줄이 `[mem:%s]` 와 어긋날 수 있다 (반대 %.2f)", number, citedID, verdict.Prob)})
			}})
	}
	return asks
}

// obsLineText 는 줄에서 번호 · 표지 · 날짜 · [mem:id] 를 떼고 남은 글이다. 빈칸은 하나로 접는다.
func obsLineText(line string) string {
	text := obsLineHead.ReplaceAllString(line, "")
	text = obsLineDate.ReplaceAllString(text, "")
	text = obsCite.ReplaceAllString(text, "")
	return strings.Join(strings.Fields(text), " ")
}

// obsCopiedText 는 consolidate 카드가 그 기억으로 쓰는 「제목 — 요약」이다 (consolidate 의
// cardBody · oneLine 과 같은 꼴 — 줄 안의 「[mem:」 은 「[mem 」 으로 바뀌어 있다).
func obsCopiedText(m *model.Memory) string {
	text := m.DisplayTitle() + " — " + m.Summary
	return strings.Join(strings.Fields(strings.ReplaceAll(text, "[mem:", "[mem ")), " ")
}
