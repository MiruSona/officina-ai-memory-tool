package embed

import (
	"path/filepath"
	"testing"
)

// unit 은 시험용 단위 벡터다.
func unit(values ...float32) []float32 { return normalize32(append([]float32{}, values...)) }

func writeThree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, VectorFileName)
	rows := map[string][]float32{
		"a": unit(1, 0, 0),
		"b": unit(0.9, 0.1, 0),
		"c": unit(0, 0, 1),
	}
	keys := map[string]uint64{"a": 1, "b": 2, "c": 3}
	if err := WriteVectors(path, "테스트모델", "판1", 3, []string{"a", "b", "c"}, rows, keys); err != nil {
		t.Fatalf("못 썼다 : %v", err)
	}
	return path
}

func TestVectorRoundTrip(t *testing.T) {
	loaded, err := LoadVectors(writeThree(t))
	if err != nil {
		t.Fatalf("못 읽었다 : %v", err)
	}
	if loaded.Len() != 3 || loaded.Dim() != 3 {
		t.Fatalf("건수 %d · 차원 %d", loaded.Len(), loaded.Dim())
	}
	if !loaded.Matches("테스트모델", "판1", 3) {
		t.Fatalf("머리말이 안 맞는다 : %s@%s", loaded.Model(), loaded.Version())
	}
	// int8 로 눌렀으니 값이 조금 어긋난다. 차례만 맞으면 된다.
	near := Cos(loaded.Get("a"), loaded.Get("b"))
	far := Cos(loaded.Get("a"), loaded.Get("c"))
	if near <= far {
		t.Fatalf("차례가 뒤집혔다 : 가까운 것 %.3f · 먼 것 %.3f", near, far)
	}
}

// 모델이 바뀌면 머리말이 안 맞고, 그러면 index 가 통째로 다시 만든다 (결정 13).
func TestVectorHeaderMismatch(t *testing.T) {
	loaded, err := LoadVectors(writeThree(t))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Matches("다른모델", "판1", 3) {
		t.Fatal("모델 이름이 다른데 맞다고 한다")
	}
	if loaded.Matches("테스트모델", "판2", 3) {
		t.Fatal("판이 다른데 맞다고 한다")
	}
	if loaded.Matches("테스트모델", "판1", 384) {
		t.Fatal("차원이 다른데 맞다고 한다")
	}
	var empty *Vectors
	if empty.Matches("테스트모델", "판1", 3) {
		t.Fatal("파일이 없는데 맞다고 한다")
	}
}

// 없는 파일·망가진 파일은 오류다. 부르는 쪽이 그것을 보고 낱말 모드로 간다.
func TestVectorBadFile(t *testing.T) {
	if _, err := parseVectors([]byte("이건 벡터가 아니다")); err == nil {
		t.Fatal("아무 바이트나 벡터로 읽혔다")
	}
	path := writeThree(t)
	loaded, err := LoadVectors(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = loaded
	if _, err := LoadVectors(filepath.Join(t.TempDir(), "없다.bin")); err == nil {
		t.Fatal("없는 파일이 읽혔다")
	}
	if OpenVectors(t.TempDir()) != nil {
		t.Fatal("벡터가 없는데 nil 이 아니다")
	}
}

// 열쇠는 「무엇으로 만들었나」다. 이것이 같으면 index --full 도 벡터를 다시
// 안 만든다 (리뷰 B · V5).
func TestVectorKeysRoundTrip(t *testing.T) {
	loaded, err := LoadVectors(writeThree(t))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Key("a") != 1 || loaded.Key("c") != 3 {
		t.Fatalf("열쇠가 안 돌아왔다 : a=%d c=%d", loaded.Key("a"), loaded.Key("c"))
	}
	if loaded.Key("없는id") != 0 {
		t.Fatal("없는 기억에 열쇠가 있다")
	}
}

// Near 는 가까운 차례로 준다. 중복 후보 셋째 신호가 이것을 쓴다 (결정 15).
func TestNearOrdersByCosine(t *testing.T) {
	loaded, err := LoadVectors(writeThree(t))
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.Near("a", 2)
	if len(got) != 2 || got[0] != "b" {
		t.Fatalf("가까운 차례가 아니다 : %v", got)
	}
	if loaded.Near("없는id", 5) != nil {
		t.Fatal("벡터가 없는 기억인데 후보가 났다")
	}
}

// 재정렬기는 벡터가 없으면 nil 이다 — 모델을 실을 이유가 없다.
func TestRerankerNeedsVectors(t *testing.T) {
	if NewReranker(DefaultModel, nil, func([]int64) map[int64]string { return nil }) != nil {
		t.Fatal("벡터가 없는데 재정렬기가 났다")
	}
	loaded, err := LoadVectors(writeThree(t))
	if err != nil {
		t.Fatal(err)
	}
	if NewReranker(DefaultModel, loaded, nil) != nil {
		t.Fatal("이름표가 없는데 재정렬기가 났다")
	}
}

// 자리와 검사 : DLL 은 **절대경로**여야 한다. 이름만 넘기면 System32 의 옛
// onnxruntime.dll 이 잡혀 죽는다 (0실측 큰 발견 2).
func TestDLLPathIsAbsolute(t *testing.T) {
	path := DLLPath()
	if path == "" {
		t.Skip("집 폴더를 못 찾는다")
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("DLL 경로가 절대경로가 아니다 : %s", path)
	}
	if filepath.Base(path) != DLLFile {
		t.Fatalf("DLL 이름이 다르다 : %s", path)
	}
}

// 해시가 안 맞는 파일은 못 쓴다 (결정 18).
func TestSHAGuard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.bin")
	if err := WriteVectors(path, "m", "v", 3, []string{"a"},
		map[string][]float32{"a": unit(1, 0, 0)}, map[string]uint64{"a": 7}); err != nil {
		t.Fatal(err)
	}
	if SHAOK(path, "0000000000000000000000000000000000000000000000000000000000000000") {
		t.Fatal("해시가 안 맞는데 통과했다")
	}
	got, err := FileSHA(path)
	if err != nil {
		t.Fatal(err)
	}
	if !SHAOK(path, got) {
		t.Fatal("제 해시인데 안 맞는다고 한다")
	}
	if SHAOK(filepath.Join(dir, "없다.bin"), "") {
		t.Fatal("없는 파일이 통과했다")
	}
	if !SizeOK(path, 0) {
		t.Fatal("크기를 안 박아 뒀는데 막았다")
	}
	if SizeOK(path, 999999) {
		t.Fatal("크기가 다른데 통과했다")
	}
}

// 모델이 없으면 nil 이다. 오류가 아니다 (결정 16 · 우아한 퇴화).
func TestOpenModelWithoutFilesIsNil(t *testing.T) {
	if Ready("있을리없는모델이름") {
		t.Skip("이름이 겹쳤다")
	}
	if OpenModel("있을리없는모델이름") != nil {
		t.Fatal("모델이 없는데 열렸다")
	}
}

// 벡터 만들기는 모델이 없으면 조용히 건너뛴다 — index 가 죽으면 안 된다.
func TestBuildSkipsWithoutModel(t *testing.T) {
	report, err := Build(t.TempDir(), "있을리없는모델이름",
		[]Item{{ID: "a", Text: "아무 글"}})
	if err != nil {
		t.Fatalf("모델이 없는데 오류가 났다 : %v", err)
	}
	if !report.Skipped {
		t.Fatal("건너뛰었다고 안 한다")
	}
}
