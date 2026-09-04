package index

// 생사 판정 한 곳이다 (설계 `2026-08-30-무효화전파설계.md` 2절).
//
//	죽음 = superseded_by 가 있다  또는  invalid_at 이 오늘까지 지났다
//
// 같은 기준이 세 자리에 있었는데 셋이 달라서, 같은 기억이 검색에서는 살아 있고
// 역참조에서는 죽어 있었다. SQL 쪽 술어는 whereOf(search_queries.go)가 쓰고,
// 행을 읽고 나서 판정하는 자리는 여기를 쓴다.

import (
	"database/sql"
	"time"
)

// LiveRow 는 색인 한 행이 지금 살아 있는지다. invalid_at·stale_after 는 NULL 이
// 있어서 sql.NullInt64 를 그대로 받는다.
func LiveRow(invalidAt sql.NullInt64, supersededBy string, now time.Time) bool {
	if supersededBy != "" {
		return false
	}
	if !invalidAt.Valid {
		return true
	}
	return invalidAt.Int64 > now.Unix()
}

// SnoozedRow 는 stale_after 가 아직 안 온 기억이다. 검토 큐가 건너뛰는 자라
// 알림 셈도 같이 건너뛴다 (quality/checks_basis.go 의 staleBasis 와 같은 규칙).
func SnoozedRow(staleAfter sql.NullInt64, now time.Time) bool {
	return staleAfter.Valid && staleAfter.Int64 > now.Unix()
}
