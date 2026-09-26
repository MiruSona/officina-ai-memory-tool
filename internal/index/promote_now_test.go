package index

// add 즉시 승격 (설계 2026-09-23 2장) — `PromoteNow` 는 이름 준 큐 파일만 같은
// 락·같은 승격 코드로 먹고, 결과(상태 · 실제로 남은 id · 까닭)를 이름별로 준다.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

func promoteNow(t *testing.T, opened *store.Store, options Options, names ...string) *PromoteResult {
	t.Helper()
	options.Store = opened
	options.Secret = settings().Secret
	promoted, err := PromoteNow(options, names)
	if err != nil {
		t.Fatalf("즉시 승격이 죽었다 : %v", err)
	}
	return promoted
}

// 이름 준 파일만 승격한다. 남의 큐 파일은 그대로 inbox/new 에 남는다.
func TestPromoteNowOnlyNamedFiles(t *testing.T) {
	opened := newStore(t)
	other, err := opened.WriteAdd(addRequest(longSummary, "남이 넣은 본문"))
	if err != nil {
		t.Fatal(err)
	}
	mine, err := opened.WriteAdd(store.AddRequest{Op: store.OpAdd, Type: model.TypeIssue, Date: today(),
		Summary: "완전히 다른 요약으로 제목이 닮지 않게 만든 두 번째 기억이다 서른 자를 넘긴다",
		Tags:    []string{"hook", "test"}, Source: model.LegacySourceAI, Scope: "mem-search",
		Severity: model.SeverityHigh, Body: "내 본문"})
	if err != nil {
		t.Fatal(err)
	}
	promoted := promoteNow(t, opened, Options{}, mine)
	outcome := promoted.Outcomes[mine]
	if outcome.State != OutcomeNew {
		t.Fatalf("새 파일이어야 한다 : %+v", outcome)
	}
	if outcome.ID != model.QueueID(mine, "내 본문", today()) {
		t.Fatalf("큐 id 와 다르다 : %+v", outcome)
	}
	if _, err := opened.ReadMemory(model.StorePath(outcome.ID)); err != nil {
		t.Fatalf("파일이 없다 : %v", err)
	}
	left, _ := opened.ListInbox()
	if len(left) != 1 || left[0] != other {
		t.Fatalf("남의 큐 파일이 그대로 있어야 한다 : %v", left)
	}
	if _, found := promoted.Outcomes[other]; found {
		t.Fatal("남의 파일 결과가 적혔다")
	}
	// 증분 색인까지 끝나 DB 에서 바로 찾힌다.
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	path, err := database.pathByID(outcome.ID)
	if err != nil || path == "" {
		t.Fatalf("색인에 없다 : %q %v", path, err)
	}
}

// 완전중복은 쌍둥이 id, 닮은 기억은 그 기억 id 를 준다.
func TestPromoteNowGivesSurvivingID(t *testing.T) {
	opened := newStore(t)
	first, err := opened.WriteAdd(addRequest(longSummary, "첫 본문"))
	if err != nil {
		t.Fatal(err)
	}
	firstID := promoteNow(t, opened, Options{}, first).Outcomes[first].ID
	twin, err := opened.WriteAdd(addRequest(longSummary, "첫 본문"))
	if err != nil {
		t.Fatal(err)
	}
	got := promoteNow(t, opened, Options{}, twin).Outcomes[twin]
	if got.State != OutcomeDuplicate || got.ID != firstID {
		t.Fatalf("완전중복은 쌍둥이 id 여야 한다 : %+v (첫 id %s)", got, firstID)
	}
	near, err := opened.WriteAdd(addRequest(longSummary, "두 번째 본문"))
	if err != nil {
		t.Fatal(err)
	}
	got = promoteNow(t, opened, Options{}, near).Outcomes[near]
	if got.State != OutcomeAppended || got.ID != firstID {
		t.Fatalf("닮은 기억은 그 기억 id 여야 한다 : %+v (첫 id %s)", got, firstID)
	}
}

// 남이 락을 쥐고 있으면 짧게 기다린 뒤 아무것도 안 만지고 돌아간다.
func TestPromoteNowLeavesQueueWhenLocked(t *testing.T) {
	opened := newStore(t)
	name, err := opened.WriteAdd(addRequest(longSummary, "기다리는 본문"))
	if err != nil {
		t.Fatal(err)
	}
	release, taken, err := TryLock(opened.Dir)
	if err != nil || !taken {
		t.Fatalf("락을 못 잡았다 : %v", err)
	}
	defer release()
	saved := PromoteWait
	PromoteWait = 60 * time.Millisecond
	defer func() { PromoteWait = saved }()
	started := time.Now()
	promoted := promoteNow(t, opened, Options{}, name)
	if !promoted.Locked || len(promoted.Outcomes) != 0 {
		t.Fatalf("락을 못 잡았으면 Locked 이고 결과가 없어야 한다 : %+v", promoted)
	}
	if time.Since(started) < 50*time.Millisecond {
		t.Fatal("기다리지 않고 바로 물러났다")
	}
	if left, _ := opened.ListInbox(); len(left) != 1 {
		t.Fatalf("큐 파일이 그대로 있어야 한다 : %v", left)
	}
}

// 관문을 지난 뒤 종류 표가 바뀌어 규격 위반이 되면 bad 로 가고 까닭이 남는다.
func TestPromoteNowParksInvalidWithReason(t *testing.T) {
	opened := newStore(t)
	name, err := opened.WriteAdd(addRequest(longSummary, "표가 바뀐 뒤의 본문"))
	if err != nil {
		t.Fatal(err)
	}
	types := model.TypeTable{}
	for _, spec := range model.DefaultTypes() {
		if spec.Name != model.TypeIssue {
			types = append(types, spec)
		}
	}
	got := promoteNow(t, opened, Options{Types: types}, name).Outcomes[name]
	if got.State != OutcomeBad || got.Secret || got.Reason == "" || got.ID != "" {
		t.Fatalf("규격 위반 bad 여야 한다 : %+v", got)
	}
	if _, err := os.Stat(filepath.Join(opened.InboxBadDir(), name)); err != nil {
		t.Fatalf("inbox/bad 에 없다 : %v", err)
	}
	if opened.BadReason(name) == "" {
		t.Fatal("까닭 파일이 없다")
	}
}

// 큐에 손으로 쓴 비밀정보는 승격이 다시 막는다. 값은 까닭에 안 실린다.
func TestPromoteNowBlocksSecret(t *testing.T) {
	opened := newStore(t)
	key := "ghp_abcdefghijklmnopqrstuvwxyz012345"
	name, err := opened.WriteAdd(addRequest(longSummary, "열쇠는 "+key+" 다"))
	if err != nil {
		t.Fatal(err)
	}
	got := promoteNow(t, opened, Options{}, name).Outcomes[name]
	if got.State != OutcomeBad || !got.Secret {
		t.Fatalf("비밀정보 bad 여야 한다 : %+v", got)
	}
	if strings.Contains(got.Reason, key) || strings.Contains(opened.BadReason(name), key) {
		t.Fatal("값이 까닭에 실렸다")
	}
}

// `add --by` 에서 덮는 기억이 bad 로 가면, 같이 넣은 덮임 표시가 옛 기억에
// 없는 id 를 달면 안 된다. 표시도 bad 로 가고 옛 기억은 그대로다.
func TestPromoteNowDropsPatchOfBadAdd(t *testing.T) {
	opened := newStore(t)
	oldName, err := opened.WriteAdd(addRequest(longSummary, "옛 본문"))
	if err != nil {
		t.Fatal(err)
	}
	oldID := promoteNow(t, opened, Options{}, oldName).Outcomes[oldName].ID
	newer := addRequest("짧다", "규격을 어긴 덮는 본문")
	newer.Supersedes = oldID
	name, err := opened.WriteAdd(newer)
	if err != nil {
		t.Fatal(err)
	}
	queued := model.QueueID(name, newer.Body, newer.Date)
	patch, err := opened.WritePatch(oldID, map[string]any{"superseded_by": queued, "invalid_at": today()})
	if err != nil {
		t.Fatal(err)
	}
	promoted := promoteNow(t, opened, Options{}, name, patch)
	if promoted.Outcomes[name].State != OutcomeBad || promoted.Outcomes[patch].State != OutcomeBad {
		t.Fatalf("add 와 덮임 표시 둘 다 bad 여야 한다 : %+v", promoted.Outcomes)
	}
	file, err := opened.ReadMemory(model.StorePath(oldID))
	if err != nil {
		t.Fatal(err)
	}
	if file.Memory.SupersededBy != "" {
		t.Fatalf("옛 기억이 없는 id 를 가리킨다 : %s", file.Memory.SupersededBy)
	}
}

// `add --by` 의 add 와 덮임 표시를 한 락 안에서 같이 먹는다. 색인을 따로 안
// 돌려도 옛 기억이 **파일이 있는** 새 id 를 가리킨다 (09-23 회차 갈림 한계).
func TestPromoteNowSupersedesInSameLock(t *testing.T) {
	opened := newStore(t)
	oldName, err := opened.WriteAdd(addRequest(longSummary, "옛 본문"))
	if err != nil {
		t.Fatal(err)
	}
	oldID := promoteNow(t, opened, Options{}, oldName).Outcomes[oldName].ID
	newer := addRequest(longSummary, "옛 결정을 덮는 새 본문")
	newer.Supersedes = oldID
	name, err := opened.WriteAdd(newer)
	if err != nil {
		t.Fatal(err)
	}
	queued := model.QueueID(name, newer.Body, newer.Date)
	patch, err := opened.WritePatch(oldID, map[string]any{"superseded_by": queued, "invalid_at": today()})
	if err != nil {
		t.Fatal(err)
	}
	promoted := promoteNow(t, opened, Options{}, name, patch)
	newID := promoted.Outcomes[name].ID
	if promoted.Outcomes[name].State != OutcomeNew || promoted.Outcomes[patch].State != OutcomeDone {
		t.Fatalf("새 파일 + 표시 먹힘이어야 한다 : %+v", promoted.Outcomes)
	}
	file, err := opened.ReadMemory(model.StorePath(oldID))
	if err != nil {
		t.Fatal(err)
	}
	if file.Memory.SupersededBy != newID {
		t.Fatalf("옛 기억이 새 id 를 안 가리킨다 : %q (새 id %s)", file.Memory.SupersededBy, newID)
	}
	if _, err := opened.ReadMemory(model.StorePath(newID)); err != nil {
		t.Fatalf("가리키는 id 의 파일이 없다 : %v", err)
	}
	if left, _ := opened.ListInbox(); len(left) != 0 {
		t.Fatalf("큐가 비어야 한다 : %v", left)
	}
}

// 즉시 승격은 이웃 링크를 미뤄 둔다. 다음 색인이 「바뀐 파일 0건」이어도
// 미룬 기억의 둘레를 다시 재고 목록을 비운다.
func TestPromoteNowDefersLinksToNextIndex(t *testing.T) {
	opened := newStore(t)
	runIndex(t, opened)
	name, err := opened.WriteAdd(addRequest(longSummary, "링크를 미룬 본문"))
	if err != nil {
		t.Fatal(err)
	}
	id := promoteNow(t, opened, Options{}, name).Outcomes[name].ID
	if pending := metaOf(t, opened, metaLinksPending); !strings.Contains(pending, id) && pending != linksAll {
		t.Fatalf("미룬 목록에 없다 : %q", pending)
	}
	result := runIndex(t, opened)
	if result.Indexed != 0 {
		t.Fatalf("다음 색인은 바뀐 파일이 없어야 한다 : %+v", result)
	}
	if pending := metaOf(t, opened, metaLinksPending); pending != "" {
		t.Fatalf("이웃 링크를 다시 재고 목록을 비워야 한다 : %q", pending)
	}
}

func metaOf(t *testing.T, opened *store.Store, key string) string {
	t.Helper()
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	value, err := database.Meta(key)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
