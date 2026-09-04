package store

import (
	"os"
	"testing"
	"time"
)

// 리뷰 A #15 — 「읽기 전용 저장소에는 안 쓴다」 를 코드가 지킨다. show 가 읽어도
// 그 저장소의 local/hits.jsonl 에 한 글자도 안 쓴다.
func TestAppendHitRefusesReadOnlyStore(t *testing.T) {
	dir := t.TempDir()
	readOnly := Open(dir, true)
	if err := readOnly.AppendHit("show", "20260823-8d27dbfc"); !IsReadOnly(err) {
		t.Fatalf("읽기 전용 저장소에 셈을 남겼다 : %v", err)
	}
	if _, err := os.Stat(HitsPath(dir)); err == nil {
		t.Fatal("hits.jsonl 이 생겼다")
	}
	writable := Open(dir, false)
	if err := writable.AppendHit("show", "20260823-8d27dbfc"); err != nil {
		t.Fatalf("쓸 수 있는 저장소는 남겨야 한다 : %v", err)
	}
}

// 리뷰 A #20 — 명령 통계 줄(id 가 빈 줄)은 압축 문턱에 안 센다.
func TestCommandLinesDoNotCountTowardCompaction(t *testing.T) {
	dir := t.TempDir()
	opened := Open(dir, false)
	for at := 0; at < 5; at++ {
		if err := AppendHit(dir, "gc", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := opened.AppendHit("show", "20260823-8d27dbfc"); err != nil {
		t.Fatal(err)
	}
	totals, lines, err := ReadHits(dir)
	if err != nil {
		t.Fatal(err)
	}
	if lines != 1 {
		t.Fatalf("실제 기억 줄만 세야 한다 : %d", lines)
	}
	if totals["20260823-8d27dbfc"].Count != 1 {
		t.Fatalf("셈이 틀렸다 : %+v", totals)
	}
	// 명령 통계는 그대로 읽힌다.
	counts, err := CommandCounts(dir)
	if err != nil || counts["gc"] != 5 {
		t.Fatalf("명령 통계가 틀렸다 : %v %v", counts, err)
	}
}

// 리뷰 C #9 — 아카이브에 남은 옛 판을 id 로 되살린다 (mem show --from-archive).
func TestArchivedMemoryReadsBack(t *testing.T) {
	dir := t.TempDir()
	opened := Open(dir, false)
	if err := opened.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	markdown := "---\nid: 20260823-8d27dbfc\ntype: history\ndate: 2026-08-23\n" +
		"summary: 아카이브에 남은 옛 본문을 그대로 되살릴 수 있어야 한다는 것을 보는 요약\n" +
		"tags: [mem]\nsource: ai\nscope: mem\n---\n\n옛 본문\n"
	if _, err := opened.AppendAmendRecord("20260823-8d27dbfc", "store/2026/08/20260823-8d27dbfc.md",
		markdown, time.Now()); err != nil {
		t.Fatal(err)
	}
	found, err := opened.ArchivedMemory("20260823-8d27dbfc")
	if err != nil {
		t.Fatalf("아카이브에서 못 찾았다 : %v", err)
	}
	if found.Body != "옛 본문" {
		t.Fatalf("옛 본문이 아니다 : %q", found.Body)
	}
	if _, err := opened.ArchivedMemory("20260823-00000000"); err == nil {
		t.Fatal("없는 id 는 오류라야 한다")
	}
}
