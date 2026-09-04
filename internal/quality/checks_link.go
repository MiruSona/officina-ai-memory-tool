package quality

import (
	"fmt"

	"github.com/mirusona/officina-ai-memory-tool/internal/link"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// D04 `link-missing` — 같은 주제인데 `links` 가 없다 (설계 결정 43 신설분).
//
// 나머지 둘은 이미 있다. `C10 orphan`(links 0 + 남이 안 가리킴 + 조회 0)과
// `S02 link-density ≥ 0.5`(status --quality)는 v0.2 부터 있었는데, 실기억
// 206건에 `links:` 가 2건뿐이라 **전부가 고아**였고 그래서 둘 다 아무 말도
// 못 했다. 링크가 채워지면 둘은 저절로 산다 — 여기서 새로 만들 것은 없다.
//
// 등급은 candidate 다. 「이 둘은 이어야 한다」는 사람이 판단할 일이라 거절도
// 경고도 아니다.

// linkMissingMin 은 후보가 이만큼 있어야 말하는 자다. 하나쯤 있는 것은 그냥
// 흔한 태그다.
const linkMissingMin = 2

// checkLinkMissing 은 links 가 비었는데 이을 만한 이웃이 뚜렷한 기억을 찾는다.
// 자동 링크(결정 42)와 **같은 셈법**을 쓴다 — 둘이 어긋나면 lint 가 도구
// 자신이 방금 안 이은 것을 나무라게 된다.
func checkLinkMissing(memories []*model.Memory, opt RepoOptions,
	add func(*model.Memory, string, string, ...string)) {
	if len(memories) < 2 {
		return
	}
	docs := make([]link.Doc, len(memories))
	for at, one := range memories {
		docs[at] = linkDocOf(one)
	}
	// 짝을 다 도는 대신 한 번에 낸다 — 20k 에서 4억 쌍이 된다 (link.SuggestAll).
	all := link.SuggestAll(docs, link.MaxLinks)
	for _, one := range memories {
		if len(one.Links) > 0 {
			continue
		}
		found := all[one.ID]
		if len(found) < linkMissingMin {
			continue
		}
		add(one, RuleLinkMissing,
			fmt.Sprintf("같은 주제로 보이는 기억이 %d건인데 links 가 비었다", len(found)),
			idsOfCandidates(found)...)
	}
}

func linkDocOf(one *model.Memory) link.Doc {
	return link.Doc{ID: one.ID, Type: one.Type, Title: one.DisplayTitle(),
		Summary: one.Summary, Tags: one.Tags, Scope: one.Scope,
		Sources: one.Sources, Date: one.Date, Links: one.Links}
}

func idsOfCandidates(found []link.Candidate) []string {
	ids := make([]string, 0, len(found))
	for _, one := range found {
		ids = append(ids, one.ID)
	}
	return ids
}
