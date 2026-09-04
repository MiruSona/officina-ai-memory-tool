package index

import (
	"fmt"
	"testing"
)

// v0.4 리뷰 A R2 — 큐를 승격하며 통째로 다시 만든 회차는 빈 쪽이 남는다.
// `index --full` 이 끝날 때 그것을 도로 내놔야 파일 크기가 「깨끗이 다시 만든
// 값」과 같아진다. 20k 에서 60.05MiB(자 밖) ↔ 58.28MiB(자 안)의 차이다.
func TestFullRebuildLeavesNoFreePages(t *testing.T) {
	opened := newStore(t)
	for at := 0; at < 60; at++ {
		body := fmt.Sprintf("본문 %d 이다. 줄이 여럿이라 조각이 는다.\n둘째 줄 %d.\n셋째 줄 %d.", at, at, at)
		if _, err := opened.WriteAdd(addRequest(longSummary, body)); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Run(Options{Store: opened, GC: settings().GC, Secret: settings().Secret,
		Quiet: true, Full: true})
	if err != nil {
		t.Fatalf("색인이 죽었다 : %v", err)
	}
	if result.Added == 0 {
		t.Fatal("승격된 것이 없다 — 시험 자료가 잘못됐다")
	}
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	free := 0
	if err := database.SQL().QueryRow("PRAGMA freelist_count").Scan(&free); err != nil {
		t.Fatal(err)
	}
	if free != 0 {
		t.Fatalf("전체 색인 뒤에도 빈 쪽이 %d 개 남았다", free)
	}
}

// v0.4 리뷰 A R2 — 승격이 끝나 큐 파일이 archive 로 간 이름은 「이미 승격했다」
// 표시를 남기지 않는다. 남기면 20k 이전 뒤 index.db 가 1.8MB 커진다.
func TestArchivedQueueLeavesNoSeenRow(t *testing.T) {
	opened := newStore(t)
	for at := 0; at < 30; at++ {
		body := fmt.Sprintf("본문 %d 이다.\n둘째 줄 %d.\n셋째 줄 %d.", at, at, at)
		if _, err := opened.WriteAdd(addRequest(longSummary, body)); err != nil {
			t.Fatal(err)
		}
	}
	result := runIndex(t, opened)
	if result.Added == 0 {
		t.Fatal("승격된 것이 없다 — 시험 자료가 잘못됐다")
	}
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	left := 0
	if err := database.SQL().QueryRow("SELECT COUNT(*) FROM inbox_seen").Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("승격 표시가 %d 줄 남았다", left)
	}
}
