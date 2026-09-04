package index

// 역참조 질의다 — 「이 기억을 근거로 삼은 기억이 누구인가」.
// `mem show` 의 「나를 근거로 삼은 기억」 절과 `mem set --by` 의 알림 한 줄이
// 쓴다. 검토 큐(`mem review --kind basis`)는 md 를 훑으므로 여기를 안 쓴다
// (설계 `2026-08-30-무효화전파설계.md` 6절).

import (
	"database/sql"
	"sort"
	"time"
)

// 어떤 관계로 걸렸나. `sources` 의 `mem:` 이 진짜 근거고 `links` 는 그보다 약하다.
const (
	UsedBySource = "source"
	UsedByLink   = "link"
)

// UsedByRow 는 이 기억을 근거·링크로 가리킨 기억 하나다.
type UsedByRow struct {
	ID    string `json:"id"`
	By    string `json:"by"`
	Type  string `json:"type"`
	Date  string `json:"date"`
	Title string `json:"title"`
	Live  bool   `json:"live"`
	// Snoozed 는 stale_after 가 아직 안 온 기억이다 — 검토 큐가 건너뛴다.
	Snoozed bool `json:"snoozed,omitempty"`
}

// usedBySQL 은 sources 의 `mem:` 과 links 를 한 번에 모은다.
// superseded_by 는 안 본다 — 그것은 「덮은 것」이지 근거가 아니다.
const usedBySQL = `SELECT m.id, m.type, m.title, m.summary, m.created_at, m.invalid_at,
	m.superseded_by, m.stale_after, 'source' FROM sources s JOIN memories m ON m.id = s.mem_id
	WHERE s.kind = 'mem' AND s.value = ? AND m.id <> ?
UNION ALL
SELECT m.id, m.type, m.title, m.summary, m.created_at, m.invalid_at,
	m.superseded_by, m.stale_after, 'link' FROM links l JOIN memories m ON m.id = l.src
	WHERE l.dst = ? AND m.id <> ?`

// UsedBy 는 이 기억을 가리킨 기억들이다. 날짜 오름차순, 같으면 id 순이다
// (review 의 차례와 같다). 같은 기억이 근거와 링크로 둘 다 걸리면 근거만 남긴다.
func (d *DB) UsedBy(id string) ([]UsedByRow, error) {
	return d.usedByAt(id, time.Now())
}

// usedByAt 은 오늘을 밖에서 받는다. 생사·스누즈가 시각에 기대는 판정이라
// 시험이 미래 날짜를 넣어 볼 수 있어야 한다.
func (d *DB) usedByAt(id string, now time.Time) ([]UsedByRow, error) {
	rows, err := d.sql.Query(usedBySQL, id, id, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := map[string]UsedByRow{}
	for rows.Next() {
		row, err := scanUsedBy(rows, now)
		if err != nil {
			return nil, err
		}
		if before, seen := found[row.ID]; seen && before.By == UsedBySource {
			continue
		}
		found[row.ID] = row
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sortedUsedBy(found), nil
}

func scanUsedBy(rows *sql.Rows, now time.Time) (UsedByRow, error) {
	row := UsedByRow{}
	title, summary := "", ""
	created := int64(0)
	invalid, stale := sql.NullInt64{}, sql.NullInt64{}
	superseded := sql.NullString{}
	err := rows.Scan(&row.ID, &row.Type, &title, &summary, &created, &invalid,
		&superseded, &stale, &row.By)
	if err != nil {
		return row, err
	}
	row.Title = title
	if row.Title == "" {
		row.Title = summary
	}
	row.Date = time.Unix(created, 0).Format("2006-01-02")
	row.Live = LiveRow(invalid, superseded.String, now)
	row.Snoozed = SnoozedRow(stale, now)
	return row, nil
}

func sortedUsedBy(found map[string]UsedByRow) []UsedByRow {
	out := make([]UsedByRow, 0, len(found))
	for _, row := range found {
		out = append(out, row)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Date != out[b].Date {
			return out[a].Date < out[b].Date
		}
		return out[a].ID < out[b].ID
	})
	return out
}
