package gc

import (
	"database/sql"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 기억이 갈 수 있는 두 단계. 뜨거운 것은 손대지 않는다 (설계 9-4).
const (
	StepWarm = "warm"
	StepCold = "cold"
)

// Item 은 이번에 옮길 기억 하나다.
type Item struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Step  string `json:"step"`
	Days  int    `json:"days"`
	Path  string `json:"path"`
	State string `json:"state"`
}

// exemptSQL 은 설계 9-4 의 면제 표다. links 는 **사람이 머리말에 적은 것만**
// 담는다 (기계가 찾은 이웃은 auto_links 파생 표에 따로 있다 · 결정 42 뒷정리),
// 그래서 "남이 나를 가리키면 안 접는다" 가 원래 뜻 그대로다.
// invalid_at 이 지난 기억은 종류와 상관없이 접는다 — 이미 뒤집힌
// 결정의 본문을 통째로 들고 있어 봐야 아무도 안 본다.
// 종류 두 자리(면제 종류·열린 할 일)만 표에서 짓고 나머지 조건은 그대로다.
func exemptSQL(table model.TypeTable) string {
	return `pinned = 0
	AND (
		(invalid_at IS NOT NULL AND invalid_at < ?)
		OR type NOT IN (` + sqlNames(table.Where(func(spec model.TypeSpec) bool { return spec.GCKeep })) + `)
	)
	AND NOT (type IN (` + sqlNames(table.Where(func(spec model.TypeSpec) bool { return spec.TodoStatus })) + `) AND (todo_status IS NULL OR todo_status <> 'done'))
	AND (last_hit_at IS NULL OR last_hit_at < ?)
	AND id NOT IN (SELECT dst FROM links WHERE dst <> src)
	AND id NOT IN (SELECT superseded_by FROM memories WHERE superseded_by IS NOT NULL)
	AND importance < 5
	AND NOT (type = 'caution' AND severity = 'high')
	AND id NOT IN (SELECT id FROM (SELECT id, ROW_NUMBER() OVER (
		PARTITION BY scope ORDER BY created_at DESC) rn
		FROM memories WHERE type = 'caution') WHERE rn <= 30)`
}

// sqlNames 는 이름 목록을 SQL IN 목록으로 만든다. 이름은 `^[a-z][a-z0-9-]{1,19}$`
// 를 이미 지난 값이라 따옴표만 씌운다. 빈 목록은 아무것도 안 맞는 목록이다.
func sqlNames(names []string) string {
	if len(names) == 0 {
		return "''"
	}
	return "'" + strings.Join(names, "', '") + "'"
}

// EffectiveBatchLimit 은 한 번 상한 max(200건, 전체의 2%) 다 (설계 9-4).
func EffectiveBatchLimit(settings config.GCConfig, total int) int {
	byPercent := total * settings.BatchPercent / 100
	if byPercent > settings.BatchLimit {
		return byPercent
	}
	return settings.BatchLimit
}

// buildPlan 은 이번에 무엇을 옮길지 고른다. 차가움을 먼저 채운다 — 더 많이
// 줄여 주고, 두 단계가 상한 하나를 나눠 쓴다.
func buildPlan(db *sql.DB, settings config.GCConfig, now time.Time, table model.TypeTable) (int, []Item, error) {
	if len(table) == 0 {
		table = model.DefaultTypes()
	}
	total, err := countMemories(db)
	if err != nil {
		return 0, nil, err
	}
	limit := EffectiveBatchLimit(settings, total)
	items := []Item{}
	if total > settings.ColdCount {
		cold, err := candidates(db, StepCold, settings, now, limit, table)
		if err != nil {
			return total, nil, err
		}
		items = append(items, cold...)
	}
	if total > settings.WarmCount && len(items) < limit {
		warm, err := candidates(db, StepWarm, settings, now, limit, table)
		if err != nil {
			return total, nil, err
		}
		items = append(items, without(warm, items)...)
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return total, items, nil
}

func candidates(db *sql.DB, step string, settings config.GCConfig, now time.Time,
	limit int, table model.TypeTable) ([]Item, error) {
	// 이미 그 단계인 기억은 안 고른다. 그래서 두 번 돌려도 아무것도 안 바뀐다.
	days, where, args := settings.ColdDays, "state <> ? AND ", []any{index.StateCold}
	if step == StepWarm {
		days, where = settings.WarmDays, "state = ? AND "
		args = []any{index.StateHot}
	}
	// ? 가 나오는 차례 그대로다 : 나이 · invalid_at · 마지막 조회 · 상한.
	args = append(args, now.AddDate(0, 0, -days).Unix(), now.Unix(),
		now.AddDate(0, 0, -settings.HitDays).Unix(), limit)
	query := `SELECT id, type, path, created_at, state FROM memories WHERE ` + where +
		`created_at < ? AND ` + exemptSQL(table) + ` ORDER BY created_at ASC LIMIT ?`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows, step, now)
}

func scanItems(rows *sql.Rows, step string, now time.Time) ([]Item, error) {
	found := []Item{}
	for rows.Next() {
		item := Item{Step: step}
		created := int64(0)
		if err := rows.Scan(&item.ID, &item.Type, &item.Path, &created, &item.State); err != nil {
			return nil, err
		}
		item.Days = int(now.Sub(time.Unix(created, 0)).Hours() / 24)
		found = append(found, item)
	}
	return found, rows.Err()
}

// without 은 다른 단계가 이미 가져간 기억을 뺀다.
func without(items, taken []Item) []Item {
	claimed := map[string]bool{}
	for _, item := range taken {
		claimed[item.ID] = true
	}
	kept := []Item{}
	for _, item := range items {
		if !claimed[item.ID] {
			kept = append(kept, item)
		}
	}
	return kept
}

func countMemories(db *sql.DB) (int, error) {
	total := 0
	err := db.QueryRow("SELECT COUNT(*) FROM memories").Scan(&total)
	return total, err
}
