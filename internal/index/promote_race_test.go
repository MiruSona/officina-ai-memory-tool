package index

// 리뷰 2026-09-26 — add 가 락을 기다리는 사이 남이 제 큐 파일을 먼저 승격한
// 경우(#1)와, 색인을 새로 세울 판의 즉시 승격(#4).

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 남(`mem index`)이 먼저 쌍둥이로 돌렸다 — add 는 큐 id 가 아니라 살아남은
// 쌍둥이 id 를 받는다. 파일을 다시 읽으려다 「색인 대기」 로 떨어지면 안 된다.
func TestPromoteNowAfterForeignDuplicate(t *testing.T) {
	opened := newStore(t)
	first, err := opened.WriteAdd(addRequest(longSummary, "먼저 들어간 본문"))
	if err != nil {
		t.Fatal(err)
	}
	firstID := promoteNow(t, opened, Options{}, first).Outcomes[first].ID
	twin := addRequest(longSummary, "먼저 들어간 본문")
	name, err := opened.WriteAdd(twin)
	if err != nil {
		t.Fatal(err)
	}
	if model.QueueID(name, twin.Body, twin.Date) == firstID {
		t.Fatal("시험 전제가 틀렸다 — 큐 id 가 쌍둥이 id 와 같다")
	}
	runIndex(t, opened) // 남이 먼저 먹는다
	got := promoteNow(t, opened, Options{}, name).Outcomes[name]
	if got.State != OutcomeDuplicate || got.ID != firstID || !got.Foreign {
		t.Fatalf("남이 돌린 쌍둥이 id 여야 한다 : %+v (쌍둥이 %s)", got, firstID)
	}
}

// 남이 새 파일로 만들었으면 그 id 다.
func TestPromoteNowAfterForeignNew(t *testing.T) {
	opened := newStore(t)
	request := addRequest(longSummary, "남이 새로 만든 본문")
	name, err := opened.WriteAdd(request)
	if err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	got := promoteNow(t, opened, Options{}, name).Outcomes[name]
	if got.State != OutcomeNew || got.ID != model.QueueID(name, request.Body, request.Date) || !got.Foreign {
		t.Fatalf("남이 만든 새 파일 id 여야 한다 : %+v", got)
	}
	if _, err := opened.ReadMemory(model.StorePath(got.ID)); err != nil {
		t.Fatalf("그 id 의 파일이 없다 : %v", err)
	}
}

// 남이 bad 로 보냈으면 bad 와 까닭이다.
func TestPromoteNowAfterForeignBad(t *testing.T) {
	opened := newStore(t)
	name, err := opened.WriteAdd(addRequest(longSummary, "열쇠는 ghp_abcdefghijklmnopqrstuvwxyz012345 다"))
	if err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	got := promoteNow(t, opened, Options{}, name).Outcomes[name]
	if got.State != OutcomeBad || got.Reason == "" || !got.Foreign {
		t.Fatalf("남이 보낸 bad 여야 한다 : %+v", got)
	}
}

// 영수증이 없으면(색인을 새로 세웠다) gone — 틀릴 수 있는 큐 id 를 안 준다.
func TestPromoteNowAfterForeignWithoutReceipt(t *testing.T) {
	opened := newStore(t)
	name, err := opened.WriteAdd(addRequest(longSummary, "영수증이 사라진 본문"))
	if err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.Exec("DELETE FROM promoted"); err != nil {
		t.Fatal(err)
	}
	database.Close()
	got := promoteNow(t, opened, Options{}, name).Outcomes[name]
	if got.State != OutcomeGone || got.ID != "" {
		t.Fatalf("gone 이고 id 가 비어야 한다 : %+v", got)
	}
}

// 남이 add 만 먹고(쌍둥이로 돌림) 뒤에 들어온 `--by` 덮임 표시는 못 봤다.
// 표시는 큐 id 를 가리키지만, add 의 즉시 승격이 영수증으로 실제 id 에 다시 댄다.
func TestPromoteNowRetargetsLatePatch(t *testing.T) {
	opened := newStore(t)
	oldName, err := opened.WriteAdd(addRequest(longSummary, "덮일 옛 본문"))
	if err != nil {
		t.Fatal(err)
	}
	oldID := promoteNow(t, opened, Options{}, oldName).Outcomes[oldName].ID
	// 옛 기억과 제목이 닮으면 거기 붙어 버린다. 요약을 딴판으로 둔다.
	otherSummary := "옛 결정과 전혀 닮지 않은 요약으로 따로 서는 기억을 만든다 서른 자를 넘긴다"
	twinName, err := opened.WriteAdd(addRequest(otherSummary, "덮는 새 본문"))
	if err != nil {
		t.Fatal(err)
	}
	twinID := promoteNow(t, opened, Options{}, twinName).Outcomes[twinName].ID
	newer := addRequest(otherSummary, "덮는 새 본문")
	newer.Supersedes = oldID
	name, err := opened.WriteAdd(newer)
	if err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened) // 남이 add 만 먹는다 — 표시는 아직 안 썼다
	queued := model.QueueID(name, newer.Body, newer.Date)
	patch, err := opened.WritePatch(oldID, map[string]any{"superseded_by": queued, "invalid_at": today()})
	if err != nil {
		t.Fatal(err)
	}
	promoted := promoteNow(t, opened, Options{}, name, patch)
	if got := promoted.Outcomes[name]; got.ID != twinID || !got.Foreign {
		t.Fatalf("add 는 남이 돌린 쌍둥이 id 여야 한다 : %+v (쌍둥이 %s)", got, twinID)
	}
	file, err := opened.ReadMemory(model.StorePath(oldID))
	if err != nil {
		t.Fatal(err)
	}
	if file.Memory.SupersededBy != twinID {
		t.Fatalf("옛 기억이 실제 id 를 가리켜야 한다 : %q (실제 %s · 큐 %s)", file.Memory.SupersededBy, twinID, queued)
	}
}

// 오래된 영수증은 다음 승격이 치운다.
func TestPromotedReceiptsArePruned(t *testing.T) {
	opened := newStore(t)
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsurePromoted(database.sql); err != nil {
		t.Fatal(err)
	}
	if err := database.keepPromoted("old.json", "q", "20260101-aaaaaaaa", OutcomeNew,
		time.Now().Add(-2*promotedKeep)); err != nil {
		t.Fatal(err)
	}
	database.Close()
	runIndex(t, opened)
	database, err = Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	found, err := database.promotedOf("old.json")
	if err != nil || found.ID != "" {
		t.Fatalf("오래된 영수증이 남았다 : %+v %v", found, err)
	}
}

// 색인이 store/ 를 한 번도 안 본 판(index.db 를 지웠다 · 새로 받았다)이면 즉시
// 승격은 큐에 두고 물러난다. 빈 색인에 대고 승격하면 쌍둥이를 못 본다.
func TestPromoteNowDefersOnFirstIndex(t *testing.T) {
	opened := newStore(t)
	first, err := opened.WriteAdd(addRequest(longSummary, "이미 있는 본문"))
	if err != nil {
		t.Fatal(err)
	}
	firstID := promoteNow(t, opened, Options{}, first).Outcomes[first].ID
	if err := removeFiles(opened.Dir); err != nil {
		t.Fatal(err)
	}
	twin, err := opened.WriteAdd(addRequest(longSummary, "이미 있는 본문"))
	if err != nil {
		t.Fatal(err)
	}
	promoted := promoteNow(t, opened, Options{}, twin)
	if !promoted.Deferred || len(promoted.Outcomes) != 0 {
		t.Fatalf("미뤄야 한다 : %+v", promoted)
	}
	if _, err := os.Stat(filepath.Join(opened.InboxNewDir(), twin)); err != nil {
		t.Fatalf("큐 파일이 그대로 있어야 한다 : %v", err)
	}
	if pending := metaOf(t, opened, metaLinksPending); pending != linksAll {
		t.Fatalf("이웃 링크를 통째로 다시 재게 적어야 한다 : %q", pending)
	}
	// 다음 색인이 새 색인과 함께 승격한다.
	runIndex(t, opened)
	if left, _ := opened.ListInbox(); len(left) != 0 {
		t.Fatalf("다음 색인이 승격해야 한다 : %v (첫 id %s)", left, firstID)
	}
}

// 방식이 바뀌어 색인을 새로 세우는 판(Rebuilt)도 미룬다.
func TestPromoteNowDefersOnRebuild(t *testing.T) {
	opened := newStore(t)
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SetMeta(metaTokenizer, "old-tokenizer"); err != nil {
		t.Fatal(err)
	}
	database.Close()
	name, err := opened.WriteAdd(addRequest(longSummary, "방식이 바뀐 뒤의 본문"))
	if err != nil {
		t.Fatal(err)
	}
	promoted := promoteNow(t, opened, Options{}, name)
	if !promoted.Deferred {
		t.Fatalf("색인을 새로 세우는 판이면 미뤄야 한다 : %+v", promoted)
	}
	if left, _ := opened.ListInbox(); len(left) != 1 {
		t.Fatalf("큐 파일이 그대로 있어야 한다 : %v", left)
	}
}
