package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 무효화 전파 — `mem set --by` 의 알림 한 줄, `mem show` 의 역참조 절,
// `mem review --kind basis` 큐 (설계 `2026-08-30-무효화전파설계.md`).

// basisMemory 는 규격을 다 갖춘 기억 하나다. 시험이 보려는 칸만 뒤에 바꾼다.
func basisMemory(id, kind, title string) *model.Memory {
	return &model.Memory{ID: id, Type: kind, Date: "2026-08-22", Spec: model.SpecV2,
		Title:   title,
		Summary: "훅이 밀어 넣는 글은 글자 수가 아니라 UTF-8 바이트로 재야 한다. 한글은 한 글자가 세 바이트다",
		Tags:    []string{"index", "search"}, Scope: "aimemorytool", Author: "human:tester",
		Sources: []string{"file:internal/index/schema.go"},
		Body:    "훅 상한은 바이트로 잰다.\n글자 수로 재면 한글에서 세 배로 틀린다.\n넘치면 우리가 먼저 자른다."}
}

// putMemory 는 store/ 에 기억 파일 하나를 놓는다. 승격을 안 거치므로 이 시험이
// 보려는 것만 좁혀서 볼 수 있다.
func putMemory(t *testing.T, memory string, one *model.Memory) string {
	t.Helper()
	path := storeFile(memory, one.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, model.Encode(one), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustRun(t *testing.T, argv ...string) string {
	t.Helper()
	out, code := capture(t, func() int { return run(argv) })
	if code != 0 {
		t.Fatalf("%v 가 실패했다 (%d) : %s", argv, code, out)
	}
	return out
}

// set --by 는 덮인 기억을 근거로 삼은 기억이 몇 건인지 알린다. 0건이면 안 찍고,
// 색인이 없으면 0건이라고 말하지 않는다.
func TestSetByTellsUsedByCount(t *testing.T) {
	memory := newRepo(t)
	base := basisMemory("20260822-aaaa0001", model.TypeDecision, "공유기를 모델 M 으로 바꾼다")
	user := basisMemory("20260822-aaaa0002", model.TypeFact, "옛 회선 속도")
	user.Sources = []string{model.SourceMem + base.ID}
	lonely := basisMemory("20260822-aaaa0003", model.TypeDecision, "아무도 안 쓰는 결정")
	for _, one := range []*model.Memory{base, user, lonely} {
		putMemory(t, memory, one)
	}
	// 색인을 아직 안 만들었으면 「못 셌다」다. 0건이라고 말하면 안 된다.
	out := mustRun(t, "set", base.ID, "--by", "20260830-bbbb0001")
	if !strings.Contains(out, "못 셌다") {
		t.Fatalf("색인이 없는데 건수를 말했다 : %s", out)
	}
	mustRun(t, "index")
	out = mustRun(t, "set", base.ID, "--by", "20260830-bbbb0001")
	if !strings.Contains(out, "1건") || !strings.Contains(out, user.ID) {
		t.Fatalf("근거로 삼은 기억을 안 알려줬다 : %s", out)
	}
	if !strings.Contains(out, "mem review --kind basis") {
		t.Fatalf("다음에 칠 명령이 없다 : %s", out)
	}
	out = mustRun(t, "set", lonely.ID, "--by", "20260830-bbbb0002")
	if strings.Contains(out, "근거로 삼은 기억") {
		t.Fatalf("0건인데 줄이 나왔다 : %s", out)
	}
}

// show --json 은 used_by 를 준다. **원본 md 에는 그 칸이 안 생긴다** — 파생값을
// 머리말에 흘리면 언젠가 파일에 샌다.
func TestShowUsedByStaysOutOfFile(t *testing.T) {
	memory := newRepo(t)
	base := basisMemory("20260822-cccc0001", model.TypeDecision, "공유기를 모델 M 으로 바꾼다")
	user := basisMemory("20260822-cccc0002", model.TypeFact, "옛 회선 속도")
	user.Sources = []string{model.SourceMem + base.ID}
	linked := basisMemory("20260822-cccc0003", model.TypeHistory, "미니PC 설치 기록")
	linked.Links = []string{base.ID}
	path := putMemory(t, memory, base)
	for _, one := range []*model.Memory{user, linked} {
		putMemory(t, memory, one)
	}
	mustRun(t, "index")

	out := mustRun(t, "show", base.ID)
	if !strings.Contains(out, "나를 근거로 삼은 기억 (2건)") {
		t.Fatalf("역참조 절이 없다 : %s", out)
	}
	if !strings.Contains(out, "근거") || !strings.Contains(out, "링크") {
		t.Fatalf("근거와 링크를 안 갈랐다 : %s", out)
	}

	out = mustRun(t, "show", base.ID, "--json")
	shown := map[string]any{}
	if err := json.Unmarshal([]byte(out), &shown); err != nil {
		t.Fatalf("JSON 이 아니다 : %v (%s)", err, out)
	}
	rows, ok := shown["used_by"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("used_by 가 두 건이 아니다 : %v", shown["used_by"])
	}
	if shown["id"] != base.ID {
		t.Fatalf("기억 칸이 사라졌다 : %v", shown)
	}

	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(text), "used_by") {
		t.Fatalf("파생값이 파일에 샜다 : %s", text)
	}

	// 역참조가 없는 기억은 절을 통째로 안 찍는다.
	if out := mustRun(t, "show", user.ID); strings.Contains(out, "나를 근거로 삼은 기억") {
		t.Fatalf("0건인데 절이 나왔다 : %s", out)
	}
}

// 골든 시나리오 — 사람이 한 칸 판정하면 연쇄가 한 칸 굴러간다 (설계 8절).
func TestBasisChainRollsOneStepAtATime(t *testing.T) {
	memory := newRepo(t)
	a := basisMemory("20260822-dddd000a", model.TypeDecision, "공유기는 모델 M 이다")
	b := basisMemory("20260822-dddd000b", model.TypeFact, "옛 회선 속도")
	b.Sources = []string{model.SourceMem + a.ID}
	c := basisMemory("20260822-dddd000c", model.TypeHistory, "미니PC 설치 기록")
	c.Links = []string{b.ID}
	for _, one := range []*model.Memory{a, b, c} {
		putMemory(t, memory, one)
	}
	mustRun(t, "index")

	// 1) A 가 죽는다. B 가 큐에 오고 C 는 안 온다 — 2단이라 안 본다.
	out := mustRun(t, "set", a.ID, "--by", "20260830-dddd000d")
	if !strings.Contains(out, b.ID) {
		t.Fatalf("A 를 근거로 삼은 B 를 안 알려줬다 : %s", out)
	}
	mustRun(t, "index")
	out = mustRun(t, "review", "--kind", "basis")
	if !strings.Contains(out, b.ID) {
		t.Fatalf("B 가 큐에 없다 : %s", out)
	}
	if strings.Contains(out, c.ID) {
		t.Fatalf("2단인 C 가 큐에 떴다 : %s", out)
	}

	// 2) 사람이 B 를 덮으면 그때 C 가 1단 대상이 된다.
	mustRun(t, "set", b.ID, "--by", "20260830-dddd000e")
	mustRun(t, "index")
	out = mustRun(t, "review", "--kind", "basis")
	if !strings.Contains(out, c.ID) {
		t.Fatalf("연쇄가 한 칸 안 굴렀다 : %s", out)
	}

	// 3) 「그대로 둠」은 stale_after 다. 그날까지 큐에서 빠진다.
	mustRun(t, "set", c.ID, "--stale-after", "2027-01-01")
	mustRun(t, "index")
	out = mustRun(t, "review", "--kind", "basis")
	if strings.Contains(out, c.ID) {
		t.Fatalf("미룬 기억이 큐에 그대로다 : %s", out)
	}
}
