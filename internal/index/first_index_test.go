package index

// 색인을 새로 세우는 판(index.db 를 지움 · `--full` · 방식 바뀜)의 첫 `mem index`
// 는 store/ 를 먼저 색인하고 승격한다. 거꾸로면 빈 색인이라 쌍둥이·닮은 기억을
// 못 봐 겹친 파일이 생긴다 (설계 2026-09-23 5절).

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// mdCount 는 store/ 아래 md 파일 수다.
func mdCount(t *testing.T, opened *store.Store) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(opened.StoreDir(), func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.HasSuffix(path, ".md") {
			count++
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}

// seedOne 은 기억 하나를 승격·색인해 두고 그 id 를 준다.
func seedOne(t *testing.T, opened *store.Store, body string) string {
	t.Helper()
	name, err := opened.WriteAdd(addRequest(longSummary, body))
	if err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	return model.QueueID(name, body, today())
}

// index.db 를 지운 뒤 첫 색인 — 같은 본문 add 는 이미 있는 기억을 가리킨다.
func TestFirstIndexSeesExistingTwin(t *testing.T) {
	opened := newStore(t)
	firstID := seedOne(t, opened, "이미 있는 본문")
	if err := removeFiles(opened.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := opened.WriteAdd(addRequest(longSummary, "이미 있는 본문")); err != nil {
		t.Fatal(err)
	}
	result := runIndex(t, opened)
	if result.Duplicated != 1 || result.Added != 0 {
		t.Fatalf("완전중복으로 쌍둥이를 가리켜야 한다 : %+v (첫 id %s)", result, firstID)
	}
	if got := mdCount(t, opened); got != 1 {
		t.Fatalf("md 는 하나여야 한다 : %d", got)
	}
	assertIndexed(t, opened, firstID)
}

// `mem index --full` 도 같다. 닮은 기억은 새 파일이 아니라 그 기억 뒤에 붙는다.
func TestFullIndexAppendsToExistingTwin(t *testing.T) {
	opened := newStore(t)
	firstID := seedOne(t, opened, "첫 본문")
	if _, err := opened.WriteAdd(addRequest(longSummary, "닮은 두 번째 본문")); err != nil {
		t.Fatal(err)
	}
	result, err := Run(Options{Store: opened, GC: settings().GC, Secret: settings().Secret,
		Quiet: true, Full: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Appended != 1 || result.Added != 0 {
		t.Fatalf("닮은 기억 뒤에 붙어야 한다 : %+v", result)
	}
	if got := mdCount(t, opened); got != 1 {
		t.Fatalf("md 는 하나여야 한다 : %d", got)
	}
	// 붙인 본문이 색인에도 들어가 있어야 한다 — 승격 뒤 증분 색인이 따라간다.
	file, err := opened.ReadMemory(model.StorePath(firstID))
	if err != nil || !strings.Contains(file.Memory.Body, "닮은 두 번째 본문") {
		t.Fatalf("붙인 본문이 없다 : %v", err)
	}
	if next := runIndex(t, opened); next.Indexed != 0 {
		t.Fatalf("붙인 뒤 색인이 따라가 다음 회차엔 바뀐 것이 없어야 한다 : %+v", next)
	}
}

// 새로 세우는 판의 Result 수 — 이미 있던 기억은 한 번만 색인된 것으로 센다.
func TestFirstIndexCountsOnce(t *testing.T) {
	opened := newStore(t)
	seedOne(t, opened, "하나")
	if err := removeFiles(opened.Dir); err != nil {
		t.Fatal(err)
	}
	result := runIndex(t, opened)
	if result.Indexed != 1 || result.Total != 1 {
		t.Fatalf("한 번만 색인해야 한다 : %+v", result)
	}
}

// 두 번째 훑기는 첫 훑기가 센 Bad·Secret·Skipped·Unindexed 를 다시 세지 않고,
// 두 번 색인된(붙인) 기억은 Indexed·Changed 에 한 번만 든다.
func TestFirstIndexSecondPassCountsOnce(t *testing.T) {
	opened := newStore(t)
	goodID := seedOne(t, opened, "첫 본문")
	secretID := seedSummary(t, opened, "비밀이 든 기억은 완전히 다른 이야기라 닮은 기억으로 안 잡히게 요약을 바꾼다", "둘째 본문")
	file, err := opened.ReadMemory(model.StorePath(secretID))
	if err != nil {
		t.Fatal(err)
	}
	file.Memory.Body = "둘째 본문\n배포에는 ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123 을 쓴다"
	if err := opened.WriteMemory(file.Memory); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(opened.Dir, filepath.FromSlash(model.StorePath("20260101-deadbeef")))
	if err := os.MkdirAll(filepath.Dir(broken), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken, []byte("머리말이 없는 md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeFiles(opened.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := opened.WriteAdd(addRequest(longSummary, "닮은 두 번째 본문")); err != nil {
		t.Fatal(err)
	}
	result := runIndex(t, opened)
	if result.Appended != 1 {
		t.Fatalf("닮은 기억 뒤에 붙어야 두 번째 훑기가 돈다 : %+v", result)
	}
	if result.Bad != 2 || result.Secret != 1 || len(result.Unindexed) != 2 || result.Skipped != 0 {
		t.Fatalf("규격 위반 1 · 비밀 1 을 한 번씩만 세야 한다 : %+v", result)
	}
	if result.Indexed != 1 || len(result.Changed) != 1 || result.Changed[0].ID != goodID {
		t.Fatalf("붙인 기억은 한 번만 색인된 것으로 센다 : %+v", result)
	}
}

// seedSummary 는 요약을 따로 준 기억 하나를 승격·색인해 두고 그 id 를 준다.
func seedSummary(t *testing.T, opened *store.Store, summary, body string) string {
	t.Helper()
	name, err := opened.WriteAdd(addRequest(summary, body))
	if err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	return model.QueueID(name, body, today())
}

// 훅 따라잡기도 같다. add 가 미뤄 둔 판(index.db 는 있고 store/ 는 아직 안 봄)에
// 훅이 먼저 돌아도 미룬 add 를 빈 색인에 대고 승격하지 않는다.
func TestHookCatchUpSeesTwinOnFirstIndex(t *testing.T) {
	opened := newStore(t)
	firstID := seedOne(t, opened, "훅이 볼 본문")
	if err := removeFiles(opened.Dir); err != nil {
		t.Fatal(err)
	}
	twin, err := opened.WriteAdd(addRequest(longSummary, "훅이 볼 본문"))
	if err != nil {
		t.Fatal(err)
	}
	if !promoteNow(t, opened, Options{}, twin).Deferred {
		t.Fatal("add 는 미뤄야 한다")
	}
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	result := database.CatchUp(opened, settings().GC, settings().Secret)
	if result.Duplicated != 1 || result.Added != 0 {
		t.Fatalf("훅도 쌍둥이를 봐야 한다 : %+v", result)
	}
	if got := mdCount(t, opened); got != 1 {
		t.Fatalf("md 는 하나여야 한다 : %d", got)
	}
	path, err := database.pathByID(firstID)
	if err != nil || path == "" {
		t.Fatalf("색인에 없다 : %v", err)
	}
}

func assertIndexed(t *testing.T, opened *store.Store, id string) {
	t.Helper()
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	path, err := database.pathByID(id)
	if err != nil || path == "" {
		t.Fatalf("색인에 없다 : %s %v", id, err)
	}
}
