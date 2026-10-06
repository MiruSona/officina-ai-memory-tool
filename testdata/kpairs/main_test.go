package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFakeStore 는 가짜 기억 count 건과 걸러져야 할 기억 넷을 만든다.
// 기억마다 낱말이 달라 무관 쌍의 Jaccard 가 낮고, 요약 속 숫자·「켠다」가 본문 문장에도 있다.
func writeFakeStore(t *testing.T, count int) string {
	t.Helper()
	dir := t.TempDir()
	scopes := []string{"alpha", "beta", "gamma"}
	for i := 0; i < count; i++ {
		origin := ""
		if i < 3 {
			origin = "origin: stop\n"
		}
		body := fmt.Sprintf("도구%d 설정%d 항목%d 범위%d 켠다 추가%d 설명%d 값 %d 개.\n", i, i, i, i, i, i, i+3)
		if i%5 == 0 {
			body += strings.Repeat(fmt.Sprintf("내용%d 긴글%d 문단입니다 ", i, i), 20) + "\n"
		}
		summary := fmt.Sprintf("도구%d 설정%d 항목%d 범위%d 켠다 값 %d 개", i, i, i, i, i+3)
		writeMemory(t, dir, fmt.Sprintf("2026%04d", i), "decision", scopes[i%3], origin, summary, body)
	}
	// 걸러져야 하는 것 : 뺄 scope · 금지어 · 비밀 · 안 쓰는 종류
	writeMemory(t, dir, "9001", "decision", "prototool", "", "비밀기획 켠다 값 7", "비밀기획 켠다 값 7 문장입니다.\n")
	writeMemory(t, dir, "9002", "decision", "alpha", "", "금지낱말 켠다 값 7", "금지낱말 켠다 값 7 문장입니다.\n")
	writeMemory(t, dir, "9003", "decision", "alpha", "", "열쇠 켠다 값 7", "열쇠 AKIAQ3Z8K2M9W4R7T1V6 켠다 값 7 문장.\n")
	writeMemory(t, dir, "9004", "history", "alpha", "", "기록 켠다 값 7", "기록 켠다 값 7 문장입니다.\n")
	return dir
}

func writeMemory(t *testing.T, dir, id, kind, scope, origin, summary, body string) {
	t.Helper()
	text := fmt.Sprintf("---\nid: %s\ntype: %s\ntitle: 제목 %s\nsummary: %s\ntags: [x]\nscope: %s\n%s---\n\n%s",
		id, kind, id, summary, scope, origin, body)
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeFrom(t *testing.T, store string, seed int64) []pair {
	t.Helper()
	memories, err := loadMemories(store, []string{"금지낱말"})
	if err != nil {
		t.Fatal(err)
	}
	pairs, err := makePairs(memories, seed)
	if err != nil {
		t.Fatal(err)
	}
	return pairs
}

func TestMakeSameSeedSameBytes(t *testing.T) {
	store := writeFakeStore(t, 90)
	read := func() string {
		out := t.TempDir()
		if code := run([]string{"make", "-store", store, "-seed", "7", "-out", out}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
			t.Fatalf("종료 코드 %d", code)
		}
		data, err := os.ReadFile(filepath.Join(out, "pairs.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		smoke, _ := os.ReadFile(filepath.Join(out, "smoke.jsonl"))
		if lines := strings.Count(string(smoke), "\n"); lines != 3 {
			t.Fatalf("smoke 줄 수 %d", lines)
		}
		return string(data)
	}
	first, second := read(), read()
	if first != second {
		t.Fatal("씨앗이 같은데 출력이 다르다")
	}
	if lines := strings.Count(first, "\n"); lines != 48 {
		t.Fatalf("pairs 줄 수 %d", lines)
	}
}

func TestFiltersDropUnsafe(t *testing.T) {
	memories, err := loadMemories(writeFakeStore(t, 10), []string{"금지낱말"})
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) != 10 {
		t.Fatalf("걸러진 뒤 %d건 (10건이어야 한다)", len(memories))
	}
	for _, item := range memories {
		if strings.HasPrefix(item.id, "900") {
			t.Fatalf("뺄 scope·금지어·비밀·종류 거르기가 %s 를 놓쳤다", item.id)
		}
	}
}

func TestMakeBranchesAndNoReuse(t *testing.T) {
	pairs := makeFrom(t, writeFakeStore(t, 90), 1)
	counts, used, rules := map[string]int{}, map[string]bool{}, map[string]int{}
	for _, item := range pairs {
		counts[item.Want]++
		rules[item.Rule]++
		for _, id := range strings.Split(item.Src, "+") {
			if used[id] {
				t.Fatalf("기억 %s 를 두 번 썼다", id)
			}
			used[id] = true
			if strings.HasPrefix(id, "900") {
				t.Fatalf("걸러져야 할 기억 %s 가 쓰였다", id)
			}
		}
	}
	for _, branch := range branches {
		if counts[branch] != perBranch {
			t.Fatalf("%s %d쌍", branch, counts[branch])
		}
	}
	if rules["long"] != 4 || rules["same"] != 12 || rules["xscope"] != 10 || rules["sscope"] != 6 {
		t.Fatalf("규칙별 수가 틀렸다: %v", rules)
	}
	if pairs[0].ID != "k001" || pairs[47].ID != "k048" {
		t.Fatalf("id 꼴: %s %s", pairs[0].ID, pairs[47].ID)
	}
}

func TestContradictionsReallyClash(t *testing.T) {
	store := writeFakeStore(t, 90)
	memories, _ := loadMemories(store, nil)
	summaries := map[string]string{}
	for _, item := range memories {
		summaries[item.id] = item.summary
	}
	for _, item := range makeFrom(t, store, 3) {
		if item.Want != wantContradict {
			continue
		}
		summary := summaries[item.Src]
		if item.Claim == summary {
			t.Fatalf("%s 주장이 요약과 같다", item.ID)
		}
		// 요약에만 있고 주장에서 사라진 낱말이 근거에 들어 있어야 한다.
		claimWords := words(item.Claim)
		found := false
		for word := range words(summary) {
			if !claimWords[word] && strings.Contains(item.Evidence, word) {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s 뒤집힌 말이 근거에 없다: %q / %q", item.ID, item.Evidence, item.Claim)
		}
	}
}

func TestFlipForms(t *testing.T) {
	item := memory{id: "x", summary: "검사는 기본 켬 이다", sentences: []string{"이 검사는 기본 켬 상태로 둔다."}}
	options := contradictions(item)
	if len(options) != 1 || options[0].Claim != "검사는 기본 끔 이다" || options[0].Rule != "neg" {
		t.Fatalf("극성 뒤집기: %+v", options)
	}
	item = memory{id: "y", summary: "재시도는 3 번", sentences: []string{"재시도는 3 번까지 한다고 정했다."}}
	options = contradictions(item)
	if len(options) != 1 || options[0].Claim != "재시도는 6 번" || options[0].Rule != "num" {
		t.Fatalf("숫자 뒤집기: %+v", options)
	}
	// 낱말에 붙은 숫자 · 날짜 · 단위가 다른 숫자는 뒤집지 않는다.
	for _, pairText := range [][2]string{
		{"in_sum2 를 쓴다", "in_sum2 를 쓴다고 적었다."},
		{"10-06 에 막혔다", "10-06 에 둘 다 막혔다."},
		{"예산 5만 이 한계다", "공수는 값의 4~5배 로 잡는다."},
	} {
		item = memory{id: "w", summary: pairText[0], sentences: []string{pairText[1]}}
		if options = contradictions(item); len(options) != 0 {
			t.Fatalf("뒤집으면 안 되는 숫자를 뒤집었다: %+v", options)
		}
	}
	// 근거 문장에 뒤집을 말이 없으면 쓰지 않는다.
	item = memory{id: "z", summary: "재시도는 3 번", sentences: []string{"재시도 횟수를 정했다는 기록이다."}}
	if options = contradictions(item); len(options) != 0 {
		t.Fatalf("부딪치지 않는 쌍을 만들었다: %+v", options)
	}
}

func TestMakeShortFails(t *testing.T) {
	store := writeFakeStore(t, 20)
	var stderr bytes.Buffer
	code := run([]string{"make", "-store", store, "-seed", "1", "-out", filepath.Join(t.TempDir(), "o")}, &bytes.Buffer{}, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "모자람") {
		t.Fatalf("종료 %d, stderr %q", code, stderr.String())
	}
}

func TestScore(t *testing.T) {
	pairs := []pair{}
	rows := []resultRow{}
	letters := map[string]string{wantSupport: "A", wantContradict: "B", wantUnrelated: "C"}
	for i := 0; i < 48; i++ {
		want := branches[i/16]
		item := pair{ID: fmt.Sprintf("k%03d", i+1), Want: want}
		if i < 4 {
			item.Rule = "long"
		}
		pairs = append(pairs, item)
		letter := letters[want]
		if i%16 < 2 { // 갈래마다 2개는 틀림 → 42/48
			letter = "C"
			if want == wantUnrelated {
				letter = "A"
			}
		}
		rows = append(rows, resultRow{ID: item.ID, Letter: letter, MS: int64(1000 + i*10)})
	}
	var out bytes.Buffer
	if verdict := score(pairs, rows, &out); verdict != "통과" {
		t.Fatalf("판정 %s\n%s", verdict, out.String())
	}
	if !strings.Contains(out.String(), "42/48") || !strings.Contains(out.String(), "p95 1450ms") {
		t.Fatalf("표가 틀렸다:\n%s", out.String())
	}
	// 지지 갈래 8개를 못 받게 하면 34/48, 지지 6/16 → 조건부
	rows[2].Problem, rows[3].Problem, rows[4].Letter = "x", "x", ""
	rows = append(rows[:5], rows[10:]...)
	out.Reset()
	if verdict := score(pairs, rows, &out); verdict != "조건부" {
		t.Fatalf("판정 %s\n%s", verdict, out.String())
	}
	if !strings.Contains(out.String(), "support    6/16") || !strings.Contains(out.String(), "못 받은 수  8") {
		t.Fatalf("갈래·못 받은 수가 틀렸다:\n%s", out.String())
	}
	// 지연만 넘고 갈래는 고르면 미달
	slow := []resultRow{}
	for i, item := range pairs {
		slow = append(slow, resultRow{ID: item.ID, Letter: letters[item.Want], MS: int64(6000 + i)})
	}
	if verdict := score(pairs, slow, &bytes.Buffer{}); verdict != "미달" {
		t.Fatalf("판정 %s", verdict)
	}
}

// -out 폴더에 이미 산출물이 있으면 덮지 않고 거절한다 (본문이 든 파일 · 링크 보호).
func TestWriteRefusesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pairs.jsonl")
	if err := os.WriteFile(path, []byte("옛것\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := writeJSONL(path, []pair{{}})
	if err == nil || !strings.Contains(err.Error(), "지우고") {
		t.Fatalf("이미 있는 파일을 거절하지 않았다 : %v", err)
	}
	if now, _ := os.ReadFile(path); string(now) != "옛것\n" {
		t.Fatal("이미 있는 파일을 덮었다")
	}
}
