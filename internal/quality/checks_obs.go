package quality

import (
	"fmt"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 모음 기억(observation · B1) 규칙 둘 (자동쌓기설계 3-2·3-4).
//
//   - B13 `obs-cite` — 본문 번호 줄마다 끝에 [mem:id] 가 있고, 그 id 가 전부 sources 안이다.
//     근거 없는 줄은 원본에 없는 말을 지어낸 것일 수 있다.
//   - C15 `obs-stale` — 근거가 덮이거나·보류되거나·고쳐져 basis_hash 가 어긋났다.
//     색인(memories.obs_stale)과 같은 해시 함수를 파일로 다시 돌린다.

// checkObsCite 는 B13 이다. 모음 기억이 아니면 아무것도 안 본다.
func checkObsCite(m *model.Memory, opt Options) []Finding {
	if m.Type != model.TypeObservation {
		return nil
	}
	reason := obsCiteReason(m)
	if reason == "" {
		reason = obsBasisReason(m, opt.Lookup)
	}
	if reason == "" {
		return nil
	}
	return []Finding{opt.finding(RuleObsCite, m, reason,
		"본문은 「1. … [mem:id]」 줄만 둔다. id 는 sources 의 mem: 과 같아야 한다")}
}

// obsCiteReason 은 B13 에 걸린 까닭 한 줄이다. 비면 통과다.
func obsCiteReason(m *model.Memory) string {
	cites, stray := model.LineCites(m.Body)
	if stray > 0 {
		return fmt.Sprintf("번호 줄이 아닌 줄이 %d개다", stray)
	}
	if len(cites) == 0 {
		return "번호 줄이 하나도 없다"
	}
	allowed := map[string]bool{}
	for _, id := range model.MemSources(m.Sources) {
		allowed[id] = true
	}
	for at, line := range cites {
		if len(line) == 0 {
			return fmt.Sprintf("%d번 줄 끝에 [mem:id] 가 없다", at+1)
		}
		for _, id := range line {
			if !allowed[id] {
				return fmt.Sprintf("%d번 줄의 [mem:%s] 가 sources 에 없다", at+1, id)
			}
		}
	}
	return ""
}

// obsBasisReason 은 근거가 또 모음 기억인지 본다. 기계 글이 기계 글을 근거로 삼는
// 고리를 손 add 길에서도 막는다 (자동쌓기설계 3-3 · 리뷰 2026-10-05).
func obsBasisReason(m *model.Memory, lookup func(string) *model.Memory) string {
	if lookup == nil {
		return ""
	}
	for _, id := range model.MemSources(m.Sources) {
		if found := lookup(id); found != nil && found.Type == model.TypeObservation {
			return i18n.T(i18n.ObsCiteObservation, id)
		}
	}
	return ""
}

// obsStale 은 C15 다. 저장소 전체를 보고 낡은 모음 기억을 찾는다.
func obsStale(memories []*model.Memory, add func(*model.Memory, string, string, ...string)) {
	byID := map[string]*model.Memory{}
	has := false
	for _, one := range memories {
		byID[one.ID] = one
		if one.Type == model.TypeObservation {
			has = true
		}
	}
	if !has {
		return
	}
	for _, m := range memories {
		if m.Type != model.TypeObservation || m.SupersededBy != "" || m.Review {
			continue
		}
		reason, related := ObservationStale(m, byID)
		if reason == "" {
			continue
		}
		add(m, RuleObsStale, reason, related...)
	}
}

// ObservationStale 은 파일로 본 모음 기억 하나의 낡음 까닭과 걸린 근거 id 다.
// 비면 안 낡았다. `mem consolidate --plan` 도 이것을 쓴다.
func ObservationStale(m *model.Memory, byID map[string]*model.Memory) (string, []string) {
	ids := model.MemSources(m.Sources)
	parts := make([]model.BasisPart, 0, len(ids))
	for _, id := range ids {
		found := byID[id]
		if found == nil {
			return fmt.Sprintf("근거 `mem:%s` 가 저장소에 없다", id), []string{id}
		}
		parts = append(parts, model.PartOf(found))
	}
	if m.BasisHash != "" && model.BasisHash(parts) == m.BasisHash {
		return "", nil
	}
	inSet := map[string]bool{}
	for _, id := range ids {
		inSet[id] = true
	}
	held, covered := []string{}, []string{}
	for _, part := range parts {
		if part.Held {
			held = append(held, part.ID)
		}
		cover := byID[part.ID].SupersededBy
		if part.Dead && (cover == "" || !inSet[cover]) {
			covered = append(covered, part.ID)
		}
	}
	switch {
	case len(held) > 0:
		return "근거가 보류됐다 : " + strings.Join(held, ", "), held
	case len(covered) > 0:
		return "근거가 덮였거나 무효다 : " + strings.Join(covered, ", "), covered
	case m.BasisHash == "":
		// 해시 없는 옛 손 모음은 견줄 것이 없다. 죽음·보류만 본다 (색인 쪽과 같다).
		return "", nil
	}
	return "근거의 제목·요약·본문이 카드를 쓴 뒤 바뀌었다. mem consolidate --plan 으로 다시 쓴다", ids
}

// obsExemptRules 는 모음 기억에 안 맞는 본문·중복 규칙이다. 카드는 원본 줄을 번호로
// 이어 붙인 글이라 「실질 3줄」·「첫 줄 결론」·「요약과 본문이 닮음」·「원본과 닮음」에
// 늘 걸린다. 카드 글의 규격은 B13 이 따로 본다.
var obsExemptRules = map[string]bool{
	RuleBodyThin: true, RuleFirstLineConclusion: true, RuleSummaryBodyMatch: true,
	RuleNoValue: true, RuleMultiSummary: true, RuleMultiTable: true, RuleMultiHeading: true,
	RuleDuplicateHard: true, RuleDuplicateSoft: true, RuleSameBody: true, RuleDecisionGate: true,
	RuleTagNotSource: true, RuleHowtoNumbered: true,
}

// obsExempt 는 모음 기억이면 위 규칙의 걸림을 뺀다. 다른 종류는 그대로 둔다.
func obsExempt(m *model.Memory, found []Finding) []Finding {
	if m == nil || m.Type != model.TypeObservation {
		return found
	}
	out := found[:0]
	for _, one := range found {
		if !obsExemptRules[one.Rule] {
			out = append(out, one)
		}
	}
	return out
}

// withoutObservations 는 모음 기억을 뺀 목록이다. 없으면 받은 것을 그대로 돌려준다
// (모음 기억 0건 저장소의 답이 B1 전과 같다).
func withoutObservations(memories []*model.Memory) []*model.Memory {
	at := 0
	for at < len(memories) && memories[at].Type != model.TypeObservation {
		at++
	}
	if at == len(memories) {
		return memories
	}
	out := append(make([]*model.Memory, 0, len(memories)), memories[:at]...)
	for _, one := range memories[at:] {
		if one.Type != model.TypeObservation {
			out = append(out, one)
		}
	}
	return out
}
