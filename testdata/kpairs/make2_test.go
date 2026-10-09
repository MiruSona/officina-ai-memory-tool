package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMemoryLinks(t *testing.T) {
	text := "---\nid: a1\ntype: decision\nsummary: 요약\nsources:\n  - note:메모\n  - mem:b2\nlinks: [c3, d4]\n" +
		"superseded_by: e5\ntags:\n  - x\n---\n\n본문"
	item, ok := parseMemory(text)
	if !ok {
		t.Fatal("못 읽음")
	}
	if item.supersededBy != "e5" {
		t.Fatalf("superseded_by %q", item.supersededBy)
	}
	if strings.Join(item.links, ",") != "b2,c3,d4" {
		t.Fatalf("links %v", item.links)
	}
	if item.summary != "요약" || item.body != "본문" {
		t.Fatalf("다른 칸이 바뀜: %+v", item)
	}
}

// writeLinkedStore 는 가짜 기억 count 건 + 덮음 짝 6 · 링크 짝 10 을 만든다.
func writeLinkedStore(t *testing.T, count int) string {
	t.Helper()
	dir := writeFakeStore(t, count)
	for i := 0; i < 6; i++ {
		oldID, newID := fmt.Sprintf("3%03d", i), fmt.Sprintf("4%03d", i)
		writeRaw(t, dir, oldID, "history", fmt.Sprintf("superseded_by: %s\n", newID),
			fmt.Sprintf("옛판%d 값 %d 초", i, i+2), fmt.Sprintf("옛판%d 처리 시간은 값 %d 초 였다.\n", i, i+2))
		writeRaw(t, dir, newID, "decision", "", fmt.Sprintf("옛판%d 값 %d 초 로 바꿨다", i, i+9),
			fmt.Sprintf("새판%d 은 값 %d 초 걸린다고 정했다.\n", i, i+9))
	}
	for i := 0; i < 10; i++ {
		fromID, toID := fmt.Sprintf("5%03d", i), fmt.Sprintf("6%03d", i)
		writeRaw(t, dir, fromID, "decision", fmt.Sprintf("links: [%s]\n", toID),
			fmt.Sprintf("연결k%d 고리 설정 바꿈", i), fmt.Sprintf("연결k%d 쪽 다른 설명 글입니다.\n", i))
		writeRaw(t, dir, toID, "decision", "", fmt.Sprintf("대상k%d 기록", i),
			fmt.Sprintf("연결k%d 고리 설정 은 이렇게 했다 문장.\n", i))
	}
	return dir
}

func writeRaw(t *testing.T, dir, id, kind, extra, summary, body string) {
	t.Helper()
	text := fmt.Sprintf("---\nid: %s\ntype: %s\ntitle: 제목\nsummary: %s\nscope: alpha\n%s---\n\n%s", id, kind, summary, extra, body)
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMake2MeasureNoReuseAndKinds(t *testing.T) {
	memories, err := loadAll(writeLinkedStore(t, 120), nil, map[string]bool{"20260001": true})
	if err != nil {
		t.Fatal(err)
	}
	got := makeCandidates(memories, 3, measureQuota, true, make2Opts{})
	used, counts := map[string]bool{}, map[string]int{}
	for _, item := range got {
		counts[item.Kind]++
		if item.Want != item.StructLabel {
			t.Fatalf("want 과 struct_label 이 다르다: %+v", item)
		}
		for _, id := range item.SrcIDs {
			if used[id] {
				t.Fatalf("measure 에서 기억 %s 를 두 번 썼다", id)
			}
			if id == "20260001" {
				t.Fatal("-exclude 한 기억이 쓰였다")
			}
			used[id] = true
		}
	}
	if counts[kindSupersede] != 6 || counts[kindSupersede]+counts[kindFlip] != contradictWant(measureQuota) {
		t.Fatalf("반대는 덮음 6 + 뒤집기 24 여야 한다: %v", counts)
	}
	if counts[kindLink] != 8 || counts[kindSame] != 15 || counts[kindNum] != 7 {
		t.Fatalf("지지 몫: %v", counts)
	}
	if counts[kindSScope]+counts[kindXScope] != 30 {
		t.Fatalf("무관 몫: %v", counts)
	}
}

func TestMake2SameSeedSameOutput(t *testing.T) {
	store := writeLinkedStore(t, 120)
	first, _ := loadAll(store, nil, nil)
	second, _ := loadAll(store, nil, nil)
	a := fmt.Sprint(makeCandidates(first, 5, trainQuota, false, make2Opts{}))
	b := fmt.Sprint(makeCandidates(second, 5, trainQuota, false, make2Opts{}))
	if a != b {
		t.Fatal("씨앗이 같은데 출력이 다르다")
	}
}

func TestMake2RefusesWrongStore(t *testing.T) {
	store := writeFakeStore(t, 10)
	err := runMake2([]string{"-store", store, "-out", t.TempDir(), "-set", "train"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "④") {
		t.Fatalf("스튜디오가 아닌 store 로 train 을 만들었다: %v", err)
	}
}

func TestLeakFindsOverlap(t *testing.T) {
	dir := t.TempDir()
	trainDir := filepath.Join(dir, "train")
	os.MkdirAll(trainDir, 0o755)
	train := filepath.Join(trainDir, "candidates.jsonl")
	os.WriteFile(train, []byte(`{"id":"t1","claim":"가나다라마바사아자차","src_ids":["m1"]}`+"\n"), 0o644)
	os.WriteFile(filepath.Join(trainDir, "MANIFEST.txt"), []byte("set: train\nstore: studio\n"), 0o644)
	clean := filepath.Join(dir, "clean.jsonl")
	os.WriteFile(clean, []byte(`{"id":"k1","claim":"전혀 다른 주장 글입니다","src":"m2+m3"}`+"\n"), 0o644)
	dirty := filepath.Join(dir, "dirty.jsonl")
	os.WriteFile(dirty, []byte(`{"id":"k2","claim":"앞 가나다라 마바사아 뒤","src":"m1"}`+"\n"), 0o644)
	exclude := filepath.Join(dir, "exclude.txt")
	os.WriteFile(exclude, []byte("m9\n"), 0o644)

	var out bytes.Buffer
	if err := runLeak([]string{"-train", train, "-test", clean, "-exclude", exclude}, &out); err != nil {
		t.Fatalf("깨끗한데 걸렸다: %v\n%s", err, out.String())
	}
	out.Reset()
	err := runLeak([]string{"-train", train, "-test", dirty, "-exclude", exclude}, &out)
	if err == nil || !strings.Contains(out.String(), "② 기억 id 겹침 train ↔ dirty.jsonl : 1") ||
		!strings.Contains(out.String(), "③ 주장 8글자 겹침 train ↔ dirty.jsonl : 1") {
		t.Fatalf("겹침을 못 찾았다: %v\n%s", err, out.String())
	}
	os.WriteFile(exclude, []byte("m1\n"), 0o644)
	out.Reset()
	if err := runLeak([]string{"-train", train, "-test", clean, "-exclude", exclude}, &out); err == nil {
		t.Fatalf("K 원천 id 를 못 찾았다\n%s", out.String())
	}
}

func TestMajority(t *testing.T) {
	cases := []struct{ s, r, a, want, by string }{
		{"A", "", "A", "A", "struct+agent"},
		{"A", "", "B", "", ""},
		{"A", "B", "B", "B", "rules+agent"},
		{"A", "", "", "", ""},
		{"C", "C", "A", "C", "struct+rules"},
	}
	for _, c := range cases {
		letter, by := majority(c.s, c.r, c.a)
		if letter != c.want || by != c.by {
			t.Fatalf("%v → %s %s", c, letter, by)
		}
	}
}

func TestLeakGramSkipsNamesAndStockPhrases(t *testing.T) {
	if grams := claimGrams("AIMemoryTool 2026-10-08"); len(grams) != 0 {
		t.Fatalf("영문·숫자 조각을 셌다: %v", grams)
	}
	train := []leakRow{
		{ID: "t1", Claim: "첫째 주장 (사용자 확정함)"},
		{ID: "t2", Claim: "둘째 주장 (사용자 확정함)"},
		{ID: "t3", Claim: "셋째 주장 (사용자 확정함)"},
	}
	test := []leakRow{{ID: "g1", Claim: "다른 주장 (사용자 확정함)"}}
	if found := gramHits(test, gramOwners(train)); len(found) != 0 {
		t.Fatalf("상투 문구를 누수로 셌다: %v", found)
	}
}

func TestMake2FlipsOnly(t *testing.T) {
	memories, err := loadAll(writeLinkedStore(t, 120), nil, map[string]bool{"20260001": true})
	if err != nil {
		t.Fatal(err)
	}
	got := flipOnly(memories, 1, 5, make2Opts{})
	if len(got) != 5 {
		t.Fatalf("뒤집기 5 쌍이어야 한다: %d", len(got))
	}
	used := map[string]bool{}
	for _, item := range got {
		if item.Kind != kindFlip || item.Want != wantContradict {
			t.Fatalf("뒤집기 반대가 아니다: %+v", item)
		}
		for _, id := range item.SrcIDs {
			if used[id] || id == "20260001" {
				t.Fatalf("기억 %s 를 다시 썼거나 뺀 기억을 썼다", id)
			}
			used[id] = true
		}
	}
	if fmt.Sprint(got) != fmt.Sprint(flipOnly(memories, 1, 5, make2Opts{})) {
		t.Fatal("씨앗이 같은데 출력이 다르다")
	}
}

// 규칙 단이 낸 답은 규칙 표에서만 센다 — 사다리 전체로 돌린 agent 표에 규칙 답이 섞여도 두 표가 안 된다.
func TestReadVotesSplitsRuleStage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "votes.jsonl")
	rows := `{"id":"r1","letter":"B","stage":"rules"}` + "\n" + `{"id":"s1","letter":"A","stage":"semif"}` + "\n" +
		`{"id":"o1","letter":"C"}` + "\n" + `{"id":"u1","letter":"A","stage":"semif","unsure":true}` + "\n"
	if err := os.WriteFile(path, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	agent, err := readVotes(path, false)
	if err != nil || len(agent) != 2 || agent["s1"] != "A" || agent["o1"] != "C" {
		t.Fatalf("agent 표 : %v %v", agent, err)
	}
	rules, err := readVotes(path, true)
	if err != nil || len(rules) != 2 || rules["r1"] != "B" || rules["o1"] != "C" {
		t.Fatalf("규칙 표 : %v %v", rules, err)
	}
}

func TestParseQuota(t *testing.T) {
	got, err := parseQuota("supersede=40, flip=60,same=5")
	if err != nil || got[kindSupersede] != 40 || got[kindFlip] != 60 || got[kindSame] != 5 || got[kindLink] != 0 {
		t.Fatalf("몫을 잘못 읽음: %v %v", got, err)
	}
	for _, bad := range []string{"", "nope=3", "same=-1", "same=x", "same"} {
		if _, err := parseQuota(bad); err == nil {
			t.Fatalf("%q 를 받았다", bad)
		}
	}
}

// -quota 로 덮음 몫을 크게 잡으면 모자란 반대는 뒤집기로 채우고, 갈래 밖 몫은 0 이다.
func TestMake2CustomQuotaFillsContradictWithFlips(t *testing.T) {
	memories, err := loadAll(writeLinkedStore(t, 120), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	quota, _ := parseQuota("supersede=10,flip=5,same=4")
	got := makeCandidates(memories, 2, quota, true, make2Opts{})
	counts := map[string]int{}
	used := map[string]bool{}
	for _, item := range got {
		counts[item.Kind]++
		for _, id := range item.SrcIDs {
			if used[id] {
				t.Fatalf("measure 에서 기억 %s 를 두 번 썼다", id)
			}
			used[id] = true
		}
	}
	if counts[kindSupersede] != 6 || counts[kindFlip] != 9 || counts[kindSame] != 4 || len(got) != 19 {
		t.Fatalf("덮음 6 + 뒤집기 9 + 같음 4 여야 한다: %v", counts)
	}
}

func TestMake2QuotaOnlyForMeasure(t *testing.T) {
	store := writeFakeStore(t, 10)
	err := runMake2([]string{"-store", store, "-out", t.TempDir(), "-set", "measure", "-flips", "3", "-quota", "same=1"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "-quota") {
		t.Fatalf("-flips 와 -quota 를 같이 받았다: %v", err)
	}
	out := t.TempDir()
	var stdout bytes.Buffer
	if err := runMake2([]string{"-store", store, "-out", out, "-set", "measure", "-quota", "same=2"}, &stdout); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "measure-draft.jsonl")); err != nil {
		t.Fatalf("measure-draft.jsonl 이 없다: %v", err)
	}
}

// -more-types 를 주면 history 도 쌍 재료가 되고, 끝나면 원래대로 돌아온다.
func TestMake2MoreTypesUsesHistory(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 6; i++ {
		writeRaw(t, dir, fmt.Sprintf("7%03d", i), "history", "", fmt.Sprintf("기록k%d 빌드 시간 값 %d 초 걸렸다", i, i+3),
			fmt.Sprintf("기록k%d 빌드 시간 은 값 %d 초 걸렸다 문장.\n", i, i+3))
	}
	out := t.TempDir()
	if err := runMake2([]string{"-store", dir, "-out", out, "-set", "measure", "-quota", "same=6"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(out, "measure-draft.jsonl")); len(data) != 0 {
		t.Fatalf("-more-types 없이 history 를 썼다: %s", data)
	}
	out2 := t.TempDir()
	var stdout bytes.Buffer
	if err := runMake2([]string{"-store", dir, "-out", out2, "-set", "measure", "-quota", "same=6", "-more-types", "history"}, &stdout); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "same\t6\t6") || !strings.Contains(stdout.String(), "more-types: history") {
		t.Fatalf("history 6 쌍이 안 나왔다:\n%s", stdout.String())
	}
	if err := runMake2([]string{"-store", dir, "-out", t.TempDir(), "-set", "train", "-more-types", "history"}, &bytes.Buffer{}); err == nil {
		t.Fatal("train 에서 -more-types 를 받았다")
	}
}

// -avoid 로 준 쌍의 근거 문장은 다시 근거로 안 고른다 — 같은 기억이면 다른 문장을 고른다.
func TestMake2AvoidSkipsUsedEvidence(t *testing.T) {
	dir := t.TempDir()
	writeRaw(t, dir, "8001", "decision", "", "회수기 범위 값 3 칸 으로 정했다",
		"회수기 범위 값 3 칸 으로 정했다 첫 문장.\n회수기 범위 는 3 칸 둘째 문장.\n")
	first := t.TempDir()
	if err := runMake2([]string{"-store", dir, "-out", first, "-set", "measure", "-quota", "same=1"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	used := filepath.Join(first, "measure-draft.jsonl")
	second := t.TempDir()
	var stdout bytes.Buffer
	if err := runMake2([]string{"-store", dir, "-out", second, "-set", "measure", "-quota", "same=1", "-avoid", used}, &stdout); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(used)
	b, _ := os.ReadFile(filepath.Join(second, "measure-draft.jsonl"))
	if !strings.Contains(string(a), "첫 문장") || !strings.Contains(string(b), "둘째 문장") {
		t.Fatalf("다른 문장을 안 골랐다:\n%s\n%s", a, b)
	}
	if !strings.Contains(stdout.String(), "avoid: ") {
		t.Fatalf("MANIFEST 에 avoid 가 없다:\n%s", stdout.String())
	}
}

// -avoid 가 한 기억의 문장을 다 거르면 그 기억은 무관 쌍 근거로 안 쓴다 — 빈 근거 쌍 없이 쌍 수만 준다.
func TestUnrelatedSkipsFullyAvoidedEvidence(t *testing.T) {
	pool := []memory{
		{id: "a", scope: "s", summary: "회수기 범위 를 정했다", sentences: []string{"회수기 범위 는 세 칸 이다."}},
		{id: "b", scope: "s", summary: "빌드 시간 을 줄였다", sentences: []string{"빌드 시간 은 십 초 다."}},
	}
	plain := unrelatedCandidates(pool, newLedger(false), false, 3, make2Opts{})
	if len(plain) != 2 {
		t.Fatalf("avoid 없이 2 쌍(a→b · b→a)이어야 한다: %d", len(plain))
	}
	avoid := map[string]bool{"회수기 범위 는 세 칸 이다.": true}
	got := unrelatedCandidates(pool, newLedger(false), false, 3, make2Opts{avoid: avoid})
	if len(got) != 1 {
		t.Fatalf("avoid 뒤 1 쌍(b→a)이어야 한다: %d", len(got))
	}
	for _, c := range got {
		if c.Evidence == "" || avoid[c.Evidence] {
			t.Fatalf("빈 근거나 avoid 문장이 나왔다: %+v", c)
		}
	}
}

// long 은 본문 전체가 근거라 -avoid 가 안 먹힌다 — 둘을 같이 주면 오류다.
func TestMake2AvoidWithLongQuotaFails(t *testing.T) {
	dir := t.TempDir()
	writeRaw(t, dir, "8101", "decision", "", "회수기 범위 값 3 칸 으로 정했다", "회수기 범위 값 3 칸 으로 정했다 첫 문장.\n")
	used := filepath.Join(t.TempDir(), "used.jsonl")
	if err := os.WriteFile(used, []byte(`{"evidence":"x"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runMake2([]string{"-store", dir, "-out", t.TempDir(), "-set", "measure", "-quota", "long=2,same=1", "-avoid", used}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "long") {
		t.Fatalf("avoid + long 이 오류가 아니다: %v", err)
	}
	if err := runMake2([]string{"-store", dir, "-out", t.TempDir(), "-set", "measure", "-quota", "long=0,same=1", "-avoid", used}, &bytes.Buffer{}); err != nil {
		t.Fatalf("long=0 은 받아야 한다: %v", err)
	}
}
