package index

// 뒷정리-2b 회귀 — 보류 기억(머리말 review: true)은 훅 절에 안 실린다 (결정 6).
// 사람이 안 본 글을 세션 머리에 밀어 넣지 않는다.

import (
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

func TestHookSkipsHeldMemories(t *testing.T) {
	opened := newStore(t)
	held := sampleMemory("20260823-a1a11111")
	held.Review = true
	writeMemory(t, opened.Dir, held)
	plain := sampleMemory("20260823-b2b22222")
	plain.Body = "다른 본문 한 줄. 보류가 아닌 기억이다."
	writeMemory(t, opened.Dir, plain)
	runIndex(t, opened)

	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	rows, err := database.HookRows(HookPick{Types: []string{model.TypeDecision}, Shape: ShapePlain, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == held.ID {
			t.Fatal("보류 기억이 훅 절에 실렸다")
		}
	}
	if len(rows) != 1 {
		t.Fatalf("보통 기억 한 건만 남아야 한다 : %d건", len(rows))
	}
}

// 보류 add 는 제목이 닮은 기존 기억이 있어도 거기 안 붙는다 (뒷정리-3).
// 합쳐지면 검토 없이 그 기존 기억을 통해 뜨기 때문이다.
func TestHoldAddDoesNotMergeIntoTwin(t *testing.T) {
	opened := newStore(t)
	if _, err := opened.WriteAdd(addRequest(longSummary, "첫 본문")); err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	held := addRequest(longSummary, "보류로 들어온 두 번째 본문")
	held.Review = true
	if _, err := opened.WriteAdd(held); err != nil {
		t.Fatal(err)
	}
	result := runIndex(t, opened)
	if result.Appended != 0 || result.Added != 1 {
		t.Fatalf("보류는 안 붙고 새 파일로 가야 한다 : %+v", result)
	}
	files, _ := opened.ListMemories()
	if len(files) != 2 {
		t.Fatalf("파일이 둘이어야 한다 : %v", files)
	}

	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	rows, err := database.ListRows(Filter{}, 10, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("보류는 기본 목록에 안 떠야 한다 (첫 기억 한 건만) : %d건", len(rows))
	}
}

// 검색 필터도 같은 규칙이다 — 기본은 빼고 IncludeHeld 로만 연다.
func TestListRowsSkipsHeldUnlessAsked(t *testing.T) {
	opened := newStore(t)
	held := sampleMemory("20260823-c3c33333")
	held.Review = true
	writeMemory(t, opened.Dir, held)
	plain := sampleMemory("20260823-d4d44444")
	plain.Body = "다른 본문 한 줄. 보류가 아닌 기억이다."
	writeMemory(t, opened.Dir, plain)
	runIndex(t, opened)

	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	now := time.Now()
	rows, err := database.ListRows(Filter{}, 10, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("기본 목록에 보류가 섞였다 : %d건", len(rows))
	}
	rows, err = database.ListRows(Filter{IncludeHeld: true}, 10, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("--include-held 인데 두 건이 아니다 : %d건", len(rows))
	}
}
