package embed

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// tinyTable 은 시험용 표다. `색인`·`인덱`·`덱싱` 을 한 방향으로, `물리` 를
// 다른 방향으로 놓아 뜻이 갈리는지 볼 수 있게 만든다.
func tinyTable(t *testing.T) *Table {
	t.Helper()
	pieces := []string{"색인", "인덱", "덱싱", "물리", "엔진"}
	vectors := [][]float64{
		{1, 0, 0}, {0.9, 0.1, 0}, {0.9, -0.1, 0}, {0, 0, 1}, {0, 0.1, 0.9},
	}
	weights := []float32{1, 1, 1, 1, 1}
	buffer := bytes.Buffer{}
	if err := Write(&buffer, pieces, weights, vectors); err != nil {
		t.Fatalf("표를 못 썼다 : %v", err)
	}
	table, err := Parse(buffer.Bytes())
	if err != nil {
		t.Fatalf("표를 못 읽었다 : %v", err)
	}
	return table
}

func TestWriteThenParse(t *testing.T) {
	table := tinyTable(t)
	if table.Dim() != 3 {
		t.Fatalf("차원이 %d 다", table.Dim())
	}
	if table.Size() != 5 {
		t.Fatalf("어휘가 %d 개다", table.Size())
	}
}

// 표의 어휘는 우리 토크나이저가 낸 조각이어야 한다. `색인` 이라는 글은
// 바이그램 하나로 쪼개져 표의 `색인` 을 그대로 짚는다.
func TestTokenMatchesOurTokenizer(t *testing.T) {
	table := tinyTable(t)
	if got := Pieces("색인"); len(got) != 1 || got[0] != "색인" {
		t.Fatalf("쪼갠 결과가 %v 다", got)
	}
	vector := table.Text("색인")
	if vector == nil {
		t.Fatal("아는 조각인데 벡터가 없다")
	}
	if math.Abs(vector[0]-1) > 1e-6 {
		t.Fatalf("벡터가 %v 다", vector)
	}
}

// 표에 없는 말만 든 글은 벡터가 없다. 여기서 nil 이 나와야 검색이 임베딩
// 갈래를 건너뛴다.
func TestUnknownTextHasNoVector(t *testing.T) {
	table := tinyTable(t)
	if vector := table.Text("파티클 이펙트"); vector != nil {
		t.Fatalf("모르는 말인데 벡터가 났다 : %v", vector)
	}
}

// 여러 조각은 평균이 된다. `인덱싱` 은 `인덱`+`덱싱` 이라 `색인` 쪽을 향한다.
func TestAverageOfPieces(t *testing.T) {
	table := tinyTable(t)
	near := Dot(table.Text("인덱싱"), table.Text("색인"))
	far := Dot(table.Text("인덱싱"), table.Text("물리엔진"))
	if near <= far {
		t.Fatalf("뜻이 안 갈렸다 : 가까운 것 %.3f · 먼 것 %.3f", near, far)
	}
	if near < 0.9 {
		t.Fatalf("같은 뜻인데 닮음이 %.3f 다", near)
	}
}

func TestBadFileIsError(t *testing.T) {
	if _, err := Parse([]byte("이건 표가 아니다")); err == nil {
		t.Fatal("아무 바이트나 표로 읽혔다")
	}
	whole := bytes.Buffer{}
	if err := Write(&whole, []string{"색인"}, []float32{1}, [][]float64{{1, 0}}); err != nil {
		t.Fatal(err)
	}
	cut := whole.Bytes()[:whole.Len()-1]
	if _, err := Parse(cut); err == nil {
		t.Fatal("잘린 표가 읽혔다")
	}
}

// 파일이 없으면 오류가 아니라 nil 이다. 그래야 낱말 검색이 그대로 돈다.
func TestOpenWithoutFileIsNil(t *testing.T) {
	Forget()
	defer Forget()
	dir := t.TempDir()
	if table := Open(filepath.Join(dir, "없는파일.bin"), dir); table != nil {
		t.Fatal("없는 파일인데 표가 났다")
	}
}

// 설정에 적힌 자리가 없으면 저장소 안 `model/ko.bin` 을 본다 (설계 4-8).
func TestFindFallsBackToRepo(t *testing.T) {
	Forget()
	defer Forget()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, DirName, FileName)
	buffer := bytes.Buffer{}
	if err := Write(&buffer, []string{"색인"}, []float32{1}, [][]float64{{1, 0}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Find(filepath.Join(dir, "없는것.bin"), dir); got != path {
		t.Fatalf("찾은 자리가 %q 다", got)
	}
	if table := Open("", dir); table == nil {
		t.Fatal("저장소 안 표를 못 읽었다")
	}
}
