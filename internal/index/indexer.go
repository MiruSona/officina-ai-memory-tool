package index

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/secret"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// Options 는 색인 한 번이 알아야 할 것이다. 나중에 늘어나는 칸은 제로값이
// 안전한 쪽이어야 바깥이 안 바뀐다.
type Options struct {
	Store *store.Store
	GC    config.GCConfig
	// Secret 은 승격 전에 훑을 패턴이다. 비어 있으면 기본 패턴을 쓴다.
	Secret config.SecretConfig
	// Quiet 은 한 줄 알림을 끈다. 훅이 켠다 (불변조건 3).
	Quiet bool
	// Verify 는 mtime·size 를 안 믿고 파일을 전부 열어 해시를 대 본다.
	Verify bool
	// Full 은 색인을 통째로 다시 만든다.
	Full bool
	// WantVectors 가 참이면 색인이 끝날 때 「기억 id · 경로 · 파일 해시」 목록을
	// 같이 담는다 (Result.Vectors). 임베딩을 쓸 때만 켠다 — 2만 행 조회가
	// 0.05초쯤 들어서, 모델이 없는 저장소가 그 값을 낼 이유가 없다 (리뷰 B · V1).
	WantVectors bool
	// Types 는 이 저장소의 기억 종류 표다. 비면 vocab.toml 에서 읽는다.
	Types model.TypeTable
	// NoLink 는 이웃 링크 자동 채우기를 끈다 (결정 42). 링크가 없는 저장소를
	// 재야 하는 시험이 켠다. 훅은 이것과 상관없이 마감이 걸려서 안 한다.
	NoLink bool
}

// Result 는 센 결과다. 한글 보고는 cmd 가 찍는다.
type Result struct {
	Locked     bool
	Added      int
	Duplicated int
	Appended   int
	Patched    int
	Bad        int
	Left       int
	Indexed    int
	Skipped    int
	Removed    int
	Total      int
	Old        int
	GCReady    bool
	Rebuilt    bool
	// OverBudget 은 훅 시간 예산을 넘어 따라잡기를 그만뒀다는 뜻이다 (불변조건 4).
	OverBudget bool
	// Unindexed 는 머리말을 못 읽어 색인에서 빠진 파일이다. 파일은 안 건드린다.
	Unindexed []string
	// Secret 은 비밀정보라 막은 건수다 (승격 거절 + 색인 제외). 0 보다 크면
	// `mem index` 는 종료 코드 4 다 (보안연동 시험 M-2).
	Secret int
	// AutoLinked 는 이번에 기계가 찾아 auto_links 표에 담은 이웃 수다 (결정 42).
	// 바뀐 파일이 없어 표를 다시 안 만든 회차는 0 이다.
	AutoLinked int
	// Changed 는 이번에 file_hash 가 바뀌어 다시 색인한 기억이다. 임베딩은 이
	// 목록만 다시 계산한다 — 2만 건 재추론을 피하는 유일한 길이다 (결정 58).
	Changed []ChangedFile
	// Vectors 는 색인에 든 기억 전부다 (Options.WantVectors 를 켰을 때만).
	// **`Validate` 를 통과한 것만 들어간다** — 규격 밖 md 에 벡터를 만들던
	// 구멍을 막는 자리다 (보안연동 M-3 · 불변조건 I7).
	Vectors []VectorFile
	// LinkBlocked 는 너무 커서 건너뛴 이웃 후보 통 수다. Biggest 는 그중 가장
	// 큰 통의 크기다 (스트레스 V8 — 조용히 링크 품질이 떨어지는 것을 알린다).
	LinkBlocked int
	LinkBiggest int
	Elapsed     time.Duration
}

// VectorFile 은 벡터를 만들 기억 하나다. 글은 안 담는다 — 정말 다시 계산할
// 건만 그때 읽는다 (리뷰 B · V1).
type VectorFile struct {
	ID   string
	Path string
	Hash string
}

// ChangedFile 은 내용이 바뀐 기억 하나다.
type ChangedFile struct {
	ID   string
	Path string
	Hash string
}

// runner 는 한 번 도는 동안의 상태다.
type runner struct {
	db      *DB
	store   *store.Store
	result  *Result
	now     time.Time
	quiet   bool
	verify  bool
	scanner *secret.Scanner
	// made 는 이번 회차에 승격해 만든 기억이다. 색인이 승격보다 나중이라
	// DB 만 봐서는 같은 회차의 중복을 못 잡는다 (리뷰 C #2).
	made []madeHere
	// redirect 는 완전중복으로 안 만든 덮는 기억 id → 살아남은 쌍둥이 id 다.
	// 같은 회차에 뒤따라오는 덮임 표시(patch)를 이 표로 다시 댄다.
	redirect map[string]string
	// deadline 은 이 회차가 끝나야 하는 벽시계 시각이다. 제로값이면 마감이 없다.
	deadline time.Time
	// types 는 승격·색인 검사가 쓸 종류 표다.
	types model.TypeTable
	// noLink 는 이웃 링크를 안 채운다는 뜻이다.
	noLink bool
	// linkStale 은 이웃 표를 버렸다는 뜻이다. 파일이 안 바뀐 회차여도 다시
	// 채워야 한다 (리뷰 B · V2 의 표 꼴 바꾸기).
	linkStale bool
	// fresh 는 방금 만든 빈 색인이라는 뜻이다. 지울 옛 행이 없어 문서마다
	// 두 번 하던 조회와 삭제를 건너뛴다.
	fresh bool
	// dirStamp 은 파일 목록을 훑기 직전에 본 store/ 폴더 자리표다. 훑은 뒤에
	// 재면 그 사이에 들어온 파일을 색인 안 하고도 「따라잡았다」 고 적게 된다.
	dirStamp string
}

// overBudget 은 마감을 넘었는지다. 넘으면 남은 단계를 건너뛰고 지금 있는
// 색인으로 블록을 만든다 (불변조건 4 · 리뷰 A #5).
func (r *runner) overBudget() bool {
	if !overdue(r.deadline) {
		return false
	}
	r.result.OverBudget = true
	return true
}

// note 는 왜 한 파일이 빠졌는지 stderr 에 한 줄로 알린다.
func (r *runner) note(path string, reason error) {
	if r.quiet || reason == nil {
		return
	}
	fmt.Fprintln(os.Stderr, i18n.T(i18n.BadIndexEntry, path, reason.Error()))
}

// scannerFor 는 빈 Options 도 안전하게 기본 패턴으로 떨어뜨린다.
func scannerFor(settings config.SecretConfig) *secret.Scanner {
	if len(settings.Patterns) == 0 {
		settings = config.Default("").Secret
	}
	return secret.New(settings.Patterns)
}

// Run 은 하나뿐인 쓰기 락 아래에서 inbox 를 승격하고 바뀐 파일을 색인한다.
// 락을 못 잡는 것은 아무것도 안 한 성공이다 (설계 5-2).
func Run(options Options) (*Result, error) {
	result := Result{Rebuilt: options.Full}
	started := time.Now()
	release, taken, err := TryLock(options.Store.Dir)
	if err != nil {
		return nil, err
	}
	if !taken {
		result.Locked = true
		result.Elapsed = time.Since(started)
		return &result, nil
	}
	defer release()
	clearStaleFiles(options.Store.Dir)
	if options.Full {
		if err := removeFiles(options.Store.Dir); err != nil {
			return nil, err
		}
	}
	err = runLocked(options, &result)
	result.Elapsed = time.Since(started)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func runLocked(options Options, result *Result) error {
	database, err := openForRun(options.Store.Dir, result)
	if err != nil {
		return err
	}
	defer database.Close()
	current := runner{db: database, store: options.Store, result: result, now: time.Now(),
		quiet: options.Quiet, verify: options.Verify, scanner: scannerFor(options.Secret),
		fresh: result.Rebuilt, noLink: options.NoLink, types: typesFor(options.Types, options.Store.Dir)}
	if err := current.promoteInbox(); err != nil {
		return err
	}
	if err := current.indexChanged(); err != nil {
		return err
	}
	// 옛 색인에도 파생 표가 있어야 검색의 1-hop 가 읽는다. 쓰기 락 안이다.
	dropped, err := EnsureAutoLinks(database.sql)
	if err != nil {
		return err
	}
	current.linkStale = dropped
	// 이웃 링크는 색인이 다 선 뒤에야 후보를 볼 수 있다 (결정 42).
	current.autoLink()
	if err := current.finish(options.GC); err != nil {
		return err
	}
	if !options.WantVectors {
		return nil
	}
	// 임베딩이 볼 목록이다. 색인에 든 것만 — 규격 밖 md 는 여기 없다 (M-3).
	found, err := database.VectorFiles()
	if err != nil {
		return err
	}
	result.Vectors = found
	return nil
}

// openForRun 은 색인을 열되, 방식이 바뀌었거나 파일이 깨졌으면 지우고 다시
// 만든다. 색인은 파생물이라 버리는 것이 늘 옳다 (설계 6-2).
func openForRun(dir string, result *Result) (*DB, error) {
	database, rebuilt, err := OpenRebuilding(dir)
	if rebuilt {
		result.Rebuilt = true
	}
	broken := &BrokenError{}
	if errors.As(err, &broken) {
		if err := removeFiles(dir); err != nil {
			return nil, err
		}
		result.Rebuilt = true
		return Open(dir)
	}
	if err != nil {
		return nil, err
	}
	changed, err := database.SchemeChanged()
	if err != nil {
		database.Close()
		return nil, err
	}
	if !changed {
		return database, nil
	}
	database.Close()
	if err := removeFiles(dir); err != nil {
		return nil, err
	}
	result.Rebuilt = true
	return Open(dir)
}

// indexChanged 는 세 갈래 판정이다 — mtime·size 가 같으면 안 열고, 열었으면
// 해시를 대 보고, 그것도 다르면 다시 색인한다.
func (r *runner) indexChanged() error {
	// 자리표는 목록을 훑기 전에 잰다 (dirStamp 주석).
	r.dirStamp, _ = r.store.StoreDirsStamp()
	files, err := r.store.ListMemories()
	if err != nil {
		return err
	}
	known, err := r.db.loadFiles()
	if err != nil {
		return err
	}
	if err := r.db.beginImmediate(); err != nil {
		return err
	}
	if r.fresh {
		if err := r.db.deferMerges(); err != nil {
			r.db.rollback()
			return err
		}
	}
	for _, file := range files {
		previous, seen := known[file.Path]
		delete(known, file.Path)
		if err := r.indexOne(file, previous, seen); err != nil {
			r.db.rollback()
			return err
		}
	}
	if err := r.dropMissing(known); err != nil {
		r.db.rollback()
		return err
	}
	if r.fresh {
		if err := r.db.optimizeFTS(); err != nil {
			r.db.rollback()
			return err
		}
	}
	return r.db.commit()
}

// coarseWindow 는 mtime 해상도가 거친 파일계층(exFAT·FAT32 는 2초)에서 「안
// 바뀌었다」 를 믿지 않는 구간이다. 이 안이면 파일을 열어 해시까지 본다
// (리뷰 A #14).
const coarseWindow = 2 * time.Second

func (r *runner) indexOne(file store.FileInfo, previous fileRow, seen bool) error {
	fresh := r.now.Sub(file.ModTime) < coarseWindow && r.now.Sub(file.ModTime) > -coarseWindow
	if seen && !r.verify && !fresh && previous.MTime == file.ModTime.UnixNano() && previous.Size == file.Size {
		r.result.Skipped++
		return nil
	}
	parsed, err := r.store.ReadListed(file)
	if err != nil {
		return r.markUnindexed(file, err)
	}
	if err := checkParsed(parsed.Memory, file.Path, r.types); err != nil {
		return r.markUnindexed(file, err)
	}
	// 손으로 고쳐 넣은 비밀정보를 막는 자리다. 걸리면 색인에 안 넣고 파일도
	// 안 건드린다 — 훅·search·show 어디에도 안 실린다 (불변조건 I4 · H-1).
	if found := r.scanner.ScanText(secret.MemoryText(parsed.Memory.Title, parsed.Memory.Summary,
		parsed.Memory.Sources, parsed.Memory.Body)); found != nil {
		return r.markUnindexed(file, blocked(errors.New(i18n.T(i18n.SecretInStore, found.Rule))))
	}
	row := fileRow{MTime: file.ModTime.UnixNano(), Size: file.Size, Hash: contentHash(parsed.Memory)}
	if seen && previous.Hash == row.Hash {
		r.result.Skipped++
		return r.db.upsertFile(file.Path, row)
	}
	if !r.fresh {
		if err := r.db.dropMemory(file.Path); err != nil {
			return err
		}
	}
	if err := r.db.insertMemory(parsed.Memory, file.Path, row, file.ModTime.Unix(),
		bodyHash(parsed.Memory.Body), r.fresh); err != nil {
		return err
	}
	r.result.Indexed++
	r.result.Changed = append(r.result.Changed,
		ChangedFile{ID: parsed.Memory.ID, Path: file.Path, Hash: row.Hash})
	return r.db.upsertFile(file.Path, row)
}

// markUnindexed 는 규격을 어긴 파일을 센다. 파일은 고치지도 옮기지도 않는다 —
// 이미 승격된 원본이라 inbox/bad 는 그 자리가 아니다 (설계 5-2).
func (r *runner) markUnindexed(file store.FileInfo, reason error) error {
	r.result.Bad++
	if IsSecretBlock(reason) {
		r.result.Secret++
	}
	r.result.Unindexed = append(r.result.Unindexed, file.Path)
	r.note(file.Path, reason)
	// 전에는 멀쩡했는데 지금 아니면 옛 행을 지워야 한다. 안 그러면 검색이
	// 파일에 없는 말을 계속 답한다.
	if err := r.db.dropMemory(file.Path); err != nil {
		return err
	}
	// mtime·size 를 적어 두면 다음 판이 「안 바뀌었다」 며 Skipped 로 넘긴다.
	// 비밀정보·규격 위반은 고칠 때까지 매번 알려야 한다 (리뷰 A4).
	return r.db.deleteFileRow(file.Path)
}

// typesFor 는 넘겨받은 표를 쓰되, 없으면 저장소 vocab.toml 에서 읽는다.
func typesFor(given model.TypeTable, dir string) model.TypeTable {
	if len(given) > 0 {
		return given
	}
	return config.TypesIn(dir)
}

// checkParsed 는 불변조건 6 이다 — 규격에 맞고 id 가 말하는 자리에 있는 파일만
// 색인한다.
func checkParsed(memory *model.Memory, path string, types model.TypeTable) error {
	if problems := model.ValidateWith(memory, memory.Spec, types); len(problems) > 0 {
		return problems[0]
	}
	if want := model.StorePath(memory.ID); want != path {
		return errors.New(i18n.T(i18n.IDPathMismatch, memory.ID, want))
	}
	return nil
}

// dropMissing 은 디스크에서 없어진 파일을 색인에서 뺀다.
func (r *runner) dropMissing(missing map[string]fileRow) error {
	for path := range missing {
		if err := r.db.dropMemory(path); err != nil {
			return err
		}
		if err := r.db.deleteFileRow(path); err != nil {
			return err
		}
		r.result.Removed++
	}
	return nil
}

// applyHits 는 local/hits.jsonl 을 행에 옮긴다. 실패해도 되는 자리다 — 파일이
// 원본이고 DB 는 비출 뿐이라, 못 읽으면 이번 판의 숫자만 잃는다.
func (r *runner) applyHits() error {
	totals, lines, err := store.ReadHits(r.store.Dir)
	if err != nil {
		r.note(store.HitsPath(r.store.Dir), err)
		return nil
	}
	if err := r.db.applyHits(totals); err != nil {
		return err
	}
	if lines <= store.HitsCompactLines || r.store.ReadOnly {
		return nil
	}
	if err := store.CompactHits(r.store.Dir, totals); err != nil {
		r.note(store.HitsPath(r.store.Dir), err)
	}
	return nil
}

func (r *runner) finish(gc config.GCConfig) error {
	if err := r.applyHits(); err != nil {
		return err
	}
	total, old, err := r.db.counts(gc.WarmDays)
	if err != nil {
		return err
	}
	r.result.Total = total
	r.result.Old = old
	r.result.GCReady = GCReady(gc, total, old)
	left, err := r.store.ListInbox()
	if err != nil {
		return err
	}
	if err := r.db.pruneInboxSeen(r.now, left); err != nil {
		return err
	}
	if err := r.db.SetMeta(metaLastIndex, r.now.Format(time.RFC3339)); err != nil {
		return err
	}
	return r.db.SetMeta(metaStoreDirs, r.dirStamp)
}

// GCReady 는 정리할 때가 됐는지만 본다. 실제 정리는 gc 패키지가 맡는데 아직
// 없다 (설계 9-4). 여기 시그니처만 먼저 못 박아 둔다.
func GCReady(gc config.GCConfig, total, old int) bool {
	return total > gc.WarmCount && old > 0
}

// contentHash 는 정규형을 해시한다. cp949 를 UTF-8 로 다시 쓴 파일이 내용이
// 바뀐 것처럼 보이면 안 된다.
func contentHash(memory *model.Memory) string {
	sum := sha256.Sum256(model.Encode(memory))
	return hex.EncodeToString(sum[:])
}
