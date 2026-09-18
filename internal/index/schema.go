// Package index 는 SQLite 쪽을 맡는다 — 스키마, 쓰기 락, inbox 승격, 증분 색인.
// store/ 의 md 를 고쳐 쓰는 것은 이 패키지뿐이다 (설계 5-2).
package index

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
	"github.com/mirusona/officina-ai-memory-tool/internal/token"
)

// 기억 한 건이 놓일 수 있는 단계 (설계 9-4).
const (
	StateHot  = "hot"
	StateWarm = "warm"
	StateCold = "cold"
)

// SchemaVersion 은 색인 스키마 판이다. v0.2 에서 2 로 올렸다 — 안 올리면
// v0.1 이 만든 index.db 를 v0.2 가 그대로 읽어 조용히 틀린 답을 낸다 (설계 6-4).
// v0.4 에서 4 로 올렸다 — FTS5 열이 3 → 4 로 늘어 옛 색인은 열 자리가 어긋난다.
// v0.4 에서 5 로 올렸다 — 역참조 질의(UsedBy)가 쓰는 인덱스 둘이 늘었다.
// 파생물이라 판이 다르면 통째로 다시 만든다.
const SchemaVersion = 5

// Tokenizer 는 색인이 어떤 방식으로 만들어졌는지다. 이 값이나 stems 판이 다르면
// 디스크의 조각을 못 읽으므로 전체를 다시 만든다 (설계 6-2).
// 값 자체는 config 에 있다. 사람이 보는 mem.toml 과 DB meta 가 서로 다른 값을
// 적고 있으면 안 된다 (리뷰 A #17).
const Tokenizer = config.Tokenizer

// FileName 은 저장소 안의 색인 파일이다. 파생물이라 지워도 아무것도 안 잃는다.
const FileName = "index.db"

// Normalize 는 norm 텍스트를 만드는 정규화기다 (결정 21). 소문자·전각/반각·
// 공백 고르기 → 조사 떼기 → 대표말 치환을 하는 쪽을 밖에서 꽂아 준다.
// 기본은 아무것도 안 하는 것이라, 안 꽂으면 fts_norm 은 fts_ko 와 같은 글을 담는다.
var Normalize = func(text string) string { return text }

// SetNormalize 는 정규화기를 꽂는다. 프로세스 시작 때 한 번 부른다. 이 함수가
// 바뀌면 색인이 다른 방식으로 만들어진 것이라 config.Tokenizer 도 같이 바뀌어야
// 한다 (결정 57).
func SetNormalize(fn func(string) string) {
	if fn == nil {
		return
	}
	Normalize = fn
}

// WireNormalize 는 `mem.toml [canon]` 을 색인·질의가 같이 타는 정규화기로
// 꽂는다 (결정 21·57). 저장소를 연 바로 뒤 한 번만 부른다 — 색인 쪽과 질의
// 쪽이 **같은 함수**를 타야 `인덱싱` 과 `색인` 이 같은 글자가 된다.
// 표가 틀렸으면 조용히 항등으로 둔다. 그 오류는 `mem lint`·`init` 이 말한다.
//
// `cmd/mem` 과 `internal/hook` 이 둘 다 부른다 — 훅의 따라잡기 색인도 같은
// 정규화기를 타야 훅이 만든 행과 `mem index` 가 만든 행이 같아진다.
func WireNormalize(repository *config.Repository) {
	if repository == nil {
		return
	}
	canon, err := repository.Config.CanonTable()
	if err != nil {
		return
	}
	SetNormalize(func(text string) string { return token.Normalize(text, canon) })
}

const schemaSQL = `
CREATE TABLE memories (
  docid         INTEGER PRIMARY KEY AUTOINCREMENT,
  id            TEXT NOT NULL UNIQUE,
  path          TEXT NOT NULL UNIQUE,
  type          TEXT NOT NULL,
  title         TEXT NOT NULL,
  summary       TEXT NOT NULL,
  tags          TEXT NOT NULL DEFAULT '',
  scope         TEXT NOT NULL,
  author        TEXT NOT NULL DEFAULT '',
  todo_status   TEXT,
  severity      TEXT,
  pinned        INTEGER NOT NULL DEFAULT 0,
  importance    INTEGER NOT NULL DEFAULT 3,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL,
  invalid_at    INTEGER,
  stale_after   INTEGER,
  superseded_by TEXT,
  hit_count     INTEGER NOT NULL DEFAULT 0,
  last_hit_at   INTEGER,
  body_hash     TEXT NOT NULL,
  simhash       INTEGER NOT NULL DEFAULT 0,
  state         TEXT NOT NULL DEFAULT 'hot',
  archived      TEXT NOT NULL DEFAULT '',
  spec          INTEGER NOT NULL DEFAULT 2,
  -- review 는 머리말 review: true — 사람이 아직 승격 안 한 자동 기억이다
  -- (결정 6). 검색·훅이 기본으로 뺀다.
  review        INTEGER NOT NULL DEFAULT 0,
  n_title       INTEGER NOT NULL DEFAULT 0,
  n_meta        INTEGER NOT NULL DEFAULT 0,
  n_sum         INTEGER NOT NULL DEFAULT 0,
  n_body        INTEGER NOT NULL DEFAULT 0,
  mtime         INTEGER NOT NULL DEFAULT 0,
  size          INTEGER NOT NULL DEFAULT 0,
  sha           TEXT NOT NULL DEFAULT ''
);
CREATE INDEX ix_mem_type   ON memories(type, updated_at DESC);
CREATE INDEX ix_mem_scope  ON memories(scope, updated_at DESC);
CREATE INDEX ix_mem_live   ON memories(invalid_at, state, updated_at DESC);
CREATE INDEX ix_mem_hash   ON memories(body_hash);
CREATE INDEX ix_mem_pinned ON memories(updated_at DESC) WHERE pinned = 1;
CREATE INDEX ix_mem_gate   ON memories(scope, type) WHERE invalid_at IS NULL;
-- (리뷰 B) ix_mem_sim 은 뺐다. simhash 로 걸러 오는 질의가 한 군데도 없고
-- (닮음 후보는 quality 가 메모리에서 밴딩으로 좁힌다) 20k 에서 352KB 를 먹었다.
CREATE INDEX ix_mem_stale  ON memories(stale_after) WHERE stale_after IS NOT NULL;
-- detail=column 은 안 쓴다 (설계 결정 20 과 다름 · 파도 B 실측).
-- contentless(content='') 표에 detail=column 을 붙이면 bm25() 가 어느 행이든
-- 0 을 낸다 — 열 가중 16/6/1 이 통째로 죽는다. 결정 20 이 detail=none 을 버린
-- 이유와 같은 일이 detail=column 에서도 일어난다. 부피는 fts 조각이 −41% 지만
-- DB 전체로는 −15% 뿐이라 바꿀 값어치가 없다. 자세한 수치는 파도 B 보고.
-- 열은 넷이다 (v0.4 C2). 제목과 메타(태그+scope)를 갈라서 열 가중이 뜻대로
-- 듣게 했다 — 셋이 한 열이면 태그가 많은 기억은 제목 낱말이 묽어진다.
CREATE VIRTUAL TABLE fts_ko USING fts5(title_s, meta_s, summary_s, body_s, tokenize='unicode61', content='', contentless_delete=1);
CREATE VIRTUAL TABLE fts_en USING fts5(title_e, meta_e, summary_e, body_e, tokenize='unicode61', content='', contentless_delete=1);
-- fts_norm 은 정규화한 글(대표말 치환·조사 뗀 것)의 바이그램이다 (결정 21·58).
-- norm 을 memories 열로 두지 않는 이유는 v0.2 가 body·stems 열을 뺀 이유와 같다 —
-- 20k 에서 DB 의 3할을 먹는다. 원문이 필요하면 md 에서 그때 읽는다 (설계 3절).
-- detail 은 기본값(full)이다. contentless 에 detail=column 을 붙이면 bm25() 가 0 이다.
CREATE VIRTUAL TABLE fts_norm USING fts5(title_n, meta_n, summary_n, body_n, tokenize='unicode61', content='', contentless_delete=1);
CREATE VIRTUAL TABLE vocab_ko USING fts5vocab(fts_ko, 'row');
CREATE TABLE tagmap (mem_id TEXT NOT NULL, tag TEXT NOT NULL, PRIMARY KEY(mem_id, tag));
CREATE INDEX ix_tag ON tagmap(tag);
-- (리뷰 B) sources 는 아직 **아무 질의도 안 읽는다.** 설계 6-4 도 인덱스를 안
-- 적었는데 구현이 둘을 붙여 20k 에서 1.69MB(ix_src_kind 1.25 · ix_src_mem 0.44)를
-- 먹고 있었다. 표는 남기고 인덱스만 뺐다 — 읽는 질의가 생기면 그때 다시 붙인다.
CREATE TABLE sources (mem_id TEXT NOT NULL, kind TEXT NOT NULL, value TEXT NOT NULL);
-- ix_src_mem 은 위 주석이 말한 「읽는 질의가 생기면 그때」다. 역참조(UsedBy)가
-- 기억 근거만 묻기에 kind='mem' 부분 인덱스라 옛 ix_src_kind 보다 훨씬 작다.
CREATE INDEX ix_src_mem ON sources(value) WHERE kind = 'mem';
CREATE TABLE files (
  path TEXT PRIMARY KEY, mtime INTEGER, size INTEGER,
  hash TEXT, indexed_at INTEGER
);
CREATE TABLE inbox_seen (key TEXT PRIMARY KEY, seen_at INTEGER);
CREATE TABLE links (src TEXT NOT NULL, dst TEXT NOT NULL, PRIMARY KEY(src, dst));
-- 기본키가 (src, dst) 라 dst 로 찾으면 훑기다. 역참조가 dst 로 묻는다.
CREATE INDEX ix_links_dst ON links(dst);
-- auto_links 는 기계가 찾은 이웃이다 (결정 42 뒷정리). index.db 와 같은 파생물이라
-- 색인할 때마다 다시 만든다. links 와 갈라 두는 자리는 gc 면제표 하나뿐이다.
CREATE TABLE auto_links (src TEXT NOT NULL, dst TEXT NOT NULL, PRIMARY KEY(src, dst));
CREATE TABLE meta (k TEXT PRIMARY KEY, v TEXT NOT NULL);
`

// openCount 는 이 프로세스가 색인을 몇 번 열었는지다. 설계 7-4a 는 프로세스당
// 2회가 상한이라고 못 박았고, 그걸 시험이 셀 수 있게 여기서 센다.
var openCount atomic.Int64

// OpenCount 는 지금까지 연 횟수다. 시험이 상한을 검사할 때 쓴다.
func OpenCount() int {
	return int(openCount.Load())
}

// DB 는 열린 색인 하나다. 연결을 하나만 두어 우리가 유일한 쓰는 쪽이 되게 한다.
// 이 핸들을 search·hook·lint 가 넘겨받아 쓴다 (설계 7-4a).
type DB struct {
	sql  *sql.DB
	Path string
	// body 는 본문을 md 에서 읽어 오는 쪽이다 (body.go).
	body bodySource
	// prepared 는 색인이 되풀이해 쓰는 문장이다 (writers.go).
	prepared *writerSet
}

// TooNewError 는 더 새 exe 가 쓴 DB 라는 뜻이다. 건드리지 않는다.
type TooNewError struct {
	Known int
	Found int
}

func (e *TooNewError) Error() string {
	return i18n.T(i18n.ExeTooOld, e.Known, e.Found)
}

// ExitCode 는 검사 실패 코드다.
func (e *TooNewError) ExitCode() int { return 2 }

// BrokenError 는 SQLite 가 아예 못 읽는 파일이다. 파생물이라 답은 늘 같다 —
// 다시 만든다.
type BrokenError struct {
	Path string
}

func (e *BrokenError) Error() string {
	return i18n.T(i18n.IndexBroken, e.Path)
}

// ExitCode 는 검사 실패 코드다.
func (e *BrokenError) ExitCode() int { return 2 }

// UnusableError 는 저장소는 찾았는데 읽을 색인이 없거나 깨졌다는 뜻이다.
// 「저장소가 없다」 와 반드시 갈라야 한다 — 사람이 `mem init` 을 다시 돌리게
// 잘못 이끈다 (스트레스시험 D4).
type UnusableError struct {
	Path string
}

func (e *UnusableError) Error() string {
	return i18n.T(i18n.IndexUnusable, e.Path)
}

// ExitCode 는 DB 손상 코드다 (설계 6-1 종료 5).
func (e *UnusableError) ExitCode() int { return 5 }

// IsUnusable 은 색인이 없거나 깨졌다는 오류인지 본다.
func IsUnusable(err error) bool {
	target := &UnusableError{}
	return errors.As(err, &target)
}

// IsCheckFailure 는 종료 코드 2 로 끝내야 하는 오류인지 본다.
func IsCheckFailure(err error) bool {
	tooNew := &TooNewError{}
	broken := &BrokenError{}
	return errors.As(err, &tooNew) || errors.As(err, &broken)
}

// isCorrupt 는 파일 자체를 못 쓴다는 SQLite 메시지를 알아본다.
func isCorrupt(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, mark := range []string{"file is not a database", "database disk image is malformed",
		"file is encrypted", "database corrupt"} {
		if strings.Contains(text, mark) {
			return true
		}
	}
	return false
}

// DBPath 는 저장소의 색인 파일 자리다.
func DBPath(dir string) string {
	return filepath.Join(dir, FileName)
}

// Open 은 색인을 열거나 만든다. **프로세스당 두 번까지만 부른다** — 프로젝트
// 저장소 하나. 아래 패키지는 이 핸들을 넘겨받아 쓴다.
func Open(dir string) (*DB, error) {
	database, _, err := OpenRebuilding(dir)
	return database, err
}

// OpenRebuilding 은 Open 과 같은데, 옛 판이라 통째로 다시 만들었으면 그것도
// 알려 준다. 색인 회차가 「다시 만들었다」 를 보고하려면 이 값이 필요하다 (T19).
func OpenRebuilding(dir string) (*DB, bool, error) {
	database, err := openOnce(dir)
	if err == nil || !errors.Is(err, errOldSchema) {
		return database, false, err
	}
	// 판이 다른 색인은 옮길 게 없다. index.db 는 파생물이라 통째로 다시
	// 만드는 편이 싸고 안전하다 (설계 6-4). 다만 지우는 것은 쓰기라서
	// 락을 잡은 쪽만 한다 — 못 잡으면 지금 있는 것을 못 읽는다고 말한다.
	release, taken, lockErr := TryLock(dir)
	if lockErr != nil || !taken {
		return nil, false, &BrokenError{Path: DBPath(dir)}
	}
	defer release()
	if err := removeFiles(dir); err != nil {
		return nil, false, err
	}
	database, err = openOnce(dir)
	return database, err == nil, err
}

// errOldSchema 는 「우리보다 낮거나 모르는 판이라 다시 만들어야 한다」는 표시다.
var errOldSchema = errors.New("old schema")

func openOnce(dir string) (*DB, error) {
	handle, err := sql.Open("sqlite", DBPath(dir))
	if err != nil {
		return nil, err
	}
	handle.SetMaxOpenConns(1)
	database := DB{sql: handle, Path: DBPath(dir)}
	if err := database.prepare(); err != nil {
		handle.Close()
		if isCorrupt(err) {
			return nil, &BrokenError{Path: DBPath(dir)}
		}
		return nil, err
	}
	openCount.Add(1)
	return &database, nil
}

func (d *DB) prepare() error {
	// synchronous=NORMAL 은 WAL 에서 안전하다 — 전원이 나가도 DB 는 안 깨지고
	// 마지막 몇 판만 잃는데, 색인은 파일에서 다시 만들 수 있는 파생물이다.
	// auto_vacuum 은 표를 하나라도 만들기 전에, WAL 로 바꾸기 전에만 정해진다.
	// FTS5 를 합치고 나면 빈 쪽이 크게 남는데 이 값이 켜져 있어야 도로 줄어든다.
	for _, pragma := range []string{"PRAGMA busy_timeout=5000", "PRAGMA auto_vacuum=FULL",
		"PRAGMA journal_mode=WAL", "PRAGMA synchronous=NORMAL", "PRAGMA cache_size=-65536"} {
		if _, err := d.sql.Exec(pragma); err != nil {
			return err
		}
	}
	version := 0
	if err := d.sql.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > SchemaVersion {
		// 번호만 보면 "더 새 exe" 로 오해한다. 우리 표가 다 있는 판이라야
		// 정말 더 새 exe 다 — 없으면 딴 스키마이니 통째로 다시 만든다.
		if !d.hasOurTables() {
			return errOldSchema
		}
		return &TooNewError{Known: SchemaVersion, Found: version}
	}
	if version == SchemaVersion {
		// 번호가 같아도 우리 표가 없으면 딴 스키마다. v0.0 이 쓰던 번호 3 이
		// v0.3 과 겹쳐서, 번호만 믿으면 v0.0 이 만든 DB 를 그대로 읽는다.
		if !d.hasOurTables() {
			return errOldSchema
		}
		return nil
	}
	if version == 0 {
		return d.create()
	}
	// v0.0(3) · v0.1(1) 이 만든 판은 스키마가 달라서 옮길 게 없다.
	return errOldSchema
}

// hasTable 은 그 이름의 표가 있는지 본다.
func (d *DB) hasTable(name string) bool {
	found := ""
	err := d.sql.QueryRow("SELECT name FROM sqlite_master WHERE type IN ('table','view') AND name = ?", name).Scan(&found)
	return err == nil && found == name
}

// hasOurTables 는 이 판이 만드는 표가 다 있는지다.
func (d *DB) hasOurTables() bool {
	for _, name := range []string{"vocab_ko", "tagmap", "fts_norm"} {
		if !d.hasTable(name) {
			return false
		}
	}
	return true
}

func (d *DB) create() error {
	if _, err := d.sql.Exec(schemaSQL); err != nil {
		return err
	}
	if _, err := d.sql.Exec("PRAGMA user_version=" + strconv.Itoa(SchemaVersion)); err != nil {
		return err
	}
	if err := d.SetMeta(metaTokenizer, Tokenizer); err != nil {
		return err
	}
	return d.SetMeta(metaStems, token.Version)
}

// Close 는 핸들을 닫는다.
func (d *DB) Close() error {
	if d.prepared != nil {
		d.prepared.close()
		d.prepared = nil
	}
	return d.sql.Close()
}

// SetMeta 는 살림값 하나를 적는다.
func (d *DB) SetMeta(key, value string) error {
	_, err := d.sql.Exec("INSERT INTO meta(k, v) VALUES(?, ?) ON CONFLICT(k) DO UPDATE SET v=excluded.v", key, value)
	return err
}

// Meta 는 살림값 하나를 읽는다. 없는 키는 빈 문자열이다.
func (d *DB) Meta(key string) (string, error) {
	value := ""
	err := d.sql.QueryRow("SELECT v FROM meta WHERE k = ?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

const (
	metaTokenizer = "tokenizer"
	metaStems     = "stems"
	metaLastIndex = "last_index_at"
	// metaStoreDirs 는 마지막 색인 때 본 store/ 폴더들의 mtime 자리표다.
	// 읽기 명령이 이 값으로 「따라잡을 것이 없다」 를 싸게 가른다.
	metaStoreDirs = "store_dirs"
)

// StoreUnchanged 는 마지막 색인 뒤로 store/ 에 파일이 늘지도 줄지도 않았는지다.
// 그렇다면 검색은 따라잡기를 통째로 건너뛴다 — 20k 에서 224ms 짜리 걸음이다.
// 자리표가 없거나(옛 색인) 못 읽으면 안전한 쪽인 false 다.
func StoreUnchanged(database *DB, opened *store.Store) bool {
	saved, err := database.Meta(metaStoreDirs)
	if err != nil || saved == "" {
		return false
	}
	stamp, err := opened.StoreDirsStamp()
	if err != nil || stamp == "" {
		return false
	}
	return saved == stamp
}

// SchemeChanged 는 색인이 다른 방식으로 만들어졌는지 본다. 둘 중 하나만 달라도
// 디스크의 조각을 못 읽으니 전체를 다시 만든다 (설계 6-2).
func (d *DB) SchemeChanged() (bool, error) {
	stored, err := d.Meta(metaTokenizer)
	if err != nil {
		return false, err
	}
	if stored != Tokenizer {
		return true, nil
	}
	scheme, err := d.Meta(metaStems)
	if err != nil {
		return false, err
	}
	return scheme != token.Version, nil
}

// LastIndexAt 은 마지막으로 색인한 시각이다. 없으면 빈 문자열이다.
func (d *DB) LastIndexAt() (string, error) {
	return d.Meta(metaLastIndex)
}

// removeFiles 는 전체 재생성을 앞두고 DB 와 딸린 파일을 지운다.
func removeFiles(dir string) error {
	// Windows 는 남이 색인을 읽는 동안 지우기가 잠깐 막힌다. 여기서 물러나면
	// `--full` 이 통째로 실패한다 (리뷰 A6).
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := removeRetry(DBPath(dir) + suffix); err != nil {
			return err
		}
	}
	return nil
}

// beginImmediate 는 쓰기 락을 미리 잡는다. 읽다가 쓰기로 커지면 busy_timeout 이
// 안 통한다.
func (d *DB) beginImmediate() error {
	delays := []time.Duration{50 * time.Millisecond, 150 * time.Millisecond, 450 * time.Millisecond}
	_, err := d.sql.Exec("BEGIN IMMEDIATE")
	for _, delay := range delays {
		if err == nil || !isBusy(err) {
			return err
		}
		time.Sleep(delay)
		_, err = d.sql.Exec("BEGIN IMMEDIATE")
	}
	return err
}

func isBusy(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "SQLITE_BUSY") || strings.Contains(text, "database is locked")
}

func (d *DB) commit() error {
	_, err := d.sql.Exec("COMMIT")
	return err
}

func (d *DB) rollback() {
	// 되돌리기가 또 실패하면 할 수 있는 게 없다. 바깥은 이미 오류를 받았다.
	d.sql.Exec("ROLLBACK")
}

// SQL 은 읽기만 하는 쪽(search·lint)에 연결을 내준다. 쓰는 것은 전부 이
// 패키지를 거쳐야 락과 BEGIN IMMEDIATE 가 한 곳에 남는다.
func (d *DB) SQL() *sql.DB {
	return d.sql
}

// Exists 는 저장소에 색인 파일이 이미 있는지다.
func Exists(dir string) bool {
	_, err := os.Stat(DBPath(dir))
	return err == nil
}
