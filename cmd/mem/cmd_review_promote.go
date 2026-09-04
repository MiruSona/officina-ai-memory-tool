package main

// mem review --promote <id> — 자동으로 만들어진 기억을 사람이 승격한다
// (설계 결정 6 · 6절).
//
// 자동 저장은 하되 승격은 사람이 한다. `add` 가 도구로 만든 기억에 머리말
// `review: true` 를 붙여 두면 검색·훅에 안 뜨고, 사람이 이 명령으로 칸을 떼야
// 비로소 보인다.
//
// **이 옵션만 auto 모드 allow 규칙 밖이다** (결정 60). `internal/install` 의
// 규칙이 `Bash(mem review)` · `Bash(mem review --kind:*)` 두 줄로 갈라져 있어
// `--promote` 는 사람 승인 창을 탄다. 규칙을 다시 넓히면 이 결정이 무너진다.

import (
	"fmt"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// promoteReview 는 `review: true` 를 떼는 큐 항목 하나를 넣는다. store/ 를 직접
// 고치는 것은 락을 잡은 `mem index` 뿐이다 (불변조건 2).
func promoteReview(parsed *options, id string) int {
	if id == "" {
		return fail(i18n.T(i18n.ReviewPromoteNeedID))
	}
	if !model.IsID(id) {
		return fail(i18n.T(i18n.BadID, id))
	}
	_, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	memory := findMemory(opened, id)
	if memory == nil {
		return fail(i18n.T(i18n.NoSuchMemory, id))
	}
	// 기다리는 중이 아닌 기억까지 받아 주면 이 명령이 「아무 기억이나 만지는
	// 명령」이 되고, 승인 창을 따로 태운 뜻이 없어진다.
	if !memory.Review {
		return fail(i18n.T(i18n.ReviewPromoteNotHeld, id))
	}
	if err := opened.EnsureDirs(); err != nil {
		return fail(err.Error())
	}
	if _, err := opened.WritePatch(id, map[string]any{"review": false}); err != nil {
		return fail(err.Error())
	}
	fmt.Println(i18n.T(i18n.ReviewPromoted, id))
	noteLog(opened, store.LogPromoted, id)
	return exitOK
}

// findMemory 는 id 로 기억 하나를 찾는다. 색인을 안 연다 — 승격은 드물게
// 부르는 명령이고, 색인이 없거나 낡은 저장소에서도 되어야 한다.
func findMemory(opened *store.Store, id string) *model.Memory {
	files, err := opened.ListMemories()
	if err != nil {
		return nil
	}
	for _, file := range files {
		one, err := opened.ReadListed(file)
		if err != nil || one.Memory == nil {
			continue
		}
		if one.Memory.ID == id {
			return one.Memory
		}
	}
	return nil
}
