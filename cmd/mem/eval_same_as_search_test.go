package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mem.toml [search] field_weights 를 극단으로 둔 저장소에서 `mem eval` 이
// 내는 차례가 `mem search` 가 내는 차례와 같아야 한다. 고치기 전에는 eval 만
// 코드 기본값으로 돌아서 둘이 달랐다 (갈래 A 가 넘긴 결함).
func TestEvalRanksSameAsSearch(t *testing.T) {
	memory := newRepo(t)
	putWeighted(t, memory)
	if _, code := capture(t, func() int { return run([]string{"index", "--full"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	byDefault := searchOrder(t)

	writeWeights(t, memory, "[1, 1, 1, 100]")
	extreme := searchOrder(t)
	// 자가 잴 것이 없으면 이 시험은 아무것도 안 잰다.
	if sameOrder(byDefault, extreme) {
		t.Fatalf("가중을 극단으로 줘도 차례가 안 바뀐다 — 시험 자료를 고쳐야 한다 : %v", extreme)
	}
	if got := evalOrder(t, memory); !sameOrder(got, extreme) {
		t.Fatalf("eval 과 search 의 차례가 다르다 :\n eval   %v\n search %v", got, extreme)
	}
}

// searchOrder 는 `mem search` 가 내는 id 차례다.
func searchOrder(t *testing.T) []string {
	t.Helper()
	out, code := capture(t, func() int { return run([]string{"search", "훅예산", "--json"}) })
	if code != exitOK {
		t.Fatalf("search 가 실패했다 : %d %s", code, out)
	}
	got := struct {
		Hits []struct {
			ID string `json:"id"`
		} `json:"hits"`
	}{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		t.Fatalf("search JSON 을 못 읽었다 : %v %q", err, out)
	}
	ids := []string{}
	for _, hit := range got.Hits {
		ids = append(ids, hit.ID)
	}
	return ids
}

// evalOrder 는 `mem eval` 이 같은 질의에서 실제로 본 답의 차례다.
func evalOrder(t *testing.T, memory string) []string {
	t.Helper()
	golden := filepath.Join(memory, "golden", "one.yaml")
	if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
		t.Fatal(err)
	}
	text := "version: 2\ncases:\n  - q: 훅예산\n    kind: extract\n    lang: ko\n    expect: [20260105-7777aaaa]\n"
	if err := os.WriteFile(golden, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := capture(t, func() int {
		return run([]string{"eval", "--json", "--golden", golden})
	})
	report := struct {
		Cases []struct {
			All []string `json:"all"`
		} `json:"results"`
	}{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &report); err != nil {
		t.Fatalf("eval JSON 을 못 읽었다 : %v %q", err, out)
	}
	if len(report.Cases) != 1 {
		t.Fatalf("문항이 하나라야 한다 : %q", out)
	}
	return report.Cases[0].All
}

func sameOrder(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for at := range left {
		if left[at] != right[at] {
			return false
		}
	}
	return true
}

// writeWeights 는 mem.toml 에 [search] field_weights 한 줄만 얹는다.
func writeWeights(t *testing.T, memory, value string) {
	t.Helper()
	path := filepath.Join(memory, "mem.toml")
	text := "schema = 1\nname = \"시험\"\n\n[search]\nfield_weights = " + value + "\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// putWeighted 는 같은 낱말이 한쪽은 제목에, 다른 쪽은 본문에만 여러 번 있는
// 기억 둘이다. 열 가중을 바꾸면 차례가 뒤집힌다.
func putWeighted(t *testing.T, memory string) {
	t.Helper()
	putRawTitled(t, memory, "20260105-7777aaaa", "훅예산 을 바이트로 잰다",
		"이 기억은 제목에만 그 낱말이 있다.\n둘째 줄이다.\n셋째 줄이다.\n")
	body := strings.Repeat("훅예산 이야기를 본문에서 되풀이한다.\n", 12)
	putRawTitled(t, memory, "20260105-7777bbbb", "상한을 무엇으로 재나",
		body+"둘째 줄이다.\n셋째 줄이다.\n")
}

// putRawTitled 는 제목과 본문을 골라 쓰는 putRaw 다.
func putRawTitled(t *testing.T, memory, id, title, body string) {
	t.Helper()
	dir := filepath.Join(memory, "store", "2026", "01")
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
		"date: 2026-01-05\n" +
		"author: mem/0.4.0\n" +
		"sources: [\"file:internal/hook/hook.go\"]\n" +
		"---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
