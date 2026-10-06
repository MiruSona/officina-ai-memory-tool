package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// B-05 · B08 의 명령 쪽 배선 시험이다 (`mem eval --rule-sample` · `--rule-score` · add 의 B08 거절).

const b08Rule = "summary-body-match"

// addGap 은 요약 이름씨가 본문에 하나도 없는 결정(받친 비율 0)을 하나 넣는다.
func addGap(t *testing.T, at int, extra ...string) (string, int) {
	t.Helper()
	argv := append([]string{"add", "--type", "decision", "--scope", "mem-search", "--new",
		// 제목에 숫자를 쓰면 `과일 결정 10` 이 `과일 결정 1` 에 닮은 제목으로 붙어(한 절로 합침) 기억이 9건에서 멈춘다.
		"--tags", "index,korean", "--title", fmt.Sprintf("과일 결정 %s", gapCode(at)),
		"--sources", "file:README.md",
		"--summary", fmt.Sprintf("사과%d 포도%d 수박%d 참외%d 딸기%d 자두%d 앵두%d 살구를 쓰기로 정했다", at, at, at, at, at, at, at),
		"--body", fmt.Sprintf("전혀 다른 이야기를 적은 본문 %d 번이다.\n두 번째 줄도 다른 이야기다.\n세 번째 줄.", at)},
		extra...)
	return captureBoth(t, func() int { return run(argv) })
}

// gapCode 는 서로 겹치는 앞머리가 없는 제목 표지를 만든다 (1→"가가", 2→"나가" …).
func gapCode(at int) string {
	letters := []rune("가나다라마바사아자차카타")
	return string(letters[at%len(letters)]) + string(letters[(at/len(letters))%len(letters)]) + "형"
}

func seedGaps(t *testing.T, count int) {
	t.Helper()
	for at := 1; at <= count; at++ {
		if out, code := addGap(t, at); code != exitOK {
			t.Fatalf("add %d 실패 (%d) : %s", at, code, out)
		}
	}
}

func readSample(t *testing.T, path string) sampleFile {
	t.Helper()
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	file := sampleFile{}
	if err := yaml.Unmarshal(text, &file); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestRuleSampleWritesAndRepeats(t *testing.T) {
	newRepo(t)
	seedGaps(t, 30)
	first, second := filepath.Join(t.TempDir(), "a.json"), filepath.Join(t.TempDir(), "b.yaml")
	for _, out := range []string{first, second} {
		got, code := capture(t, func() int {
			return run([]string{"eval", "--rule-sample", b08Rule, "--n", "10", "--seed", "3", "--out", out})
		})
		if code != exitOK {
			t.Fatalf("표본 실패 (%d) : %s", code, got)
		}
	}
	a, b := readSample(t, first), readSample(t, second)
	if len(a.Items) != 10 || a.Total < 10 {
		t.Fatalf("10건 · 울림 14 여야 한다 : %d · %d", len(a.Items), a.Total)
	}
	for at := range a.Items {
		if a.Items[at].ID != b.Items[at].ID || a.Items[at].Label != "" || a.Items[at].View == "" {
			t.Fatalf("씨앗이 같으면 같은 목록 · 빈 label · view 가 있어야 한다 : %+v", a.Items[at])
		}
	}
	// 이미 있는 파일은 덮지 않는다.
	before, _ := os.ReadFile(first)
	_, code := captureBoth(t, func() int {
		return run([]string{"eval", "--rule-sample", b08Rule, "--seed", "9", "--out", first})
	})
	after, _ := os.ReadFile(first)
	if code == exitOK || string(before) != string(after) {
		t.Fatalf("있는 파일을 덮었다 (%d)", code)
	}
}

func TestRuleSampleTooFew(t *testing.T) {
	newRepo(t)
	seedGaps(t, 3)
	out := filepath.Join(t.TempDir(), "few.yaml")
	got, code := capture(t, func() int { return run([]string{"eval", "--rule-sample", b08Rule, "--out", out}) })
	if code != exitCheck || !strings.Contains(got, "못 잼") {
		t.Fatalf("못 잼이어야 한다 (%d) : %s", code, got)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("10건 미만인데 파일을 썼다")
	}
}

func TestRuleScoreCountsLabels(t *testing.T) {
	memory := newRepo(t)
	seedGaps(t, 30)
	path := filepath.Join(t.TempDir(), "s.json")
	if got, code := capture(t, func() int {
		return run([]string{"eval", "--rule-sample", b08Rule, "--n", "12", "--out", path})
	}); code != exitOK {
		t.Fatalf("표본 실패 : %s", got)
	}
	file := readSample(t, path)
	labels := []string{"진짜", "true", "진짜", "border", "진짜", "진짜", "진짜", "진짜", "진짜", "진짜", "진짜", ""}
	for at := range file.Items {
		file.Items[at].Label = labels[at]
	}
	scored := filepath.Join(t.TempDir(), "scored.json")
	if err := writeSampleFile(scored, file); err != nil {
		t.Fatal(err)
	}
	got, code := capture(t, func() int { return run([]string{"eval", "--rule-score", scored}) })
	if code != exitOK || !strings.Contains(got, "정밀도 1.000") || !strings.Contains(got, "안 판정 1") ||
		!strings.Contains(got, "통과") || !strings.Contains(got, "대조군 : 못 봄") {
		t.Fatalf("통과여야 한다 (%d) : %s", code, got)
	}
	// 오탐 하나 + 본문이 바뀐 항목 하나 → 정밀도 미달 · 다시 판정 1.
	file.Items[0].Label = "오탐"
	file.Items[1].Hash = "0000000000000000"
	changed := filepath.Join(t.TempDir(), "changed.yaml")
	if err := writeSampleFile(changed, file); err != nil {
		t.Fatal(err)
	}
	got, code = capture(t, func() int { return run([]string{"eval", "--rule-score", changed}) })
	if code != exitCheck || !strings.Contains(got, "다시 판정 1") || !strings.Contains(got, "미달") {
		t.Fatalf("미달 · 다시 판정 1 이어야 한다 (%d) : %s", code, got)
	}
	_ = memory
}

// TestRuleScoreRehashAfterFileEdit 는 판정 뒤 저장소 .md 본문을 직접 고치면
// 그 항목이 「다시 판정」 으로 세지고 진짜/오탐 셈에서 빠지는지 본다.
// (mem set --body 는 inbox 큐에만 들어가 mem index 전엔 파일이 안 바뀌므로 파일을 직접 고친다.)
func TestRuleScoreRehashAfterFileEdit(t *testing.T) {
	memory := newRepo(t)
	seedGaps(t, 30)
	path := filepath.Join(t.TempDir(), "s.yaml")
	if got, code := capture(t, func() int {
		return run([]string{"eval", "--rule-sample", b08Rule, "--n", "12", "--out", path})
	}); code != exitOK {
		t.Fatalf("표본 실패 : %s", got)
	}
	file := readSample(t, path)
	if len(file.Items) != 12 {
		t.Fatalf("12건이어야 한다 : %d", len(file.Items))
	}
	// 고칠 항목은 오탐으로 적는다 — 셈에서 빠지면 오탐 0 이 나와야 한다.
	for at := range file.Items {
		file.Items[at].Label = "진짜"
	}
	target := file.Items[0]
	file.Items[0].Label = "오탐"
	scored := filepath.Join(t.TempDir(), "scored.yaml")
	if err := writeSampleFile(scored, file); err != nil {
		t.Fatal(err)
	}

	// 저장소에서 그 기억 파일을 찾아 머리말은 두고 본문 끝에 한 줄 덧붙인다.
	found := ""
	walkErr := filepath.WalkDir(filepath.Join(memory, "store"), func(p string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		text, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.Contains(string(text), "id: "+target.ID) {
			found = p
		}
		return nil
	})
	if walkErr != nil || found == "" {
		t.Fatalf("기억 파일을 못 찾았다 (%s) : %v", target.ID, walkErr)
	}
	before, err := os.ReadFile(found)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.TrimRight(string(before), "\n") + "\n판정 뒤에 덧붙인 줄이다.\n"
	if err := os.WriteFile(found, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	got, code := capture(t, func() int { return run([]string{"eval", "--rule-score", scored}) })
	want := "표본 12건 · 진짜 11 · 오탐 0 · 경계 0 · 다시 판정 1 · 안 판정 0 · 정밀도 1.000"
	if code != exitOK || !strings.Contains(got, want) || !strings.Contains(got, "통과") {
		t.Fatalf("다시 판정 1 · 오탐 0 · 통과여야 한다 (%d) : %s", code, got)
	}
	t.Logf("%s", strings.TrimSpace(got))
}

func TestRuleScoreBrokenFile(t *testing.T) {
	newRepo(t)
	broken := filepath.Join(t.TempDir(), "broken.yaml")
	if err := os.WriteFile(broken, []byte("items: [\n  - : :"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, code := captureBoth(t, func() int { return run([]string{"eval", "--rule-score", broken}) })
	if code != exitUsage {
		t.Fatalf("사용법/입력 오류여야 한다 : %d", code)
	}
}

func TestAddB08RejectOnlyWhenOn(t *testing.T) {
	memory := newRepo(t)
	if out, code := addGap(t, 1); code != exitOK {
		t.Fatalf("꺼진 저장소는 기존처럼 받아야 한다 (%d) : %s", code, out)
	}
	toml := "schema = 1\nname = \"시험\"\n\n[quality]\nsummary_body_reject = true\n"
	if err := os.WriteFile(filepath.Join(memory, "mem.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := addGap(t, 2); code != exitCheck {
		t.Fatalf("켜면 거절이어야 한다 (%d) : %s", code, out)
	}
	if out, code := addGap(t, 3, "--pin"); code != exitOK || !strings.Contains(out, b08Rule) {
		t.Fatalf("--pin 이면 경고로 통과해야 한다 (%d) : %s", code, out)
	}
}
