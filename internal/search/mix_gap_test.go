package search

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/embed"
)

// 뜻 답 전용 관문 mix_gap (2026-10-07 뜻답전용관문설계) — 도드라짐은
// cos − 좁히기 전 Nearest 코사인 가운데값이고, 뜻으로만 온 답에만 건다.

func gapOptions(gap float64) Options {
	options := ruleOptions(0)
	options.Embed.MixGap = gap
	return options
}

// has 는 목록에 있는지다 (indexOf 는 없으면 len 을 준다).
func has(list []string, want string) bool { return indexOf(list, want) < len(list) }

func standPick(id string, place int, cos, stand float64) meaningPick {
	got := pick(id, place, cos)
	got.stand = stand
	return got
}

// shape 는 비교용 한 줄이다 — hitOf 의 시각 칸을 빼고 차례·몫·explain 조각만 본다.
type shape struct {
	ID    string
	Score float64
	From  []string
	Rung  int
}

func shapes(hits []Hit) []shape {
	out := make([]shape, 0, len(hits))
	for _, hit := range hits {
		out = append(out, shape{ID: hit.ID, Score: hit.Score, From: hit.Parts.From, Rung: int(hit.Rung)})
	}
	return out
}

// ① mix_gap = 0 은 관문 끔이다 — 도드라짐이 음수(가운데값 아래)인 뜻 후보도
// 옛 판처럼 받고, explain 조각에 도드라짐이 안 붙는다. 도드라짐 칸이 없던 옛 입력과 같다.
func TestMixGapZeroSameAsBefore(t *testing.T) {
	hits := wordHits("w1", "w2", "w3")
	withStand := []meaningPick{standPick("m1", 1, 0.9, -0.05), standPick("w2", 2, 0.8, 0.01), standPick("m3", 3, 0.7, 0.2)}
	old := []meaningPick{pick("m1", 1, 0.9), pick("w2", 2, 0.8), pick("m3", 3, 0.7)}
	got := shapes(fuse(hits, withStand, gapOptions(0)))
	want := shapes(fuse(hits, old, ruleOptions(0)))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mix_gap 0 인데 달라졌다 :\n%v\n%v", got, want)
	}
	for _, line := range got {
		for _, from := range line.From {
			if strings.Contains(from, "도드라짐") {
				t.Fatalf("관문이 꺼졌는데 도드라짐이 붙었다 : %q", from)
			}
		}
	}
}

// ② 도드라짐이 문턱 미만인 뜻 후보는 뜻 답에서 빠지고, 이상이면 남는다.
func TestMixGapGatesMeaningOnly(t *testing.T) {
	hits := wordHits("w1", "w2")
	picks := []meaningPick{standPick("flat", 1, 0.62, 0.03), standPick("sharp", 2, 0.70, 0.09), standPick("edge", 3, 0.66, 0.06)}
	got := fuse(hits, picks, gapOptions(0.06))
	ids := order(got)
	if has(ids, "flat") {
		t.Fatalf("고르게 비슷한 뜻 후보가 들어왔다 : %v", ids)
	}
	if !has(ids, "sharp") || !has(ids, "edge") {
		t.Fatalf("도드라진 뜻 후보(문턱과 같은 것 포함)가 빠졌다 : %v", ids)
	}
	for _, hit := range got {
		if hit.ID == "sharp" && (len(hit.Parts.From) != 1 || hit.Parts.From[0] != "뜻(2 · 0.700 · 도드라짐 0.090)") {
			t.Fatalf("explain 조각이 틀렸다 : %v", hit.Parts.From)
		}
	}
}

// ③ 낱말로도 걸린 답은 mix_gap 과 상관없이 몫·차례가 그대로다 (조각 꼬리만 붙는다).
func TestMixGapLeavesWordHits(t *testing.T) {
	hits := wordHits("w1", "w2", "w3", "w4")
	picks := []meaningPick{standPick("w4", 1, 0.9, -0.1), standPick("w3", 2, 0.9, 0)}
	open := fuse(hits, picks, gapOptions(0))
	shut := fuse(hits, picks, gapOptions(0.5))
	if !reflect.DeepEqual(order(open), order(shut)) {
		t.Fatalf("낱말 답 차례가 바뀌었다 : %v → %v", order(open), order(shut))
	}
	for at := range open {
		if open[at].Score != shut[at].Score {
			t.Fatalf("%s 몫이 바뀌었다 : %v → %v", open[at].ID, open[at].Score, shut[at].Score)
		}
	}
}

// ④ 가운데값은 있는 것만으로 잰다 — 0개 0 · 1개 그 값 · 짝수는 가운데 둘의 평균.
func TestMiddleCos(t *testing.T) {
	near := func(values ...float64) []embed.Near {
		out := make([]embed.Near, 0, len(values))
		for _, value := range values {
			out = append(out, embed.Near{Cos: value})
		}
		return out
	}
	cases := []struct {
		in   []embed.Near
		want float64
	}{
		{nil, 0},
		{near(0.7), 0.7},
		{near(0.6, 0.8), 0.7},
		{near(0.9, 0.5, 0.6), 0.6},
		{near(0.9, 0.5, 0.6, 0.55), 0.575},
	}
	for _, item := range cases {
		if got := middleCos(item.in); got < item.want-1e-9 || got > item.want+1e-9 {
			t.Fatalf("%v 의 가운데값이 %v, 기대 %v", item.in, got, item.want)
		}
	}
	// 1개뿐이면 도드라짐 0 이라 mix_gap > 0 이면 뜻 답이 못 된다 (모르면 답하지 않는다).
	one := []meaningPick{standPick("only", 1, 0.8, 0)}
	if ids := order(fuse(nil, one, gapOptions(0.04))); len(ids) != 0 {
		t.Fatalf("후보 1개인 판에서 뜻 답이 나왔다 : %v", ids)
	}
}

// ⑤ 이상값 : 음수는 0(끔)으로 본다. 1 을 넘으면 그대로 써서 뜻 답이 다 막힌다
// (config.Problems 가 알린다).
func TestMixGapOddValues(t *testing.T) {
	negative := gapOptions(-0.3)
	if got := negative.mixGap(); got != 0 {
		t.Fatalf("음수 mix_gap 이 %v 로 읽혔다", got)
	}
	picks := []meaningPick{standPick("m1", 1, 0.9, 0.3)}
	if ids := order(fuse(nil, picks, gapOptions(1.5))); len(ids) != 0 {
		t.Fatalf("mix_gap 1.5 인데 뜻 답이 나왔다 : %v", ids)
	}
}

// ⑥ 뒷문 MEM_MIX_GAP 이 mem.toml 값을 덮는다 (0 도 「끔」으로 덮는다).
func TestMixGapEnvOverrides(t *testing.T) {
	saved := mixGapE
	defer func() { mixGapE = saved }()
	options := gapOptions(0.08)
	mixGapE = 0.2
	if got := options.mixGap(); got != 0.2 {
		t.Fatalf("뒷문이 안 먹었다 : %v", got)
	}
	mixGapE = 0
	if got := options.mixGap(); got != 0 {
		t.Fatalf("뒷문 0 이 안 먹었다 : %v", got)
	}
	mixGapE = -1
	if got := options.mixGap(); got != 0.08 {
		t.Fatalf("뒷문이 없는데 설정을 안 썼다 : %v", got)
	}
}
