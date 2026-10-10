package gc

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// Retired 는 덮였거나 무효인데 본문이 아직 안 접힌 기억 하나다 (설계 7절).
// 사람이 이미 `mem set --by` · `invalid_at` 으로 죽였다고 정한 것이라, 접기는
// 그 판단을 집행할 뿐이다 (결정 10).
type Retired struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
	// SupersededBy 는 이 기억을 덮은 기억 id 다. 비면 InvalidAt 으로 죽은 것이다.
	SupersededBy string `json:"superseded_by,omitempty"`
	// InvalidAt 은 무효 날짜(YYYY-MM-DD)다. 덮인 것도 날짜가 있으면 같이 적는다.
	InvalidAt string `json:"invalid_at,omitempty"`
	Path      string `json:"path"`
	State     string `json:"state"`
}

// cardBasisSQL 은 「산 모음 카드의 `mem:` 근거가 아니다」 조건이다. 접으면 본문
// 해시가 바뀌어 카드가 [낡음] 이 된다 (10-06). exemptSQL 과 gc --retired 가 같이
// 쓴다. ? 하나 = 지금(유닉스 초).
const cardBasisSQL = `id NOT IN (SELECT TRIM(s.value) FROM sources s JOIN memories o ON o.id = s.mem_id
		WHERE s.kind = 'mem' AND o.type = '` + model.TypeObservation + `'
		AND o.superseded_by IS NULL AND o.review = 0
		AND (o.invalid_at IS NULL OR o.invalid_at >= ?))`

// retiredSQL 은 조건 넷이다 — 죽었다(index.LiveRow 의 거꾸로) · 안 접혔다 ·
// 핀 아님 · 카드 근거 아님. 나이·건수 문턱은 없다 (결정 10).
// ? 차례 : 지금(무효) · cold · 지금(카드) · 상한.
const retiredSQL = `SELECT id, type, title, path, state, COALESCE(superseded_by, ''), invalid_at
	FROM memories
	WHERE ((superseded_by IS NOT NULL AND superseded_by <> '')
		OR (invalid_at IS NOT NULL AND invalid_at <= ?))
	AND state <> ?
	AND pinned = 0
	AND ` + cardBasisSQL + `
	ORDER BY created_at ASC, id ASC LIMIT ?`

// pickRetired 는 이번에 접을 죽은 기억을 고른다. 한 번 상한은 다른 gc 와 같다.
func pickRetired(database *index.DB, options Options, result *Result) error {
	total, err := countMemories(database.SQL())
	if err != nil {
		return err
	}
	result.Total = total
	result.Limit = EffectiveBatchLimit(options.GC, total)
	unix := options.Now.Unix()
	rows, err := database.SQL().Query(retiredSQL, unix, index.StateCold, unix, result.Limit)
	if err != nil {
		return err
	}
	defer rows.Close()
	result.Retired, err = scanRetired(rows)
	if err != nil {
		return err
	}
	result.Capped = len(result.Retired) >= result.Limit
	return nil
}

func scanRetired(rows *sql.Rows) ([]Retired, error) {
	found := []Retired{}
	for rows.Next() {
		one := Retired{}
		invalid := sql.NullInt64{}
		if err := rows.Scan(&one.ID, &one.Type, &one.Title, &one.Path, &one.State,
			&one.SupersededBy, &invalid); err != nil {
			return nil, err
		}
		if invalid.Valid {
			one.InvalidAt = time.Unix(invalid.Int64, 0).Format("2006-01-02")
		}
		found = append(found, one)
	}
	return found, rows.Err()
}

// runRetired 는 고른 것을 접는다. 미리보기면 고르기만 한다. 접기는 --fold 와
// 같은 길(foldOne)이라 `mem gc --restore <id>` 로 그대로 되돌린다.
func runRetired(database *index.DB, options Options, result *Result) error {
	if err := pickRetired(database, options, result); err != nil {
		return err
	}
	if options.DryRun {
		return nil
	}
	worker := mover{db: database, store: options.Store, now: options.Now}
	// log.md 는 모아 두고 끝에 한 번 쓴다 (A8). defer 라 중간에 멈춰도 이미
	// 접은 것은 적힌다.
	var reasons []string
	defer func() {
		store.AppendLogLines(options.Store.Dir, options.Now, store.LogFolded, reasons)
	}()
	for _, one := range result.Retired {
		done := foldQuiet(&worker, options, one.ID)
		result.Manual = append(result.Manual, done)
		if done.Done() {
			result.Cooled++
			reasons = append(reasons, one.ID+" (gc --retired)")
		}
	}
	return nil
}

// RetiredLines 는 미리보기 표를 사람이 읽는 줄로 바꾼다. 마지막 줄은 건수와
// 실제로 접는 명령이다.
func RetiredLines(result *Result) []string {
	lines := []string{"  id                 종류        왜 죽었나                       접으면  제목"}
	for _, one := range result.Retired {
		lines = append(lines, fmt.Sprintf("  %-18s %-10s %-30s %-6s %s",
			one.ID, one.Type, retiredWhy(one), "cold", one.Title))
	}
	foot := fmt.Sprintf("%d건, 접으려면 `mem gc --retired --apply` (원본은 아카이브, 되돌리기는 `mem gc --restore <id>`)",
		len(result.Retired))
	if result.Capped {
		foot += fmt.Sprintf(" — 한 번 상한 %d건에 걸렸다, 접은 뒤 다시 돌린다", result.Limit)
	}
	return append(lines, foot)
}

func retiredWhy(one Retired) string {
	if one.SupersededBy != "" {
		return "덮임 → " + one.SupersededBy
	}
	return "무효 " + one.InvalidAt
}
