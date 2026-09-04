package index

import (
	"database/sql"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// writerSet 은 색인 알맹이가 되풀이해 쓰는 문장이다. 문서 하나마다 SQL 을 다시
// 파싱하지 않으려고 한 번 준비해 두고 계속 쓴다.
type writerSet struct {
	memory *sql.Stmt
	ftsKO  *sql.Stmt
	ftsEN  *sql.Stmt
	ftsNRM *sql.Stmt
	file   *sql.Stmt
	tag    *sql.Stmt
	source *sql.Stmt
}

const insertMemorySQL = `INSERT INTO memories
	(id, path, type, title, summary, tags, scope, author, todo_status, severity, pinned, importance,
	 created_at, updated_at, invalid_at, stale_after, superseded_by, body_hash, simhash, state, archived, spec,
	 review, n_title, n_meta, n_sum, n_body, mtime, size, sha)
	VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

const upsertFileSQL = `INSERT INTO files(path, mtime, size, hash, indexed_at) VALUES(?, ?, ?, ?, ?)
	ON CONFLICT(path) DO UPDATE SET mtime=excluded.mtime, size=excluded.size, hash=excluded.hash, indexed_at=excluded.indexed_at`

// writerPlan 은 준비할 문장 하나와 그것을 받을 자리다.
type writerPlan struct {
	into **sql.Stmt
	text string
}

// writers 는 준비한 문장 묶음이다. 처음 부를 때 만들고 Close 가 정리한다.
func (d *DB) writers() (*writerSet, error) {
	if d.prepared != nil {
		return d.prepared, nil
	}
	set := writerSet{}
	statements := []writerPlan{
		{&set.memory, insertMemorySQL},
		{&set.ftsKO, "INSERT INTO fts_ko(rowid, title_s, meta_s, summary_s, body_s) VALUES(?, ?, ?, ?, ?)"},
		{&set.ftsEN, "INSERT INTO fts_en(rowid, title_e, meta_e, summary_e, body_e) VALUES(?, ?, ?, ?, ?)"},
		{&set.ftsNRM, "INSERT INTO fts_norm(rowid, title_n, meta_n, summary_n, body_n) VALUES(?, ?, ?, ?, ?)"},
		{&set.file, upsertFileSQL},
		{&set.tag, "INSERT OR IGNORE INTO tagmap(mem_id, tag) VALUES(?, ?)"},
		{&set.source, "INSERT INTO sources(mem_id, kind, value) VALUES(?, ?, ?)"},
	}
	for _, item := range statements {
		statement, err := d.sql.Prepare(item.text)
		if err != nil {
			set.close()
			return nil, err
		}
		*item.into = statement
	}
	d.prepared = &set
	return d.prepared, nil
}

func (w *writerSet) close() {
	for _, statement := range []*sql.Stmt{w.memory, w.ftsKO, w.ftsEN, w.ftsNRM, w.file, w.tag, w.source} {
		if statement != nil {
			statement.Close()
		}
	}
}

// ftsTables 는 색인이 쓰는 FTS5 표다.
var ftsTables = []string{"fts_ko", "fts_en", "fts_norm"}

// deferMerges 는 통째로 다시 만드는 동안 FTS5 의 중간 합치기를 미룬다. 넣는
// 중에 조각을 합쳐 봐야 마지막에 한 번 더 합칠 뿐이다.
func (d *DB) deferMerges() error {
	for _, table := range ftsTables {
		if _, err := d.sql.Exec("INSERT INTO " + table + "(" + table + ", rank) VALUES('automerge', 0)"); err != nil {
			return err
		}
	}
	return nil
}

// optimizeFTS 는 조각을 하나로 합치고 자동 합치기를 원래대로 돌린다.
func (d *DB) optimizeFTS() error {
	for _, table := range ftsTables {
		if _, err := d.sql.Exec("INSERT INTO " + table + "(" + table + ") VALUES('optimize')"); err != nil {
			return err
		}
		if _, err := d.sql.Exec("INSERT INTO " + table + "(" + table + ", rank) VALUES('automerge', 4)"); err != nil {
			return err
		}
	}
	return nil
}

// RewriteFTS 는 한 문서의 FTS 세 표를 지우고 다시 넣는다. contentless 표는
// UPDATE 가 안 되기 때문이다 (결정 58 · v0.1 에서 warm 접기가 100% 실패한 자리).
// gc 가 본문을 접은 뒤 이것을 부르면 fts_norm 까지 한 번에 맞는다.
func (d *DB) RewriteFTS(docid int64, memory *model.Memory) error {
	text := textOf(memory)
	for _, table := range ftsTables {
		if _, err := d.sql.Exec("DELETE FROM "+table+" WHERE rowid = ?", docid); err != nil {
			return err
		}
	}
	writers, err := d.writers()
	if err != nil {
		return err
	}
	if _, err := writers.ftsKO.Exec(docid, text.TitleKO, text.MetaKO, text.SummaryKO, text.BodyKO); err != nil {
		return err
	}
	if _, err := writers.ftsEN.Exec(docid, text.TitleEN, text.MetaEN, text.SummaryEN, text.BodyEN); err != nil {
		return err
	}
	if !text.NormChanged() {
		return nil
	}
	_, err = writers.ftsNRM.Exec(docid, text.TitleNM, text.MetaNM, text.SummaryNM, text.BodyNM)
	return err
}
