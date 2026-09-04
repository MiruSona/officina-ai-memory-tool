package quality

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/simhash"
)

// TestTuneDuplicate 는 중복 문턱 곡선을 찍는다. `mem eval --tune-dup` 의 바탕이다.
// 자를 만드는 시험이라 실패시키지 않고 표만 남긴다 — -v 로 본다.
func TestTuneDuplicate(t *testing.T) {
	set, memories := loadForTune(t)
	settings := config.Default("tune")
	points := TuneDuplicate(set, memories, RepoOptions{Options: Options{
		Config: settings, Vocab: testdataVocab(t), Now: testNow()}})
	t.Log("\n" + TuneTable(points))
	reject, warn := Recommend(points)
	if reject <= 0 || warn <= 0 {
		t.Fatalf("권장 문턱을 못 골랐다 : %.2f / %.2f", reject, warn)
	}
	if warn > reject {
		t.Errorf("경고선이 거절선보다 높다 : %.2f > %.2f", warn, reject)
	}
	// 기본값이 이 곡선에서 나온 값이라 둘이 같다. 부동소수 오차를 감안해 견준다.
	if reject > settings.Quality.DupReject+0.001 {
		t.Errorf("권장 거절선이 설정값보다 높다 : %.2f > %.2f", reject, settings.Quality.DupReject)
	}
}

// TestTuneSimhash 는 해밍 문턱 3·4·6·8 이 DUP 36건을 후보로 잡는지 견준다.
// simhash 는 판정자가 아니라 후보 좁히기라 재현율만 본다 (설계 3-2).
func TestTuneSimhash(t *testing.T) {
	set, memories := loadForTune(t)
	docs := make([]*Doc, 0, len(memories))
	for _, m := range memories {
		docs = append(docs, NewDoc(m))
	}
	dup := map[string]bool{}
	for _, one := range set.Flag {
		for _, defect := range one.Defects {
			if defect == TypeDup {
				dup[one.ID] = true
			}
		}
	}
	// 정답 짝 : 둘 다 DUP 로 판정된 기억 중 닮음 점수가 실측 상위(0.15 이상)인 짝.
	// 설계의 dup_warn 0.55 를 쓰면 이 저장소에서는 짝이 하나도 안 나온다 —
	// 그 문턱이 이 자에 안 맞는다는 것이 TestTuneDuplicate 의 결론이다.
	pairs := [][2]int{}
	for left := range docs {
		for right := left + 1; right < len(docs); right++ {
			if dup[docs[left].ID] && dup[docs[right].ID] && Score(docs[left], docs[right]) >= 0.15 {
				pairs = append(pairs, [2]int{left, right})
			}
		}
	}
	out := "| 해밍 문턱 | 정답 짝 잡음 | 재현율 | 문서 하나가 볼 후보 평균 |\n| --- | --- | --- | --- |\n"
	for _, hamming := range []int{3, 4, 6, 8, 12, 16} {
		caught := 0
		for _, pair := range pairs {
			if simhash.Distance(docs[pair[0]].Fingerprint, docs[pair[1]].Fingerprint) <= hamming {
				caught++
			}
		}
		total := 0
		for left := range docs {
			for right := range docs {
				if left != right && simhash.Distance(docs[left].Fingerprint, docs[right].Fingerprint) <= hamming {
					total++
				}
			}
		}
		recall := 0.0
		if len(pairs) > 0 {
			recall = float64(caught) / float64(len(pairs))
		}
		out += fmt.Sprintf("| %d | %d | %.3f | %.1f |\n", hamming, caught, recall, float64(total)/float64(len(docs)))
	}
	// 실제로 쓰는 후보 좁히기(밴딩 + 태그·scope 겹침 + 작은 저장소 전수 지문)가
	// 정답 짝을 다 물어 오는지. 이것이 판정자에게 갈 후보의 진짜 재현율이다.
	table := NewSimilar(8)
	for _, doc := range docs {
		table.Add(doc)
	}
	seen, load := 0, 0
	for _, doc := range docs {
		load += len(table.Candidates(doc))
	}
	for _, pair := range pairs {
		for _, one := range table.Candidates(docs[pair[0]]) {
			if one.ID == docs[pair[1]].ID {
				seen++
				break
			}
		}
	}
	out += fmt.Sprintf("\n실제 후보 좁히기(밴딩+태그·scope) : 정답 짝 %d/%d (%.3f) · 문서당 후보 평균 %.1f\n",
		seen, len(pairs), float64(seen)/float64(len(pairs)), float64(load)/float64(len(docs)))

	t.Logf("정답 짝 %d개\n%s", len(pairs), out)
}

func loadForTune(t *testing.T) (*GoldenSet, []*model.Memory) {
	t.Helper()
	set, err := LoadGolden(filepath.FromSlash(goldenPath))
	if err != nil {
		t.Fatalf("골든셋을 못 읽었다 : %v", err)
	}
	memories, err := LoadMemories(filepath.FromSlash(storePath))
	if err != nil {
		t.Fatalf("저장소를 못 읽었다 : %v", err)
	}
	return set, memories
}
