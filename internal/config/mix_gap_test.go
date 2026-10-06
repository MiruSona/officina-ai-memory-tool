package config

import (
	"strings"
	"testing"
)

// [embed] mix_gap 은 없으면 0(관문 끔), 있으면 그 값이고 쓰고 읽어도 같다.
// 0~1 밖이면 Problems 가 알린다 (2026-10-07 뜻답전용관문설계).
func TestMixGapKnob(t *testing.T) {
	plain, err := Parse("")
	if err != nil {
		t.Fatal(err)
	}
	if plain.Embed.MixGap != DefaultEmbedMixGap || Default("시험").Embed.MixGap != DefaultEmbedMixGap {
		t.Fatalf("기본값이 틀렸다 : %v", plain.Embed.MixGap)
	}
	set, err := Parse("[embed]\nmix_gap = 0.07\n")
	if err != nil {
		t.Fatal(err)
	}
	if set.Embed.MixGap != 0.07 {
		t.Fatalf("값을 못 읽었다 : %v", set.Embed.MixGap)
	}
	again, err := Parse(string(Encode(set)))
	if err != nil {
		t.Fatal(err)
	}
	if again.Embed.MixGap != 0.07 {
		t.Fatalf("쓰고 읽으니 달라졌다 : %v", again.Embed.MixGap)
	}
	for _, item := range []struct {
		value float64
		bad   bool
	}{{0, false}, {0.07, false}, {1, false}, {-0.1, true}, {1.5, true}} {
		config := Default("시험")
		config.Embed.MixGap = item.value
		flagged := strings.Contains(strings.Join(config.Problems(), "\n"), "mix_gap")
		if flagged != item.bad {
			t.Fatalf("mix_gap %v : 알림 %v, 기대 %v", item.value, flagged, item.bad)
		}
	}
}
