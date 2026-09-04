package index

import (
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/simhash"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
	"github.com/mirusona/officina-ai-memory-tool/internal/token"
)

// fileRow 는 파일 하나의 증분 색인 상태다.
type fileRow struct {
	MTime int64
	Size  int64
	Hash  string
}

// candidate 는 새 기억을 합칠 수도 있는 이미 있는 기억이다.
type candidate struct {
	ID    string
	Path  string
	Title string
}

// indexText 는 한 문서에서 뽑은 네 열의 색인 문자열이다. ForIndex 를 열마다
// 한 번씩만 부르고 그 결과를 fts_ko 와 fts_en 이 나눠 쓴다 (설계 6-2).
// 조각 자체는 DB 에 안 담는다 — 본문의 2.5배를 먹던 자리다. 재순위가 필요할
// 때 상위 몇 건만 원문에서 다시 만든다 (rerank.go).
type indexText struct {
	TitleKO   string
	MetaKO    string
	SummaryKO string
	BodyKO    string
	TitleEN   string
	MetaEN    string
	SummaryEN string
	BodyEN    string
	TitleNM   string
	MetaNM    string
	SummaryNM string
	BodyNM    string
}

// NormChanged 는 fts_norm 에 넣을 것이 남았는지다.
func (t *indexText) NormChanged() bool {
	return t.TitleNM != "" || t.MetaNM != "" || t.SummaryNM != "" || t.BodyNM != ""
}

// trimNorm 은 정규화가 글자를 안 바꾼 열을 비운다. 질의도 같은 Normalize 를
// 지나므로 그런 열은 fts_ko 가 이미 같은 글자로 담고 있다 — 두 번 담을 까닭이
// 없다. 20k 실측 : 모든 문서·열을 담으면 2.50MB, 이 규칙과 조사 뗀 짧은 글이
// 겹쳐 1.75MB 다 (R4).
func (t *indexText) trimNorm() {
	if t.TitleNM == t.TitleKO {
		t.TitleNM = ""
	}
	if t.MetaNM == t.MetaKO {
		t.MetaNM = ""
	}
	if t.SummaryNM == t.SummaryKO {
		t.SummaryNM = ""
	}
	if t.BodyNM == t.bodyKOHead() {
		t.BodyNM = ""
	}
}

// bodyKOHead 는 fts_norm 이 담는 만큼의 본문을 정규화 없이 조각낸 것이다.
func (t *indexText) bodyKOHead() string {
	if normBodyRunes <= 0 {
		return ""
	}
	return t.BodyKO
}

// normBodyRunes 는 fts_norm 에 담는 본문 앞부분의 길이다 (설계 3절 「본문 앞부분」).
// 0 이면 본문을 안 담는다(제목·태그·scope·요약만). 20k 부피 실측으로 정했다 —
// 본문까지 담으면 index.db 가 85.4MB 로 60MB 자를 크게 넘고(fts_norm 만 26.8MB),
// 안 담으면 fts_norm 이 1.75MB 로 60.15MB 다 (R4).
const normBodyRunes = 0

// headOf 는 글의 앞부분만 잘라 준다. 0 이하면 빈 글이다.
func headOf(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	letters := []rune(text)
	if len(letters) <= limit {
		return text
	}
	return string(letters[:limit])
}

// Lens 는 필드별 낱말 수다. 재순위 bm25 의 길이 정규화가 이 값을 쓴다
// (설계 4-1b · 4-3). 네 정수라 자리값이 거의 안 든다.
func (t *indexText) Lens() [4]int {
	return [4]int{len(strings.Fields(t.TitleKO)), len(strings.Fields(t.MetaKO)),
		len(strings.Fields(t.SummaryKO)), len(strings.Fields(t.BodyKO))}
}

// TitleField 는 제목만 담는 열이다. v0.3 까지는 태그·scope 와 한 열이었는데,
// 태그가 열 개 붙은 기억은 그 열 안에서 제목 낱말이 묽어져 열 가중이 뜻을
// 잃었다 (v0.4 결정 6).
func TitleField(memory *model.Memory) string {
	return memory.DisplayTitle()
}

// MetaField 는 태그와 scope 를 담는 열이다. 둘 다 "어디에 속한 기억인가" 라
// 같은 열이 맞고, 제목과는 무게가 다르다.
func MetaField(memory *model.Memory) string {
	return strings.Join(memory.Tags, " ") + " " + memory.Scope
}

// textOf 는 문서 하나의 색인 문자열을 만든다. 열마다 ForIndex 한 번씩이다.
func textOf(memory *model.Memory) indexText {
	title := token.ForIndex(TitleField(memory))
	meta := token.ForIndex(MetaField(memory))
	summary := token.ForIndex(memory.Summary)
	body := token.ForIndex(memory.Body)
	text := indexText{
		TitleKO: title, MetaKO: meta, SummaryKO: summary, BodyKO: body,
		TitleEN: token.LatinOnly(title), MetaEN: token.LatinOnly(meta),
		SummaryEN: token.LatinOnly(summary), BodyEN: token.LatinOnly(body),
		TitleNM:   token.ForIndex(Normalize(TitleField(memory))),
		MetaNM:    token.ForIndex(Normalize(MetaField(memory))),
		SummaryNM: token.ForIndex(Normalize(memory.Summary)),
		BodyNM:    token.ForIndex(Normalize(headOf(memory.Body, normBodyRunes))),
	}
	text.trimNorm()
	return text
}

// TagField 는 LIKE 로 정확히 거를 수 있는 ' a b ' 꼴이다. 태그를 먼저 정렬해서
// `mem,gc` 와 `gc,mem` 이 같은 자리에 떨어지게 한다.
func TagField(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	sorted := append([]string{}, tags...)
	sort.Strings(sorted)
	return " " + strings.Join(sorted, " ") + " "
}

func (d *DB) loadFiles() (map[string]fileRow, error) {
	rows, err := d.sql.Query("SELECT path, mtime, size, hash FROM files")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known := map[string]fileRow{}
	for rows.Next() {
		path := ""
		row := fileRow{}
		if err := rows.Scan(&path, &row.MTime, &row.Size, &row.Hash); err != nil {
			return nil, err
		}
		known[path] = row
	}
	return known, rows.Err()
}

func (d *DB) upsertFile(path string, row fileRow) error {
	writers, err := d.writers()
	if err != nil {
		return err
	}
	_, err = writers.file.Exec(path, row.MTime, row.Size, row.Hash, time.Now().Unix())
	return err
}

// dropMemory 는 세 표에서 한 문서를 한꺼번에 지운다. docid 가 rowid 라 FTS 행도
// 같이 간다.
func (d *DB) dropMemory(path string) error {
	docid, id, err := d.docidByPath(path)
	if err != nil || docid == 0 {
		return err
	}
	// contentless 표는 UPDATE 가 안 된다 — 지우고 다시 넣는 길뿐이다 (결정 58).
	for _, statement := range []string{"DELETE FROM fts_ko WHERE rowid = ?", "DELETE FROM fts_en WHERE rowid = ?",
		"DELETE FROM fts_norm WHERE rowid = ?", "DELETE FROM memories WHERE docid = ?"} {
		if _, err := d.sql.Exec(statement, docid); err != nil {
			return err
		}
	}
	return d.dropSideRows(id)
}

// dropSideRows 는 memories 에 딸린 표에서 그 기억의 자국을 지운다. 색인은
// 같은 파일을 몇 번이고 다시 읽으므로 여기가 빠지면 태그·근거가 겹쳐 쌓인다.
func (d *DB) dropSideRows(id string) error {
	if id == "" {
		return nil
	}
	for _, statement := range []string{"DELETE FROM links WHERE src = ?",
		"DELETE FROM tagmap WHERE mem_id = ?", "DELETE FROM sources WHERE mem_id = ?"} {
		if _, err := d.sql.Exec(statement, id); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) docidByPath(path string) (int64, string, error) {
	docid := int64(0)
	id := ""
	err := d.sql.QueryRow("SELECT docid, id FROM memories WHERE path = ?", path).Scan(&docid, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", nil
	}
	return docid, id, err
}

func (d *DB) deleteFileRow(path string) error {
	_, err := d.sql.Exec("DELETE FROM files WHERE path = ?", path)
	return err
}

// insertMemory 는 행 하나와 두 FTS 행을 같은 docid 로 쓴다. fresh 는 방금 만든
// 빈 색인이라는 뜻이고, 그때는 같은 id 를 지울 일이 없다.
func (d *DB) insertMemory(memory *model.Memory, path string, file fileRow, updatedAt int64, bodyHash string, fresh bool) error {
	if !fresh {
		if _, err := d.sql.Exec("DELETE FROM memories WHERE id = ?", memory.ID); err != nil {
			return err
		}
		if err := d.dropSideRows(memory.ID); err != nil {
			return err
		}
	}
	writers, err := d.writers()
	if err != nil {
		return err
	}
	text := textOf(memory)
	lens := text.Lens()
	result, err := writers.memory.Exec(
		memory.ID, path, memory.Type, memory.DisplayTitle(), memory.Summary, TagField(memory.Tags),
		memory.Scope, memory.Author, nullable(memory.TodoStatus), nullable(memory.Severity),
		boolToInt(memory.Pinned), importanceOr(memory.Importance), dayToUnix(memory.Date), updatedAt,
		nullableDay(memory.InvalidAt), nullableDay(memory.StaleAfter), nullable(memory.SupersededBy), bodyHash,
		simhashOf(memory), stateOr(memory.State), memory.Archived, int(memory.Spec),
		boolToInt(memory.Review), lens[0], lens[1], lens[2], lens[3], file.MTime, file.Size, file.Hash)
	if err != nil {
		return err
	}
	docid, err := result.LastInsertId()
	if err != nil {
		return err
	}
	if _, err := writers.ftsKO.Exec(docid, text.TitleKO, text.MetaKO, text.SummaryKO, text.BodyKO); err != nil {
		return err
	}
	if _, err := writers.ftsEN.Exec(docid, text.TitleEN, text.MetaEN, text.SummaryEN, text.BodyEN); err != nil {
		return err
	}
	if text.NormChanged() {
		if _, err := writers.ftsNRM.Exec(docid, text.TitleNM, text.MetaNM, text.SummaryNM, text.BodyNM); err != nil {
			return err
		}
	}
	for _, tag := range memory.Tags {
		if _, err := writers.tag.Exec(memory.ID, tag); err != nil {
			return err
		}
	}
	for _, entry := range memory.Sources {
		kind := model.SourceKind(entry)
		if _, err := writers.source.Exec(memory.ID, kind, strings.TrimPrefix(entry, kind+":")); err != nil {
			return err
		}
	}
	return d.insertLinks(memory)
}

// simhashOf 는 중복 후보를 좁힐 지문이다. 판정은 자카드가 하고 이 값은
// 20k 에서 O(n²) 을 피하는 용도로만 쓴다 (설계 결정 11).
func simhashOf(memory *model.Memory) int64 {
	return int64(simhash.Of(memory.Summary + " " + memory.Body))
}

func (d *DB) insertLinks(memory *model.Memory) error {
	for _, link := range memory.Links {
		if _, err := d.sql.Exec("INSERT OR IGNORE INTO links(src, dst) VALUES(?, ?)", memory.ID, link); err != nil {
			return err
		}
	}
	return nil
}

// sameBody 는 완전히 같은 기억이 이미 있는지 본다. 같은 본문이라도 type 이나
// scope 가 다르면 다른 기억이다 (설계 5-2 순서 1).
func (d *DB) sameBody(bodyHash, memoryType, scope string) (string, error) {
	id := ""
	err := d.sql.QueryRow(`SELECT id FROM memories
		WHERE body_hash = ? AND type = ? AND scope = ? LIMIT 1`, bodyHash, memoryType, scope).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// applyHits 는 local/hits.jsonl 의 셈을 행에 덮어쓴다. 더하는 게 아니라 맞추는
// 것이라 색인을 두 번 돌려도 같은 값이 된다.
func (d *DB) applyHits(totals map[string]store.HitTotal) error {
	if err := d.beginImmediate(); err != nil {
		return err
	}
	if _, err := d.sql.Exec("UPDATE memories SET hit_count = 0, last_hit_at = NULL WHERE hit_count <> 0"); err != nil {
		d.rollback()
		return err
	}
	for id, total := range totals {
		if _, err := d.sql.Exec("UPDATE memories SET hit_count = ?, last_hit_at = ? WHERE id = ?",
			total.Count, total.LastAt, id); err != nil {
			d.rollback()
			return err
		}
	}
	return d.commit()
}

// Row 는 색인이 들고 있는 기억 한 건이다. show 와 search 가 같이 쓴다.
type Row struct {
	ID         string
	Path       string
	Type       string
	Title      string
	Summary    string
	Tags       []string
	Scope      string
	Body       string
	Pinned     bool
	Importance int
	HitCount   int
}

// ByID 는 id 로 기억 한 건을 꺼낸다. 없으면 nil 이다.
func (d *DB) ByID(id string) (*Row, error) {
	row := Row{}
	tags := ""
	pinned := 0
	err := d.sql.QueryRow(`SELECT id, path, type, title, summary, tags, scope, pinned, importance, hit_count
		FROM memories WHERE id = ?`, id).Scan(&row.ID, &row.Path, &row.Type, &row.Title, &row.Summary,
		&tags, &row.Scope, &pinned, &row.Importance, &row.HitCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row.Tags = strings.Fields(tags)
	row.Pinned = pinned == 1
	row.Body = d.BodyOf(row.Path)
	return &row, nil
}

// HasPath 는 그 파일이 색인에 들어 있는지다. status 의 "색인 안 된 파일" 줄이
// 쓴다.
func (d *DB) HasPath(path string) bool {
	found := 0
	err := d.sql.QueryRow("SELECT 1 FROM memories WHERE path = ?", path).Scan(&found)
	return err == nil && found == 1
}

// PinnedCount 는 지금 고정된 기억이 몇 건인지다.
func (d *DB) PinnedCount() (int, error) {
	count := 0
	err := d.sql.QueryRow("SELECT COUNT(*) FROM memories WHERE pinned = 1").Scan(&count)
	return count, err
}

// KnownWord 는 그 낱말이 색인에 실제로 있는지 본다. 붙은 낱말 쪼개기(설계 6-1b)가
// 이걸로 자를 자리를 고른다.
func (d *DB) KnownWord(word string) bool {
	expression := token.PhraseFor(word)
	if expression == `""` {
		return false
	}
	found := 0
	err := d.sql.QueryRow("SELECT 1 FROM fts_ko WHERE fts_ko MATCH ? LIMIT 1", expression).Scan(&found)
	return err == nil && found == 1
}

// VocabCount 는 낱말 하나와 몇 번 나오는지다.
type VocabCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// VocabLike 는 접두가 같은 색인 조각을 많이 나온 것부터 준다. 0건일 때 "비슷한
// 낱말" 을 보여주는 자리(설계 6-9 ③)가 이걸 쓴다.
func (d *DB) VocabLike(prefix string, limit int) ([]VocabCount, error) {
	rows, err := d.sql.Query(`SELECT term, cnt FROM vocab_ko WHERE term >= ? AND term < ?
		ORDER BY cnt DESC LIMIT ?`, prefix, prefix+"￿", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []VocabCount{}
	for rows.Next() {
		item := VocabCount{}
		if err := rows.Scan(&item.Name, &item.Count); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// mergeCandidates 는 같은 type·같은 tags 로 최근에 쓰인 기억이다. 고정된 것과
// 접힌 것은 남의 글을 받으면 안 되니 뺀다 (설계 5-2 순서 2). 보류(review=1)도
// 뺀다 — 거기 붙으면 그 글도 사람 검토 전엔 안 뜨는 채로 묻힌다 (뒷정리-3).
func (d *DB) mergeCandidates(memoryType string, tags []string, since int64) ([]candidate, error) {
	rows, err := d.sql.Query(`SELECT id, path, title FROM memories
		WHERE type = ? AND tags = ? AND created_at >= ? AND invalid_at IS NULL
		AND pinned = 0 AND state = 'hot' AND review = 0`, memoryType, TagField(tags), since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := []candidate{}
	for rows.Next() {
		item := candidate{}
		if err := rows.Scan(&item.ID, &item.Path, &item.Title); err != nil {
			return nil, err
		}
		found = append(found, item)
	}
	return found, rows.Err()
}

func (d *DB) pathByID(id string) (string, error) {
	path := ""
	err := d.sql.QueryRow("SELECT path FROM memories WHERE id = ?", id).Scan(&path)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return path, err
}

func (d *DB) seenInbox(key string) (bool, error) {
	seen := 0
	err := d.sql.QueryRow("SELECT 1 FROM inbox_seen WHERE key = ?", key).Scan(&seen)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return seen == 1, err
}

func (d *DB) markInbox(key string) error {
	_, err := d.sql.Exec("INSERT OR IGNORE INTO inbox_seen(key, seen_at) VALUES(?, ?)", key, time.Now().Unix())
	return err
}

// forgetInbox 는 큐에서 없어진 이름의 승격 표시를 지운다. 한 문장에 몰아
// 넣는다 — 2만 건 이전에서 한 줄씩 지우면 그것만으로 몇 초다.
func (d *DB) forgetInbox(names []string) error {
	const chunk = 400
	for at := 0; at < len(names); at += chunk {
		end := at + chunk
		if end > len(names) {
			end = len(names)
		}
		part := names[at:end]
		places := make([]string, len(part))
		args := make([]any, len(part))
		for i, name := range part {
			places[i] = "?"
			args[i] = name
		}
		if _, err := d.sql.Exec("DELETE FROM inbox_seen WHERE key IN ("+
			strings.Join(places, ",")+")", args...); err != nil {
			return err
		}
	}
	return nil
}

// inboxSeenDays 는 승격한 큐 파일 이름을 얼마나 기억하는지다.
const inboxSeenDays = 30

// pruneInboxSeen 은 오래된 승격 표시를 지운다. 다만 **아직 inbox/new 에 있는**
// 이름은 남긴다 — 표시를 지우면 그 큐 파일이 두 번째로 승격돼 본문 교체가 옛
// 본문을 또 덮는다 (리뷰 A #11).
func (d *DB) pruneInboxSeen(now time.Time, keep []string) error {
	cutoff := now.AddDate(0, 0, -inboxSeenDays).Unix()
	if len(keep) == 0 {
		_, err := d.sql.Exec("DELETE FROM inbox_seen WHERE seen_at < ?", cutoff)
		return err
	}
	rows, err := d.sql.Query("SELECT key FROM inbox_seen WHERE seen_at < ?", cutoff)
	if err != nil {
		return err
	}
	staying := map[string]bool{}
	for _, name := range keep {
		staying[name] = true
	}
	old := []string{}
	for rows.Next() {
		key := ""
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return err
		}
		if !staying[key] {
			old = append(old, key)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, key := range old {
		if _, err := d.sql.Exec("DELETE FROM inbox_seen WHERE key = ?", key); err != nil {
			return err
		}
	}
	return nil
}

// counts 는 전체 건수와 days 보다 오래된 건수다.
func (d *DB) counts(days int) (int, int, error) {
	total, old := 0, 0
	if err := d.sql.QueryRow("SELECT COUNT(*) FROM memories").Scan(&total); err != nil {
		return 0, 0, err
	}
	cutoff := time.Now().AddDate(0, 0, -days).Unix()
	err := d.sql.QueryRow("SELECT COUNT(*) FROM memories WHERE created_at < ?", cutoff).Scan(&old)
	return total, old, err
}

// Count 는 색인이 들고 있는 기억 건수다.
func (d *DB) Count() (int, error) {
	total := 0
	err := d.sql.QueryRow("SELECT COUNT(*) FROM memories").Scan(&total)
	return total, err
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableDay(day string) any {
	if day == "" {
		return nil
	}
	stamp := dayToUnix(day)
	if stamp == 0 {
		// 날짜가 아니면 「날짜 없음」 이다. 오늘로 바꾸면 무효 날짜가 오늘
		// 지난 것처럼 읽힌다 (리뷰 B #17).
		return nil
	}
	return stamp
}

// dayToUnix 는 YYYY-MM-DD 를 초로 바꾼다. 못 읽으면 **0** 이다 — 오늘로 바꾸면
// 감쇠와 정렬이 조용히 틀어진다. 애초에 그런 날짜는 model.Validate 가 걸러
// 색인에 못 들어온다 (리뷰 B #17).
func dayToUnix(day string) int64 {
	if !model.IsDate(day) {
		return 0
	}
	parsed, err := time.ParseInLocation(model.DayLayout, day, time.Local)
	if err != nil {
		return 0
	}
	return parsed.Unix()
}

// defaultImportance 는 아무 말도 없는 기억의 값이다.
const defaultImportance = 3

func importanceOr(value int) int {
	if value == 0 {
		return defaultImportance
	}
	return value
}

func stateOr(state string) string {
	if state == "" {
		return StateHot
	}
	return state
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
