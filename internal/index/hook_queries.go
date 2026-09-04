package index

// 훅이 절을 채울 때 쓰는 질의다. 훅은 search 를 안 거치고 여기로 바로 온다 —
// 절에 필요한 것은 조건과 날짜 정렬뿐이고, 의존을 늘리면 열기 횟수가 는다 (설계 7-4a).

import (
	"database/sql"
	"os"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// PickPinned 은 종류와 상관없이 고정된 기억을 뽑는 갈래다. 나머지 절은 종류
// 표가 만든다 (설계 7-4 의 절 표).
const PickPinned = "pinned"

// 절 안에서 종류를 묶는 세 꼴이다. 조건과 정렬이 꼴마다 다르다.
const (
	ShapeSeverity = "severity"
	ShapeTodo     = "todo"
	ShapePlain    = "plain"
)

// HookRow 는 주입 줄 하나를 만들 만큼의 칸이다. 본문은 안 담는다.
type HookRow struct {
	ID      string
	Type    string
	Summary string
	Scope   string
	// Source 는 옛 규격의 `source` 값이다. 스키마 v2 에는 열이 없어 늘 비어
	// 있다 — 훅이 Author 가 빌 때 쓰던 퇴로다.
	Source string
	// Author 는 누가 쓴 기억인지다 (설계 결정 3).
	Author   string
	Status   string
	Severity string
	Pinned   bool
	Date     string
}

// HookPick 은 한 절이 무엇을 몇 줄 달라는지다. Scope 가 비면 무필터다.
type HookPick struct {
	// Kind 는 PickPinned 하나뿐이다. 비어 있으면 Types·Shape 를 본다.
	Kind  string
	Types []string
	Shape string
	Scope string
	Limit int
}

// HookPicksFor 는 절 하나를 채울 질의 조각들이다. 한 절에 꼴이 섞이면
// severity → todo → 그 밖 차례로 뽑는다 — 「열린 이슈·할 일」 절이 issue·caution
// 을 먼저 싣고 남는 자리에 todo 를 싣던 차례 그대로다.
func HookPicksFor(table model.TypeTable, section string) []HookPick {
	byShape := map[string][]string{}
	for _, spec := range table.HookTypes(section) {
		shape := shapeOf(spec)
		byShape[shape] = append(byShape[shape], spec.Name)
	}
	picks := []HookPick{}
	for _, shape := range []string{ShapeSeverity, ShapeTodo, ShapePlain} {
		if names := byShape[shape]; len(names) > 0 {
			picks = append(picks, HookPick{Types: names, Shape: shape})
		}
	}
	return picks
}

// shapeOf 는 종류 하나가 어느 꼴로 뽑히는지다.
func shapeOf(spec model.TypeSpec) string {
	switch {
	case spec.Severity:
		return ShapeSeverity
	case spec.TodoStatus:
		return ShapeTodo
	}
	return ShapePlain
}

const hookSelect = `SELECT id, type, summary, scope, author, todo_status, severity, pinned, created_at
	FROM memories WHERE `

// liveWhere 는 무효 기억을 빼는 조건이다. 검색과 같은 규칙이어야 한다 —
// 기한이 지난 것도, 새 기억이 덮은 것도 세션에 안 싣는다 (리뷰 A #19).
// 승격을 기다리는 자동 기억(`review: true`)도 뺀다 — 사람이 안 본 글을 세션
// 머리에 싣지 않는다 (결정 6).
const liveWhere = " AND invalid_at IS NULL AND (superseded_by IS NULL OR superseded_by = '')" +
	" AND review = 0"

// hookWhere 는 갈래·꼴마다 다른 조건과 정렬이다. 모르는 것은 빈 문자열이다.
func hookWhere(pick HookPick) (string, string) {
	if pick.Kind == PickPinned {
		return "pinned = 1" + liveWhere, "updated_at DESC"
	}
	if len(pick.Types) == 0 {
		return "", ""
	}
	where := "type IN (" + inList(pick.Types) + ")" + liveWhere
	switch pick.Shape {
	case ShapeTodo:
		// 끝난 것과 무효 날짜가 지난 todo 는 훅에 안 싣는다 (리뷰 A #19).
		return where + " AND (todo_status IS NULL OR todo_status <> 'done')", "updated_at DESC"
	case ShapeSeverity:
		return where, "(severity = 'high') DESC, created_at DESC"
	}
	return where, "created_at DESC"
}

// inList 는 종류 이름을 SQL 목록으로 만든다. 이름은 `^[a-z][a-z0-9-]{1,19}$` 를
// 이미 지난 값이라 따옴표만 씌운다.
func inList(names []string) string {
	return "'" + strings.Join(names, "', '") + "'"
}

// HookRows 는 한 절 몫을 뽑는다. 상한이 작아 정렬 비용은 색인이 다 먹는다.
func (d *DB) HookRows(pick HookPick) ([]HookRow, error) {
	where, order := hookWhere(pick)
	if where == "" || pick.Limit <= 0 {
		return nil, nil
	}
	arguments := []any{}
	if pick.Scope != "" {
		where += " AND scope = ?"
		arguments = append(arguments, pick.Scope)
	}
	arguments = append(arguments, pick.Limit)
	rows, err := d.sql.Query(hookSelect+where+" ORDER BY "+order+" LIMIT ?", arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanHookRows(rows)
}

func scanHookRows(rows *sql.Rows) ([]HookRow, error) {
	out := []HookRow{}
	for rows.Next() {
		row := HookRow{}
		status, severity := sql.NullString{}, sql.NullString{}
		pinned, created := 0, int64(0)
		err := rows.Scan(&row.ID, &row.Type, &row.Summary, &row.Scope, &row.Author,
			&status, &severity, &pinned, &created)
		if err != nil {
			return out, err
		}
		row.Status, row.Severity, row.Pinned = status.String, severity.String, pinned == 1
		row.Date = time.Unix(created, 0).Format("01-02")
		out = append(out, row)
	}
	return out, rows.Err()
}

// Usable 은 훅이 읽어도 되는 색인인지다. 빈 파일은 깨진 것으로 본다 — 훅은
// 여기서 전수 스캔으로 떨어지지 않는다 (설계 7-7).
func Usable(dir string) bool {
	info, err := os.Stat(DBPath(dir))
	return err == nil && info.Size() > 0
}

// Totals 는 전체 건수와 days 보다 오래된 건수다. 훅이 gc 알림을 낼 때 쓴다.
func (d *DB) Totals(days int) (int, int, error) {
	return d.counts(days)
}

// CatchUp 은 이미 열린 핸들 그대로 승격과 증분 색인을 한다. Run 과 달리 색인을
// 다시 열지 않아 훅의 열기 횟수가 2회 안에 든다 (설계 7-4a).
func (d *DB) CatchUp(opened *store.Store, gc config.GCConfig, secrets config.SecretConfig) *Result {
	return d.CatchUpBy(opened, gc, secrets, time.Time{})
}

// CatchUpBy 는 마감시각까지만 따라잡는다. 넘으면 남은 단계를 건너뛰고 지금
// 있는 색인 그대로 돌아간다 — 훅 한 번은 벽시계 1초 안에 끝나야 한다
// (불변조건 4 · 리뷰 A #5). deadline 이 제로값이면 마감이 없다.
func (d *DB) CatchUpBy(opened *store.Store, gc config.GCConfig, secrets config.SecretConfig, deadline time.Time) *Result {
	result := Result{}
	started := time.Now()
	if overdue(deadline) {
		result.OverBudget = true
		return &result
	}
	release, taken, err := TryLock(opened.Dir)
	if err != nil || !taken {
		result.Locked = true
		result.Elapsed = time.Since(started)
		return &result
	}
	defer release()
	current := runner{db: d, store: opened, result: &result, now: time.Now(),
		quiet: true, scanner: scannerFor(secrets), deadline: deadline,
		types: config.TypesIn(opened.Dir)}
	runQuiet(&current, gc)
	result.Elapsed = time.Since(started)
	return &result
}

// overdue 는 마감을 넘었는지다. 제로값은 마감이 없다는 뜻이다.
func overdue(deadline time.Time) bool {
	return !deadline.IsZero() && !time.Now().Before(deadline)
}

// runQuiet 은 한 단계라도 실패하면 거기서 멈춘다. 훅은 실패해도 있는 색인으로
// 그냥 읽으므로 오류를 위로 올리지 않는다 (설계 7-7).
func runQuiet(current *runner, gc config.GCConfig) {
	if err := current.promoteInbox(); err != nil {
		return
	}
	if current.overBudget() {
		return
	}
	if err := current.indexChanged(); err != nil {
		return
	}
	if current.overBudget() {
		return
	}
	_ = current.finish(gc)
}
