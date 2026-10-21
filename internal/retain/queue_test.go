package retain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// putQueue 는 큐 표 하나를 쓰고 그 경로를 준다. transcript 가 참이면 기록 파일도 만든다.
// 기록 파일 수정 시각은 오래전으로 돌려 둔다 — 방금 쓴 파일은 Pick 이 살아 있는 세션으로 본다.
func putQueue(t *testing.T, dir, short string, entry QueueEntry, transcript bool) string {
	t.Helper()
	if transcript {
		entry.TranscriptPath = filepath.Join(dir, short+".jsonl")
		writeTranscript(t, entry.TranscriptPath, 1, 0)
		old := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		os.Chtimes(entry.TranscriptPath, old, old)
	} else {
		entry.TranscriptPath = filepath.Join(dir, "gone-"+short+".jsonl")
	}
	entry.SessionID = short + "-0000"
	data, _ := json.Marshal(entry)
	path := filepath.Join(QueueDir(dir), short+".json")
	os.MkdirAll(QueueDir(dir), 0o755)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// onlyPicked 는 PickWithSkipped 의 건너뛴 수를 버린다.
func onlyPicked(items []QueueItem, _ int) []QueueItem { return items }

func shortsOf(items []QueueItem) string {
	out := ""
	for _, item := range items {
		out += Short(item.Entry.SessionID) + " "
	}
	return out
}

func TestPickConditionsAndOrder(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	day := func(back int) string { return now.AddDate(0, 0, -back).Format(time.RFC3339) }
	putQueue(t, dir, "aaaa0001", QueueEntry{At: day(1)}, true)
	putQueue(t, dir, "aaaa0002", QueueEntry{At: day(5)}, true)
	putQueue(t, dir, "aaaa0003", QueueEntry{At: day(3)}, true)
	putQueue(t, dir, "bbbb0001", QueueEntry{At: day(2), Covered: true}, true)
	putQueue(t, dir, "bbbb0002", QueueEntry{At: day(2), DoneAt: day(1)}, true)
	putQueue(t, dir, "bbbb0003", QueueEntry{At: day(2)}, false)
	putQueue(t, dir, "bbbb0004", QueueEntry{At: day(20)}, true)
	os.WriteFile(filepath.Join(QueueDir(dir), "broken.json"), []byte("{"), 0o644)
	items, err := ReadQueue(dir)
	if err != nil || len(items) != 7 {
		t.Fatalf("깨진 파일만 빼고 일곱을 읽어야 한다 : %d %v", len(items), err)
	}
	if got := shortsOf(onlyPicked(PickWithSkipped(items, now, 14, 2))); got != "aaaa0002 aaaa0003 " {
		t.Fatalf("조건에 맞는 것을 오래된 것부터 max 건 : %q", got)
	}
	if got := shortsOf(onlyPicked(PickWithSkipped(items, now, 14, 0))); got != "aaaa0002 aaaa0003 aaaa0001 " {
		t.Fatalf("max 0 은 다 : %q", got)
	}
}

func TestPickSkipsCompactEntry(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	at := now.Add(-time.Hour).Format(time.RFC3339)
	putQueue(t, dir, "aaaa0001", QueueEntry{At: at, Reason: ReasonCompact}, true)
	putQueue(t, dir, "aaaa0002", QueueEntry{At: at, Reason: ReasonCompact}, true)
	// 30분 전 : end 창(10분)은 지났지만 compact 창(60분) 안이다.
	recent := now.Add(-30 * time.Minute)
	os.Chtimes(filepath.Join(dir, "aaaa0001.jsonl"), recent, recent)
	old := now.Add(-2 * time.Hour)
	os.Chtimes(filepath.Join(dir, "aaaa0002.jsonl"), old, old)
	items, _ := ReadQueue(dir)
	if got := shortsOf(onlyPicked(PickWithSkipped(items, now, 14, 0))); got != "aaaa0002 " {
		t.Fatalf("compact 표는 기록이 60분 안이면 건너뛰고 2시간 전이면 집힌다 : %q", got)
	}
}

func TestPickSkipsRecentlyWrittenTranscript(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	at := now.Add(-time.Hour).Format(time.RFC3339)
	putQueue(t, dir, "aaaa0001", QueueEntry{At: at, Reason: ReasonEnd}, true)
	putQueue(t, dir, "aaaa0002", QueueEntry{At: at, Reason: ReasonEnd}, true)
	recent := now.Add(-liveWindowEnd / 2)
	os.Chtimes(filepath.Join(dir, "aaaa0001.jsonl"), recent, recent)
	items, _ := ReadQueue(dir)
	if got := shortsOf(onlyPicked(PickWithSkipped(items, now, 14, 0))); got != "aaaa0002 " {
		t.Fatalf("기록이 liveWindowEnd 안에 바뀐 세션은 건너뛴다 : %q", got)
	}
	later := now.Add(liveWindowEnd)
	if got := shortsOf(onlyPicked(PickWithSkipped(items, later, 14, 0))); got != "aaaa0002 aaaa0001 " && got != "aaaa0001 aaaa0002 " {
		t.Fatalf("건너뛴 세션은 다음 판에 다시 집힌다 : %q", got)
	}
}

func TestReadQueueWithoutFolder(t *testing.T) {
	items, err := ReadQueue(t.TempDir())
	if err != nil || len(items) != 0 {
		t.Fatalf("큐 폴더가 없으면 빈 목록이다 : %v %v", items, err)
	}
}

func TestMarkDoneKeepsEntryAndWritesAtomically(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	path := putQueue(t, dir, "aaaa0001", QueueEntry{At: now.Format(time.RFC3339), Reason: ReasonEnd, Offset: 42}, true)
	if marked, err := MarkDone(path, now.Format(time.RFC3339), ResultSaved, []string{"20261019-aaaaaaaa"}, now); err != nil || !marked {
		t.Fatalf("표시를 못 했다 : %v %v", marked, err)
	}
	items, _ := ReadQueue(dir)
	entry := items[0].Entry
	if entry.DoneAt != now.Format(time.RFC3339) || entry.Result != ResultSaved || len(entry.Saved) != 1 {
		t.Fatalf("표시 칸 셋이 틀렸다 : %+v", entry)
	}
	if entry.Reason != ReasonEnd || entry.Offset != 42 {
		t.Fatalf("원래 칸을 잃었다 : %+v", entry)
	}
	leftovers, _ := filepath.Glob(filepath.Join(QueueDir(dir), "*.tmp"))
	if len(leftovers) != 0 {
		t.Fatalf("임시 파일이 남았다 : %v", leftovers)
	}
	if _, err := MarkDone(filepath.Join(dir, "none.json"), "", ResultNothing, nil, now); err == nil {
		t.Fatal("없는 큐 파일에 표시했다")
	}
}

// 처리 도중 훅이 같은 세션 표를 새로 썼으면(At 이 바뀜) 새 항목에 done_at 을 달면 안 된다 (리뷰 13 #5).
func TestMarkDoneSkipsEntryRewrittenByHook(t *testing.T) {
	dir := t.TempDir()
	picked := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	path := putQueue(t, dir, "aaaa0002", QueueEntry{At: picked.Format(time.RFC3339), Reason: ReasonCompact}, true)
	items, _ := ReadQueue(dir)
	pickedAt := items[0].Entry.At
	rewritten := picked.Add(3 * time.Minute)
	putQueue(t, dir, "aaaa0002", QueueEntry{At: rewritten.Format(time.RFC3339), Reason: ReasonEnd, Offset: 99}, true)
	marked, err := MarkDone(path, pickedAt, ResultNothing, nil, rewritten.Add(time.Minute))
	if err != nil || marked {
		t.Fatalf("바뀐 항목에 표시했다 : %v %v", marked, err)
	}
	items, _ = ReadQueue(dir)
	if items[0].Entry.DoneAt != "" || items[0].Entry.Offset != 99 {
		t.Fatalf("훅이 새로 쓴 항목을 건드렸다 : %+v", items[0].Entry)
	}
}

func TestPrunePlanAndPrune(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	day := func(back int) string { return now.AddDate(0, 0, -back).Format(time.RFC3339) }
	putQueue(t, dir, "aaaa0001", QueueEntry{At: day(1)}, true)
	putQueue(t, dir, "aaaa0002", QueueEntry{At: day(1)}, false)
	putQueue(t, dir, "aaaa0003", QueueEntry{At: day(20)}, true)
	putQueue(t, dir, "aaaa0004", QueueEntry{At: day(1), DoneAt: day(15)}, true)
	putQueue(t, dir, "aaaa0005", QueueEntry{At: day(1), DoneAt: day(2)}, true)
	items, _ := ReadQueue(dir)
	plan := PrunePlan(items, now, 14)
	if got := shortsOf(plan); got != "aaaa0002 aaaa0003 aaaa0004 " {
		t.Fatalf("기록 없음 · 오래됨 · 처리 뒤 오래됨 셋 : %q", got)
	}
	if left, _ := ReadQueue(dir); len(left) != 5 {
		t.Fatal("미리보기는 지우지 않는다")
	}
	paths := []string{}
	for _, item := range plan {
		paths = append(paths, item.Path)
	}
	if err := Prune(append(paths, filepath.Join(dir, "already-gone.json"))); err != nil {
		t.Fatal(err)
	}
	if left, _ := ReadQueue(dir); shortsOf(left) != "aaaa0001 aaaa0005 " {
		t.Fatalf("prune 뒤 남은 것이 틀렸다 : %q", shortsOf(left))
	}
}

func TestFindQueueTranscript(t *testing.T) {
	dir := t.TempDir()
	putQueue(t, dir, "aaaa0001", QueueEntry{At: time.Now().Format(time.RFC3339)}, true)
	path, ok := FindQueueTranscript(dir, "aaaa0001")
	if !ok || filepath.Base(path) != "aaaa0001.jsonl" {
		t.Fatalf("큐 항목의 transcript_path 를 줘야 한다 : %q %v", path, ok)
	}
	if _, ok := FindQueueTranscript(dir, "zzzz9999"); ok {
		t.Fatal("없는 세션을 찾았다")
	}
	if _, ok := FindQueueTranscript(dir, "../../ev"); ok {
		t.Fatal("못 쓰는 세션 글자로 파일을 열었다")
	}
}
