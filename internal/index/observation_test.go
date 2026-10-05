package index

import (
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 모음 기억(B1) — 판 7 다시 만들기와 색인 때 낡음 판정 (자동쌓기설계 3-4).

// TestSchemaV6IsRebuiltForObservation 은 B1 앞 판(6) 색인을 열면 통째로 다시
// 만들어 새 열(basis_hash · obs_stale)이 생기는지 본다.
func TestSchemaV6IsRebuiltForObservation(t *testing.T) {
	if SchemaVersion < 7 {
		t.Fatalf("B1 은 판 7 이다 : %d", SchemaVersion)
	}
	opened := newStore(t)
	writeMemory(t, opened.Dir, sampleMemory("20260823-5c8e1a02"))
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL().Exec("PRAGMA user_version=6"); err != nil {
		t.Fatal(err)
	}
	database.Close()
	again, err := Open(opened.Dir)
	if err != nil {
		t.Fatalf("판 6 은 다시 만들어야 한다 : %v", err)
	}
	defer again.Close()
	version := 0
	again.SQL().QueryRow("PRAGMA user_version").Scan(&version)
	if version != SchemaVersion {
		t.Fatalf("다시 만든 뒤 판이 %d 다", version)
	}
	for _, column := range []string{"basis_hash", "obs_stale"} {
		if _, err := again.SQL().Exec("SELECT " + column + " FROM memories LIMIT 1"); err != nil {
			t.Fatalf("열이 없다 : %s (%v)", column, err)
		}
	}
}

// obsCard 는 근거 둘을 묶은 모음 기억이다. 해시는 지금 근거로 맞춰 둔다.
func obsCard(id string, basis ...*model.Memory) *model.Memory {
	parts := []model.BasisPart{}
	sources := []string{}
	body := ""
	for at, one := range basis {
		parts = append(parts, model.PartOf(one))
		sources = append(sources, model.SourceMem+one.ID)
		body += string(rune('1'+at)) + ". " + one.Date + " " + one.Title + " [mem:" + one.ID + "]\n"
	}
	return &model.Memory{ID: id, Type: model.TypeObservation, Date: "2026-08-24", Spec: model.SpecV2,
		Title: "모음 · 색인 스키마 판", Summary: "색인 스키마를 판 2 로 올린 일과 이어진 기억 두 건을 모은 카드다",
		Tags: []string{"index", "search"}, Scope: "aimemorytool", Author: "mem-consolidate/b1",
		Sources: sources, Origin: "card", BasisHash: model.BasisHash(parts), Rev: 1, Body: body}
}

func staleOfID(t *testing.T, dir, id string) string {
	t.Helper()
	database, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	found := "?"
	if err := database.SQL().QueryRow("SELECT obs_stale FROM memories WHERE id = ?", id).Scan(&found); err != nil {
		t.Fatal(err)
	}
	return found
}

// TestObservationStaleness 는 근거가 그대로면 안 낡고, 고치면 changed,
// 보류하면 held, 근거 밖 기억이 덮으면 superseded, 없어지면 missing 인지 본다.
func TestObservationStaleness(t *testing.T) {
	opened := newStore(t)
	left := sampleMemory("20260823-aaaa0001")
	right := sampleMemory("20260823-aaaa0002")
	right.Title = "색인 열을 하나 더 둔다"
	card := obsCard("20260824-cccc0001", left, right)
	for _, one := range []*model.Memory{left, right, card} {
		writeMemory(t, opened.Dir, one)
	}
	runIndex(t, opened)
	if got := staleOfID(t, opened.Dir, card.ID); got != "" {
		t.Fatalf("근거가 그대로인데 낡았다 : %q", got)
	}
	steps := []struct {
		name string
		edit func()
		want string
	}{
		{"고침", func() { right.Summary += " 고쳤다" }, ObsStaleChanged},
		{"보류", func() { right.Review = true }, ObsStaleHeld},
		{"덮임", func() {
			right.Review = false
			right.SupersededBy, right.InvalidAt = "20260823-dddd0001", "2026-08-30"
			cover := sampleMemory(right.SupersededBy)
			cover.Title = "색인 열을 둘 더 둔다"
			writeMemory(t, opened.Dir, cover)
		}, ObsStaleSuperseded},
	}
	for _, step := range steps {
		step.edit()
		writeMemory(t, opened.Dir, right)
		runIndex(t, opened)
		if got := staleOfID(t, opened.Dir, card.ID); got != step.want {
			t.Fatalf("%s 뒤 낡음 까닭이 %q 다. 바란 것 %q", step.name, got, step.want)
		}
	}
	// 근거 안에서 덮은 사슬 카드는 옛 구성원이 죽어 있어도 안 낡았다.
	chainOld := sampleMemory("20260823-eeee0001")
	chainNew := sampleMemory("20260823-eeee0002")
	chainNew.Title = "색인 스키마를 판 3 으로 올린다"
	chainOld.SupersededBy, chainOld.InvalidAt = chainNew.ID, "2026-08-24"
	chain := obsCard("20260824-cccc0002", chainOld, chainNew)
	for _, one := range []*model.Memory{chainOld, chainNew, chain} {
		writeMemory(t, opened.Dir, one)
	}
	runIndex(t, opened)
	if got := staleOfID(t, opened.Dir, chain.ID); got != "" {
		t.Fatalf("사슬 카드가 제 옛 구성원 때문에 낡았다 : %q", got)
	}
	chain.Sources = append(chain.Sources, model.SourceMem+"20260823-ffff0009")
	writeMemory(t, opened.Dir, chain)
	runIndex(t, opened)
	if got := staleOfID(t, opened.Dir, chain.ID); got != ObsStaleMissing {
		t.Fatalf("없는 근거인데 %q 다", got)
	}
}

// TestNoObservationLeavesIndexAlone 은 모음 기억이 없는 저장소에서 새 열이 다
// 비어 있는지 본다 — 검색 점수가 B1 앞과 같아야 한다.
func TestNoObservationLeavesIndexAlone(t *testing.T) {
	opened := newStore(t)
	writeMemory(t, opened.Dir, sampleMemory("20260823-5c8e1a02"))
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if database.HasObservations() {
		t.Fatal("모음 기억이 없는데 있다고 한다")
	}
	count := -1
	database.SQL().QueryRow("SELECT COUNT(*) FROM memories WHERE obs_stale <> '' OR basis_hash <> ''").Scan(&count)
	if count != 0 {
		t.Fatalf("모음 기억이 없는데 새 열에 값이 %d건 있다", count)
	}
}
