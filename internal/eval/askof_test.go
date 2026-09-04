package eval

import (
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
)

// 자와 검색이 같은 손잡이로 돌아야 한다. askOf 가 mem.toml [search] 를 안
// 넘기면 eval 은 코드 기본값으로 돌고 `mem search` 는 파일 값으로 돌아서,
// 잰 순위가 실제 검색 순위와 달라진다 (갈래 A 가 넘긴 결함).
func TestAskCarriesSearchConfig(t *testing.T) {
	want := config.SearchConfig{
		FieldWeights: [4]float64{1, 1, 1, 100},
		K1:           9, B: 0.9, RerankTop: 7, SynWeight: 0.9,
		RRFK: 55, BonusCap: 2.5, AbstainFloor: 0.4,
	}
	options := Options{Search: want, RRFK: want.RRFK, BonusCap: want.BonusCap}
	ask := askOf(options, Case{Q: "훅 예산", Kind: KindExtract})
	if ask.Search != want {
		t.Fatalf("askOf 가 [search] 를 안 넘겼다 : %+v", ask.Search)
	}
	raw := rawOf(options, Case{Q: "훅 예산", Kind: KindExtract})
	if raw.Search != want {
		t.Fatalf("rawOf 가 [search] 를 안 넘겼다 : %+v", raw.Search)
	}
}
