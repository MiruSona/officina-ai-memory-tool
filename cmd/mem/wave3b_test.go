package main

// 3회차 물결 3 갈래 3B 회귀 — review --promote · migrate 부분 이전 ·
// tags --suggest · status 의 --new 비율.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/install"
)

// putRaw 는 기억 파일 하나를 store/ 에 그대로 놓는다.
func putRaw(t *testing.T, memory, id, head, body string) {
	t.Helper()
	dir := filepath.Join(memory, "store", "2026", "01")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	text := "---\n" +
		"id: " + id + "\n" +
		"type: decision\n" +
		"title: 훅 주입 상한을 바이트로 잰다\n" +
		"summary: 훅이 밀어 넣는 글은 글자 수가 아니라 UTF-8 바이트로 재야 한다. 한글은 한 글자가 세 바이트다\n" +
		"tags: [index, korean]\n" +
		"scope: mem-search\n" +
		"date: 2026-01-05\n" +
		"author: mem/0.3.0\n" +
		"sources: [\"file:internal/hook/hook.go\"]\n" +
		head +
		"---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPromoteNeedsHeldMemory 는 승격이 기다리는 기억에만 듣는지 본다 (결정 6).
func TestPromoteNeedsHeldMemory(t *testing.T) {
	memory := newRepo(t)
	putRaw(t, memory, "20260105-aaaa1111", "review: true\n", "본문 한 줄.\n")
	putRaw(t, memory, "20260105-bbbb2222", "", "본문 한 줄.\n")

	if _, code := capture(t, func() int { return run([]string{"review", "--promote"}) }); code != exitUsage {
		t.Fatalf("id 없는 --promote 는 1 이어야 한다 : %d", code)
	}
	if _, code := capture(t, func() int {
		return run([]string{"review", "--promote", "20260105-bbbb2222"})
	}); code != exitUsage {
		t.Fatalf("기다리는 중이 아닌 기억은 거절이어야 한다 : %d", code)
	}
	out, code := capture(t, func() int {
		return run([]string{"review", "--promote", "20260105-aaaa1111"})
	})
	if code != exitOK {
		t.Fatalf("승격이 실패했다 : %d (%s)", code, out)
	}
	if !strings.Contains(out, "승격했다") {
		t.Fatalf("승격했다는 말이 없다 : %s", out)
	}
	// store/ 는 안 건드리고 큐에만 넣는다 (불변조건 2).
	if queuedCount(t, memory) != 1 {
		t.Fatal("승격이 큐에 안 들어갔다")
	}
	raw, err := os.ReadFile(filepath.Join(memory, "store", "2026", "01", "20260105-aaaa1111.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "review: true") {
		t.Fatal("승격이 store/ 를 바로 고쳤다")
	}
}

// TestPromoteIsOutsideAllowRules 는 auto 모드 규칙이 --promote 를 안 통과시키는지
// 본다 (결정 60). 넓은 `Bash(mem review:*)` 한 줄이면 AI 가 승인 없이 승격한다.
func TestPromoteIsOutsideAllowRules(t *testing.T) {
	for _, rule := range install.AllowRules {
		if rule == "Bash(mem review:*)" {
			t.Fatal("넓은 review 규칙이 그대로다 (--promote 가 승인 없이 통과한다)")
		}
	}
	want := map[string]bool{"Bash(mem review)": false, "Bash(mem review --kind:*)": false}
	for _, rule := range install.AllowRules {
		if _, found := want[rule]; found {
			want[rule] = true
		}
	}
	for rule, found := range want {
		if !found {
			t.Errorf("%s 규칙이 없다", rule)
		}
	}
	retired := false
	for _, rule := range install.RetiredAllowRules {
		if rule == "Bash(mem review:*)" {
			retired = true
		}
	}
	if !retired {
		t.Error("옛 넓은 규칙이 RetiredAllowRules 에 없어 --undo 때 안 빠진다")
	}
}

// TestMigratePartialMovesOtherFields 는 태그 한 칸이 막혀도 나머지 칸이
// 옮겨지는지 본다 (조사 E #6 · 실기억 49건이 이렇게 버려졌다).
func TestMigratePartialMovesOtherFields(t *testing.T) {
	memory := newRepo(t)
	id := "20260822-4444dddd"
	legacyMemory(t, memory, id, "impl", "mem")
	if _, code := capture(t, func() int { return run([]string{"migrate", "--apply"}) }); code != exitOK {
		t.Fatal("migrate --apply 가 실패했다")
	}
	if _, code := capture(t, func() int { return run([]string{"index", "--quiet"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	after := readStore(t, memory, id)
	// 근거는 옮겼다.
	if !strings.Contains(after, "sources:") {
		t.Fatalf("막힌 칸 하나 때문에 근거까지 버렸다 :\n%s", after)
	}
	// 규격은 안 올렸다. 올리면 태그 하한에 걸려 승격이 통째로 거절된다 (I7).
	if strings.Contains(after, "migrated: true") {
		t.Fatalf("막힌 칸이 있는데 규격을 올렸다 :\n%s", after)
	}
	if !strings.Contains(after, "source: ai") {
		t.Fatalf("옛 칸이 사라졌다 :\n%s", after)
	}
}

// TestMigrateKeepsSourceTagWhenDroppingWouldBlock 는 출처 태그를 떼면 하한을
// 못 채우는 기억에서 태그를 **안 떼고** 나머지를 다 옮기는지 본다.
func TestMigrateKeepsSourceTagWhenDroppingWouldBlock(t *testing.T) {
	memory := newRepo(t)
	id := "20260822-5555eeee"
	// ai 는 출처 태그라 vocab 이 거절한다. 떼면 impl 하나만 남아 하한 미달이다.
	legacyMemory(t, memory, id, "impl, ai", "mem")
	if _, code := capture(t, func() int { return run([]string{"migrate", "--apply"}) }); code != exitOK {
		t.Fatal("migrate --apply 가 실패했다")
	}
	if _, code := capture(t, func() int { return run([]string{"index", "--quiet"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	after := readStore(t, memory, id)
	if !strings.Contains(after, "migrated: true") {
		t.Fatalf("떼면 막히는 태그를 떼어 이전을 막았다 :\n%s", after)
	}
	if !strings.Contains(after, "author:") {
		t.Fatalf("author 를 안 옮겼다 :\n%s", after)
	}
}

// TestMigrateRefusesSecrets 는 비밀정보가 든 기억을 만나면 **한 건도 안 옮기고**
// 종료 코드 4 로 멈추는지 본다 (설계 10절 · P8).
func TestMigrateRefusesSecrets(t *testing.T) {
	memory := newRepo(t)
	legacyMemory(t, memory, "20260822-6666ffff", "design, impl", "mem")
	putLegacySecret(t, memory, "20260822-7777aaaa")
	before := readStore(t, memory, "20260822-6666ffff")
	_, code := capture(t, func() int { return run([]string{"migrate", "--apply"}) })
	if code != exitSecurity {
		t.Fatalf("비밀정보를 만나면 4 여야 한다 : %d", code)
	}
	if queuedCount(t, memory) != 0 {
		t.Fatal("멈췄다면서 큐에 넣었다")
	}
	if readStore(t, memory, "20260822-6666ffff") != before {
		t.Fatal("멈췄다면서 파일을 고쳤다")
	}
}

// putLegacySecret 은 본문에 비밀정보로 보이는 줄이 든 옛 규격 기억이다.
func putLegacySecret(t *testing.T, memory, id string) {
	t.Helper()
	text := "---\n" +
		"id: " + id + "\n" +
		"type: history\n" +
		"summary: 배포 열쇠를 본문에 적어 둔 옛 기억이다. 이전이 이것을 그대로 다시 쓰면 안 된다\n" +
		"tags: [design, impl]\nscope: mem\ndate: 2026-08-22\nsource: ai\n---\n\n" +
		"옛 규격 기억이다.\n\n" +
		"열쇠를 AKIAIOSFODNN7EXAMPLE 로 그대로 적어 뒀다.\n"
	path := filepath.Join(memory, "store", id[:4], id[4:6], id+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestTagsSuggestOnlyProposes 는 --suggest 가 후보를 내되 **아무것도 안 고치는지**
// 본다 (결정 40). 동의어 표는 잘못 넣으면 오탐이 폭발한다.
func TestTagsSuggestOnlyProposes(t *testing.T) {
	memory := newRepo(t)
	before, err := os.ReadFile(filepath.Join(memory, "vocab.toml"))
	if err != nil {
		t.Fatal(err)
	}
	// 괄호 표기가 두 번 나와야 후보가 된다 (한 번은 글쓴이 버릇이다).
	putRaw(t, memory, "20260105-cccc1111", "",
		"색인(indexing) 을 다시 만들었다.\n둘째 줄.\n셋째 줄이다.\n")
	putRaw(t, memory, "20260105-cccc2222", "",
		"색인(indexing) 판이 올라갔다.\n둘째 줄.\n셋째 줄이다.\n")
	out, code := capture(t, func() int { return run([]string{"tags", "--suggest"}) })
	if code != exitOK {
		t.Fatalf("--suggest 는 0 이어야 한다 : %d (%s)", code, out)
	}
	if !strings.Contains(out, "indexing → 색인") {
		t.Fatalf("괄호 표기 후보를 못 뽑았다 : %s", out)
	}
	if !strings.Contains(out, "제안만 한다") {
		t.Fatalf("제안뿐이라는 말이 없다 : %s", out)
	}
	after, err := os.ReadFile(filepath.Join(memory, "vocab.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("--suggest 가 vocab.toml 을 고쳤다")
	}
	if queuedCount(t, memory) != 0 {
		t.Fatal("--suggest 가 큐에 넣었다")
	}
}

// TestStatusShowsNewForceRate 는 `--new` 로 밀고 들어온 비율이 S05 옆에 뜨는지
// 본다 (결정 39 · C04 되돌림 신호).
func TestStatusShowsNewForceRate(t *testing.T) {
	newRepo(t)
	if _, code := capture(t, func() int { return run(addArgs("첫 기억")) }); code != exitOK {
		t.Fatal("첫 add 가 실패했다")
	}
	if _, code := capture(t, func() int {
		return run(append(addArgs("둘째 기억"), "--new"))
	}); code != exitOK {
		t.Fatal("--new add 가 실패했다")
	}
	out, _ := capture(t, func() int { return run([]string{"status", "--quality"}) })
	if !strings.Contains(out, "new-force-rate") {
		t.Fatalf("--new 비율 줄이 없다 : %s", out)
	}
	if !strings.Contains(out, "0.50") {
		t.Fatalf("두 건 중 한 건이 --new 인데 0.50 이 아니다 : %s", out)
	}
}
