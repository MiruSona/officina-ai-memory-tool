package main

// 물결 3 (3A) 의 못 박기 시험.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/embed"
)

// 결정 15 — **훅은 모델을 절대 안 싣는다.** 준비에만 0.4초라 1초 예산의 절반이
// 날아간다. 코드가 그렇게 되어 있는지를 소스에서 못 박는다 : 훅 경로가
// `internal/embed` 를 아예 안 부른다.
func TestHookNeverLoadsModel(t *testing.T) {
	for _, path := range []string{"cmd_hook.go", filepath.Join("..", "..", "internal", "hook", "hook.go")} {
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s 를 못 읽었다 : %v", path, err)
		}
		if strings.Contains(string(text), "internal/embed") {
			t.Fatalf("%s 가 임베딩을 부른다 — 훅 예산 1초가 깨진다 (결정 15)", path)
		}
	}
	// 훅 패키지 전체가 임베딩을 안 쓰는지도 본다.
	dir := filepath.Join("..", "..", "internal", "hook")
	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, one := range names {
		if !strings.HasSuffix(one.Name(), ".go") || strings.HasSuffix(one.Name(), "_test.go") {
			continue
		}
		text, err := os.ReadFile(filepath.Join(dir, one.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(text), "internal/embed") {
			t.Fatalf("hook/%s 가 임베딩을 부른다 (결정 15)", one.Name())
		}
	}
}

// 결정 16 — 모델이 없어도 어느 명령도 안 죽는다. 벡터를 못 찾으면 꽂을 것이
// 없고, 그러면 검색은 낱말 모드 그대로다.
func TestNoModelMeansWordMode(t *testing.T) {
	dir := t.TempDir()
	if rerankerFor(dir, nil) != nil {
		t.Fatal("색인이 없는데 재정렬기가 났다")
	}
	if vectorsFor(dir) != nil {
		t.Fatal("벡터가 없는데 뭔가 났다")
	}
	// **빈 포인터를 인터페이스에 담으면 안 된다.** 담으면 quality 가
	// 「셋째 신호가 있다」고 잘못 보고 nil 을 부른다.
	if nearOf(nil) != nil {
		t.Fatal("빈 벡터가 인터페이스에 담겼다")
	}
	if nearOf(vectorsFor(dir)) != nil {
		t.Fatal("빈 벡터가 인터페이스에 담겼다")
	}
}

// 결정 13 — vectors.bin 은 `Memory/` 안의 파생물이다.
func TestVectorPathIsInsideRepo(t *testing.T) {
	got := embed.VectorPath(filepath.Join("아무", "Memory"))
	if filepath.Base(got) != embed.VectorFileName {
		t.Fatalf("자리가 다르다 : %s", got)
	}
	if embed.VectorPath("") != "" {
		t.Fatal("저장소가 없는데 자리가 났다")
	}
}
