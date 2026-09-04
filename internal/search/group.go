package search

import (
	"strconv"

	"github.com/mirusona/officina-ai-memory-tool/internal/link"
)

// 묶음 반환 (설계 결정 44).
//
// multisession 질의의 답은 문서 하나가 아니라 **한 덩이**다. 화면에 흩어져
// 있으면 읽는 쪽이 「둘이 한 이야기」인 줄 모른다. 같은 주제끼리 번호를 매겨
// `--json` 의 `group` 칸과 표의 꼬리표로 알려 준다.
//
// **점수와 차례는 안 건드린다.** 묶음은 보여 주는 방식일 뿐이라, 이것 때문에
// 순위가 바뀌면 자(eval)가 재는 것이 달라진다. 훅 주입에도 안 실린다
// (결정 15 · 7절 — 훅은 이 자리를 안 지난다).

// groupMin 은 묶음으로 부를 최소 건수다. 혼자면 묶음이 아니다.
const groupMin = 2

// groupHits 는 보여 줄 답에 묶음 번호를 매긴다. 1부터 세고, 어느 묶음에도 안
// 드는 것은 0(= JSON 에서 안 보임)이다.
func groupHits(hits []Hit) int {
	if len(hits) < groupMin {
		return 0
	}
	docs := make([]link.Doc, len(hits))
	for at := range hits {
		docs[at] = docOf(hits[at])
	}
	// 이어진 덩이 찾기 — 차례대로 훑으며 이미 선 묶음에 붙인다. 위에 선 답이
	// 묶음의 머리가 되므로 번호가 곧 좋은 차례다.
	group := make([]int, len(hits))
	next := 0
	for at := range hits {
		for before := 0; before < at; before++ {
			if group[before] != 0 && link.SameTopic(docs[at], docs[before]) {
				group[at] = group[before]
				break
			}
		}
		if group[at] != 0 {
			continue
		}
		for after := at + 1; after < len(hits); after++ {
			if link.SameTopic(docs[at], docs[after]) {
				next++
				group[at] = next
				break
			}
		}
	}
	return renumber(hits, group)
}

// renumber 는 식구가 하나뿐인 묶음을 지우고 번호를 1부터 다시 매긴다.
// 앞의 훑기는 「나중에 짝이 있다」를 보고 번호를 주는데, 그 짝이 더 앞의
// 묶음에 먼저 붙어 버리면 혼자 남는 묶음이 생긴다.
func renumber(hits []Hit, group []int) int {
	size := map[int]int{}
	for _, one := range group {
		size[one]++
	}
	renamed := map[int]int{}
	next := 0
	for at := range hits {
		old := group[at]
		if old == 0 || size[old] < groupMin {
			hits[at].Group = 0
			continue
		}
		if renamed[old] == 0 {
			next++
			renamed[old] = next
		}
		hits[at].Group = renamed[old]
	}
	return next
}

// groupMark 는 표 한 줄에 붙일 묶음 꼬리표다. 남의 글이 아니라 도구가 만든
// 숫자라 살균할 것이 없다.
func groupMark(hit Hit) string {
	if hit.Group == 0 {
		return ""
	}
	return " [묶음" + strconv.Itoa(hit.Group) + "]"
}
