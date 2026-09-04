package gc

import (
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/token"
)

// 접기 뒤에도 fts_norm 이 살아 있어야 한다 (결정 58).
//
// v0.1 은 contentless 표를 UPDATE 하려다 warm 접기가 100% 실패했다. v0.3 은
// 표가 셋(fts_ko·fts_en·fts_norm)이 되어, 접기가 손으로 둘만 다시 넣으면
// fts_norm 만 옛 본문을 답하는 자리가 새로 생긴다. 그래서 색인이 쓰는
// index.RewriteFTS 를 같이 쓴다.
func TestFoldKeepsNormTable(t *testing.T) {
	// 대표말 표를 꽂아야 fts_norm 에 행이 선다 — 기본 정규화기는 아무것도 안 한다.
	canon, err := token.NewCanon(map[string]string{"인덱싱": "색인"})
	if err != nil {
		t.Fatal(err)
	}
	old := index.Normalize
	index.SetNormalize(func(text string) string { return token.Normalize(text, canon) })
	t.Cleanup(func() { index.SetNormalize(old) })

	one := oldMemory(idAt(0), 300)
	// 조사가 붙은 낱말이라야 정규화한 글이 원문과 달라져 fts_norm 에 행이 선다.
	one.Summary = "인덱싱을 안 하고 접으면 검색이 옛 본문을 계속 답한다는 것을 알아낸 기억이다"
	opened := newStore(t, one, oldMemory(idAt(1), 300))

	expr := token.NormQueryExpr("색인", canon)
	before, err := index.Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	seen, err := before.MatchNorm(expr, 10)
	before.Close()
	if err != nil || len(seen) == 0 {
		t.Skipf("이 글은 fts_norm 에 행이 안 선다. 시험 자료를 고쳐야 한다 : %v %d", err, len(seen))
	}

	settings := testConfig()
	settings.ColdCount = 1
	settings.ColdDays = 180
	if result := runGC(t, opened, settings, now, false); result.Cooled != 2 {
		t.Fatalf("두 건이 차가움으로 가야 한다 : %+v", result)
	}

	database, err := index.Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	found, err := database.MatchNorm(expr, 10)
	if err != nil {
		t.Fatalf("fts_norm 질의가 죽었다 : %v", err)
	}
	if len(found) == 0 {
		t.Fatal("접은 뒤 fts_norm 이 이 기억을 잃었다")
	}
	// 접혔는지도 같이 확인한다. 안 접혔으면 위 검사가 헛돈다.
	if text := readFile(t, opened, one.ID); !strings.Contains(text, "state: cold") {
		t.Fatal("접히지 않았다. 시험이 헛돌고 있다")
	}
}
