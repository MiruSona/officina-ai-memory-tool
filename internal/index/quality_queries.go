package index

import (
	"database/sql"
	"time"
)

// status 가 한 화면을 채우려면 세는 질의가 몇 개 필요하다. 스키마는 안 건드리고
// 읽기만 한다 (설계 9-5).

// GroupCount 는 이름 하나와 그 건수다.
type GroupCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// CountByType 은 종류별 건수를 많은 것부터 준다.
func (d *DB) CountByType() ([]GroupCount, error) {
	return d.groupCount("SELECT type, COUNT(*) FROM memories GROUP BY type ORDER BY COUNT(*) DESC")
}

// CountByStatus 는 todo 의 상태별 건수다.
func (d *DB) CountByStatus() ([]GroupCount, error) {
	return d.groupCount(`SELECT todo_status, COUNT(*) FROM memories WHERE todo_status IS NOT NULL
		GROUP BY todo_status ORDER BY COUNT(*) DESC`)
}

// CountByState 는 단계별(hot·warm·cold) 건수다.
func (d *DB) CountByState() (map[string]int, error) {
	rows, err := d.groupCount("SELECT state, COUNT(*) FROM memories GROUP BY state")
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, row := range rows {
		counts[row.Name] = row.Count
	}
	return counts, nil
}

func (d *DB) groupCount(query string) ([]GroupCount, error) {
	rows, err := d.sql.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GroupCount{}
	for rows.Next() {
		item := GroupCount{}
		if err := rows.Scan(&item.Name, &item.Count); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// InvalidCount 는 지금 무효인 기억 건수다.
func (d *DB) InvalidCount(now time.Time) (int, error) {
	count := 0
	err := d.sql.QueryRow("SELECT COUNT(*) FROM memories WHERE invalid_at IS NOT NULL AND invalid_at < ?",
		now.Unix()).Scan(&count)
	return count, err
}

// IndexedPaths 는 색인에 든 파일 경로 집합이다. 파일마다 물어보면 2만 번
// 질의가 되므로 한 번에 가져온다.
func (d *DB) IndexedPaths() (map[string]bool, error) {
	rows, err := d.sql.Query("SELECT path FROM memories")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known := map[string]bool{}
	for rows.Next() {
		path := ""
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		known[path] = true
	}
	return known, rows.Err()
}

// CheckRow 는 mem add --check 가 견줄 이미 있는 기억 한 줄이다.
type CheckRow struct {
	ID      string
	Title   string
	Summary string
	Type    string
	Scope   string
}

// RecentForCheck 는 같은 종류의 최근 기억이다. 상한이 있어 2만 건 저장소에서도
// --check 한 번이 일정한 시간에 끝난다.
func (d *DB) RecentForCheck(memoryType string, limit int) ([]CheckRow, error) {
	rows, err := d.sql.Query(`SELECT id, title, summary, type, scope FROM memories
		WHERE type = ? ORDER BY created_at DESC LIMIT ?`, memoryType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := []CheckRow{}
	for rows.Next() {
		item := CheckRow{}
		title, summary := sql.NullString{}, sql.NullString{}
		if err := rows.Scan(&item.ID, &title, &summary, &item.Type, &item.Scope); err != nil {
			return nil, err
		}
		item.Title, item.Summary = title.String, summary.String
		found = append(found, item)
	}
	return found, rows.Err()
}

// SameBodyID 는 본문이 글자 하나까지 같은 기억의 id 다. 승격이 쓰는 것과 같은
// 판정이라 --check 가 「넣으면 중복이 된다」 를 미리 말해 줄 수 있다.
func (d *DB) SameBodyID(body, memoryType, scope string) (string, error) {
	return d.sameBody(bodyHash(body), memoryType, scope)
}

// UserVersion 은 디스크에 적힌 스키마 판이다. 아는 판과 다르면 색인 건강에
// 문제가 있는 것이다.
func (d *DB) UserVersion() (int, error) {
	version := 0
	err := d.sql.QueryRow("PRAGMA user_version").Scan(&version)
	return version, err
}

// VectorFiles 는 색인에 든 기억의 「id · 경로 · 파일 해시」다. 임베딩이 무엇을
// 다시 계산할지 고르는 데 쓴다 (리뷰 B · V1·V5·M-3).
//
// **파일을 하나도 안 연다.** 옛 판은 저장소 md 2만 개를 통째로 읽어 파싱한
// 뒤에야 모델이 없다는 것을 알았다 — 그것이 증분 색인 0.2초를 2.3초로 만든
// 자리다.
func (d *DB) VectorFiles() ([]VectorFile, error) {
	rows, err := d.sql.Query(`SELECT m.id, m.path, COALESCE(f.hash, '')
		FROM memories m LEFT JOIN files f ON f.path = m.path ORDER BY m.docid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []VectorFile{}
	for rows.Next() {
		one := VectorFile{}
		if err := rows.Scan(&one.ID, &one.Path, &one.Hash); err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	return out, rows.Err()
}
