package index

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// newStore 는 시험용 저장소 하나를 만든다.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	opened := store.Open(t.TempDir(), false)
	if err := opened.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	return opened
}

func settings() config.Config {
	return config.Default("시험")
}

func runIndex(t *testing.T, opened *store.Store) *Result {
	t.Helper()
	result, err := Run(Options{Store: opened, GC: settings().GC, Secret: settings().Secret, Quiet: true})
	if err != nil {
		t.Fatalf("색인이 죽었다 : %v", err)
	}
	return result
}

func addRequest(summary, body string) store.AddRequest {
	return store.AddRequest{Op: store.OpAdd, Type: model.TypeIssue, Date: today(),
		Summary: summary, Tags: []string{"fts5", "korean"}, Source: model.LegacySourceAI,
		Scope: "mem-search", Severity: model.SeverityHigh, Body: body}
}

// today 는 addRequest 가 쓰는 날짜다. 날짜를 박아 두면 합치기 창(7일)을 벗어나
// 시험이 시한폭탄이 된다 — 오늘 기준으로 늘 창 안에 있게 한다.
func today() string {
	return time.Now().Format("2006-01-02")
}

// 긴 요약 하나 — Validate 가 30~120자를 요구한다.
const longSummary = "두 글자 한글 검색어에 trigram 이 조용히 0건을 돌려줘 검색이 죽었다는 것을 알아냈다"

// 설계 5-2 순서 3 — 안 걸리면 store/YYYY/MM/<id>.md 를 새로 만든다.
func TestPromoteCreatesNewFile(t *testing.T) {
	opened := newStore(t)
	name, err := opened.WriteAdd(addRequest(longSummary, "본문 한 줄"))
	if err != nil {
		t.Fatal(err)
	}
	result := runIndex(t, opened)
	if result.Added != 1 || result.Indexed != 1 {
		t.Fatalf("새로 만들고 색인해야 한다 : %+v", result)
	}
	id := model.QueueID(name, "본문 한 줄", today())
	year, month := time.Now().Format("2006"), time.Now().Format("01")
	if _, err := os.Stat(filepath.Join(opened.Dir, "store", year, month, id+".md")); err != nil {
		t.Fatalf("md 가 안 생겼다 : %v", err)
	}
	if left, _ := opened.ListInbox(); len(left) != 0 {
		t.Fatalf("큐가 안 비었다 : %v", left)
	}
	// 순서 6 — 접수 원본이 남아 있어야 한다.
	received, err := opened.ReadReceived(time.Now())
	if err != nil || len(received) != 1 {
		t.Fatalf("접수 원본이 안 남았다 : %v %v", received, err)
	}
}

// 설계 5-2 순서 0 — 비밀정보는 inbox/bad 로. 값은 절대 안 찍는다.
func TestPromoteParksSecretInBad(t *testing.T) {
	opened := newStore(t)
	if _, err := opened.WriteAdd(addRequest(longSummary, "열쇠는 ghp_abcdefghijklmnopqrstuvwxyz012345 다")); err != nil {
		t.Fatal(err)
	}
	result := runIndex(t, opened)
	if result.Bad != 1 || result.Added != 0 {
		t.Fatalf("비밀정보는 bad 로 가야 한다 : %+v", result)
	}
	parked, err := os.ReadDir(opened.InboxBadDir())
	if err != nil || len(parked) != 1 {
		t.Fatalf("bad 에 한 건이 있어야 한다 : %v %v", parked, err)
	}
}

// 설계 5-2 순서 1 — 본문·type·scope 가 같으면 버리고 조회수만 올린다.
func TestPromoteDropsExactDuplicate(t *testing.T) {
	opened := newStore(t)
	if _, err := opened.WriteAdd(addRequest(longSummary, "똑같은 본문")); err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	if _, err := opened.WriteAdd(addRequest(longSummary, "똑같은 본문")); err != nil {
		t.Fatal(err)
	}
	result := runIndex(t, opened)
	if result.Duplicated != 1 || result.Added != 0 {
		t.Fatalf("완전 중복은 버려야 한다 : %+v", result)
	}
	totals, _, err := store.ReadHits(opened.Dir)
	if err != nil || len(totals) != 1 {
		t.Fatalf("중복은 조회수로 남아야 한다 : %v %v", totals, err)
	}
}

// 설계 5-2 순서 2 — 제목이 닮은 최근 기억에는 `## 덧붙임` 으로 붙인다.
func TestPromoteAppendsToTwin(t *testing.T) {
	opened := newStore(t)
	if _, err := opened.WriteAdd(addRequest(longSummary, "첫 본문")); err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	if _, err := opened.WriteAdd(addRequest(longSummary, "두 번째 본문")); err != nil {
		t.Fatal(err)
	}
	result := runIndex(t, opened)
	if result.Appended != 1 {
		t.Fatalf("닮은 기억에 붙여야 한다 : %+v", result)
	}
	files, _ := opened.ListMemories()
	if len(files) != 1 {
		t.Fatalf("파일이 하나여야 한다 : %v", files)
	}
	file, err := opened.ReadMemory(files[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(file.Memory.Body, "## 덧붙임") || !strings.Contains(file.Memory.Body, "두 번째 본문") {
		t.Fatalf("덧붙임이 없다 :\n%s", file.Memory.Body)
	}
}

// 붙여서 120줄이 넘으면 새 파일로 간다 (설계 5-2 순서 2).
func TestPromoteSplitsWhenTooLong(t *testing.T) {
	opened := newStore(t)
	long := strings.Repeat("한 줄이다\n", 125)
	if _, err := opened.WriteAdd(addRequest(longSummary, long)); err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	if _, err := opened.WriteAdd(addRequest(longSummary, "짧은 새 본문")); err != nil {
		t.Fatal(err)
	}
	result := runIndex(t, opened)
	if result.Added != 1 || result.Appended != 0 {
		t.Fatalf("길면 새 파일이어야 한다 : %+v", result)
	}
}

// 설계 5-2 순서 4 — Validate 실패는 bad 로, 조용히 안 지운다.
func TestPromoteParksInvalidInBad(t *testing.T) {
	opened := newStore(t)
	request := addRequest("짧다", "본문")
	if _, err := opened.WriteAdd(request); err != nil {
		t.Fatal(err)
	}
	result := runIndex(t, opened)
	if result.Bad != 1 {
		t.Fatalf("규격 위반은 bad 로 가야 한다 : %+v", result)
	}
	parked, _ := os.ReadDir(opened.InboxBadDir())
	if len(parked) != 1 {
		t.Fatalf("bad 에 그대로 있어야 한다 : %v", parked)
	}
}

// op:patch 와 op:body — set 이 큐에 넣는 두 가지.
func TestPromotePatchAndBody(t *testing.T) {
	opened := newStore(t)
	name, err := opened.WriteAdd(addRequest(longSummary, "첫 본문"))
	if err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	id := model.QueueID(name, "첫 본문", today())
	if _, err := opened.WritePatch(id, map[string]any{"pinned": true, "superseded_by": "20260901-aa11bb22"}); err != nil {
		t.Fatal(err)
	}
	if _, err := opened.WriteAmend(id, "새 본문 전체"); err != nil {
		t.Fatal(err)
	}
	result := runIndex(t, opened)
	if result.Patched != 2 {
		t.Fatalf("두 건을 고쳐야 한다 : %+v", result)
	}
	file, err := opened.ReadMemory(model.StorePath(id))
	if err != nil {
		t.Fatal(err)
	}
	if !file.Memory.Pinned || file.Memory.SupersededBy != "20260901-aa11bb22" {
		t.Fatalf("머리말이 안 바뀌었다 : %+v", file.Memory)
	}
	if file.Memory.Body != "새 본문 전체" {
		t.Fatalf("본문이 안 바뀌었다 : %q", file.Memory.Body)
	}
	// 본문 교체는 아카이브가 먼저다 — 옛 본문이 남아 있어야 한다.
	if !archiveHas(t, opened, "첫 본문") {
		t.Fatal("옛 본문이 아카이브에 없다")
	}
}

func archiveHas(t *testing.T, opened *store.Store, want string) bool {
	t.Helper()
	for _, path := range store.ArchiveFiles(opened.ArchiveDir()) {
		lines, _ := store.ReadArchiveFile(path)
		for _, line := range lines {
			if strings.Contains(string(line), want) {
				return true
			}
		}
	}
	return false
}

// 증분 — (mtime,size) 가 같으면 파일을 안 열고 건너뛴다.
func TestIncrementalSkipsUnchanged(t *testing.T) {
	opened := newStore(t)
	if _, err := opened.WriteAdd(addRequest(longSummary, "본문")); err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	second := runIndex(t, opened)
	if second.Skipped != 1 || second.Indexed != 0 {
		t.Fatalf("안 바뀐 파일은 건너뛰어야 한다 : %+v", second)
	}
}

// --full 은 전부 다시 만들고, 결과는 같아야 한다.
func TestFullRebuildIsIdempotent(t *testing.T) {
	opened := newStore(t)
	for _, body := range []string{"본문 하나", "본문 둘"} {
		if _, err := opened.WriteAdd(addRequest(longSummary+body, body)); err != nil {
			t.Fatal(err)
		}
		runIndex(t, opened)
	}
	first := countRows(t, opened)
	result, err := Run(Options{Store: opened, GC: settings().GC, Secret: settings().Secret, Quiet: true, Full: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Rebuilt || result.Indexed != first {
		t.Fatalf("전부 다시 색인해야 한다 : %+v (앞서 %d)", result, first)
	}
	if again := countRows(t, opened); again != first {
		t.Fatalf("다시 만든 뒤 건수가 다르다 : %d → %d", first, again)
	}
}

func countRows(t *testing.T, opened *store.Store) int {
	t.Helper()
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	count, err := database.Count()
	if err != nil {
		t.Fatal(err)
	}
	return count
}

// 조사E #13 — 머리말에 `\` 나 따옴표가 있어도 색인에 들어가야 한다.
func TestIndexesAwkwardFrontMatter(t *testing.T) {
	opened := newStore(t)
	summary := `경로 C:\Users\bob 와 "따옴표" 가 든 요약이다 그리고 스물다섯 자를 더 채운다`
	if _, err := opened.WriteAdd(addRequest(summary, "본문")); err != nil {
		t.Fatal(err)
	}
	result := runIndex(t, opened)
	if result.Added != 1 || result.Indexed != 1 || len(result.Unindexed) != 0 {
		t.Fatalf("특수문자 때문에 빠지면 안 된다 : %+v", result)
	}
}

// 머리말이 깨진 파일은 옮기지도 고치지도 않고 "색인 안 된 파일" 로만 센다.
func TestBrokenFileIsCountedNotMoved(t *testing.T) {
	opened := newStore(t)
	path := filepath.Join(opened.Dir, "store", "2026", "08", "20260822-3f9a2c1b.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("머리말이 없다\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runIndex(t, opened)
	if len(result.Unindexed) != 1 || result.Bad != 1 {
		t.Fatalf("색인 안 된 파일로 세야 한다 : %+v", result)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("파일을 옮기면 안 된다")
	}
}

// 색인 방식이 바뀌면 통째로 다시 만든다 (설계 6-2).
func TestSchemeChangeRebuilds(t *testing.T) {
	opened := newStore(t)
	if _, err := opened.WriteAdd(addRequest(longSummary, "본문")); err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SetMeta("tokenizer", "옛날방식"); err != nil {
		t.Fatal(err)
	}
	database.Close()
	result := runIndex(t, opened)
	if !result.Rebuilt || result.Indexed != 1 {
		t.Fatalf("방식이 바뀌면 다시 만들어야 한다 : %+v", result)
	}
}

// 락 — 둘이 동시에 가면 하나만 잡고 다른 하나는 조용히 물러난다.
func TestOnlyOneLockHolder(t *testing.T) {
	dir := t.TempDir()
	release, taken, err := TryLock(dir)
	if err != nil || !taken {
		t.Fatalf("첫 번째가 잡아야 한다 : %v %v", taken, err)
	}
	defer release()
	if _, second, err := TryLock(dir); err != nil || second {
		t.Fatalf("두 번째는 물러나야 한다 : %v %v", second, err)
	}
}

// 죽은 락은 치우고 잡는다. 락 파일에는 PID 와 시작시각이 같이 적힌다.
func TestStaleLockIsTakenOver(t *testing.T) {
	dir := t.TempDir()
	// 아무도 안 쓰는 PID 와 아주 오래된 시각을 적어 둔다.
	line := "999999 12345 1 " + hostName(t) + "\n"
	if err := os.WriteFile(lockPath(dir), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	release, taken, err := TryLock(dir)
	if err != nil || !taken {
		t.Fatalf("죽은 락은 뺏어야 한다 : %v %v", taken, err)
	}
	release()
	if _, err := os.Stat(lockPath(dir) + ".stale.999999"); err == nil {
		t.Fatal("치워 둔 파일이 남으면 안 된다")
	}
}

// 산 락은 안 뺏는다 — 내 PID 와 내 시작시각이 적혀 있으면 살아 있는 것이다.
func TestLiveLockIsNotStolen(t *testing.T) {
	dir := t.TempDir()
	release, taken, err := TryLock(dir)
	if err != nil || !taken {
		t.Fatal("첫 번째가 잡아야 한다")
	}
	defer release()
	info, ok := readLock(lockPath(dir))
	if !ok || info.PID != os.Getpid() {
		t.Fatalf("락 파일에 내 PID 가 적혀야 한다 : %+v", info)
	}
	if abandoned(info) {
		t.Fatal("산 락을 죽었다고 보면 안 된다")
	}
	// PID 는 같은데 시작시각이 다르면 재사용된 PID 다 (조사E #8).
	reused := info
	reused.Started = info.Started + 1
	if started, known := processStart(os.Getpid()); known && !abandoned(reused) {
		t.Fatalf("PID 재사용을 못 걸렀다 (%d)", started)
	}
}

func hostName(t *testing.T) string {
	t.Helper()
	name, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	return name
}

// 죽은 락을 둘이 동시에 치워도 하나만 성공해야 한다 (설계 5-2 · 조사E #7).
func TestStaleLockRaceHasOneWinner(t *testing.T) {
	for round := 0; round < 20; round++ {
		dir := t.TempDir()
		line := "999999 12345 1 " + hostName(t) + "\n"
		if err := os.WriteFile(lockPath(dir), []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
		// 락을 잡은 채로 둔다. 여기서 release 하면 뒤 주자가 **정당하게** 또
		// 잡아서, 서로 겹치지 않는 승자를 겹친 것으로 잘못 센다.
		holds := []func(){}
		guard := sync.Mutex{}
		group := sync.WaitGroup{}
		start := make(chan struct{})
		for racer := 0; racer < 8; racer++ {
			group.Add(1)
			go func() {
				defer group.Done()
				<-start
				release, taken, err := TryLock(dir)
				if err != nil || !taken {
					return
				}
				guard.Lock()
				holds = append(holds, release)
				guard.Unlock()
			}()
		}
		close(start)
		group.Wait()
		for _, release := range holds {
			release()
		}
		if len(holds) != 1 {
			t.Fatalf("죽은 락을 잡은 사람이 하나가 아니다 : %d", len(holds))
		}
	}
}

// 동시 add — 16고루틴 × 200회에 유실도 중복도 없어야 한다 (설계 12-3).
func TestConcurrentAddLosesNothing(t *testing.T) {
	opened := newStore(t)
	group := sync.WaitGroup{}
	for worker := 0; worker < 16; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			for round := 0; round < 200; round++ {
				if _, err := opened.WriteAdd(addRequest(longSummary, "본문")); err != nil {
					t.Error(err)
					return
				}
			}
		}(worker)
	}
	group.Wait()
	names, err := opened.ListInbox()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 16*200 {
		t.Fatalf("유실이 있다 : %d 건", len(names))
	}
	unique := map[string]bool{}
	for _, name := range names {
		if unique[name] {
			t.Fatalf("이름이 겹쳤다 : %s", name)
		}
		unique[name] = true
	}
}

// fts5vocab 이 modernc SQLite 에서 도는지 — 설계 6-1b 가 여기에 걸려 있다.
func TestVocabTableWorks(t *testing.T) {
	opened := newStore(t)
	if _, err := opened.WriteAdd(addRequest(longSummary, "바이그램 검색이 색인 어휘를 쓴다")); err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	found, err := database.VocabLike("바이", 5)
	if err != nil {
		t.Fatalf("어휘표를 못 읽었다 : %v", err)
	}
	if len(found) == 0 {
		t.Fatal("어휘표가 비었다")
	}
	if !database.KnownWord("바이그램") {
		t.Fatal("색인에 있는 낱말을 모른다고 한다")
	}
	if database.KnownWord("타일맵셰이더") {
		t.Fatal("없는 낱말을 안다고 한다")
	}
}

// head 열 — 태그에만 있는 낱말도 찾을 수 있어야 한다 (설계 6-2 · 조사E #1).
func TestTagsAreSearchable(t *testing.T) {
	opened := newStore(t)
	if _, err := opened.WriteAdd(addRequest(longSummary, "본문에는 fts5 라는 말이 없다")); err != nil {
		t.Fatal(err)
	}
	runIndex(t, opened)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	count := 0
	if err := database.SQL().QueryRow(`SELECT COUNT(*) FROM fts_en WHERE fts_en MATCH ?`, "fts5").Scan(&count); err != nil {
		t.Fatalf("fts_en 질의가 죽었다 : %v", err)
	}
	if count != 1 {
		t.Fatalf("태그에만 있는 낱말을 못 찾았다 : %d 건", count)
	}
	if err := database.SQL().QueryRow(`SELECT COUNT(*) FROM fts_ko WHERE fts_ko MATCH ?`, `"mem-search"`).Scan(&count); err != nil {
		t.Fatalf("fts_ko 질의가 죽었다 : %v", err)
	}
	if count != 1 {
		t.Fatalf("scope 를 못 찾았다 : %d 건", count)
	}
}

// v0.0 이 만든 판은 번호가 3 이지만 스키마가 아예 다르다. 통째로 다시 만든다.
func TestOldSchemaIsRebuilt(t *testing.T) {
	opened := newStore(t)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	// v0.0 에는 vocab_ko 가 없다. 그 상태를 그대로 흉내 낸다.
	for _, statement := range []string{"DROP TABLE vocab_ko", "PRAGMA user_version=3"} {
		if _, err := database.SQL().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	database.Close()
	again, err := Open(opened.Dir)
	if err != nil {
		t.Fatalf("옛 판은 통째로 다시 만들어야 한다 : %v", err)
	}
	if !again.hasTable("vocab_ko") || !again.hasTable("tagmap") {
		t.Fatal("다시 만든 색인에 v0.2 표가 없다")
	}
	again.Close()
	// 다시 만든 뒤에는 보통 색인이 그대로 돈다.
	if result := runIndex(t, opened); result.Locked {
		t.Fatal("다시 만든 뒤 색인이 락에 걸렸다")
	}
}

// 더 새 exe 가 쓴 DB 는 건드리지 않는다.
func TestNewerSchemaIsRefused(t *testing.T) {
	opened := newStore(t)
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL().Exec("PRAGMA user_version=99"); err != nil {
		t.Fatal(err)
	}
	database.Close()
	_, err = Open(opened.Dir)
	tooNew := &TooNewError{}
	if !asTooNew(err, tooNew) {
		t.Fatalf("더 새 판은 거절해야 한다 : %v", err)
	}
}

func asTooNew(err error, target *TooNewError) bool {
	found, ok := err.(*TooNewError)
	if ok {
		*target = *found
	}
	return ok
}

// 색인은 프로세스당 두 번까지만 연다 (설계 7-4a). 여기서는 셈이 도는지만 본다.
func TestOpenCountIsCounted(t *testing.T) {
	opened := newStore(t)
	before := OpenCount()
	database, err := Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if OpenCount() != before+1 {
		t.Fatalf("열기 횟수를 안 센다 : %d → %d", before, OpenCount())
	}
}

// GCReady 는 조건만 본다. 실제 정리는 다음 파도다 (설계 9-4).
func TestGCReadyIsThresholdOnly(t *testing.T) {
	gc := config.GCConfig{WarmCount: 1000, WarmDays: 90}
	if GCReady(gc, 999, 500) {
		t.Fatal("건수가 모자라면 아직 아니다")
	}
	if GCReady(gc, 1001, 0) {
		t.Fatal("오래된 것이 없으면 아직 아니다")
	}
	if !GCReady(gc, 1001, 1) {
		t.Fatal("둘 다 넘으면 때가 됐다")
	}
}
