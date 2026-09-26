package index

// 승격 영수증 (리뷰 2026-09-26 #1).
//
// `mem add` 가 큐에 쓴 뒤 락을 기다리는 사이 남(`mem index` · 다른 add)이 그 큐
// 파일을 먼저 승격할 수 있다. 그러면 add 가 락을 잡았을 때 파일이 없다. 이
// 표는 「큐 파일 이름 → 실제로 남은 id」 를 잠깐 들고 있어서, 늦게 온 add 가
// 남이 합치거나 쌍둥이로 돌린 뒤의 **진짜 id** 를 찍을 수 있게 한다.
//
// 파생 표다. 색인을 새로 세우면 사라진다 — 그때 add 는 「이미 승격됨 — id 는
// mem index 뒤 확인」 으로 알린다. 스키마 판은 안 올린다 (auto_links 와 같은 길).

import (
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

// promotedDDL 은 영수증 표 꼴이다.
const promotedDDL = `CREATE TABLE IF NOT EXISTS promoted (name TEXT PRIMARY KEY,
	queued TEXT NOT NULL, id TEXT NOT NULL, state TEXT NOT NULL, at INTEGER NOT NULL) WITHOUT ROWID`

// promotedKeep 은 영수증을 들고 있는 시간이다. add 가 락을 기다리는 것은 1초
// 남짓이라 10분이면 넉넉하다. 2만 건 이전이 남긴 줄도 다음 회차에 치운다.
const promotedKeep = 10 * time.Minute

// EnsurePromoted 는 영수증 표를 만든다. 쓰기 락 안에서 부른다.
func EnsurePromoted(db *sql.DB) error {
	_, err := db.Exec(promotedDDL)
	return err
}

// keepPromoted 는 이름 하나의 영수증을 남긴다. 같은 이름이 이미 있으면 먼저
// 적힌 것이 맞다 — record 의 「먼저 정해진 결과가 맞다」 와 같은 규칙이다.
func (d *DB) keepPromoted(name, queued, id, state string, at time.Time) error {
	_, err := d.sql.Exec("INSERT OR IGNORE INTO promoted(name, queued, id, state, at) VALUES(?, ?, ?, ?, ?)",
		name, queued, id, state, at.Unix())
	return err
}

// dropPromoted 는 끝나지 않은 항목의 영수증을 지운다.
func (d *DB) dropPromoted(name string) error {
	_, err := d.sql.Exec("DELETE FROM promoted WHERE name = ?", name)
	return err
}

// prunePromoted 는 오래된 영수증을 치운다.
func (d *DB) prunePromoted(now time.Time) error {
	_, err := d.sql.Exec("DELETE FROM promoted WHERE at < ?", now.Add(-promotedKeep).Unix())
	return err
}

// receipt 는 영수증 한 줄이다.
type receipt struct {
	Queued string
	ID     string
	State  string
}

// promotedOf 는 이름 하나의 영수증이다. 없으면 빈 영수증이다.
func (d *DB) promotedOf(name string) (receipt, error) {
	found := receipt{}
	err := d.sql.QueryRow("SELECT queued, id, state FROM promoted WHERE name = ?", name).
		Scan(&found.Queued, &found.ID, &found.State)
	if errors.Is(err, sql.ErrNoRows) {
		return receipt{}, nil
	}
	return found, err
}

// needsFirstIndex 는 색인이 store/ 를 한 번도 안 본 판인지다 — 색인 파일 표가
// 비었는데 store/ 에 md 가 있다. index.db 는 git 밖이라 새로 받은 저장소나
// index.db 를 지운 뒤가 이렇다. 이 판에 add 가 승격하면 빈 색인에 대고
// 쌍둥이를 못 보고, 증분 색인이 store 전체를 떠안는다 (리뷰 2026-09-26 #4).
// 막 init 한 저장소는 store/ 가 비어 여기 안 걸린다.
func needsFirstIndex(d *DB, storeDir string) (bool, error) {
	seen := 0
	err := d.sql.QueryRow("SELECT EXISTS(SELECT 1 FROM files)").Scan(&seen)
	if err != nil || seen == 1 {
		return false, err
	}
	return hasMarkdown(storeDir), nil
}

// errFound 는 첫 md 를 찾으면 훑기를 멈추는 표시다.
var errFound = errors.New("found")

// hasMarkdown 은 dir 아래에 md 가 하나라도 있는지다. 하나 찾으면 바로 멈춘다.
func hasMarkdown(dir string) bool {
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			return errFound
		}
		return nil
	})
	return errors.Is(err, errFound)
}
