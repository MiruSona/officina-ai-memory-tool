package main

// 모음 기억 B1 — mem consolidate --plan/--apply, 검색 자리(근거 줄·[낡음]),
// origin: card 되돌리기 (자동쌓기설계 3절). 벡터 없이 돈다.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// b1Repo 는 덮음 사슬 하나(셋)와 따로 선 기억 하나를 둔 저장소다.
func b1Repo(t *testing.T) (string, []*model.Memory) {
	t.Helper()
	memory := newRepo(t)
	first := basisMemory("20260822-b1000001", model.TypeDecision, "공유기를 모델 L 로 쓴다")
	second := basisMemory("20260825-b1000002", model.TypeDecision, "공유기를 모델 M 으로 바꾼다")
	third := basisMemory("20260830-b1000003", model.TypeDecision, "공유기를 모델 N 으로 다시 바꾼다")
	first.Date, second.Date, third.Date = "2026-08-22", "2026-08-25", "2026-08-30"
	first.SupersededBy, first.InvalidAt = second.ID, "2026-08-25"
	second.SupersededBy, second.InvalidAt = third.ID, "2026-08-30"
	alone := basisMemory("20260830-b1000004", model.TypeFact, "옛 회선 속도")
	all := []*model.Memory{first, second, third, alone}
	for _, one := range all {
		putMemory(t, memory, one)
	}
	mustRun(t, "index")
	return memory, all
}

func TestConsolidatePlanWritesNothing(t *testing.T) {
	memory, all := b1Repo(t)
	out := mustRun(t, "consolidate", "--plan")
	if !strings.Contains(out, "묶음 1개") || !strings.Contains(out, "새로") || !strings.Contains(out, all[0].ID) {
		t.Fatalf("계획 표가 틀리다 :\n%s", out)
	}
	if !strings.Contains(out, "벡터가 없다") {
		t.Fatalf("벡터 없이 의미 무리를 건너뛴다고 안 알렸다 :\n%s", out)
	}
	if found := mustRun(t, "search", "공유기"); strings.Contains(found, "observation") {
		t.Fatalf("--plan 이 카드를 썼다 :\n%s", found)
	}
	_ = memory
	llm := mustRun(t, "consolidate", "--llm")
	if !strings.Contains(llm, "B2") {
		t.Fatalf("--llm 자리 안내가 없다 : %s", llm)
	}
	if _, code := capture(t, func() int { return run([]string{"consolidate", "--rule", "topic"}) }); code != exitUsage {
		t.Fatal("모르는 규칙을 받았다")
	}
}

func TestConsolidateApplyStaleUndo(t *testing.T) {
	memory, all := b1Repo(t)
	out := mustRun(t, "consolidate", "--apply", "--json")
	report := consolidateReport{}
	if err := json.Unmarshal([]byte(out[strings.Index(out, "{"):]), &report); err != nil {
		t.Fatalf("JSON 이 아니다 : %v\n%s", err, out)
	}
	if len(report.Written) != 1 {
		t.Fatalf("카드 한 장을 써야 한다 : %+v", report)
	}
	card := report.Written[0]
	raw := readMemoryFile(t, memory, card)
	for _, want := range []string{"type: observation", "origin: card", "basis_hash:", "rev: 1",
		"mem:" + all[0].ID, "[mem:" + all[2].ID + "]"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("카드에 %q 가 없다 :\n%s", want, raw)
		}
	}
	found := mustRun(t, "search", "공유기", "--limit", "10")
	if !strings.Contains(found, card) || !strings.Contains(found, "↳ "+all[0].ID) {
		t.Fatalf("카드와 근거 줄이 검색에 안 보인다 :\n%s", found)
	}
	if strings.Contains(found, "[낡음]") {
		t.Fatalf("막 쓴 카드가 낡았다 :\n%s", found)
	}
	// 머리말의 「상위 N건」 은 근거 줄을 안 센다.
	answers := 0
	for _, line := range strings.Split(found, "\n") {
		if strings.HasPrefix(line, "| ") && len(line) > 2 && line[2] >= '1' && line[2] <= '9' {
			answers++
		}
	}
	if !strings.Contains(found, fmt.Sprintf("상위 %d건", answers)) {
		t.Fatalf("머리말 건수가 답 줄 수(%d)와 다르다 :\n%s", answers, found)
	}
	// 다시 계획하면 그대로다.
	if again := mustRun(t, "consolidate", "--plan"); !strings.Contains(again, "그대로 1") {
		t.Fatalf("같은 묶음을 다시 쓰려 한다 :\n%s", again)
	}
	// 근거 밖 기억이 머리를 덮으면 낡는다.
	newest := basisMemory("20260905-b1000005", model.TypeDecision, "공유기는 모델 P 로 정한다")
	newest.Date = "2026-09-05"
	putMemory(t, memory, newest)
	mustRun(t, "index")
	mustRun(t, "set", all[2].ID, "--by", newest.ID)
	found = mustRun(t, "search", "공유기", "--limit", "10")
	if !strings.Contains(found, "[낡음]") {
		t.Fatalf("근거가 덮였는데 [낡음] 이 없다 :\n%s", found)
	}
	plan := mustRun(t, "consolidate", "--plan")
	if !strings.Contains(plan, "다시") || !strings.Contains(plan, card) {
		t.Fatalf("낡은 카드를 다시 쓰기로 안 잡았다 :\n%s", plan)
	}
	mustRun(t, "consolidate", "--apply")
	raw = readMemoryFile(t, memory, card)
	if !strings.Contains(raw, "rev: 2") || !strings.Contains(raw, "[mem:"+newest.ID+"]") {
		t.Fatalf("다시 쓴 카드가 판·근거를 안 고쳤다 :\n%s", raw)
	}
	if found = mustRun(t, "search", "공유기", "--limit", "10"); strings.Contains(found, "[낡음]") {
		t.Fatalf("다시 쓴 뒤에도 낡았다 :\n%s", found)
	}
	// 카드를 한꺼번에 되돌린다.
	mustRun(t, "auto", "undo", "--origin", "card", "--apply")
	if found = mustRun(t, "search", "공유기", "--limit", "10"); strings.Contains(found, card) {
		t.Fatalf("되돌린 카드가 검색에 뜬다 :\n%s", found)
	}
	if !strings.Contains(readMemoryFile(t, memory, card), "review: true") {
		t.Fatal("되돌린 카드는 보류로 남아야 한다")
	}
	mustRun(t, "auto", "redo", "--apply")
	if found = mustRun(t, "search", "공유기", "--limit", "10"); !strings.Contains(found, card) {
		t.Fatalf("되살린 카드가 안 뜬다 :\n%s", found)
	}
}

// TestSearchWithoutObservationHasNoBasisRows 는 모음 기억이 없는 저장소의 검색
// 결과에 B1 표시(근거 줄·[낡음]·JSON 새 칸)가 하나도 안 생기는지 본다.
func TestSearchWithoutObservationHasNoBasisRows(t *testing.T) {
	b1Repo(t)
	found := mustRun(t, "search", "공유기", "--limit", "10")
	if strings.Contains(found, "↳") || strings.Contains(found, "[낡음]") {
		t.Fatalf("모음 기억이 없는데 B1 표시가 나왔다 :\n%s", found)
	}
	raw := mustRun(t, "search", "공유기", "--json")
	for _, key := range []string{"\"basis\"", "\"stale\"", "\"obs_loose\""} {
		if strings.Contains(raw, key) {
			t.Fatalf("JSON 에 새 칸 %s 가 샜다 :\n%s", key, raw)
		}
	}
}
