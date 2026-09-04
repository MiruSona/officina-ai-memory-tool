package embed

// 모델이 실제로 깔린 기계에서만 도는 시험이다. 없으면 건너뛴다 —
// 시험이 113MB 파일을 요구하면 아무 데서도 못 돈다.

import (
	"os"
	"path/filepath"
	"testing"
)

func liveModel(t *testing.T) *Model {
	t.Helper()
	if !Ready(DefaultModel) {
		t.Skip("의미 검색 모델이 안 깔려 있다")
	}
	made, err := openModel(DefaultModel)
	if err != nil {
		t.Skipf("모델을 못 열었다 : %v", err)
	}
	t.Cleanup(made.Close)
	return made
}

// e5 규약대로 뜻이 갈리는지 본다 (0실측 눈검사와 같은 짝).
func TestLiveMeaningSeparates(t *testing.T) {
	made := liveModel(t)
	first := made.Query("기억 저장소 색인")
	second := made.Query("메모리 인덱싱")
	third := made.Query("오늘 점심 메뉴")
	if first == nil || second == nil || third == nil {
		t.Fatal("벡터가 안 났다")
	}
	if len(first) != Dim {
		t.Fatalf("차원이 %d 다", len(first))
	}
	near, far := Cos(first, second), Cos(first, third)
	if near <= far {
		t.Fatalf("뜻이 안 갈렸다 : 가까운 것 %.4f · 먼 것 %.4f", near, far)
	}
}

// 토크나이저 캐시가 있으나 없으나 **같은 벡터**가 나와야 한다. 안 그러면
// 캐시가 조용히 품질을 바꾼다 (결정 10).
func TestLiveTokenizerCacheKeepsResult(t *testing.T) {
	if !Ready(DefaultModel) {
		t.Skip("의미 검색 모델이 안 깔려 있다")
	}
	cache := filepath.Join(ModelDir(DefaultModel), CacheFile)
	os.Remove(cache)
	cold, err := openModel(DefaultModel)
	if err != nil {
		t.Skipf("모델을 못 열었다 : %v", err)
	}
	first := cold.Query("기억 저장소 색인")
	cold.Close()
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("캐시를 안 구웠다 : %v", err)
	}
	warm, err := openModel(DefaultModel)
	if err != nil {
		t.Fatalf("캐시로 못 열었다 : %v", err)
	}
	defer warm.Close()
	second := warm.Query("기억 저장소 색인")
	if len(first) != len(second) {
		t.Fatalf("길이가 다르다 : %d · %d", len(first), len(second))
	}
	for at := range first {
		if first[at] != second[at] {
			t.Fatalf("%d번째 값이 다르다 : %v · %v", at, first[at], second[at])
		}
	}
}

// 배치로 넣은 것과 한 건씩 넣은 것이 거의 같아야 한다.
//
// **똑같지는 않다.** 길이가 같은 것끼리 넣으면 코사인 1.00000 인데, 짧은 것과
// 긴 것을 섞으면 0.979 다 — optimum 이 뽑은 ko-v2 그래프가 패딩 자리를 완전히
// 0 으로 만들지 못한다(입력이 input_ids·attention_mask 둘뿐이다). 그래서
// `Passages` 가 **길이 차례로 정렬해서** 패딩을 최소로 만든다. 잰 값은 0.962 라
// 자를 0.95 로 둔다. 진짜로 지켜야 할 것은 그 다음 줄이다 — **짝이 안 바뀐다.**
func TestLiveBatchMatchesSingle(t *testing.T) {
	made := liveModel(t)
	texts := []string{"짧은 글", "조금 더 긴 글이다. 기억 저장소는 SQLite 로 낱말 검색을 한다."}
	batch, err := made.Passages(texts)
	if err != nil {
		t.Fatal(err)
	}
	alone := make([][]float32, len(texts))
	for at, text := range texts {
		one, err := made.Passages([]string{text})
		if err != nil {
			t.Fatal(err)
		}
		alone[at] = one[0]
		if Cos(batch[at], one[0]) < 0.95 {
			t.Fatalf("%d번째가 배치에서 달라졌다 : %.5f", at, Cos(batch[at], one[0]))
		}
	}
	// 배치로 넣은 것이 제 짝에 가장 가까워야 한다. 이것이 깨지면 배치가
	// 답을 바꾸는 것이다.
	for at := range texts {
		other := 1 - at
		if Cos(batch[at], alone[at]) <= Cos(batch[at], alone[other]) {
			t.Fatalf("%d번째가 남의 짝에 더 가깝다", at)
		}
	}
}
