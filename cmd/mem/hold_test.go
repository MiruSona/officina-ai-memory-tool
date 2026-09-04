package main

// 뒷정리-2b 회귀 — `add --hold` 로 넣은 기억은 검색·훅에 안 뜨고,
// `review --promote` 뒤에 뜬다 (결정 6).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// TestHoldHidesUntilPromoted 는 보류 → 검색 제외 → 승격 → 검색 포함까지
// 한 줄기로 본다. 셋 중 하나만 어긋나도 결정 6 이 무너진다.
func TestHoldHidesUntilPromoted(t *testing.T) {
	memory := newRepo(t)
	out, code := capture(t, func() int { return run(append(addArgs("본문 한 줄"), "--hold")) })
	if code != exitOK {
		t.Fatalf("add --hold 가 실패했다 : %s", out)
	}
	id := strings.TrimSpace(out)
	if _, code := capture(t, func() int { return run([]string{"index"}) }); code != exitOK {
		t.Fatal("색인이 실패했다")
	}
	raw := readMemoryFile(t, memory, id)
	if !strings.Contains(raw, "review: true") {
		t.Fatalf("머리말에 review 칸이 없다 :\n%s", raw)
	}

	if found, _ := capture(t, func() int { return run([]string{"search", "trigram"}) }); strings.Contains(found, id) {
		t.Fatalf("보류 기억이 검색에 떴다 :\n%s", found)
	}
	found, _ := capture(t, func() int { return run([]string{"search", "trigram", "--include-held"}) })
	if !strings.Contains(found, id) {
		t.Fatalf("--include-held 인데도 안 뜬다 :\n%s", found)
	}

	if _, code := capture(t, func() int { return run([]string{"review", "--promote", id}) }); code != exitOK {
		t.Fatal("승격이 실패했다")
	}
	if _, code := capture(t, func() int { return run([]string{"index"}) }); code != exitOK {
		t.Fatal("승격 뒤 색인이 실패했다")
	}
	found, _ = capture(t, func() int { return run([]string{"search", "trigram"}) })
	if !strings.Contains(found, id) {
		t.Fatalf("승격했는데 검색에 안 뜬다 :\n%s", found)
	}
}

// TestHoldIsOptIn 은 옵션 없는 add 가 지금까지와 똑같은지 본다. 모든 add 를
// 보류로 만들면 저장이 사실상 멈춘다.
func TestHoldIsOptIn(t *testing.T) {
	memory := newRepo(t)
	out, code := capture(t, func() int { return run(addArgs("본문 한 줄")) })
	if code != exitOK {
		t.Fatalf("add 가 실패했다 : %s", out)
	}
	id := strings.TrimSpace(out)
	if _, code := capture(t, func() int { return run([]string{"index"}) }); code != exitOK {
		t.Fatal("색인이 실패했다")
	}
	if raw := readMemoryFile(t, memory, id); strings.Contains(raw, "review:") {
		t.Fatalf("옵션을 안 줬는데 보류로 들어갔다 :\n%s", raw)
	}
	found, _ := capture(t, func() int { return run([]string{"search", "trigram"}) })
	if !strings.Contains(found, id) {
		t.Fatalf("보통 add 가 검색에 안 뜬다 :\n%s", found)
	}
}

// TestHoldIsWrittenToLog 는 log.md 만 보고도 어느 기억이 멈춰 있는지 아는지
// 본다 (설계 3-5 — 무엇이 얼마나 막혔는지가 재료다).
func TestHoldIsWrittenToLog(t *testing.T) {
	memory := newRepo(t)
	if _, code := capture(t, func() int { return run(append(addArgs("본문 한 줄"), "--hold")) }); code != exitOK {
		t.Fatal("add --hold 가 실패했다")
	}
	raw, err := os.ReadFile(store.LogPath(memory))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), holdMark) {
		t.Fatalf("log.md 에 보류 표시가 없다 :\n%s", raw)
	}
}

// TestVectorsAreIgnored 는 파생물인 vectors.bin 이 .gitignore 에 드는지 본다.
func TestVectorsAreIgnored(t *testing.T) {
	found := false
	for _, line := range i18n.InstallIgnoreLines {
		if strings.Contains(line, "vectors.bin") {
			found = true
		}
	}
	if !found {
		t.Fatal("vectors.bin 이 .gitignore 줄에 없다")
	}
}

// readMemoryFile 은 승격된 기억 파일 하나를 글자 그대로 읽는다.
func readMemoryFile(t *testing.T, memory, id string) string {
	t.Helper()
	found := ""
	root := filepath.Join(memory, "store")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, id+".md") {
			return err
		}
		raw, err := os.ReadFile(path)
		found = string(raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == "" {
		t.Fatalf("기억 파일을 못 찾았다 : %s", id)
	}
	return found
}
