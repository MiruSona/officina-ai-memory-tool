package index

// 사용 피드백 2026-09-20 회귀 — `mem add --by <옛id>` 로 들어온 기억이 색인 때
// 제목이 닮은 옛 기억에 합쳐져 새 id 가 사라졌다. 덮는 기억은 옛 기억과 닮은
// 것이 당연하니 합침 대상에서 빼고 늘 새 파일로 둔다.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

func TestSupersedingAddDoesNotMergeIntoTwin(t *testing.T) {
	opened := newStore(t)
	if _, err := opened.WriteAdd(addRequest(longSummary, "첫 본문")); err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	newer := addRequest(longSummary, "옛 결정을 덮는 두 번째 본문")
	newer.Supersedes = "20260101-aaaaaaaa"
	name, err := opened.WriteAdd(newer)
	if err != nil {
		t.Fatal(err)
	}
	result := runIndex(t, opened)
	if result.Appended != 0 || result.Added != 1 {
		t.Fatalf("덮는 기억은 안 붙고 새 파일로 가야 한다 : %+v", result)
	}
	id := model.QueueID(name, newer.Body, newer.Date)
	files, _ := opened.ListMemories()
	found := false
	for _, file := range files {
		if strings.HasSuffix(filepath.ToSlash(file.Path), "/"+id+".md") {
			found = true
		}
	}
	if !found {
		t.Fatalf("add 가 알려 준 id %s 의 파일이 없다 : %v", id, files)
	}
}

// 리뷰 2026-09-23 — 같은 `add --by X` 를 두 번 치면 글자까지 같은 결정이 두 벌
// 사면 안 된다. 완전중복 검사는 남기고, 두 번째 add 가 보낸 덮임 표시는 살아남은
// 쌍둥이 id 로 다시 댄다 — X 의 superseded_by 가 없는 id 를 가리키면 안 된다.
func TestSupersedingTwiceKeepsOneFile(t *testing.T) {
	opened := newStore(t)
	oldName, err := opened.WriteAdd(addRequest(longSummary, "옛 본문"))
	if err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	oldID := model.QueueID(oldName, "옛 본문", today())
	ids := []string{}
	for round := 0; round < 2; round++ {
		newer := addRequest(longSummary, "옛 결정을 덮는 같은 본문")
		newer.Supersedes = oldID
		name, err := opened.WriteAdd(newer)
		if err != nil {
			t.Fatal(err)
		}
		id := model.QueueID(name, newer.Body, newer.Date)
		ids = append(ids, id)
		if _, err := opened.WritePatch(oldID, map[string]any{"superseded_by": id, "invalid_at": today()}); err != nil {
			t.Fatal(err)
		}
	}
	result := runIndex(t, opened)
	if result.Added != 1 || result.Duplicated != 1 {
		t.Fatalf("새 파일 1 · 중복 1 이어야 한다 : %+v", result)
	}
	files, _ := opened.ListMemories()
	if len(files) != 2 {
		t.Fatalf("옛 기억 + 덮는 기억 한 벌, 파일이 둘이어야 한다 : %v", files)
	}
	file, err := opened.ReadMemory(model.StorePath(oldID))
	if err != nil {
		t.Fatal(err)
	}
	if file.Memory.SupersededBy != ids[0] {
		t.Fatalf("옛 기억이 살아남은 쌍둥이(%s)가 아니라 %q 를 가리킨다", ids[0], file.Memory.SupersededBy)
	}
}

// 옛 기억과 글자까지 같은 본문으로 `--by` 를 걸면 덮을 것이 없다. 옛 기억이
// 제 자신을 덮는다고 적히면 안 된다.
func TestSupersedingWithSameBodyDoesNotSelfSupersede(t *testing.T) {
	opened := newStore(t)
	oldName, err := opened.WriteAdd(addRequest(longSummary, "같은 본문"))
	if err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	oldID := model.QueueID(oldName, "같은 본문", today())
	same := addRequest(longSummary, "같은 본문")
	same.Supersedes = oldID
	name, err := opened.WriteAdd(same)
	if err != nil {
		t.Fatal(err)
	}
	newID := model.QueueID(name, same.Body, same.Date)
	if _, err := opened.WritePatch(oldID, map[string]any{"superseded_by": newID, "invalid_at": today()}); err != nil {
		t.Fatal(err)
	}
	result := runIndex(t, opened)
	if result.Bad != 0 {
		t.Fatalf("못 쓴 항목이 생겼다 : %+v", result)
	}
	file, err := opened.ReadMemory(model.StorePath(oldID))
	if err != nil {
		t.Fatal(err)
	}
	if file.Memory.SupersededBy != "" {
		t.Fatalf("옛 기억이 덮였다고 적혔다 : %q", file.Memory.SupersededBy)
	}
}
