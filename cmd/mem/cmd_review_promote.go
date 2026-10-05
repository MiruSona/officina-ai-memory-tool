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

	"github.com/mirusona/officina-ai-memory-tool/internal/gc"
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

// rejectReview 는 보류 기억을 버린다 — 지우지 않고 접는다(cold · 본문은 archive).
// `gc --restore <id>` 로 되돌릴 수 있다. --promote 처럼 allow 규칙 밖이다 (결정 60).
func rejectReview(parsed *options, id string) int {
	if id == "" {
		return fail(i18n.T(i18n.ReviewPromoteNeedID))
	}
	if !model.IsID(id) {
		return fail(i18n.T(i18n.BadID, id))
	}
	repository, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	memory := findMemory(opened, id)
	if memory == nil {
		return fail(i18n.T(i18n.NoSuchMemory, id))
	}
	if !memory.Review {
		return fail(i18n.T(i18n.ReviewRejectNot, id))
	}
	result, err := gc.Run(gc.Options{Store: opened, GC: repository.Config.GC, Fold: []string{id}})
	if err != nil {
		return fail(err.Error())
	}
	if code := manualFailed(result); code != exitOK {
		return code
	}
	fmt.Println(i18n.T(i18n.ReviewRejectDone, id))
	noteLog(opened, store.LogDiscarded, id)
	return exitOK
}

// manualFailed 는 손으로 접기가 안 됐을 때 까닭을 찍는다 — 색인이 없거나, 락을
// 못 잡았거나, 이미 접혀 있다.
func manualFailed(result *gc.Result) int {
	if len(result.Manual) == 1 && result.Manual[0].Done() {
		return exitOK
	}
	printGC(result, "")
	return exitCheck
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
