package index

import (
	"database/sql"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
)

// ErrBadExpr 는 FTS5 가 못 읽은 질의다. 조용히 0건으로 삼키면 0건 설명이
// "저장소에 그 낱말이 없다" 고 거짓말을 한다 (리뷰B #12). 부르는 쪽이 이걸
// 보고 "질의를 못 읽었다" 한 줄을 남긴다.
var ErrBadExpr = errors.New("bad fts expression")

// Ranked 는 랭킹 하나가 낸 순위 한 자리다. Score 는 bm25 라 작을수록 좋다.
type Ranked struct {
	Docid int64
	Score float64
}

// SearchRow 는 검색이 점수를 매길 때 필요한 칸이다. 본문은 안 담는다 — 흔한
// 낱말 하나가 저장소의 3분의 1을 통째로 메모리에 올린다.
type SearchRow struct {
	Docid        int64
	ID           string
	Type         string
	Title        string
	Summary      string
	Tags         string
	Scope        string
	Status       string
	Severity     string
	Source       string
	Pinned       bool
	CreatedAt    int64
	UpdatedAt    int64
	InvalidAt    int64
	HitCount     int
	Importance   int
	SupersededBy string
	// v0.2 에서 늘어난 칸. Status·Source 는 옛 이름이라 같은 값을 함께 채운다
	// — 파도 C 가 옮겨 타면 그때 뺀다.
	Author     string
	TodoStatus string
	StaleAfter int64
	Simhash    int64
	State      string
}

// Filter 는 읽기 명령이 같이 쓰는 좁히기 조건이다.
type Filter struct {
	Types    []string
	Scope    string
	Tags     []string
	Status   string
	Severity string
	Since    time.Time
	Until    time.Time
	All      bool
	Pinned   bool
	// IncludeHeld 는 `search --include-held` 다. 기본은 거짓 — 사람이 아직
	// 승격 안 한 자동 기억(`review: true`)은 검색에 안 뜬다 (결정 6).
	IncludeHeld bool
}

// fieldWeights 는 title·meta·summary·body 순 가중이다.
// mem.toml [search] field_weights 가 덮는다 (설계 결정 16 · 4-3).
// 값은 프로세스마다 한 번 SetFieldWeights 로 정한다.
var fieldWeights = config.DefaultFieldWeights

// SetFieldWeights 는 설정에서 읽은 가중을 색인에 알린다. 0 이하가 섞여 있으면
// bm25 가 그 열을 통째로 버리므로 그런 값은 안 받는다.
func SetFieldWeights(weights [4]float64) {
	for _, one := range weights {
		if one <= 0 {
			return
		}
	}
	fieldWeights = weights
}

// FieldWeights 는 지금 쓰는 가중이다. 재순위도 같은 값을 써야 짝이 맞는다.
func FieldWeights() [4]float64 { return fieldWeights }

const searchColumns = `SELECT docid, id, type, title, summary, tags, scope,
	COALESCE(todo_status, ''), COALESCE(severity, ''), author, pinned, created_at, updated_at,
	COALESCE(invalid_at, 0), hit_count, importance, COALESCE(superseded_by, ''),
	COALESCE(stale_after, 0), simhash, state FROM memories`

// MatchKO 는 한글 색인에 묻는다. bm25 는 음수고 작을수록 좋아서 그대로 정렬한다.
func (d *DB) MatchKO(expression string, limit int) ([]Ranked, error) {
	return d.matchTable("fts_ko", expression, limit)
}

// MatchNorm 은 정규화한 색인에 묻는다. 질의도 같은 Normalize 를 지나 와야
// 「인덱싱」 과 「색인」 이 같은 글자로 만난다 (결정 21).
func (d *DB) MatchNorm(expression string, limit int) ([]Ranked, error) {
	return d.matchTable("fts_norm", expression, limit)
}

// MatchEN 은 영문·코드 색인에 묻는다.
func (d *DB) MatchEN(expression string, limit int) ([]Ranked, error) {
	return d.matchTable("fts_en", expression, limit)
}

func (d *DB) matchTable(table, expression string, limit int) ([]Ranked, error) {
	if expression == "" {
		return nil, nil
	}
	weights := ""
	for _, one := range fieldWeights {
		weights += "," + formatWeight(one)
	}
	query := "SELECT rowid, bm25(" + table + weights + ") AS s FROM " + table +
		" WHERE " + table + " MATCH ? ORDER BY s LIMIT ?"
	rows, err := d.sql.Query(query, expression, limit)
	if err != nil {
		// 질의 문법이 틀리면 SQLite 가 오류를 낸다. 사람이 친 글자가 원인이라
		// 이 랭킹만 비우되, 삼켰다는 표시는 남긴다 (리뷰B #12).
		return nil, ErrBadExpr
	}
	defer rows.Close()
	out := []Ranked{}
	for rows.Next() {
		item := Ranked{}
		if err := rows.Scan(&item.Docid, &item.Score); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func formatWeight(value float64) string {
	return strconv.FormatFloat(value, 'f', 1, 64)
}

// TagScopeRank 는 태그·scope 가 정확히 맞는 기억을 최근 것부터 준다 (R3).
func (d *DB) TagScopeRank(words []string, limit int) ([]Ranked, error) {
	if len(words) == 0 {
		return nil, nil
	}
	clause := []string{}
	args := []any{}
	for _, word := range words {
		clause = append(clause, `tags LIKE ? ESCAPE '\'`, "scope = ?")
		args = append(args, "% "+LikeEscape(word)+" %", word)
	}
	args = append(args, limit)
	query := "SELECT docid, -CAST(updated_at AS REAL) FROM memories WHERE " +
		strings.Join(clause, " OR ") + " ORDER BY updated_at DESC LIMIT ?"
	return d.rankQuery(query, args)
}

// LikeHeadRank 는 제목·요약의 부분 문자열이다 (R4). 20k 에서 한 번에 20ms 라
// 앞 랭킹이 limit 을 못 채웠을 때만 켠다 (설계 6-4).
func (d *DB) LikeHeadRank(word string, limit int) ([]Ranked, error) {
	if word == "" {
		return nil, nil
	}
	pattern := "%" + LikeEscape(word) + "%"
	query := `SELECT docid, CASE WHEN title LIKE ? ESCAPE '\' THEN -1.0 ELSE -0.7 END AS s
		FROM memories WHERE title LIKE ? ESCAPE '\' OR summary LIKE ? ESCAPE '\'
		ORDER BY s, updated_at DESC LIMIT ?`
	return d.rankQuery(query, []any{pattern, pattern, pattern, limit})
}

func (d *DB) rankQuery(query string, args []any) ([]Ranked, error) {
	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Ranked{}
	for rows.Next() {
		item := Ranked{}
		if err := rows.Scan(&item.Docid, &item.Score); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// LikeEscape 는 LIKE 가 문법으로 읽는 글자를 막는다. `%` 한 글자를 찾으면
// 저장소 전체가 나오는 일을 여기서 끊는다.
func LikeEscape(text string) string {
	replacer := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)
	return replacer.Replace(text)
}

// countCap 은 셈을 몇 건에서 끊는지다. `훅` 같은 한 글자 접두는 20k 에서
// 어휘 전체를 훑어 500ms 가 든다. 흔한지 아닌지만 알면 되는 자리라 상한을
// 두고 끊는다 (리뷰B #3).
const countCap = 500

// WordCount 는 그 조각이 든 기억이 몇 건인지다. countCap 에서 끊으므로
// 「500건 이상」 은 전부 500 으로 나온다 (0건 설명·흔한 낱말 고르기용).
func (d *DB) WordCount(table, expression string) int {
	found, _ := d.WordCountOK(table, expression)
	return found
}

// WordCountOK 는 셈과 함께 질의를 읽을 수 있었는지를 준다.
func (d *DB) WordCountOK(table, expression string) (int, bool) {
	if expression == "" {
		return 0, true
	}
	found := 0
	query := "SELECT COUNT(*) FROM (SELECT rowid FROM " + table + " WHERE " + table +
		" MATCH ? LIMIT " + strconv.Itoa(countCap) + ")"
	if err := d.sql.QueryRow(query, expression).Scan(&found); err != nil {
		return 0, false
	}
	return found, true
}

// WordExists 는 그 조각이 든 기억이 하나라도 있는지다. 자격 관문은 몇 건인지가
// 아니라 있는지만 알면 되는데, COUNT(*) 는 전부 훑는다 (리뷰B #3).
func (d *DB) WordExists(table, expression string) (bool, bool) {
	if expression == "" {
		return false, true
	}
	found := 0
	query := "SELECT 1 FROM " + table + " WHERE " + table + " MATCH ? LIMIT 1"
	err := d.sql.QueryRow(query, expression).Scan(&found)
	if err == nil {
		return true, true
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, true
	}
	return false, false
}

// headScan 은 닮은 낱말을 찾을 때 제목·요약을 몇 줄까지 훑는지다.
const headScan = 300

// HeadWordsLike 는 제목·요약에 그 글자로 시작하는 낱말이 무엇이 있는지다.
// 어휘표(vocab_ko)는 바이그램이라 접두로 찾으면 자기 자신밖에 안 나온다 —
// 0건 설명 ③ 이 늘 비었던 까닭이다 (리뷰B #19).
func (d *DB) HeadWordsLike(prefix string, limit int) ([]VocabCount, error) {
	out := []VocabCount{}
	if prefix == "" || limit <= 0 {
		return out, nil
	}
	pattern := "%" + LikeEscape(prefix) + "%"
	rows, err := d.sql.Query(`SELECT title, summary FROM memories
		WHERE title LIKE ? ESCAPE '' OR summary LIKE ? ESCAPE '' LIMIT ?`,
		pattern, pattern, headScan)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	totals := map[string]int{}
	for rows.Next() {
		title, summary := "", ""
		if err := rows.Scan(&title, &summary); err != nil {
			return out, err
		}
		countHeadWords(totals, title+" "+summary, prefix)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	found := sortedCounts(totals)
	if len(found) > limit {
		found = found[:limit]
	}
	return found, nil
}

// countHeadWords 는 그 글자로 시작하는 낱말을 세되, 앞뒤 기호는 뗀다.
func countHeadWords(totals map[string]int, text, prefix string) {
	for _, word := range strings.Fields(text) {
		word = strings.TrimFunc(word, func(letter rune) bool {
			return !unicode.IsLetter(letter) && !unicode.IsDigit(letter)
		})
		if len([]rune(word)) < 2 || word == prefix || !strings.HasPrefix(word, prefix) {
			continue
		}
		totals[word]++
	}
}

// FetchRows 는 랭킹에 든 문서를 좁히기 조건과 함께 읽는다.
func (d *DB) FetchRows(docids []int64, narrow Filter, now time.Time) ([]SearchRow, error) {
	if len(docids) == 0 {
		return nil, nil
	}
	places := make([]string, 0, len(docids))
	for _, docid := range docids {
		places = append(places, strconv.FormatInt(docid, 10))
	}
	clause, args := whereOf(narrow, now)
	query := searchColumns + " WHERE docid IN (" + strings.Join(places, ",") + ")" + clause
	return d.scanSearch(query, args)
}

// ListRows 는 질의 없이 조건만 준 목록이다. 점수 없이 날짜 내림이다 (설계 8-1).
func (d *DB) ListRows(narrow Filter, limit int, now time.Time) ([]SearchRow, error) {
	clause, args := whereOf(narrow, now)
	query := searchColumns + " WHERE 1=1" + clause + " ORDER BY created_at DESC, id ASC LIMIT ?"
	return d.scanSearch(query, append(args, limit))
}

// FacetCounts 는 scope 나 tag 를 값마다 몇 건인지 센다 (설계 8-1 `--facet`).
func (d *DB) FacetCounts(kind string, narrow Filter, now time.Time) ([]VocabCount, error) {
	clause, args := whereOf(narrow, now)
	column := "scope"
	if kind == "tag" {
		column = "tags"
	}
	rows, err := d.sql.Query("SELECT "+column+" FROM memories WHERE 1=1"+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	totals := map[string]int{}
	for rows.Next() {
		value := ""
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		countFacet(totals, kind, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sortedCounts(totals), nil
}

func countFacet(totals map[string]int, kind, value string) {
	if kind != "tag" {
		if value != "" {
			totals[value]++
		}
		return
	}
	for _, tag := range strings.Fields(value) {
		totals[tag]++
	}
}

func sortedCounts(totals map[string]int) []VocabCount {
	out := make([]VocabCount, 0, len(totals))
	for name, count := range totals {
		out = append(out, VocabCount{Name: name, Count: count})
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Count != out[b].Count {
			return out[a].Count > out[b].Count
		}
		return out[a].Name < out[b].Name
	})
	return out
}

// PhraseDocids 는 그 구절이 통째로 든 문서다. 따옴표를 필터에서 가산으로
// 옮긴 자리다 (설계 6-5).
func (d *DB) PhraseDocids(docids []int64, phrase string) (map[int64]bool, error) {
	found := map[int64]bool{}
	if len(docids) == 0 || strings.TrimSpace(phrase) == "" {
		return found, nil
	}
	places := make([]string, 0, len(docids))
	for _, docid := range docids {
		places = append(places, strconv.FormatInt(docid, 10))
	}
	// 제목·요약은 색인에 있으니 SQL 이 보고, 본문은 DB 에 없으니 안 걸린 것만
	// md 를 열어 본다. 랭킹 깊이(50)만큼만 열린다.
	pattern := "%" + LikeEscape(phrase) + "%"
	query := `SELECT docid, path, (title LIKE ? ESCAPE '\' OR summary LIKE ? ESCAPE '\')` +
		" FROM memories WHERE docid IN (" + strings.Join(places, ",") + ")"
	rows, err := d.sql.Query(query, pattern, pattern)
	if err != nil {
		return found, err
	}
	rest := map[int64]string{}
	for rows.Next() {
		docid, hit, path := int64(0), 0, ""
		if err := rows.Scan(&docid, &path, &hit); err != nil {
			rows.Close()
			return found, err
		}
		if hit == 1 {
			found[docid] = true
			continue
		}
		rest[docid] = path
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return found, err
	}
	for docid, path := range rest {
		if strings.Contains(d.BodyOf(path), phrase) {
			found[docid] = true
		}
	}
	return found, nil
}

// LinkTargets 는 이 중 남이 links·superseded_by 로 지목한 id 다. 답 전체에
// 한 번만 물어서 건수가 늘어도 값이 같다.
func (d *DB) LinkTargets(ids []string) (map[string]bool, error) {
	found := map[string]bool{}
	if len(ids) == 0 {
		return found, nil
	}
	args := make([]any, 0, len(ids)*2)
	for _, id := range ids {
		args = append(args, id)
	}
	for _, id := range ids {
		args = append(args, id)
	}
	marks := placeholders(len(ids))
	query := "SELECT dst FROM links WHERE dst IN (" + marks + ")" +
		" UNION SELECT superseded_by FROM memories WHERE superseded_by IN (" + marks + ")"
	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return found, err
	}
	defer rows.Close()
	for rows.Next() {
		target := ""
		if err := rows.Scan(&target); err != nil {
			return found, err
		}
		found[target] = true
	}
	return found, rows.Err()
}

func (d *DB) scanSearch(query string, args []any) ([]SearchRow, error) {
	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := []SearchRow{}
	for rows.Next() {
		item := SearchRow{}
		pinned := 0
		err := rows.Scan(&item.Docid, &item.ID, &item.Type, &item.Title, &item.Summary, &item.Tags,
			&item.Scope, &item.Status, &item.Severity, &item.Source, &pinned, &item.CreatedAt,
			&item.UpdatedAt, &item.InvalidAt, &item.HitCount, &item.Importance, &item.SupersededBy,
			&item.StaleAfter, &item.Simhash, &item.State)
		if err != nil {
			return nil, err
		}
		item.Pinned = pinned == 1
		item.Author, item.TodoStatus = item.Source, item.Status
		found = append(found, item)
	}
	return found, rows.Err()
}

func whereOf(narrow Filter, now time.Time) (string, []any) {
	clause := strings.Builder{}
	args := []any{}
	if !narrow.All {
		// 무효는 둘이다 — 기한이 지난 것(invalid_at)과 새 기억이 덮은 것
		// (superseded_by). 둘 다 기본 검색에서 빠지고 --all 에만 보인다
		// (명세 `--all`). 예전에는 앞엣것만 걸러서 뒤집힌 결정이 그냥 나왔다.
		clause.WriteString(" AND (invalid_at IS NULL OR invalid_at > ?)" +
			" AND (superseded_by IS NULL OR superseded_by = '')")
		args = append(args, now.Unix())
	}
	if len(narrow.Types) > 0 {
		clause.WriteString(" AND type IN (" + placeholders(len(narrow.Types)) + ")")
		for _, item := range narrow.Types {
			args = append(args, item)
		}
	}
	if narrow.Scope != "" {
		clause.WriteString(" AND scope IN (?, 'all')")
		args = append(args, narrow.Scope)
	}
	for _, tag := range narrow.Tags {
		if tag == "" {
			continue
		}
		clause.WriteString(` AND tags LIKE ? ESCAPE '\'`)
		args = append(args, "% "+LikeEscape(tag)+" %")
	}
	if narrow.Status != "" {
		clause.WriteString(" AND status = ?")
		args = append(args, narrow.Status)
	}
	if narrow.Severity != "" {
		clause.WriteString(" AND severity = ?")
		args = append(args, narrow.Severity)
	}
	if narrow.Pinned {
		clause.WriteString(" AND pinned = 1")
	}
	if !narrow.IncludeHeld {
		clause.WriteString(" AND review = 0")
	}
	if !narrow.Since.IsZero() {
		clause.WriteString(" AND created_at >= ?")
		args = append(args, narrow.Since.Unix())
	}
	if !narrow.Until.IsZero() {
		clause.WriteString(" AND created_at < ?")
		args = append(args, narrow.Until.Unix())
	}
	return clause.String(), args
}

func placeholders(count int) string {
	marks := make([]string, count)
	for i := range marks {
		marks[i] = "?"
	}
	return strings.Join(marks, ",")
}

// CountRows 는 좁히기 조건에 맞는 기억이 몇 건인지다. 목록의 「총 N건」 이
// 화면에 보인 수가 아니라 진짜 수여야 한다.
func (d *DB) CountRows(narrow Filter, now time.Time) (int, error) {
	clause, args := whereOf(narrow, now)
	total := 0
	err := d.sql.QueryRow("SELECT COUNT(*) FROM memories WHERE 1=1"+clause, args...).Scan(&total)
	return total, err
}

// MatchWithin 은 후보 문서 중 그 식에 맞는 것만 골라 준다. 몇 낱말을 맞췄나로
// 자격을 가릴 때 쓴다 — 순위표는 깊이가 잘려 있어 셈에 못 쓴다 (설계 6-6).
//
// **`rowid IN (…)` 을 MATCH 와 같이 쓰면 안 된다.** 그렇게 쓰면 SQLite 가
// 후보 하나마다 doclist 를 다시 뒤져서 20k · 후보 241개에 5초가 걸렸다
// (리뷰B #3 프로파일 : 한 질의의 95%가 여기였다). 한 번 훑어 놓고 Go 에서
// 걸러내면 같은 답이 10ms 다.
func (d *DB) MatchWithin(table, expression string, docids []int64) (map[int64]bool, error) {
	found := map[int64]bool{}
	if expression == "" || len(docids) == 0 {
		return found, nil
	}
	wanted := make(map[int64]bool, len(docids))
	for _, docid := range docids {
		wanted[docid] = true
	}
	rows, err := d.sql.Query("SELECT rowid FROM "+table+" WHERE "+table+" MATCH ?", expression)
	if err != nil {
		// 질의 문법이 틀리면 맞은 게 없다고 본다. 자격은 다른 낱말이 준다.
		return found, ErrBadExpr
	}
	defer rows.Close()
	for rows.Next() {
		docid := int64(0)
		if err := rows.Scan(&docid); err != nil {
			return found, err
		}
		if wanted[docid] {
			found[docid] = true
		}
	}
	return found, rows.Err()
}
