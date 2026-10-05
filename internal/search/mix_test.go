package search

import (
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/embed"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
)

// plainVectors 는 Nearest 가 없는 Vectors 다 — 옛 재정렬만 하는 꽂개.
// 문서 벡터를 안 줘서 옛 재정렬도 아무 일을 안 한다.
type plainVectors struct{}

func (plainVectors) Query(string) []float32           { return []float32{1} }
func (plainVectors) Docs([]int64) map[int64][]float32 { return nil }

// scanVectors 는 뜻 후보를 정해 둔 대로 돌려주는 꽂개다.
type scanVectors struct {
	plainVectors
	near []embed.Near
}

func (s scanVectors) Nearest([]float32, int) []embed.Near { return s.near }

func mixSettings(on bool) config.EmbedConfig {
	return config.EmbedConfig{Mix: on, MixTop: 50, MixWeight: 1, MixK: 60, MixFloor: 0.8, MixRank: 3}
}

func askMix(t *testing.T, database *index.DB, query string, vectors Vectors, settings config.EmbedConfig) *Result {
	t.Helper()
	result, err := Search(Options{
		Sources: []Source{{DB: database}}, Query: query, Limit: 10,
		Stopwords: config.DefaultStopwords(),
		Synonym:   map[string][]string{"기억": {"memory"}, "훅": {"hook"}, "색인": {"index"}},
		Embed:     settings, Vectors: vectors,
	})
	if err != nil {
		t.Fatalf("검색이 죽었다 : %v", err)
	}
	return result
}

// 낱말이 하나도 안 겹치는 질의라도 뜻이 가까우면 `[뜻]` 칸 strict 답으로 나온다.
func TestMixBringsMeaningOnly(t *testing.T) {
	database, ids := newRepo(t)
	query := "물리엔진 중력"
	near := []embed.Near{{ID: ids[5], Cos: 0.91}, {ID: ids[2], Cos: 0.70}}
	off := askMix(t, database, query, scanVectors{near: near}, mixSettings(false))
	if len(off.Hits) != 0 {
		t.Fatalf("끈 판에 답이 났다 : %v", idsOf(off))
	}
	on := askMix(t, database, query, scanVectors{near: near}, mixSettings(true))
	if len(on.Hits) != 1 || on.Hits[0].ID != ids[5] {
		t.Fatalf("뜻 1위만 나와야 한다(2위는 바닥 아래) : %v", idsOf(on))
	}
	hit := on.Hits[0]
	if hit.Rung != RungMeaning || !hit.Meaning || !Strict(hit.Rung) {
		t.Fatalf("뜻 칸 표시가 틀렸다 : 칸 %d · meaning %v", hit.Rung, hit.Meaning)
	}
	if LabelOf(hit.Rung) != "[뜻]" {
		t.Fatalf("꼬리표가 %q 다", LabelOf(hit.Rung))
	}
	if on.Mode != ModeMeaning {
		t.Fatalf("모드가 %q 다", on.Mode)
	}
	if on.Relaxed {
		t.Fatal("뜻 칸만 났는데 넓혔다고 한다")
	}
}

// 리뷰 2026-10-05 나-10 — 뜻으로만 온 답도 덮였으면 신뢰 ×0.5 를 받고, 섞기 순위
// 몫이 parts.mix 와 --explain 한 줄로 따로 보인다.
func TestMixMeaningOnlyTrustAndExplain(t *testing.T) {
	alive := index.SearchRow{ID: "20261005-aaaa0001", Type: "decision"}
	dead := index.SearchRow{ID: "20261005-aaaa0002", Type: "decision", SupersededBy: "20261005-aaaa0003"}
	picks := []meaningPick{{row: alive, place: 1, cos: 0.95}, {row: dead, place: 2, cos: 0.94}}
	options := Options{Embed: mixSettings(true)}
	out := fuse(nil, picks, options)
	if len(out) != 2 {
		t.Fatalf("둘 다 나와야 한다 : %+v", out)
	}
	byID := map[string]Hit{}
	for _, hit := range out {
		byID[hit.ID] = hit
	}
	if byID[alive.ID].Parts.Trust != 1 || byID[dead.ID].Parts.Trust != invalidPenalty {
		t.Fatalf("신뢰 깎기가 틀렸다 : %+v / %+v", byID[alive.ID].Parts, byID[dead.ID].Parts)
	}
	if byID[dead.ID].Score != byID[dead.ID].Parts.RRF*invalidPenalty || byID[dead.ID].Parts.Mix != byID[dead.ID].Score {
		t.Fatalf("score·parts 가 안 맞는다 : %v %+v", byID[dead.ID].Score, byID[dead.ID].Parts)
	}
	if text := Explain(&Result{Hits: out}); !strings.Contains(text, "섞기 순위 몫") {
		t.Fatalf("--explain 에 섞기 몫 줄이 없다 :\n%s", text)
	}
}

// 바닥(코사인·뜻 순위)을 못 넘으면 아예 안 낸다.
func TestMixFloorAndRank(t *testing.T) {
	database, ids := newRepo(t)
	query := "물리엔진 중력"
	low := []embed.Near{{ID: ids[5], Cos: 0.79}}
	if result := askMix(t, database, query, scanVectors{near: low}, mixSettings(true)); len(result.Hits) != 0 {
		t.Fatalf("코사인 바닥 아래가 나왔다 : %v", idsOf(result))
	}
	deep := []embed.Near{{ID: ids[0], Cos: 0.95}, {ID: ids[1], Cos: 0.94},
		{ID: ids[2], Cos: 0.93}, {ID: ids[3], Cos: 0.92}}
	result := askMix(t, database, query, scanVectors{near: deep}, mixSettings(true))
	if len(result.Hits) != 3 || hasID(result, ids[3]) {
		t.Fatalf("뜻 4위는 mix_rank 3 밖이라 빠져야 한다 : %v", idsOf(result))
	}
}

// 꺼졌거나 Nearest 가 없는 꽂개면 C2 전과 건마다 같다.
func TestMixOffIsIdentical(t *testing.T) {
	database, ids := newRepo(t)
	near := []embed.Near{{ID: ids[0], Cos: 0.99}, {ID: ids[6], Cos: 0.98}}
	for _, query := range []string{"성능 색인", "훅 예산", "결정 충돌", "물리엔진 중력"} {
		base := askMix(t, database, query, nil, mixSettings(false))
		sameHits(t, query, base, askMix(t, database, query, scanVectors{near: near}, mixSettings(false)))
		sameHits(t, query, base, askMix(t, database, query, plainVectors{}, mixSettings(true)))
		sameHits(t, query, base, askMix(t, database, query, nil, mixSettings(true)))
	}
}

// 낱말 답은 칸을 안 바꾸고 빠지지도 않는다 — 섞기는 차례만 바꾼다.
func TestMixKeepsWordHits(t *testing.T) {
	database, ids := newRepo(t)
	query := "성능 색인"
	base := askMix(t, database, query, nil, mixSettings(false))
	near := []embed.Near{{ID: ids[0], Cos: 0.95}, {ID: base.Hits[len(base.Hits)-1].ID, Cos: 0.9}}
	on := askMix(t, database, query, scanVectors{near: near}, mixSettings(true))
	rungOf := map[string]int{}
	for _, hit := range on.Hits {
		rungOf[hit.ID] = hit.Rung
	}
	for _, hit := range base.Hits {
		rung, found := rungOf[hit.ID]
		if !found || rung != hit.Rung {
			t.Fatalf("낱말 답 %s 가 빠졌거나 칸이 %d → %d 로 바뀌었다", hit.ID, hit.Rung, rung)
		}
	}
	if !hasID(on, ids[0]) || rankOfID(on, ids[0]) == 0 {
		t.Fatalf("뜻 1위가 안 들어왔다 : %v", idsOf(on))
	}
	if last := base.Hits[len(base.Hits)-1].ID; rankOfID(on, last) >= rankOfID(base, last) {
		t.Fatalf("뜻이 가까운 낱말 답이 안 올라갔다 : %d → %d", rankOfID(base, last), rankOfID(on, last))
	}
}

// 좁히기(--type)를 지나지 못한 뜻 후보는 안 들어온다.
func TestMixHonorsFilter(t *testing.T) {
	database, ids := newRepo(t)
	// ids[4] 는 history, ids[5] 는 decision 이다.
	near := []embed.Near{{ID: ids[4], Cos: 0.95}, {ID: ids[5], Cos: 0.94}}
	result, err := Search(Options{
		Sources: []Source{{DB: database}}, Query: "물리엔진 중력", Limit: 10,
		Stopwords: config.DefaultStopwords(), Embed: mixSettings(true),
		Vectors: scanVectors{near: near}, Filter: index.Filter{Types: []string{"decision"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hasID(result, ids[4]) || !hasID(result, ids[5]) {
		t.Fatalf("좁히기가 안 먹었다 : %v", idsOf(result))
	}
}
