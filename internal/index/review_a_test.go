package index

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// 리뷰 C #2 — 한 번의 index 안에서도 중복은 하나만 남는다. 승격이 색인보다
// 먼저 돌기 때문에 DB 만 봐서는 같은 회차의 둘이 서로를 못 봤다.
func TestSameRunDuplicateKeepsOne(t *testing.T) {
	opened := newStore(t)
	for at := 0; at < 2; at++ {
		if _, err := opened.WriteAdd(addRequest(longSummary, "똑같은 본문 한 줄")); err != nil {
			t.Fatal(err)
		}
	}
	result := runIndex(t, opened)
	if result.Added != 1 || result.Duplicated != 1 {
		t.Fatalf("한 회차 중복이 안 걸렸다 : %+v", result)
	}
	files, err := opened.ListMemories()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("파일이 하나여야 한다 : %d", len(files))
	}
}

// 리뷰 A #1 — 고치기(patch)도 비밀정보 차단선을 지난다.
func TestPatchWithSecretGoesToBad(t *testing.T) {
	opened := newStore(t)
	name, err := opened.WriteAdd(addRequest(longSummary, "본문 한 줄"))
	if err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	id := model.QueueID(name, "본문 한 줄", today())
	set := map[string]any{"summary": "새 요약인데 토큰 ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123 이 들어 있어 막혀야 한다"}
	if _, err := opened.WritePatch(id, set); err != nil {
		t.Fatal(err)
	}
	result := runIndex(t, opened)
	if result.Bad != 1 || result.Patched != 0 {
		t.Fatalf("비밀정보가 든 고치기를 막아야 한다 : %+v", result)
	}
	file, err := opened.ReadMemory(model.StorePath(id))
	if err != nil {
		t.Fatal(err)
	}
	if file.Memory.Summary != longSummary {
		t.Fatal("막았는데도 요약이 바뀌었다")
	}
}

// 리뷰 A #1 — 본문 갈아 끼우기도 마찬가지고, 아카이브에 옛 판을 더 남기지 않는다.
func TestBodyWithSecretGoesToBad(t *testing.T) {
	opened := newStore(t)
	name, err := opened.WriteAdd(addRequest(longSummary, "본문 한 줄"))
	if err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	id := model.QueueID(name, "본문 한 줄", today())
	if _, err := opened.WriteAmend(id, "새 본문\n키 sk-ant-api03ABCDEFGHIJKLMNOPQRSTUVWXYZ0123"); err != nil {
		t.Fatal(err)
	}
	result := runIndex(t, opened)
	if result.Bad != 1 || result.Patched != 0 {
		t.Fatalf("비밀정보가 든 본문을 막아야 한다 : %+v", result)
	}
	if files := store.ArchiveFiles(opened.ArchiveDir()); len(files) != 0 {
		t.Fatalf("막힌 본문 때문에 아카이브가 늘었다 : %v", files)
	}
}

// 리뷰 A #13 — pinned 는 참·거짓이어야 한다. "true" 나 1 은 조용히 false 가 되면 안 된다.
func TestPinnedMustBeBool(t *testing.T) {
	memory := model.Memory{}
	if err := patchFields(&memory, map[string]any{"pinned": "true"}); err == nil {
		t.Fatal("문자열 true 를 받아 주면 안 된다")
	}
	if err := patchFields(&memory, map[string]any{"pinned": float64(1)}); err == nil {
		t.Fatal("숫자 1 을 받아 주면 안 된다")
	}
	if err := patchFields(&memory, map[string]any{"pinned": true}); err != nil || !memory.Pinned {
		t.Fatalf("참은 받아야 한다 : %v", err)
	}
}

// 리뷰 A #11 — 아직 inbox/new 에 남은 큐 파일의 승격 표시는 안 지운다.
func TestPruneKeepsMarkOfWaitingQueue(t *testing.T) {
	opened := newStore(t)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.markInbox("남은.json"); err != nil {
		t.Fatal(err)
	}
	if err := database.markInbox("사라진.json"); err != nil {
		t.Fatal(err)
	}
	later := time.Now().AddDate(0, 0, inboxSeenDays+1)
	if err := database.pruneInboxSeen(later, []string{"남은.json"}); err != nil {
		t.Fatal(err)
	}
	kept, err := database.seenInbox("남은.json")
	if err != nil || !kept {
		t.Fatal("아직 큐에 있는 이름의 표시를 지웠다")
	}
	gone, err := database.seenInbox("사라진.json")
	if err != nil || gone {
		t.Fatal("오래된 표시는 지웠어야 한다")
	}
}

// 리뷰 A #16 — 시계가 앞선 기계가 남긴 락은 영영 안 늙으면 안 된다.
func TestFutureLockIsAbandoned(t *testing.T) {
	future := lockInfo{PID: 999999, Started: 1, Taken: time.Now().Add(10 * time.Hour).Unix(), Host: "다른기계"}
	if !abandoned(future) {
		t.Fatal("미래에 잡힌 락을 산 락으로 봤다")
	}
}

// 리뷰 A #4 — release 는 **내 락일 때만** 지운다. 남이 새로 잡은 락을 지우면
// 둘이 동시에 주인이 된다.
func TestReleaseOnlyRemovesOwnLock(t *testing.T) {
	dir := t.TempDir()
	release, taken, err := TryLock(dir)
	if err != nil || !taken {
		t.Fatalf("락을 못 잡았다 : %v", err)
	}
	// 남이 잡은 것처럼 내용을 갈아 끼운다.
	if err := os.WriteFile(lockPath(dir), []byte("12345 6789 100 남의기계\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	release()
	if _, err := os.Stat(lockPath(dir)); err != nil {
		t.Fatal("남의 락을 지웠다")
	}
}

// 불변조건 4 · 리뷰 A #5 — 마감을 넘긴 따라잡기는 아무것도 안 하고 그대로 돌아온다.
func TestCatchUpStopsAtDeadline(t *testing.T) {
	opened := newStore(t)
	if _, err := opened.WriteAdd(addRequest(longSummary, "본문 한 줄")); err != nil {
		t.Fatal(err)
	}
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	result := database.CatchUpBy(opened, settings().GC, settings().Secret, time.Now().Add(-time.Second))
	if !result.OverBudget {
		t.Fatal("마감을 넘겼다고 말해야 한다")
	}
	if result.Added != 0 {
		t.Fatalf("마감을 넘겼는데 승격했다 : %+v", result)
	}
	// 락 파일이 남으면 다음 index 가 90초를 기다린다.
	if _, err := os.Stat(filepath.Join(opened.Dir, LockName)); err == nil {
		t.Fatal("락이 남았다")
	}
	// 큐는 그대로 남아 다음 회차가 이어서 한다.
	if left, _ := opened.ListInbox(); len(left) != 1 {
		t.Fatalf("큐가 남아 있어야 한다 : %v", left)
	}
}

// 리뷰 A #12 — 같은 날 32비트 해시가 겹쳐도 멀쩡한 기억을 안 덮는다.
func TestIDCollisionDoesNotOverwrite(t *testing.T) {
	opened := newStore(t)
	current := runner{store: opened, now: time.Now()}
	taken := current.freeID("첫.json", "첫 본문", "2026-08-22")
	memory := model.Memory{ID: taken, Type: model.TypeIssue, Date: "2026-08-22",
		Summary: longSummary, Tags: []string{"a"}, LegacySource: model.LegacySourceAI, Scope: "mem",
		Severity: model.SeverityHigh, Body: "첫 본문"}
	if err := opened.WriteMemory(&memory); err != nil {
		t.Fatal(err)
	}
	// 같은 자리를 노리는 다른 내용은 다른 id 를 받아야 한다.
	same := current.freeID("첫.json", "다른 본문", "2026-08-22")
	if same == taken {
		t.Fatal("다른 내용이 남의 자리를 덮는다")
	}
	// 같은 항목을 다시 승격하면 같은 id 라야 두 벌이 안 생긴다.
	if again := current.freeID("첫.json", "첫 본문", "2026-08-22"); again != taken {
		t.Fatalf("같은 항목이 다른 id 를 받았다 : %s %s", again, taken)
	}
}

// 리뷰 B #17 — 달력에 없는 날짜를 오늘로 바꾸면 감쇠·정렬이 조용히 틀어진다.
// 그런 파일은 애초에 색인에 못 들어오고, 들어와도 0(날짜 없음)이라야 한다.
func TestBadDateIsNotToday(t *testing.T) {
	if stamp := dayToUnix("2026-13-45"); stamp != 0 {
		t.Fatalf("잘못된 날짜를 날짜로 읽었다 : %d", stamp)
	}
	if got := nullableDay("2026-13-45"); got != nil {
		t.Fatalf("잘못된 무효 날짜를 값으로 넣었다 : %v", got)
	}
	if dayToUnix("2026-08-23") == 0 {
		t.Fatal("멀쩡한 날짜를 못 읽었다")
	}
	// 규격 검사가 먼저 막는다 — 색인도 lint 도 그 자리에서 걸린다.
	memory := model.Memory{ID: "20260823-8d27dbfc", Type: model.TypeHistory, Date: "2026-13-45",
		Summary: longSummary, Tags: []string{"a"}, LegacySource: model.LegacySourceAI, Scope: "mem"}
	if problems := model.Validate(&memory); len(problems) == 0 {
		t.Fatal("달력에 없는 날짜를 규격 검사가 통과시켰다")
	}
	if err := checkParsed(&memory, model.StorePath(memory.ID), model.DefaultTypes()); err == nil {
		t.Fatal("그런 파일이 색인에 들어갔다")
	}
}
