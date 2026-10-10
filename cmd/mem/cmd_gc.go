package main

import (
	"encoding/json"
	"fmt"

	"github.com/mirusona/officina-ai-memory-tool/internal/gc"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

var gcBools = []string{"dry-run", "json", "retired", "apply"}
var gcValues = []string{"repo", "fold", "restore"}

func init() {
	register(command{name: "gc", run: runGC, bools: gcBools, values: gcValues})
}

// runGC 는 오래된 기억의 본문을 접는다. 파일은 하나도 안 지운다. 종료 코드는
// 사용법이 틀렸을 때(1)만 빼고 늘 0 이다 (설계 8-1).
// `--retired` 만 기본이 미리보기다 — `--apply` 를 줘야 접는다 (기억점검정리설계 7절).
func runGC(argv []string) int {
	parsed, err := parseOptions(argv, gcBools, gcValues)
	if err != nil {
		return fail(err.Error())
	}
	retired, apply := parsed.flags["retired"], parsed.flags["apply"]
	if apply && !retired {
		return fail("--apply 는 --retired 와 함께만 쓴다 (다른 gc 는 --dry-run 이 없으면 바로 접는다)")
	}
	fold, restore := parsed.list("fold"), parsed.list("restore")
	if retired && (len(fold) > 0 || len(restore) > 0) {
		return fail("--retired 는 --fold · --restore 와 같이 못 쓴다. 따로 친다")
	}
	repository, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	result, err := gc.Run(gc.Options{Store: opened, GC: repository.Config.GC,
		DryRun: parsed.flags["dry-run"] || (retired && !apply), Retired: retired,
		Fold: fold, Restore: restore})
	if err != nil {
		return fail(err.Error())
	}
	// 셈을 못 남겨도 정리 결과는 그대로다.
	store.AppendHit(opened.Dir, "gc", "")
	if parsed.flags["json"] {
		data, err := json.Marshal(result)
		if err != nil {
			return fail(err.Error())
		}
		fmt.Println(string(data))
		return exitOK
	}
	if retired {
		printRetired(result)
		return exitOK
	}
	printGC(result, "")
	return exitOK
}

// printRetired 는 --retired 결과를 찍는다. 미리보기는 표, 접었으면 한 건씩.
func printRetired(result *gc.Result) {
	if stopped(result) {
		return
	}
	if result.DryRun {
		for _, line := range gc.RetiredLines(result) {
			fmt.Println(line)
		}
		return
	}
	for _, line := range gc.ManualLines(result) {
		fmt.Println(line)
	}
	fmt.Printf("%d건 중 %d건 접었다. 되돌리려면 mem gc --restore <id>\n", len(result.Retired), result.Cooled)
}

// printGC 는 결과를 한글로 찍는다. prefix 는 mem index 가 이어서 돌렸을 때
// 앞에 붙이는 줄이다.
func printGC(result *gc.Result, prefix string) {
	if prefix != "" {
		fmt.Println(prefix)
	}
	if stopped(result) {
		return
	}
	// 사람이 집은 것이 있으면 그것만 말한다. 나이로 고르는 자동 계획은 안 돈다.
	if len(result.Manual) > 0 {
		for _, line := range gc.ManualLines(result) {
			fmt.Println(line)
		}
		return
	}
	if len(result.Items) == 0 {
		fmt.Println(i18n.T(i18n.GCNothing, result.Total))
		return
	}
	printGCPlan(result)
	fmt.Println(i18n.T(i18n.GCDone, result.Warmed, result.Cooled, result.Total, result.Elapsed.Seconds()))
	if result.Capped {
		fmt.Println(i18n.T(i18n.GCCapped, result.Limit))
	}
	if result.DryRun {
		fmt.Println(i18n.T(i18n.GCDryRunFoot))
	}
}

// stopped 는 아예 안 돈 이유가 있으면 그것만 말한다.
func stopped(result *gc.Result) bool {
	switch {
	case result.ReadOnly:
		fmt.Println(i18n.T(i18n.GCReadOnly))
	case result.NoIndex:
		fmt.Println(i18n.T(i18n.GCNoIndex))
	case result.Locked:
		fmt.Println(i18n.T(i18n.GCLocked))
	case result.TooSoon:
		fmt.Println(i18n.T(i18n.GCTooSoon, result.LastGC))
	default:
		return false
	}
	return true
}

// printGCPlan 은 무엇을 옮기는지 표로 보여준다. 너무 길면 앞쪽만 적는다.
func printGCPlan(result *gc.Result) {
	const shown = 20
	fmt.Println(i18n.T(i18n.GCPlanHeader))
	for at, item := range result.Items {
		if at >= shown {
			break
		}
		fmt.Println(i18n.T(i18n.GCPlanRow, item.ID, item.Type, item.Step, item.Days))
	}
}
