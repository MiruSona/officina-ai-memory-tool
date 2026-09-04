package index

import (
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 역참조 — sources 의 mem: 과 links 를 다 모으고, 둘 다인 기억은 한 번만 낸다.
func TestUsedByGathersSourcesAndLinks(t *testing.T) {
	opened := newStore(t)
	base := sampleMemory("20260823-aaaa0001")
	bySource := sampleMemory("20260823-bbbb0002")
	bySource.Sources = []string{model.SourceMem + base.ID}
	byLink := sampleMemory("20260823-cccc0003")
	byLink.Links = []string{base.ID}
	byBoth := sampleMemory("20260823-dddd0004")
	byBoth.Sources = []string{model.SourceMem + base.ID}
	byBoth.Links = []string{base.ID}
	// 아무 관계도 없는 기억은 안 나와야 한다.
	stranger := sampleMemory("20260823-eeee0005")
	for _, one := range []*model.Memory{base, bySource, byLink, byBoth, stranger} {
		writeMemory(t, opened.Dir, one)
	}
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	rows, err := database.UsedBy(base.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("역참조가 3건이라야 한다 : %+v", rows)
	}
	by := map[string]string{}
	for _, row := range rows {
		by[row.ID] = row.By
		if row.Date == "" || row.Title == "" || !row.Live {
			t.Fatalf("칸이 비었다 : %+v", row)
		}
	}
	if by[bySource.ID] != UsedBySource || by[byLink.ID] != UsedByLink {
		t.Fatalf("관계를 잘못 적었다 : %v", by)
	}
	if by[byBoth.ID] != UsedBySource {
		t.Fatalf("근거와 링크가 둘 다면 근거로 낸다 : %v", by)
	}
}

// 자기 자신은 안 센다. 죽은 기억은 나오되 live 가 거짓이다.
func TestUsedByMarksDeadAndSkipsSelf(t *testing.T) {
	opened := newStore(t)
	base := sampleMemory("20260823-1111aaaa")
	base.Links = []string{base.ID}
	dead := sampleMemory("20260823-2222bbbb")
	dead.Sources = []string{model.SourceMem + base.ID}
	dead.InvalidAt = "2026-08-24"
	dead.SupersededBy = "20260823-3333cccc"
	for _, one := range []*model.Memory{base, dead} {
		writeMemory(t, opened.Dir, one)
	}
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	rows, err := database.UsedBy(base.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != dead.ID {
		t.Fatalf("자기참조를 뺀 한 건이라야 한다 : %+v", rows)
	}
	if rows[0].Live {
		t.Fatal("이미 무효인 기억을 살아 있다고 했다")
	}
}

// 판이 5 보다 낮은 색인은 통째로 다시 만든다 — 역참조 인덱스가 늘었다.
func TestSchemaV4IsRebuilt(t *testing.T) {
	opened := newStore(t)
	writeMemory(t, opened.Dir, sampleMemory("20260823-4444dddd"))
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL().Exec("PRAGMA user_version=4"); err != nil {
		t.Fatal(err)
	}
	database.Close()
	again, err := Open(opened.Dir)
	if err != nil {
		t.Fatalf("옛 판은 다시 만들어야 한다 : %v", err)
	}
	defer again.Close()
	version := 0
	if err := again.SQL().QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion {
		t.Fatalf("판 번호가 틀리다 : %d", version)
	}
}

// 리뷰 #2 — 생사 판정은 세 자리가 같아야 한다. 아직 안 온 invalid_at 은
// 살아 있는 것이고(search 의 WHERE 절과 같다), stale_after 가 미래면 검토 큐가
// 건너뛰는 자라 여기서도 표시해 준다.
func TestUsedByFollowsSearchLiveness(t *testing.T) {
	opened := newStore(t)
	base := sampleMemory("20260823-5555eeee")
	later := sampleMemory("20260823-6666ffff")
	later.Sources = []string{model.SourceMem + base.ID}
	later.StaleAfter = ""
	writeMemory(t, opened.Dir, base)
	writeMemory(t, opened.Dir, later)
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// invalid_at 만 든 기억은 관문이 거절해서 파일로는 못 만든다. 색인에는
	// 옛 판에서 넘어온 행이 그렇게 남아 있을 수 있어 여기서 직접 만든다.
	future := time.Now().AddDate(1, 0, 0).Unix()
	if _, err := database.SQL().Exec(
		"UPDATE memories SET invalid_at = ?, superseded_by = NULL WHERE id = ?",
		future, later.ID); err != nil {
		t.Fatal(err)
	}
	rows, err := database.UsedBy(base.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("역참조가 한 건이라야 한다 : %+v", rows)
	}
	if !rows[0].Live {
		t.Fatal("아직 안 온 invalid_at 인데 죽었다고 한다")
	}
	if rows[0].Snoozed {
		t.Fatal("stale_after 가 없는데 미뤄 뒀다고 한다")
	}
}

// stale_after 가 미래면 검토 큐가 안 잡는다 — 알림 셈도 같이 건너뛰라고 표시한다.
func TestUsedByMarksSnoozed(t *testing.T) {
	opened := newStore(t)
	base := sampleMemory("20260823-7777aaaa")
	later := sampleMemory("20260823-8888bbbb")
	later.Sources = []string{model.SourceMem + base.ID}
	later.StaleAfter = "2099-01-01"
	writeMemory(t, opened.Dir, base)
	writeMemory(t, opened.Dir, later)
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	rows, err := database.UsedBy(base.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !rows[0].Snoozed {
		t.Fatalf("미뤄 둔 기억을 표시 안 했다 : %+v", rows)
	}
}
