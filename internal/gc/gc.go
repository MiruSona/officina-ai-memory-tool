// Package gc 는 오래된 기억의 본문을 규칙으로 접고 원본을 아카이브에 남긴다.
// **파일은 하나도 안 지운다** (불변조건 5). 훅 안에서는 절대 안 돈다 (설계 9-4).
package gc

import (
	"strconv"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// MetaKey 는 마지막으로 정리한 시각을 적어 두는 자리다.
const MetaKey = "last_gc_at"

// Cooldown 은 한 번 정리하고 다음까지 기다리는 시간이다 (설계 9-4).
const Cooldown = 24 * time.Hour

// Options 는 정리 한 번이다.
type Options struct {
	Store  *store.Store
	GC     config.GCConfig
	DryRun bool
	Now    time.Time
	// Fold 는 사람이 손으로 집어 접을 기억 id 다 (`--fold`). 하나라도 있으면
	// 나이로 고르는 자동 계획은 안 돈다.
	Fold []string
	// Restore 는 접은 본문을 아카이브에서 되돌릴 기억 id 다 (`--restore`).
	Restore []string
	// Types 는 이 저장소의 기억 종류 표다. 비면 vocab.toml 에서 읽는다.
	Types model.TypeTable
}

// Result 는 센 결과다. 한글 보고는 cmd 가 찍는다.
type Result struct {
	DryRun   bool   `json:"dry_run"`
	Locked   bool   `json:"locked"`
	NoIndex  bool   `json:"no_index"`
	ReadOnly bool   `json:"read_only"`
	TooSoon  bool   `json:"too_soon"`
	LastGC   string `json:"last_gc_at,omitempty"`
	Total    int    `json:"total"`
	Items    []Item `json:"items"`
	Warmed   int    `json:"warmed"`
	Cooled   int    `json:"cooled"`
	Capped   bool   `json:"capped"`
	Limit    int    `json:"limit"`
	// Deleted 는 늘 0 이다. 값을 내보내는 이유는 "안 지웠다" 를 눈으로
	// 확인할 수 있게 하기 위해서다 (불변조건 5).
	Deleted int `json:"deleted"`
	// Manual 은 --fold · --restore 가 기억 하나하나에 한 일이다.
	Manual  []Manual      `json:"manual,omitempty"`
	Elapsed time.Duration `json:"-"`
}

// Run 은 정리를 계획하고, 미리보기가 아니면 실제로 한다.
func Run(options Options) (*Result, error) {
	if options.Now.IsZero() {
		options.Now = time.Now()
	}
	if len(options.Types) == 0 && options.Store != nil {
		options.Types = config.TypesIn(options.Store.Dir)
	}
	started := time.Now()
	result := Result{DryRun: options.DryRun}
	err := dispatch(options, &result)
	result.Elapsed = time.Since(started)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// dispatch 는 미리보기를 락 밖에 둔다. 아무것도 안 쓰는 미리보기가 지금 도는
// 색인을 막을 이유가 없다.
func dispatch(options Options, result *Result) error {
	if options.Store.ReadOnly {
		result.ReadOnly = true
		return nil
	}
	if !index.Exists(options.Store.Dir) {
		result.NoIndex = true
		return nil
	}
	if options.DryRun && !manualWanted(options) {
		return preview(options, result)
	}
	release, taken, err := index.TryLock(options.Store.Dir)
	if err != nil {
		return err
	}
	if !taken {
		result.Locked = true
		return nil
	}
	defer release()
	return runLocked(options, result)
}

// preview 는 아무것도 안 쓴다. 없는 DB 를 열면 만들어지므로 그 앞에서 막았다.
func preview(options Options, result *Result) error {
	database, err := index.Open(options.Store.Dir)
	if err != nil {
		return err
	}
	defer database.Close()
	tooSoon(database, options.Now, result)
	return plan(database, options, result)
}

func runLocked(options Options, result *Result) error {
	database, err := index.Open(options.Store.Dir)
	if err != nil {
		return err
	}
	defer database.Close()
	// 사람이 집은 것은 쉬는 시간(Cooldown)과 상관없이 바로 한다. 자동으로
	// 도는 정리가 아니라 시킨 일이다.
	if manualWanted(options) {
		return runManual(database, options, result)
	}
	if tooSoon(database, options.Now, result) {
		return nil
	}
	if err := plan(database, options, result); err != nil {
		return err
	}
	if err := move(database, options, result); err != nil {
		return err
	}
	return database.SetMeta(MetaKey, options.Now.Format(time.RFC3339))
}

func plan(database *index.DB, options Options, result *Result) error {
	total, items, err := buildPlan(database.SQL(), options.GC, options.Now, options.Types)
	if err != nil {
		return err
	}
	result.Total = total
	result.Items = items
	result.Limit = EffectiveBatchLimit(options.GC, total)
	result.Capped = len(items) >= result.Limit
	for _, item := range items {
		if item.Step == StepCold {
			result.Cooled++
			continue
		}
		result.Warmed++
	}
	return nil
}

// move 는 계획을 하나씩 적용한다. 파일을 먼저 쓰고 색인 행을 맞춘다. 중간에
// 실패해도 이미 쓴 파일은 남고, 다음 index 가 그것을 다시 색인한다.
func move(database *index.DB, options Options, result *Result) error {
	if len(result.Items) == 0 {
		return nil
	}
	worker := mover{db: database, store: options.Store, now: options.Now}
	for _, item := range result.Items {
		if err := worker.applyItem(item); err != nil {
			return err
		}
		// 접힌 것도 시간순 기록에 남긴다 (설계 3-5).
		store.AppendLog(options.Store.Dir, options.Now, store.LogFolded,
			foldReason(item))
	}
	return nil
}

// foldReason 은 log.md 한 줄에 적는 「무엇을 왜」다.
func foldReason(item Item) string {
	return item.ID + " (gc, " + item.Step + " " + itoa(item.Days) + "일)"
}

func itoa(value int) string { return strconv.Itoa(value) }

// tooSoon 은 마지막으로 돈 때를 본다. 못 읽거나 없으면 돌아도 된다는 뜻이다.
func tooSoon(database *index.DB, now time.Time, result *Result) bool {
	stamp, err := database.Meta(MetaKey)
	if err != nil || stamp == "" {
		return false
	}
	last, err := time.Parse(time.RFC3339, stamp)
	if err != nil || now.Sub(last) >= Cooldown {
		return false
	}
	result.TooSoon = true
	result.LastGC = stamp
	return true
}
