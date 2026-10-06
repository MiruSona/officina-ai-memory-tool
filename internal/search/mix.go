package search

// 뜻 후보 섞기 (C2 · 모음기억설계 4-6 · 2026-10-05 뜻후보섞기측정 7절).
//
// 옛 의미 길(rerank.go 의 vectorsInto)은 **낱말이 찾아 놓은 후보의 차례만**
// 바꾼다. 낱말이 하나도 안 겹치는 기억은 뜻이 아무리 가까워도 못 나왔다.
// 여기서는 기억 전체 벡터에서 코사인 상위 MixTop 건을 **따로** 데려와,
// 사다리가 다 끝난 화면 차례(낱말 순위)와 섞는다.
//
// 섞는 법 (2차 판) : 한 후보의 몫은 낱말 쪽 1/(k+j) 와 뜻 쪽 w/(k+p) 중
// **큰 쪽 하나**다. 1차 판은 둘을 더했는데, 두 목록에 다 든 후보가 두 몫을 받아
// 낱말 1·2위 답을 10위 밖으로 밀어냈다. 큰 쪽 하나면 낱말 j 위 앞에는 뜻
// 1~(j-1) 위만 끼어들 수 있어 j 위는 2j-1 위 안에 남는다. 그 위에 낱말 상위
// MixKeep 건은 제 자리보다 뒤로 안 간다(자리 지킴).
//
// 자리 : 사다리 → 관문 → MMR → 1-hop 가산이 다 끝난 **뒤**, 자르기 앞이다.
// 사다리 안의 옛 재정렬은 섞기가 돌아도 그대로 돈다 — 낱말 차례가 C2 전과 같다.
// 섞기를 끄면 결과가 C2 전과 건마다 같다.
//
// 뜻 몫은 바닥(코사인 MixFloor · 뜻 순위 MixRank)을 넘는 뜻 후보만 받는다.
// 뜻으로만 올라온 기억(낱말 근거 없음)은 들어오면 `[뜻]` 칸(RungMeaning)에 앉아
// strict 답으로 센다 (결정 16·29 를 고쳐 씀 · 사용자 승인 2026-10-05).
// 낱말 칸 답은 칸을 안 바꾼다 — 섞기는 차례만 바꾼다.

import (
	"math"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/embed"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
)

// Scanner 는 기억 전체에서 뜻이 가까운 것을 데려오는 자리다. Vectors 를 꽂은
// 쪽이 이것까지 갖췄을 때만 섞기가 돈다 — 시험용 Vectors 는 안 갖춰도 된다.
type Scanner interface {
	Nearest(query []float32, limit int) []embed.Near
}

// 아래는 **시험용 뒷문**이다. 손잡이를 고를 때 같은 exe 로 판을 바꿔 재려고
// 쓴다 (blend.go 의 MEM_EMBED_* 와 같은 성격). 평소에는 아무도 안 준다.
// 주면 mem.toml [embed] 값보다 세다.
var (
	mixSwitch = os.Getenv("MEM_MIX")
	mixFloorE = envFloat("MEM_MIX_FLOOR", -1)
	mixRankE  = envInt("MEM_MIX_RANK", 0)
	mixTopE   = envInt("MEM_MIX_TOP", 0)
	mixWtE    = envFloat("MEM_MIX_WEIGHT", -1)
	mixKE     = envFloat("MEM_MIX_K", -1)
	mixKeepE  = envInt("MEM_MIX_KEEP", -1)
	mixGapE   = envFloat("MEM_MIX_GAP", -1)
)

// mixOn 은 섞기를 켤지다. 뒷문 MEM_MIX=0/1 이 mem.toml 보다 세다.
func (o *Options) mixOn() bool {
	switch mixSwitch {
	case "0", "false", "off":
		return false
	case "1", "true", "on":
		return true
	}
	return o.Embed.Mix
}

func (o *Options) mixTop() int {
	if mixTopE > 0 {
		return mixTopE
	}
	if o.Embed.MixTop <= 0 {
		return config.DefaultEmbedMixTop
	}
	return o.Embed.MixTop
}

func (o *Options) mixWeight() float64 {
	if mixWtE >= 0 {
		return mixWtE
	}
	if o.Embed.MixWeight <= 0 {
		return config.DefaultEmbedMixWeight
	}
	return o.Embed.MixWeight
}

func (o *Options) mixK() float64 {
	if mixKE > 0 {
		return mixKE
	}
	if o.Embed.MixK <= 0 {
		return config.DefaultEmbedMixK
	}
	return o.Embed.MixK
}

func (o *Options) mixFloor() float64 {
	if mixFloorE >= 0 {
		return mixFloorE
	}
	if o.Embed.MixFloor <= 0 {
		return config.DefaultEmbedMixFloor
	}
	return o.Embed.MixFloor
}

func (o *Options) mixRank() int {
	if mixRankE > 0 {
		return mixRankE
	}
	if o.Embed.MixRank <= 0 {
		return config.DefaultEmbedMixRank
	}
	return o.Embed.MixRank
}

// mixKeep 은 자리를 지키는 낱말 상위 건수다. 0 은 「안 지킴」이라 기본값
// 갈음을 안 한다 — 음수만 0 으로 본다.
func (o *Options) mixKeep() int {
	if mixKeepE >= 0 {
		return mixKeepE
	}
	if o.Embed.MixKeep < 0 {
		return 0
	}
	return o.Embed.MixKeep
}

// mixGap 은 뜻으로만 온 답의 도드라짐 문턱이다. 0 은 「관문 끔」이라 기본값
// 갈음을 안 한다 — 음수만 0 으로 본다 (mixKeep 과 같은 꼴). 1 을 넘는 값은
// config.Validate 가 알리고 여기서는 그대로 쓴다.
func (o *Options) mixGap() float64 {
	if mixGapE >= 0 {
		return mixGapE
	}
	if o.Embed.MixGap < 0 {
		return 0
	}
	return o.Embed.MixGap
}

// meaningPick 은 뜻 순위 한 줄이다 — 좁히기를 지난 기억만, 코사인 차례로.
// stand 는 도드라짐(cos − 좁히기 전 Nearest 코사인 가운데값)이다.
type meaningPick struct {
	row   index.SearchRow
	place int
	cos   float64
	stand float64
}

// mixMeaning 은 화면 차례(hits)에 뜻 순위를 섞은 새 차례를 준다. 둘째 답은
// 섞기가 실제로 돌았는지다(모드 표시). 꺼졌거나 벡터·모델이 없으면 hits 를
// 그대로 돌려준다 — 그때 결과가 C2 전과 건마다 같다.
func mixMeaning(hits []Hit, options Options, query *Query, narrow index.Filter) ([]Hit, bool) {
	if options.Raw || !options.mixOn() || options.Vectors == nil || len(options.Sources) == 0 {
		return hits, false
	}
	scanner, ok := options.Vectors.(Scanner)
	if !ok {
		return hits, false
	}
	wanted := options.Vectors.Query(embedQueryText(query))
	if len(wanted) == 0 {
		return hits, false
	}
	picks, err := meaningPicks(scanner, wanted, options, narrow)
	if err != nil {
		return hits, false
	}
	// 뜻 후보가 0건이어도 뜻을 물어본 판이다 — 모드를 「낱말+의미」로 둔다.
	// 뜻 몫이 하나도 없으면 fuse 는 낱말 차례를 그대로 돌려준다.
	return fuse(hits, picks, options), true
}

// meaningPicks 는 뜻 후보를 색인 행으로 잇고 좁히기를 지난 것만 남긴다.
// 벡터는 첫 저장소 것이라(rerankerFor) 첫 저장소 색인에서만 찾는다.
func meaningPicks(scanner Scanner, wanted []float32, options Options, narrow index.Filter) ([]meaningPick, error) {
	near := scanner.Nearest(wanted, options.mixTop())
	if len(near) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(near))
	for _, item := range near {
		ids = append(ids, item.ID)
	}
	database := options.Sources[0].DB
	docids, err := database.DocidsOf(ids)
	if err != nil {
		return nil, err
	}
	asked := make([]int64, 0, len(docids))
	for _, item := range near {
		if docid, found := docids[item.ID]; found {
			asked = append(asked, docid)
		}
	}
	rows, err := database.FetchRows(asked, narrow, time.Now())
	if err != nil {
		return nil, err
	}
	byID := make(map[string]index.SearchRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	middle := middleCos(near)
	out := []meaningPick{}
	for _, item := range near {
		row, found := byID[item.ID]
		if !found {
			continue
		}
		out = append(out, meaningPick{row: row, place: len(out) + 1, cos: item.Cos, stand: item.Cos - middle})
	}
	return out, nil
}

// middleCos 는 Nearest 코사인의 가운데값이다. 좁히기 **전** 전체로 재야
// --type 같은 거르개에 따라 같은 질문의 문턱이 안 흔들린다. 평균이 아닌 까닭은
// 카드·원본처럼 거의 같은 기억 쌍이 평균을 끌어올리기 때문이다 (설계 2-2).
// 짝수 개면 가운데 둘의 평균, 1개면 그 값, 0개면 0 이다.
func middleCos(near []embed.Near) float64 {
	if len(near) == 0 {
		return 0
	}
	values := make([]float64, len(near))
	for at, item := range near {
		values[at] = item.Cos
	}
	sort.Float64s(values)
	half := len(values) / 2
	if len(values)%2 == 1 {
		return values[half]
	}
	return (values[half-1] + values[half]) / 2
}

// fuse 는 화면 차례와 뜻 순위를 섞는다. 몫은 낱말 1/(k+j) 와 뜻 w/(k+p) 중 큰
// 쪽 하나다. 바닥을 못 넘은 뜻 후보는 몫이 없다 — 낱말 답이면 낱말 몫만, 뜻으로만
// 온 기억이면 아예 안 들어온다. 같은 몫이면 낱말 쪽이 앞이다.
func fuse(hits []Hit, picks []meaningPick, options Options) []Hit {
	softener, weight := options.mixK(), options.mixWeight()
	floor, ceiling, gap := options.mixFloor(), options.mixRank(), options.mixGap()
	meaningOf := make(map[string]meaningPick, len(picks))
	for _, pick := range picks {
		meaningOf[pick.row.ID] = pick
	}
	counts := func(pick meaningPick) bool { return pick.cos >= floor && pick.place <= ceiling }
	merged := make([]fused, 0, len(hits)+len(picks))
	seen := make(map[string]bool, len(hits))
	for at, hit := range hits {
		seen[hit.ID] = true
		value := 1 / (softener + float64(at+1))
		if pick, found := meaningOf[hit.ID]; found {
			hit.Parts.From = append(hit.Parts.From, meaningTag(pick, gap))
			if counts(pick) {
				value = math.Max(value, weight/(softener+float64(pick.place)))
			}
		}
		hit.Score = value
		hit.Parts.Mix = value
		merged = append(merged, fused{hit: hit, value: value, word: at + 1})
	}
	now := time.Now()
	for _, pick := range picks {
		// 도드라짐 관문은 뜻으로만 온 답에만 건다 — 낱말 답의 뜻 몫(위 고리)은 그대로다.
		// gap 0 은 관문 끔이다. 가운데값 아래(도드라짐 음수) 후보도 끄면 옛 판처럼 받는다.
		if seen[pick.row.ID] || !counts(pick) || (gap > 0 && pick.stand < gap) {
			continue
		}
		hit := hitOf(pick.row, now)
		hit.Rung = RungMeaning
		hit.Meaning = true
		hit.Score = weight / (softener + float64(pick.place))
		// 뜻으로만 온 답도 무효·낡음이면 낱말 길과 같은 자(×0.5)로 깎는다 (리뷰 2026-10-05).
		trust := 1.0
		if invalidated(pick.row, now) || pick.row.ObsStale != "" {
			trust = invalidPenalty
		}
		hit.Score *= trust
		hit.Parts = Parts{RRF: hit.Score / trust, Bonus: 1, Decay: 1, Trust: trust, Mix: hit.Score,
			From: []string{meaningTag(pick, gap)}}
		merged = append(merged, fused{hit: hit, value: hit.Score, word: len(hits) + pick.place})
	}
	sort.SliceStable(merged, func(a, b int) bool {
		if merged[a].value != merged[b].value {
			return merged[a].value > merged[b].value
		}
		return merged[a].word < merged[b].word
	})
	return keepPlaces(merged, options.mixKeep())
}

// fused 는 섞는 중인 한 줄이다. word 는 낱말 자리(뜻으로만 온 것은 낱말 끝 뒤).
type fused struct {
	hit   Hit
	value float64
	word  int
}

// keepPlaces 는 낱말 상위 keep 건이 제 자리(word)보다 뒤로 가지 않게 다시 앉힌다.
// 자리를 앞에서부터 채우며, 자리 n 에서 「낱말 자리 ≤ n 인데 아직 못 앉은 지킴
// 후보」가 있으면 그것을 먼저 앉힌다. 나머지는 섞은 차례 그대로다.
func keepPlaces(merged []fused, keep int) []Hit {
	out := make([]Hit, 0, len(merged))
	placed := make([]bool, len(merged))
	next := 0
	for len(out) < len(merged) {
		slot := len(out) + 1
		chosen := -1
		for at, item := range merged {
			if !placed[at] && item.word <= keep && item.word <= slot {
				chosen = at
				break
			}
		}
		if chosen < 0 {
			for next < len(merged) && placed[next] {
				next++
			}
			chosen = next
		}
		placed[chosen] = true
		out = append(out, merged[chosen].hit)
	}
	return out
}

// meaningTag 는 --explain 의 「어느 랭킹 몇 위」 조각이다. 도드라짐 관문이
// 켜졌을 때(gap > 0)만 꼬리에 도드라짐을 붙인다 — 끄면 도입 전과 글자까지 같다.
func meaningTag(pick meaningPick, gap float64) string {
	tail := ""
	if gap > 0 {
		tail = " · 도드라짐 " + strconv.FormatFloat(pick.stand, 'f', 3, 64)
	}
	return "뜻(" + placeText(pick.place) + " · " + strconv.FormatFloat(pick.cos, 'f', 3, 64) + tail + ")"
}
