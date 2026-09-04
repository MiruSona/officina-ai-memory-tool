package search

import (
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
)

// strict 의 경계는 칸 번호 하나가 아니라 목록이다 (설계 결정 29 · 4-1a).
// 0·1·2·5 는 정답으로 세고 3(OR)·4(동의어)·6(LIKE)은 안 센다.
func TestStrictIsAList(t *testing.T) {
	want := map[int]bool{
		RungPlain: true, RungAnd: true, RungDropWord: true, RungSplit: true,
		RungOr: false, RungSynonym: false, RungLike: false,
	}
	for rung, yes := range want {
		if Strict(rung) != yes {
			t.Fatalf("%d번 칸 strict 판정이 %v 다", rung, Strict(rung))
		}
	}
	if Strict(-1) || Strict(rungCount) {
		t.Fatal("칸 밖 번호가 strict 로 샜다")
	}
	// 안 세는 칸은 셋 다 신뢰계수가 0.85 이하다 — 세는 칸이 늘 위에 온다.
	for rung := 0; rung < rungCount; rung++ {
		if Strict(rung) && TrustOf(rung) < TrustOf(RungSplit) {
			t.Fatalf("%d번 칸은 세는데 신뢰계수가 0.60 아래다", rung)
		}
	}
}

// 붙여 쓴 낱말은 5번 칸에서만 잡힌다. 예전 자는 그것을 오답으로 셌다
// (조사E 3절 `바이그램검색`·`기억저장소`).
func TestGluedWordCountsAsFound(t *testing.T) {
	database, ids := newRepo(t)
	result := ask(t, database, "바이그램검색", 5)
	if len(result.Hits) == 0 {
		t.Fatal("붙여 쓴 질의가 0건이다")
	}
	found := false
	for _, hit := range result.Hits {
		if hit.ID == ids[3] && Strict(hit.Rung) {
			found = true
		}
	}
	if !found {
		t.Fatalf("붙여 쓴 낱말의 답이 strict 밖이다 : %+v", result.Rungs)
	}
}

// 2번 칸은 「낱말 하나 뺀 것」 이지 「낱말 하나만 남긴 것」 이 아니다.
// 넉 낱말 중 하나만 남는 질의는 2번 칸에 못 앉는다 — 앉으면 abstain 이
// 무너진다 (설계 4-1a · G2).
func TestDropRungKeepsMajority(t *testing.T) {
	database, _ := newRepo(t)
	result := ask(t, database, "물리엔진 중력 마찰 색인", 10)
	for _, hit := range result.Hits {
		if hit.Rung == RungDropWord {
			t.Fatalf("낱말 하나만 남았는데 2번 칸에 앉았다 : %s", hit.ID)
		}
	}
}

// 낱말이 셋이면 하나를 빼도 과반이라 2번 칸이 산다 — 관문이 사다리를 통째로
// 죽이면 안 된다.
func TestDropRungStillWorksWithThree(t *testing.T) {
	database, _ := newRepo(t)
	query := ParseQuery("공수 리듬 셰이더", config.DefaultStopwords())
	helper := &rungHelper{db: database, synWeight: config.DefaultSynWeight}
	if len(dropRankings(query, helper)) == 0 {
		t.Fatal("낱말 셋짜리 질의에서 2번 칸이 죽었다")
	}
}

// 재순위는 RRF 표를 한 장 더 얹는다 — --explain 에 bm25 줄이 보여야 한다
// (설계 결정 18 · 4-1).
func TestRerankAddsItsOwnRanking(t *testing.T) {
	database, _ := newRepo(t)
	result := ask(t, database, "바이그램 검색", 5)
	if len(result.Hits) == 0 {
		t.Fatal("0건이다")
	}
	seen := false
	for _, hit := range result.Hits {
		for _, from := range hit.Parts.From {
			if strings.HasPrefix(from, "bm25(") {
				seen = true
			}
		}
	}
	if !seen {
		t.Fatalf("재순위 표가 안 얹혔다 : %+v", result.Hits[0].Parts.From)
	}
}

// 우리 bm25 는 값이 클수록 좋고 k1·b 를 우리가 고른다. 같은 낱말이 더 자주
// 나오는 문서가 더 높아야 한다 (설계 4-3).
func TestOurBM25IsHigherIsBetter(t *testing.T) {
	stats := index.CorpusStats{Docs: 100, AvgLen: [4]float64{5, 3, 20, 100}}
	freq := map[string]int{"색인": 5}
	many := index.RerankRow{SummaryKO: "색인 색인 색인", Lens: [4]int{0, 0, 20, 0}}
	few := index.RerankRow{SummaryKO: "색인", Lens: [4]int{0, 0, 20, 0}}
	weights := config.DefaultFieldWeights
	high := index.BM25(many, []string{"색인"}, freq, stats, weights, config.DefaultK1, config.DefaultB)
	low := index.BM25(few, []string{"색인"}, freq, stats, weights, config.DefaultK1, config.DefaultB)
	if !(high > low) {
		t.Fatalf("자주 나온 문서가 더 낮다 : %v 대 %v", high, low)
	}
}

// 동의어 랭킹의 몫은 mem.toml [search] syn_weight 가 정한다. 안 적으면 0.5 다.
func TestSynonymWeightComesFromConfig(t *testing.T) {
	options := Options{}
	if options.synWeight() != config.DefaultSynWeight {
		t.Fatalf("기본 동의어 가중이 %v 다", options.synWeight())
	}
	options.Search.SynWeight = 0.25
	if options.synWeight() != 0.25 {
		t.Fatal("설정한 동의어 가중이 안 먹었다")
	}
	helper := rungHelper{synonym: map[string][]string{"기억": {"memory"}}, synWeight: 0.25}
	ranks := synonymRanks(ParseQuery("기억 저장소", nil).Terms, &helper, false)
	if len(ranks) == 0 {
		t.Fatal("동의어 랭킹이 안 생겼다")
	}
	for _, item := range ranks {
		if item.weight != 0.25 {
			t.Fatalf("동의어 랭킹 가중이 %v 다", item.weight)
		}
	}
}

// 재순위·임베딩 손잡이는 안 주면 설계 기본값이다 (열 넷 · k1 1.2 · b 0.4 · 200).
func TestSearchKnobDefaults(t *testing.T) {
	options := Options{}
	if options.weights() != config.DefaultFieldWeights {
		t.Fatalf("필드 가중 기본값이 %v 다", options.weights())
	}
	if options.k1() != config.DefaultK1 || options.b() != config.DefaultB {
		t.Fatalf("k1·b 기본값이 %v %v 다", options.k1(), options.b())
	}
	if options.rerankTop() != config.DefaultRerankTop {
		t.Fatalf("재순위 건수 기본값이 %d 다", options.rerankTop())
	}
	// 0 이나 음수가 섞인 가중은 통째로 기본값으로 되돌린다.
	options.Search.FieldWeights = [4]float64{16, 6, 0, 1}
	if options.weights() != config.DefaultFieldWeights {
		t.Fatal("0 이 섞인 가중이 그대로 쓰였다")
	}
}

// 임베딩은 이번 판에 안 넣었다. 켜도 낱말 검색이 그대로 돌아야 한다
// (설계 4-8 우아한 퇴화).
func TestEmbedSlotIsQuiet(t *testing.T) {
	database, _ := newRepo(t)
	settings := config.Default("시험")
	settings.Embed.Enabled = true
	result, err := Search(Options{Sources: []Source{{DB: database}}, Query: "바이그램 검색",
		Limit: 5, Stopwords: settings.Stopword.Words, Embed: settings.Embed})
	if err != nil {
		t.Fatalf("임베딩을 켜니 검색이 죽었다 : %v", err)
	}
	if len(result.Hits) == 0 {
		t.Fatal("임베딩을 켜니 0건이 됐다")
	}
}

// G2 — 저장소에 없는 것을 물으면 strict 답이 하나도 없어야 한다. 이 시험이
// 깨지면 「없으면 없다고 한다」 가 깨진 것이다.
func TestAbstainKeepsNoStrictHit(t *testing.T) {
	database, _ := newRepo(t)
	for _, query := range []string{"물리엔진 중력", "결제모듈", "블렌더 리깅", "파티클 이펙트"} {
		result := ask(t, database, query, 10)
		for _, hit := range result.Hits {
			if Strict(hit.Rung) {
				t.Fatalf("%q 에 strict 답이 났다 : %s (칸 %d)", query, hit.ID, hit.Rung)
			}
		}
	}
}

// JSON 칸 이름은 v0.2 것이다 — `author`·`todo_status` (파도 B 스키마).
func TestHitUsesNewFieldNames(t *testing.T) {
	database, _ := newRepo(t)
	result := ask(t, database, "바이그램 검색", 1)
	if len(result.Hits) == 0 {
		t.Fatal("0건이다")
	}
	if result.Hits[0].Author == "" {
		t.Fatal("author 칸이 비었다")
	}
}
