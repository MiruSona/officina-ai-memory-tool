package search

import (
	"github.com/mirusona/officina-ai-memory-tool/internal/link"
)

// MMR 다양화 (설계 4절 · 결정 44).
//
// multisession 질의는 *여러 문서를 다 찾아야* 답이 된다. 그런데 같은 주제 문서
// 다섯 건이 나란히 서면 답의 다른 반쪽이 6위로 밀린다. MMR 은 「이미 고른 것과
// 닮은 것」의 값을 깎아 그 자리를 비운다.
//
// **설계의 `λ·관련도 − (1−λ)·닮음` 을 곱셈으로 적었다.** RRF 점수는 0~1 자가
// 아니라(0.02~0.3 근처) 빼기를 그대로 쓰면 상위 한 건의 값으로 아래가 통째로
// 0 이 된다. 그래서 뺄 몫을 **그 항목 자기 점수에 대한 비율**로 준다 —
// λ=0.7 이면 완전히 겹친 항목이 자기 점수의 (1−λ)/λ = 0.43 을 잃는다.
// 차례는 여전히 점수 내림이라 --explain 과 어긋나지 않는다.

// mmrLambda 는 관련도와 다양성의 저울이다 (결정 44 · λ=0.7).
const mmrLambda = 0.7

// mmrFloor 는 「겹친다」고 볼 최소 닮음이다. 이 아래는 안 깎는다.
//
// 없으면 안 된다 : 같은 scope 라는 것만으로 닮음이 0.2 씩 깔리는데, 벌점은
// **자기 위에 선 것 전부**와 견주므로 아래 칸일수록 벌점이 쌓인다. 그러면 MMR 이
// 다양화가 아니라 「꼬리를 누르는 장치」가 된다 (embed 회귀 시험이 잡은 자국).
const mmrFloor = 0.35

// mmrPool 은 MMR 이 도는 상위 건수다. 설계 4절 예산이 「50² 닮음」이라 적었다.
const mmrPool = 50

// diversify 는 상위 mmrPool 건을 MMR 로 다시 세운다. 후보를 자르지 않는다 —
// 값을 깎을 뿐이라 관문을 지난 것은 그대로 남는다.
// 이어진 짝을 벌점에서 빼 보기도 했다 — multisession MRR 은 0.135 → 0.160 으로
// 오르는데 전체 r@5 가 0.716 → 0.701 로 내려 안 넣었다 (206건 실측).
func diversify(hits []Hit, raw bool) []Hit {
	if raw || len(hits) < 2 {
		return hits
	}
	pool := len(hits)
	if pool > mmrPool {
		pool = mmrPool
	}
	penalty := (1 - mmrLambda) / mmrLambda
	docs := make([]link.Doc, pool)
	for at := 0; at < pool; at++ {
		docs[at] = docOf(hits[at])
	}
	// 1위는 늘 그대로다. 그 다음부터 앞에 선 것들과의 닮음만큼 깎는다.
	for at := 1; at < pool; at++ {
		worst := 0.0
		for before := 0; before < at; before++ {
			if same := link.Similarity(docs[at], docs[before]); same > worst {
				worst = same
			}
		}
		if worst < mmrFloor {
			continue
		}
		factor := 1 - penalty*worst
		hits[at].Score *= factor
		hits[at].Parts.Diverse = factor
	}
	sortHits(hits[:pool])
	return hits
}

// docOf 는 답 한 줄을 이웃 셈법이 보는 꼴로 바꾼다.
func docOf(hit Hit) link.Doc {
	return link.Doc{ID: hit.ID, Type: hit.Type, Title: hit.Title,
		Summary: hit.Summary, Tags: hit.Tags, Scope: hit.Scope}
}
