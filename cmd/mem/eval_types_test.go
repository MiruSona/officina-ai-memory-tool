package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// vocab.toml 의 종류 표(반감기)를 바꾸면 `mem eval` 의 차례도 `mem search` 와
// 같이 바뀌어야 한다. 고치기 전에는 eval 만 종류 표를 안 넘겨 늘 기본 반감기로
// 돌았다 — 반감기 손잡이를 eval 로 잴 수 없었다 (2026-10-05 재순위 손잡이 실험).
func TestEvalUsesVocabTypes(t *testing.T) {
	memory := newRepo(t)
	putAged(t, memory)
	if _, code := capture(t, func() int { return run([]string{"index", "--full"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	byDefault := searchOrder(t)

	writeDecisionHalfLife(t, memory, "none")
	noDecay := searchOrder(t)
	// 자가 잴 것이 없으면 이 시험은 아무것도 안 잰다.
	if sameOrder(byDefault, noDecay) {
		t.Fatalf("반감기를 꺼도 차례가 안 바뀐다 — 시험 자료를 고쳐야 한다 : %v", noDecay)
	}
	if got := evalOrder(t, memory); !sameOrder(got, noDecay) {
		t.Fatalf("반감기를 끈 저장소에서 eval 과 search 의 차례가 다르다 :\n eval   %v\n search %v", got, noDecay)
	}
}

// writeDecisionHalfLife 는 시험 vocab 에 decision 종류의 반감기 한 칸만 얹는다.
// 종류 표는 칸별 덮어쓰기라 나머지는 기본표 그대로다.
func writeDecisionHalfLife(t *testing.T, memory, value string) {
	t.Helper()
	text := testVocabTOML + "\n[type.decision]\nhalf_life = \"" + value + "\"\n"
	if err := os.WriteFile(filepath.Join(memory, "vocab.toml"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// putAged 는 낱말이 제목에 있는 아주 오래된 결정과, 본문에만 있는 새 결정이다.
// 기본 반감기(decision 720일)에서는 감쇠가 옛 것을 끌어내리고, 반감기를 끄면
// 낱말이 더 잘 맞는 옛 것이 앞선다.
func putAged(t *testing.T, memory string) {
	t.Helper()
	putDated(t, memory, "20200105-7777cccc", "2020-01-05", "훅예산 을 바이트로 잰다",
		"이 기억은 제목에만 그 낱말이 있다.\n둘째 줄이다.\n셋째 줄이다.\n")
	body := strings.Repeat("훅예산 이야기를 본문에서 되풀이한다.\n", 2)
	putDated(t, memory, "20260105-7777dddd", "2026-01-05", "상한을 무엇으로 재나",
		body+"둘째 줄이다.\n셋째 줄이다.\n")
}

// putDated 는 날짜를 골라 쓰는 putRawTitled 다.
func putDated(t *testing.T, memory, id, date, title, body string) {
	t.Helper()
	dir := filepath.Join(memory, "store", date[:4], date[5:7])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	text := "---\n" +
		"id: " + id + "\n" +
		"type: decision\n" +
		"title: " + title + "\n" +
		"summary: 훅이 밀어 넣는 글을 무엇으로 재는지 정한 기억이다. 한글은 한 글자가 세 바이트다\n" +
		"tags: [index, korean]\n" +
		"scope: mem-search\n" +
		"date: " + date + "\n" +
		"author: mem/0.4.0\n" +
		"sources: [\"file:internal/hook/hook.go\"]\n" +
		"---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
