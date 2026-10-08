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
	got := makeCandidates(memories, 3, measureQuota, true)
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
	if counts[kindSupersede] != 6 || counts[kindSupersede]+counts[kindFlip] != measureContradict {
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
	a := fmt.Sprint(makeCandidates(first, 5, trainQuota, false))
	b := fmt.Sprint(makeCandidates(second, 5, trainQuota, false))
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
	got := flipOnly(memories, 1, 5)
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
	if fmt.Sprint(got) != fmt.Sprint(flipOnly(memories, 1, 5)) {
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
