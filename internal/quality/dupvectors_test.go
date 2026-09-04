package quality

import "testing"

// fakeNear 는 「무엇을 물어도 서로가 서로의 첫째」라고 답하는 벡터다. 스위치가
// 꺼져 있으면 이것이 꽂혀도 표가 안 받아야 한다 (설계 18-7).
type fakeNear struct{}

func (fakeNear) Near(string, int) []string  { return []string{"other"} }
func (fakeNear) Cos(string, string) float64 { return 1 }

// TestDupVectorsOffByDefault 는 중복 판정의 벡터 항이 **기본으로 꺼져 있고**
// 후보원으로도 안 쓰인다는 것을 지킨다 (설계 18-7 · R1·R2).
func TestDupVectorsOffByDefault(t *testing.T) {
	opt := testOptions()
	if opt.Config.Quality.DupVectors {
		t.Fatal("dup_vectors 기본값이 켜져 있다")
	}
	opt.Near = fakeNear{}
	if table := newSimilarFor(opt); table.vectors != nil {
		t.Fatal("스위치가 꺼졌는데 벡터가 꽂혔다")
	}
}

// TestDupVectorsOnUsesNear 는 켜면 전처럼 벡터를 쓴다는 것이다.
func TestDupVectorsOnUsesNear(t *testing.T) {
	opt := testOptions()
	opt.Config.Quality.DupVectors = true
	opt.Near = fakeNear{}
	if table := newSimilarFor(opt); table.vectors == nil {
		t.Fatal("스위치를 켰는데 벡터가 안 꽂혔다")
	}
}
