package search

// 임베딩 순위를 낱말 순위에 어떻게 섞을지 (설계 18절 · 뒷정리-2a 실측).
//
// 세 갈래를 다 재 보고 골랐다.
//   - rrf  : 코사인 순위표를 RRF 표 한 장으로 얹는다 (v0.2·3A 방식).
//   - mix  : 낱말 상위 N 안에서 **낱말 정규화 점수와 코사인을 α 로 섞어**
//            다시 줄 세운다. α=0 이면 순수 임베딩 재정렬이다.
//   - flat : 낱말 점수가 납작할 때만 mix 를 쓴다.
//
// 어느 갈래든 **후보를 새로 데려오지 않는다.** 창(窓) 밖 문서는 점수를 안
// 건드리고, 창 안 문서는 창의 점수 띠 [최저, 최고] 안에서만 자리를 바꾼다.
// 그래서 창 밖 문서를 넘어서지도, 창 밖 문서에 밀리지도 않는다 —
// abstain(G2) 판정이 낱말 결과 그대로 남는다.

import (
	"os"
	"sort"
	"strconv"
)

const (
	// BlendRRF·BlendMix·BlendFlat 은 섞는 갈래 이름이다.
	BlendRRF  = "rrf"
	BlendMix  = "mix"
	BlendFlat = "flat"
)

// 기본값 — 뒷정리-2a 가 골든셋 3벌(v2 80·손 30·42)로 재서 고른 값이다 (설계 18절).
// **rrf 가 이겼다.** v0.2 가 파이썬으로 잰 「순수 재정렬 0.836」 은 이 짜임새에서
// 재현되지 않는다 — mix α=0(순수)은 v2 80건에서 0.687 로 오히려 떨어지고 오배제
// 0.015 를 낸다. mix·flat 은 다음 판이 다른 모델로 다시 재 볼 수 있게 남긴다.
const (
	DefaultEmbedBlend   = BlendRRF
	DefaultEmbedAlpha   = 0.5
	DefaultEmbedTopN    = 50
	DefaultEmbedFlatGap = 0.25
)

// flatK 는 「납작한가」 를 볼 때 견주는 깊이다.
const flatK = 5

// tieEps 는 코사인이 같을 때 낱말 점수로 가르는 몫이다.
const tieEps = 1e-6

// 아래 넷은 **시험용 뒷문**이다. 스윕이 같은 exe 로 조합을 바꿔 재려고 쓴다.
// 평소에는 아무도 안 준다 (MEM_EMBED_MODEL 과 같은 성격).
var (
	embedBlend   = envText("MEM_EMBED_BLEND", DefaultEmbedBlend)
	embedAlpha   = envFloat("MEM_EMBED_ALPHA", DefaultEmbedAlpha)
	embedTopN    = envInt("MEM_EMBED_TOPN", DefaultEmbedTopN)
	embedFlatGap = envFloat("MEM_EMBED_FLATGAP", DefaultEmbedFlatGap)
)

func envText(name, fallback string) string {
	if given := os.Getenv(name); given != "" {
		return given
	}
	return fallback
}

func envFloat(name string, fallback float64) float64 {
	value, err := strconv.ParseFloat(os.Getenv(name), 64)
	if err != nil {
		return fallback
	}
	return value
}

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

// applyEmbed 는 코사인 표를 merged 에 반영한다. docids 는 낱말 점수 순으로
// 이미 잘린 창이고, scored 는 그 안에서 floor 를 넘은 것만 담은 코사인이다.
func applyEmbed(merged *rungResult, docids []int64, scored []scoredDoc, options Options) {
	sort.Slice(scored, func(a, b int) bool {
		if scored[a].value != scored[b].value {
			return scored[a].value > scored[b].value
		}
		return scored[a].docid < scored[b].docid
	})
	if embedBlend == BlendRRF {
		rrfApply(merged, scored, options)
		return
	}
	window := docids
	if len(window) > embedTopN {
		window = window[:embedTopN]
	}
	alpha := embedAlpha
	if embedBlend == BlendFlat && !isFlat(merged, window) {
		return
	}
	mixApply(merged, window, scored, alpha)
}

// rrfApply 는 코사인 순위표를 RRF 표 한 장으로 얹는다 (v0.2·3A 방식).
func rrfApply(merged *rungResult, scored []scoredDoc, options Options) {
	if len(scored) > rankDepth {
		scored = scored[:rankDepth]
	}
	for place, item := range scored {
		merged.scores[item.docid] += options.embedWeight() / (options.softener() + float64(place+1))
		merged.from[item.docid] = append(merged.from[item.docid], "emb("+placeText(place+1)+")")
	}
}

// isFlat 은 낱말 점수 상위 flatK 의 차이가 작은지다. 1등과 k등이 거의 같으면
// 낱말은 「모르겠다」 고 말한 것이라, 그럴 때만 임베딩에 맡긴다.
func isFlat(merged *rungResult, window []int64) bool {
	if len(window) < 2 {
		return false
	}
	top := merged.scores[window[0]]
	if top <= 0 {
		return false
	}
	at := flatK - 1
	if at >= len(window) {
		at = len(window) - 1
	}
	return (top-merged.scores[window[at]])/top < embedFlatGap
}

// mixApply 는 창 안에서 α·낱말 + (1−α)·코사인 으로 다시 줄 세운다.
// 벡터가 없거나 floor 아래인 문서는 코사인 자리에 제 낱말 점수를 넣는다 —
// 「모르는 것」 이 「나쁜 것」 이 되면 안 된다.
func mixApply(merged *rungResult, window []int64, scored []scoredDoc, alpha float64) {
	if len(window) < 2 {
		return
	}
	high, low := merged.scores[window[0]], merged.scores[window[0]]
	for _, docid := range window {
		if value := merged.scores[docid]; value < low {
			low = value
		} else if value > high {
			high = value
		}
	}
	if high <= low {
		return
	}
	inWindow := make(map[int64]bool, len(window))
	for _, docid := range window {
		inWindow[docid] = true
	}
	cosHigh, cosLow, seen := 0.0, 0.0, false
	cosOf := make(map[int64]float64, len(scored))
	for _, item := range scored {
		if !inWindow[item.docid] {
			continue
		}
		cosOf[item.docid] = item.value
		if !seen {
			cosHigh, cosLow, seen = item.value, item.value, true
			continue
		}
		if item.value < cosLow {
			cosLow = item.value
		}
		if item.value > cosHigh {
			cosHigh = item.value
		}
	}
	if !seen {
		return
	}
	place := map[int64]int{}
	for at, item := range scored {
		place[item.docid] = at + 1
	}
	for _, docid := range window {
		word := (merged.scores[docid] - low) / (high - low)
		near := word
		if value, found := cosOf[docid]; found && cosHigh > cosLow {
			near = (value - cosLow) / (cosHigh - cosLow)
		} else if found {
			near = 1
		}
		mixed := (alpha*word + (1-alpha)*near + tieEps*word) / (1 + tieEps)
		merged.scores[docid] = low + (high-low)*mixed
		if at, found := place[docid]; found {
			merged.from[docid] = append(merged.from[docid], "emb("+placeText(at)+")")
		}
	}
}
