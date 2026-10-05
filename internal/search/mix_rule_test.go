package search

import (
	"reflect"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
)

// 2차 판 섞기 규칙 (뜻후보섞기측정 7절) — 몫은 더하지 않고 큰 쪽 하나다.

func ruleOptions(keep int) Options {
	return Options{Embed: config.EmbedConfig{Mix: true, MixTop: 20, MixWeight: 1, MixK: 60,
		MixFloor: 0.5, MixRank: 3, MixKeep: keep}}
}

func wordHits(ids ...string) []Hit {
	out := make([]Hit, 0, len(ids))
	for _, id := range ids {
		out = append(out, Hit{ID: id, Rung: 1})
	}
	return out
}

func pick(id string, place int, cos float64) meaningPick {
	return meaningPick{row: index.SearchRow{ID: id}, place: place, cos: cos}
}

func order(hits []Hit) []string {
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		out = append(out, hit.ID)
	}
	return out
}

// 1차 판을 무너뜨린 꼴 : 낱말 1위는 뜻 목록에 없고, 그 아래 후보들이 뜻 목록에도
// 들어 두 몫을 받던 경우. 큰 쪽 하나면 낱말 1위는 1위 그대로다.
func TestMixRuleKeepsWordTop(t *testing.T) {
	hits := wordHits("w1", "w2", "w3", "w4", "w5")
	picks := []meaningPick{pick("w5", 1, 0.9), pick("w4", 2, 0.9), pick("w3", 3, 0.9)}
	got := order(fuse(hits, picks, ruleOptions(0)))
	if got[0] != "w1" {
		t.Fatalf("낱말 1위가 밀렸다 : %v", got)
	}
	// 낱말 2위 앞에는 뜻 1위만 끼어든다 → 3위 안.
	if rank := indexOf(got, "w2") + 1; rank > 3 {
		t.Fatalf("낱말 2위가 %d위로 밀렸다 : %v", rank, got)
	}
}

// 낱말 j 위는 섞은 뒤 2j-1 위 안에 남는다 (뜻으로만 온 후보가 앞자리를 다 채워도).
func TestMixRuleBoundsWordPlaces(t *testing.T) {
	hits := wordHits("w1", "w2", "w3", "w4")
	picks := []meaningPick{pick("m1", 1, 0.9), pick("m2", 2, 0.9), pick("m3", 3, 0.9)}
	got := order(fuse(hits, picks, ruleOptions(0)))
	want := []string{"w1", "m1", "w2", "m2", "w3", "m3", "w4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("차례가 %v, 기대 %v", got, want)
	}
}

// 뜻으로만 온 답은 낱말 p 위 바로 뒤로 끼어들고 `[뜻]` 칸에 앉는다.
// 바닥(코사인 · 뜻 순위)을 못 넘으면 아무 몫도 없다.
func TestMixRuleInsertsMeaningOnly(t *testing.T) {
	hits := wordHits("w1", "w2", "w3")
	picks := []meaningPick{pick("m1", 1, 0.9), pick("low", 2, 0.4), pick("w3", 3, 0.9), pick("deep", 4, 0.9)}
	fusedHits := fuse(hits, picks, ruleOptions(0))
	got := order(fusedHits)
	want := []string{"w1", "m1", "w2", "w3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("차례가 %v, 기대 %v", got, want)
	}
	if hit := fusedHits[1]; hit.Rung != RungMeaning || !hit.Meaning {
		t.Fatalf("뜻으로만 온 답의 칸이 %d · meaning %v", hit.Rung, hit.Meaning)
	}
	for _, hit := range fusedHits {
		if hit.ID != "m1" && (hit.Rung != 1 || hit.Meaning) {
			t.Fatalf("낱말 답 %s 의 칸이 바뀌었다", hit.ID)
		}
	}
}

// 낱말 깊은 곳(뜻 1위)에 걸린 답은 뜻 몫으로 올라온다 — 낱말 후보라서 막히지 않는다.
func TestMixRuleLiftsDeepWordHit(t *testing.T) {
	hits := wordHits("w1", "w2", "w3", "w4", "w5", "w6", "w7", "w8")
	picks := []meaningPick{pick("w8", 1, 0.9)}
	got := order(fuse(hits, picks, ruleOptions(0)))
	if got[1] != "w8" {
		t.Fatalf("뜻 1위인 낱말 8위가 2위로 안 올라왔다 : %v", got)
	}
}

// 자리 지킴 : mix_keep 안의 낱말 답은 제 자리보다 뒤로 안 간다.
func TestMixRuleKeepPlaces(t *testing.T) {
	hits := wordHits("w1", "w2", "w3", "w4")
	picks := []meaningPick{pick("m1", 1, 0.9), pick("m2", 2, 0.9)}
	options := ruleOptions(2)
	options.Embed.MixWeight = 2 // 뜻 몫이 낱말 1위보다 커도
	got := order(fuse(hits, picks, options))
	if got[0] != "w1" || got[1] != "w2" {
		t.Fatalf("지킴 2건이 제 자리를 못 지켰다 : %v", got)
	}
	if indexOf(got, "m1") > 3 {
		t.Fatalf("뜻 답이 빠졌다 : %v", got)
	}
	// 지킴 0 이면 무게 2 의 뜻 답이 낱말 1위 앞에 선다.
	if free := order(fuse(hits, picks, func() Options { o := ruleOptions(0); o.Embed.MixWeight = 2; return o }())); free[0] != "m1" {
		t.Fatalf("지킴 없이도 낱말 1위가 앞이다 : %v", free)
	}
}

// 뜻 후보가 0건이면 낱말 차례 그대로다.
func TestMixRuleNoPicksIsIdentity(t *testing.T) {
	hits := wordHits("w1", "w2", "w3")
	if got := order(fuse(hits, nil, ruleOptions(3))); !reflect.DeepEqual(got, []string{"w1", "w2", "w3"}) {
		t.Fatalf("차례가 바뀌었다 : %v", got)
	}
}

func indexOf(list []string, want string) int {
	for at, item := range list {
		if item == want {
			return at
		}
	}
	return len(list)
}
