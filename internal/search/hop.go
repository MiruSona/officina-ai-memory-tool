package search

// 1-hop 링크 가산 (설계 4절 · 결정 44).
//
// **가산으로만 쓴다. 후보를 안 늘린다.** v0.2 결정 19 가 이웃 넓히기를 통째로
// 뺀 이유가 「abstain 1.000 을 깨뜨릴 위험이 이득보다 크다」였다. 링크 이웃은
// 질의 낱말을 안 담을 수 있어서, 이웃이라는 이유로 후보에 넣으면 「없다」고
// 답해야 할 질의가 무언가를 내놓는다.
//
// 그래서 고삐 둘을 그대로 지킨다 —
//  ① RRF 순위에 이미 있는 문서만 본다 (여기 오는 hits 가 곧 그것이다).
//  ② 가산 상한 bonus_cap 을 넘지 않는다.
//
// 차례는 관문 뒤 · MMR 뒤다 (설계 4절 그림). 앞에 선 답이 가리키는 기억을
// 끌어올리는 것이라, 무엇이 앞에 섰는지가 정해진 뒤라야 뜻이 있다.

// hopStep 은 이웃 하나마다 붙는 가산이다. 실기억 206건 골든셋으로 재서 골랐다
// (보고 참조). 이미 있는 `지목됨` 가산 0.1 보다 크게 잡는다 — 그것은 저장소
// 전체에서 지목됐다는 뜻이고, 이것은 **이번 답 안에서** 이어졌다는 뜻이라
// 신호가 훨씬 세다.
const hopStep = 0.25

// hopMax 는 한 항목이 받을 수 있는 이웃 수 상한이다. 링크가 많은 기억 하나가
// 가산만으로 1위를 먹는 것을 막는다.
const hopMax = 2

// hopPool 은 이웃을 따지는 상위 건수다. MMR 과 같은 폭이다.
const hopPool = mmrPool

// hopBonus 는 앞에 선 답이 가리키는(또는 앞선 답을 가리키는) 기억의 값을
// 올린다. 새 문서는 한 건도 안 들어온다.
func hopBonus(hits []Hit, edges linkEdges, cap float64, raw bool) []Hit {
	if raw || len(hits) < 2 || len(edges) == 0 {
		return hits
	}
	pool := len(hits)
	if pool > hopPool {
		pool = hopPool
	}
	changed := false
	for at := 1; at < pool; at++ {
		near, ceiling := 0, 0.0
		for before := 0; before < at; before++ {
			if !edges.joined(hits[at].ID, hits[before].ID) {
				continue
			}
			if near == 0 {
				// 맨 위에 선 이웃이 천장이다 (아래 applyHop 주석).
				ceiling = hits[before].Score
			}
			near++
		}
		if near > hopMax {
			near = hopMax
		}
		if near == 0 {
			continue
		}
		if applyHop(&hits[at], near, cap, ceiling) {
			changed = true
		}
	}
	if changed {
		sortHits(hits[:pool])
	}
	return hits
}

// applyHop 은 가산 상한을 지키며 한 항목의 값을 올린다. 상한에 이미 닿아
// 있으면 아무 일도 안 한다.
//
// **자기를 끌어올린 이웃보다 위로는 못 간다**(ceiling). 이웃 가산은 질의가
// 아니라 곁이 준 값이라, 질의를 실제로 맞힌 기억을 넘어서면 안 된다. 이 천장이
// 없으면 링크가 많은 기억이 곁 값만으로 1위를 먹는다 — v0.0 에서 bm25 0.017 인
// 기억이 가산 0.8 로 1위를 먹은 것과 같은 꼴이다.
func applyHop(hit *Hit, near int, cap, ceiling float64) bool {
	if hit.Parts.Bonus <= 0 {
		return false
	}
	want := hit.Parts.Bonus + hopStep*float64(near)
	if want > cap {
		want = cap
	}
	if want <= hit.Parts.Bonus {
		return false
	}
	lifted := hit.Score * want / hit.Parts.Bonus
	if ceiling > 0 && lifted > ceiling {
		lifted = ceiling
	}
	if lifted <= hit.Score {
		return false
	}
	hit.Score = lifted
	hit.Parts.Bonus = want
	hit.Parts.Why = append(hit.Parts.Why, "이웃")
	return true
}

// linkEdges 는 이 답들 사이의 링크다. 방향은 안 따진다 — 「같은 이야기의
// 다른 조각」이라는 뜻만 쓰기 때문이다.
type linkEdges map[string]map[string]bool

func (e linkEdges) joined(a, b string) bool {
	return e[a][b] || e[b][a]
}

// edgesAmong 은 이 답들 사이에만 있는 링크다. 저장소 전체 그래프를 안 읽는다.
func edgesAmong(sources []Source, hits []Hit) (linkEdges, error) {
	edges := linkEdges{}
	pool := len(hits)
	if pool > hopPool {
		pool = hopPool
	}
	if pool < 2 {
		return edges, nil
	}
	ids := make([]string, pool)
	for at := 0; at < pool; at++ {
		ids[at] = hits[at].ID
	}
	for _, from := range sources {
		found, err := from.DB.LinksAmong(ids)
		if err != nil {
			return nil, err
		}
		for src, targets := range found {
			if edges[src] == nil {
				edges[src] = map[string]bool{}
			}
			for dst := range targets {
				edges[src][dst] = true
			}
		}
	}
	return edges, nil
}
