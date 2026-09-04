package index

import (
	"database/sql"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/link"
)

// 자동 이웃 링크 (설계 결정 42 · 뒷정리 반영).
//
// **머리말은 안 건드린다.** 기계가 찾은 이웃은 `auto_links` 표에만 둔다.
// 처음 판은 201개 md 의 `links:` 에 적어 넣었는데, 그러면 ①도구가 사람 문서
// 201개를 고쳐 git 이 시끄럽고 ②file_hash 가 바뀌어 다음 색인이 저장소를 다시
// 읽고 ③gc 면제표가 기계 링크를 사람 링크로 잘못 읽는다. 얻는 것은
// multisession r@5 이득 0 이었다.
//
// `auto_links` 는 `index.db` 와 같은 파생물이다. 색인할 때마다 통째로 다시
// 만들고, 지워도 다음 색인이 되살린다. 그래서 스키마 판을 안 올린다.
//
// 사람이 「이 둘은 이어야 한다」고 정하는 길은 lint D04 `link-missing` 이 후보를
// 내고 사람이 `review` 로 머리말에 올리는 것 하나뿐이다.

// autoLinkDDL 은 파생 표 꼴이다. docid 정수 쌍이라 같은 자료를 두 번 안 담는다
// (리뷰 B · V2).
const autoLinkDDL = `CREATE TABLE auto_links (src INTEGER NOT NULL, dst INTEGER NOT NULL,
	PRIMARY KEY(src, dst)) WITHOUT ROWID`

// EnsureAutoLinks 는 파생 표를 만든다. **옛 글자 꼴이면 버리고 다시 만든다** —
// 파생물이라 버리는 것이 늘 옳고, 다음 색인이 되살린다. 스키마 판은 안 올린다.
// 버렸으면 참을 돌려준다 — 그 회차는 파일이 안 바뀌었어도 표를 다시 채워야 한다.
func EnsureAutoLinks(db *sql.DB) (bool, error) {
	old, err := autoLinkIsText(db)
	if err != nil {
		return false, err
	}
	if old {
		if _, err := db.Exec("DROP TABLE auto_links"); err != nil {
			return false, err
		}
	}
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS" + autoLinkDDL[len("CREATE TABLE"):])
	return old, err
}

// autoLinkIsText 는 지금 있는 표가 옛 글자 꼴인지다. 표가 없으면 거짓이다.
func autoLinkIsText(db *sql.DB) (bool, error) {
	rows, err := db.Query("PRAGMA table_info(auto_links)")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		cid, name, kind, notNull, dflt, pk := 0, "", "", 0, any(nil), 0
		if err := rows.Scan(&cid, &name, &kind, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == "src" {
			return strings.EqualFold(kind, "TEXT"), rows.Err()
		}
	}
	return false, rows.Err()
}

// autoLink 는 색인이 끝난 뒤 이웃 표를 다시 만든다. 실패해도 색인 자체는
// 성공이다 — 링크는 검색을 돕는 덤이고, 여기서 오류를 올리면 색인이 통째로
// 물러난다.
func (r *runner) autoLink() {
	// 훅 안에서는 안 한다. 마감이 걸린 회차는 훅이 부른 것이라 벽시계 1초를
	// 지켜야 한다 (불변조건 4 · 설계 7절). 표는 지난 회차 것을 그대로 쓴다 —
	// 파생물이라 조금 뒤처져도 검색 가산이 덜 붙을 뿐이다.
	if r.noLink || !r.deadline.IsZero() {
		return
	}
	// 아무것도 안 바뀐 회차는 표도 안 바뀐다. 20k 에서 이 한 줄이 헛수고
	// 2.6초를 없앤다 (뒷정리 실측).
	if !r.linkStale && r.result.Indexed == 0 && r.result.Removed == 0 {
		return
	}
	docs, err := r.db.LinkDocs()
	if err != nil {
		r.note(DBPath(r.store.Dir), err)
		return
	}
	// **바뀐 기억이 적으면 그 둘레만 다시 잰다** (리뷰 B · V1 증분). 20k 에서
	// 열 건을 고쳤을 뿐인데 표를 통째로 다시 만드느라 2.4초를 더 썼다.
	if ids := r.smallChange(len(docs)); ids != nil {
		found, names, blocked := link.SuggestSome(docs, link.MaxLinks, ids)
		r.result.LinkBlocked, r.result.LinkBiggest = blocked.Skipped, blocked.Biggest
		if err := r.db.updateAutoLinks(names, found); err != nil {
			r.note(DBPath(r.store.Dir), err)
			return
		}
		r.result.AutoLinked = countLinks(found)
		return
	}
	found, blocked := link.SuggestAllStats(docs, link.MaxLinks)
	r.result.LinkBlocked, r.result.LinkBiggest = blocked.Skipped, blocked.Biggest
	if err := r.db.replaceAutoLinks(found); err != nil {
		r.note(DBPath(r.store.Dir), err)
		return
	}
	r.result.AutoLinked = countLinks(found)
}

func countLinks(found map[string][]link.Candidate) int {
	made := 0
	for _, list := range found {
		made += len(list)
	}
	return made
}

// smallChangeRatio 는 「둘레만 다시 재는 것이 싼」 몫이다. 이보다 많이 바뀌면
// 통째로 다시 만드는 편이 빠르다.
const smallChangeRatio = 20

// smallChange 는 이번에 바뀐 기억 id 다. 통째로 다시 만들어야 하면 nil 이다 —
// 표를 새로 만들었거나(꼴이 바뀜·전체 색인) 지워진 기억이 있거나 너무 많이
// 바뀐 회차다. **지워진 기억이 있으면 통째로 간다** — 없어진 기억을 가리키던
// 줄을 남기면 검색이 없는 기억을 데려온다.
func (r *runner) smallChange(total int) map[string]bool {
	if r.linkStale || r.fresh || r.result.Removed > 0 || total == 0 {
		return nil
	}
	if len(r.result.Changed) == 0 || len(r.result.Changed)*smallChangeRatio > total {
		return nil
	}
	ids := make(map[string]bool, len(r.result.Changed))
	for _, one := range r.result.Changed {
		ids[one.ID] = true
	}
	return ids
}

// updateAutoLinks 는 다시 잰 기억의 줄만 갈아 끼운다.
func (d *DB) updateAutoLinks(names []string, found map[string][]link.Candidate) error {
	if _, err := EnsureAutoLinks(d.sql); err != nil {
		return err
	}
	tx, err := d.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at, err := d.docidByID(tx)
	if err != nil {
		return err
	}
	remove, err := tx.Prepare("DELETE FROM auto_links WHERE src = ?")
	if err != nil {
		return err
	}
	defer remove.Close()
	insert, err := tx.Prepare("INSERT OR IGNORE INTO auto_links(src, dst) VALUES(?, ?)")
	if err != nil {
		return err
	}
	defer insert.Close()
	for _, id := range names {
		from, know := at[id]
		if !know {
			continue
		}
		if _, err := remove.Exec(from); err != nil {
			return err
		}
		for _, one := range found[id] {
			to, know := at[one.ID]
			if !know {
				continue
			}
			if _, err := insert.Exec(from, to); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// replaceAutoLinks 는 표를 통째로 새것으로 바꾼다.
func (d *DB) replaceAutoLinks(found map[string][]link.Candidate) error {
	if _, err := EnsureAutoLinks(d.sql); err != nil {
		return err
	}
	tx, err := d.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM auto_links"); err != nil {
		return err
	}
	insert, err := tx.Prepare("INSERT OR IGNORE INTO auto_links(src, dst) VALUES(?, ?)")
	if err != nil {
		return err
	}
	defer insert.Close()
	at, err := d.docidByID(tx)
	if err != nil {
		return err
	}
	for src, list := range found {
		from, know := at[src]
		if !know {
			continue
		}
		for _, one := range list {
			to, know := at[one.ID]
			if !know {
				continue
			}
			if _, err := insert.Exec(from, to); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// docidByID 는 기억 id → docid 다. auto_links 가 담는 것이 docid 라 필요하다.
func (d *DB) docidByID(tx *sql.Tx) (map[string]int64, error) {
	rows, err := tx.Query("SELECT id, docid FROM memories")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	at := map[string]int64{}
	for rows.Next() {
		id, docid := "", int64(0)
		if err := rows.Scan(&id, &docid); err != nil {
			return nil, err
		}
		at[id] = docid
	}
	return at, rows.Err()
}

// LinkDocs 는 이웃 셈법이 보는 것 전부를 색인에서 한 번에 읽는다. 파일을 안
// 연다 — 20k 에서 파일 2만 개를 여는 값이 이웃 하나 값보다 크다.
func (d *DB) LinkDocs() ([]link.Doc, error) {
	rows, err := d.sql.Query(`SELECT id, type, title, summary, tags, scope, date(created_at, 'unixepoch')
		FROM memories WHERE archived = '' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	docs := []link.Doc{}
	at := map[string]int{}
	for rows.Next() {
		one := link.Doc{}
		tags := ""
		if err := rows.Scan(&one.ID, &one.Type, &one.Title, &one.Summary, &tags, &one.Scope, &one.Date); err != nil {
			return nil, err
		}
		one.Tags = strings.Fields(tags)
		at[one.ID] = len(docs)
		docs = append(docs, one)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := d.fillSources(docs, at); err != nil {
		return nil, err
	}
	return docs, d.fillLinks(docs, at)
}

func (d *DB) fillSources(docs []link.Doc, at map[string]int) error {
	rows, err := d.sql.Query("SELECT mem_id, kind, value FROM sources")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		id, kind, value := "", "", ""
		if err := rows.Scan(&id, &kind, &value); err != nil {
			return err
		}
		if place, ok := at[id]; ok {
			docs[place].Sources = append(docs[place].Sources, kind+":"+value)
		}
	}
	return rows.Err()
}

func (d *DB) fillLinks(docs []link.Doc, at map[string]int) error {
	rows, err := d.sql.Query("SELECT src, dst FROM links")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		src, dst := "", ""
		if err := rows.Scan(&src, &dst); err != nil {
			return err
		}
		if place, ok := at[src]; ok {
			docs[place].Links = append(docs[place].Links, dst)
		}
	}
	return rows.Err()
}
