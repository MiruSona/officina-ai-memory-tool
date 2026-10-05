package config

import "testing"

// [embed] mix* 손잡이는 없으면 기본값, 있으면 그 값이다. 쓰고 읽으면 그대로다 (C2).
func TestMixKnobs(t *testing.T) {
	plain, err := Parse("")
	if err != nil {
		t.Fatal(err)
	}
	if plain.Embed.Mix != DefaultEmbedMix || plain.Embed.MixTop != DefaultEmbedMixTop ||
		plain.Embed.MixFloor != DefaultEmbedMixFloor || plain.Embed.MixRank != DefaultEmbedMixRank {
		t.Fatalf("기본값이 틀렸다 : %+v", plain.Embed)
	}
	set, err := Parse("[embed]\nmix = true\nmix_top = 30\nmix_weight = 0.5\nmix_k = 40\nmix_floor = 0.85\nmix_rank = 2\n")
	if err != nil {
		t.Fatal(err)
	}
	got := set.Embed
	if !got.Mix || got.MixTop != 30 || got.MixWeight != 0.5 || got.MixK != 40 || got.MixFloor != 0.85 || got.MixRank != 2 {
		t.Fatalf("값을 못 읽었다 : %+v", got)
	}
	again, err := Parse(string(Encode(set)))
	if err != nil {
		t.Fatal(err)
	}
	if again.Embed.Mix != got.Mix || again.Embed.MixFloor != got.MixFloor || again.Embed.MixRank != got.MixRank ||
		again.Embed.MixTop != got.MixTop || again.Embed.MixWeight != got.MixWeight || again.Embed.MixK != got.MixK {
		t.Fatalf("쓰고 읽으니 달라졌다 : %+v → %+v", got, again.Embed)
	}
}

// 섞기는 기본 끔이다 — 바닥 0.60 으로 조여도 헛답이 늘어서다 (2026-10-05 3차 판 ·
// 사용자 결정). mem.toml 의 mix = true 로 켤 수 있고, 켠 값은 쓰고 읽어도 켜진 채다.
// 다른 손잡이는 그대로 기본값이다 (켰을 때 바닥 0.58 · 뜻 순위 3).
func TestMixDefaultOffAndSwitchOn(t *testing.T) {
	if Default("시험").Embed.Mix {
		t.Fatal("섞기 기본이 켜져 있다")
	}
	on, err := Parse("[embed]\nmix = true\n")
	if err != nil {
		t.Fatal(err)
	}
	if !on.Embed.Mix {
		t.Fatal("mix = true 를 못 읽었다")
	}
	if on.Embed.MixTop != 20 || on.Embed.MixWeight != 1 || on.Embed.MixK != 60 ||
		on.Embed.MixFloor != 0.58 || on.Embed.MixRank != 3 || on.Embed.MixKeep != 0 {
		t.Fatalf("다른 손잡이가 바뀌었다 : %+v", on.Embed)
	}
	again, err := Parse(string(Encode(on)))
	if err != nil {
		t.Fatal(err)
	}
	if !again.Embed.Mix {
		t.Fatal("켠 값이 쓰고 읽으니 꺼졌다")
	}
}
