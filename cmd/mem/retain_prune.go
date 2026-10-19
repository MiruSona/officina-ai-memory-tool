package main

// mem retain --prune — 다 쓴 큐 항목을 치운다 (A2 retain 설계 2절). 미리보기가 기본이고 --apply 로 지운다.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/retain"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// prunedJSON 은 --prune --json 의 항목 한 줄이다.
type prunedJSON struct {
	File   string `json:"file"`
	At     string `json:"at"`
	DoneAt string `json:"done_at,omitempty"`
}

// retainPrune 은 치울 큐 항목을 보여 주고, --apply 일 때만 지운다.
func retainPrune(parsed *options, repository *config.Repository, opened *store.Store) int {
	items, err := retain.ReadQueue(repository.Dir)
	if err != nil {
		return exitFor(err)
	}
	plan := retain.PrunePlan(items, time.Now(), repository.Config.Retain.QueueDays)
	paths, rows := []string{}, []prunedJSON{}
	for _, item := range plan {
		paths = append(paths, item.Path)
		rows = append(rows, prunedJSON{File: filepath.Base(item.Path), At: item.Entry.At, DoneAt: item.Entry.DoneAt})
	}
	apply := parsed.flags["apply"]
	if apply && len(paths) > 0 {
		if err := retain.Prune(paths); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
		}
		noteLog(opened, store.LogDiscarded, i18n.T(i18n.RetainLogPrune, len(paths)))
	}
	if parsed.flags["json"] {
		data, _ := json.Marshal(map[string]any{"apply": apply, "count": len(rows), "items": rows})
		fmt.Println(string(data))
		return exitOK
	}
	fmt.Println(i18n.T(i18n.RetainPrunePlan, len(rows)))
	for _, row := range rows {
		done := row.DoneAt
		if done == "" {
			done = "-"
		}
		fmt.Println(i18n.T(i18n.RetainPruneRow, row.File, row.At, done))
	}
	if apply {
		fmt.Println(i18n.T(i18n.RetainPruneDone, len(rows)))
	} else if len(rows) > 0 {
		fmt.Println(i18n.T(i18n.RetainPruneDryRun))
	}
	return exitOK
}
