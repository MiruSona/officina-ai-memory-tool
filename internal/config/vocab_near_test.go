package config

import (
	"strings"
	"testing"
)

func nearVocab() Vocab {
	return Vocab{Tags: map[string][]string{
		"tilemap": {"tileset"},
		"index":   {"fts5"},
		"korean":  {},
		"search":  {},
		"build":   {},
	}}
}

// 앞머리가 겹치면 가장 가깝다. `tile` → `tilemap` · `tileset`.
func TestNearTagsSharesHeadFirst(t *testing.T) {
	got := nearVocab().NearTags("tile", 3)
	if len(got) < 2 || got[0] != "tilemap" || got[1] != "tileset" {
		t.Fatalf("앞머리 후보가 먼저 나와야 한다 : %v", got)
	}
}

// 한 글자 오타는 후보다. 짧은 태그(5자 미만)는 거리 1까지만 본다.
func TestNearTagsEditDistance(t *testing.T) {
	if got := nearVocab().NearTags("koreen", 3); len(got) == 0 || got[0] != "korean" {
		t.Fatalf("오타 후보를 못 찾는다 : %v", got)
	}
	// `bxxld` 는 build 와 거리 2 이고 짧은 쪽이 5자라 후보로 들어온다.
	if got := nearVocab().NearTags("bxxld", 3); len(got) == 0 {
		t.Fatalf("거리 2 후보를 놓친다 : %v", got)
	}
	// `abc` 는 아무것과도 안 닮았다.
	if got := nearVocab().NearTags("abc", 3); len(got) != 0 {
		t.Fatalf("안 닮은 것을 후보로 올린다 : %v", got)
	}
}

// 짧은 쪽이 3자 미만이면 건너뛴다. limit 도 지킨다.
func TestNearTagsSkipsShortAndObeysLimit(t *testing.T) {
	if got := nearVocab().NearTags("ab", 3); len(got) != 0 {
		t.Fatalf("두 자짜리는 후보를 안 내야 한다 : %v", got)
	}
	if got := nearVocab().NearTags("tile", 1); len(got) != 1 {
		t.Fatalf("limit 를 안 지킨다 : %v", got)
	}
}

func TestEditDistanceAndSharesHead(t *testing.T) {
	if EditDistance("abc", "abd") != 1 {
		t.Fatal("편집 거리가 1 이어야 한다")
	}
	if !SharesHead("aimemory", "aimemorytool") || SharesHead("abc", "xyz") {
		t.Fatal("앞머리 판정이 틀렸다")
	}
}

// 가나다순 꼬리 정렬 — 점수·길이 차가 같으면 이름이 앞선 것이 먼저다.
func TestNearTagsSortsByName(t *testing.T) {
	got := nearVocab().NearTags("tile", 2)
	if strings.Join(got, ",") != "tilemap,tileset" {
		t.Fatalf("같은 점수는 가나다순이어야 한다 : %v", got)
	}
}
