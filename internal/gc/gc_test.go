package gc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// now 는 시험이 쓰는 「지금」이다. 날짜를 못 박아야 며칠 지났는지가 안 흔들린다.
var now = time.Date(2026, 8, 22, 12, 0, 0, 0, time.Local)

// longBody 는 접을 거리가 있는 본문이다. 빈 줄과 되풀이 줄과 코드 블록을 넣었다.
const longBody = `결론부터 : 이 기억은 접혀도 되는 기억이다.


같은 줄이 이어진다.
같은 줄이 이어진다.

` + "```" + `
줄 1
줄 2
줄 3
줄 4
줄 5
줄 6
줄 7
줄 8
줄 9
줄 10
` + "```" + `

마지막 줄이다.`

func testConfig() config.GCConfig {
	settings := config.Default("test").GC
	settings.WarmCount = 1
	settings.ColdCount = 1000000
	return settings
}

// newStore 는 기억 파일을 쓰고 색인까지 만든 저장소를 준다.
func newStore(t *testing.T, memories ...*model.Memory) *store.Store {
	t.Helper()
	opened := store.Open(filepath.Join(t.TempDir(), "Memory"), false)
	if err := opened.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	for _, memory := range memories {
		if err := opened.WriteMemory(memory); err != nil {
			t.Fatal(err)
		}
	}
	reindex(t, opened)
	return opened
}

func reindex(t *testing.T, opened *store.Store) {
	t.Helper()
	result, err := index.Run(index.Options{Store: opened, GC: testConfig(),
		Secret: config.Default("test").Secret, Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Bad > 0 {
		t.Fatalf("색인이 못 읽은 파일이 %d건이다", result.Bad)
	}
}

// oldMemory 는 며칠 전에 만든, 면제에 안 걸리는 기억이다.
func oldMemory(id string, days int) *model.Memory {
	return &model.Memory{ID: id, Type: model.TypeHistory,
		Date:    now.AddDate(0, 0, -days).Format("2006-01-02"),
		Summary: "정리 시험용으로 만든 기억이다. 요약은 규격이 요구하는 길이를 채우려고 이만큼 길게 쓴다",
		Tags:    []string{"gc", "test"}, LegacySource: model.LegacySourceAI, Scope: "mem-gc", Body: longBody}
}

func idAt(at int) string {
	return fmt.Sprintf("20260101-%08x", at+1)
}

func runGC(t *testing.T, opened *store.Store, settings config.GCConfig, at time.Time, dry bool) *Result {
	t.Helper()
	result, err := Run(Options{Store: opened, GC: settings, Now: at, DryRun: dry})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func readFile(t *testing.T, opened *store.Store, id string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(opened.Dir, filepath.FromSlash(model.StorePath(id))))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	count := 0
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			count++
		}
		return nil
	})
	return count
}

// TestWarmFoldsBodyAndArchivesFirst 는 따뜻함이 본문을 줄이고 원본을 먼저
// 아카이브에 남기는지 본다.
func TestWarmFoldsBodyAndArchivesFirst(t *testing.T) {
	opened := newStore(t, oldMemory(idAt(0), 200), oldMemory(idAt(1), 200))
	result := runGC(t, opened, testConfig(), now, false)
	if result.Warmed != 2 {
		t.Fatalf("두 건이 따뜻함으로 가야 한다 : %+v", result)
	}
	text := readFile(t, opened, idAt(0))
	if !strings.Contains(text, "state: warm") {
		t.Fatalf("state 가 warm 이 아니다 :\n%s", text)
	}
	if !strings.Contains(text, "archived: archive/") {
		t.Fatalf("archived 칸이 없다 :\n%s", text)
	}
	if strings.Contains(text, "줄 5") {
		t.Fatalf("코드 블록이 안 접혔다 :\n%s", text)
	}
	archive := readArchive(t, opened)
	if !strings.Contains(archive, "줄 5") {
		t.Fatal("아카이브에 원본이 없다")
	}
	if !strings.Contains(archive, `"op":"gc"`) {
		t.Fatalf("아카이브 줄의 op 가 gc 가 아니다 : %s", archive[:120])
	}
}

func readArchive(t *testing.T, opened *store.Store) string {
	t.Helper()
	files := store.ArchiveFiles(opened.ArchiveDir())
	if len(files) == 0 {
		t.Fatal("아카이브 파일이 없다")
	}
	text := ""
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text += string(raw)
	}
	return text
}

// TestIdempotent 는 두 번째 돌 때 아무것도 안 바뀌는지 본다 (설계 13-2).
func TestIdempotent(t *testing.T) {
	opened := newStore(t, oldMemory(idAt(0), 200), oldMemory(idAt(1), 200))
	runGC(t, opened, testConfig(), now, false)
	first := readFile(t, opened, idAt(0))
	archived := readArchive(t, opened)
	later := now.Add(48 * time.Hour)
	second := runGC(t, opened, testConfig(), later, false)
	if len(second.Items) != 0 {
		t.Fatalf("두 번째에 또 옮겼다 : %+v", second.Items)
	}
	if readFile(t, opened, idAt(0)) != first {
		t.Fatal("두 번째에 파일이 바뀌었다")
	}
	if readArchive(t, opened) != archived {
		t.Fatal("두 번째에 아카이브가 늘었다")
	}
}

// TestNoFileDeleted 는 파일을 하나도 안 지우는지 본다 (불변조건 5).
func TestNoFileDeleted(t *testing.T) {
	opened := newStore(t, oldMemory(idAt(0), 200), oldMemory(idAt(1), 200))
	before := countFiles(t, opened.StoreDir())
	result := runGC(t, opened, testConfig(), now, false)
	if after := countFiles(t, opened.StoreDir()); after != before {
		t.Fatalf("파일 수가 %d 에서 %d 로 바뀌었다", before, after)
	}
	if result.Deleted != 0 {
		t.Fatalf("지운 것이 %d 건이라고 한다", result.Deleted)
	}
}

// TestCooldown 은 하루가 안 지났으면 아무것도 안 하는지 본다.
func TestCooldown(t *testing.T) {
	opened := newStore(t, oldMemory(idAt(0), 200), oldMemory(idAt(1), 200))
	runGC(t, opened, testConfig(), now, false)
	again := runGC(t, opened, testConfig(), now.Add(time.Hour), false)
	if !again.TooSoon {
		t.Fatalf("쿨다운이 안 걸렸다 : %+v", again)
	}
}

// TestBatchLimit 은 한 번 상한을 지키는지 본다 (max(200, 2%)).
func TestBatchLimit(t *testing.T) {
	memories := []*model.Memory{}
	for at := 0; at < 6; at++ {
		memories = append(memories, oldMemory(idAt(at), 200))
	}
	opened := newStore(t, memories...)
	settings := testConfig()
	settings.BatchLimit = 2
	settings.BatchPercent = 0
	result := runGC(t, opened, settings, now, false)
	if len(result.Items) != 2 || !result.Capped {
		t.Fatalf("상한 2건을 안 지켰다 : %d건 capped=%v", len(result.Items), result.Capped)
	}
	if EffectiveBatchLimit(config.Default("t").GC, 20000) != 400 {
		t.Fatalf("2%% 상한이 400 이어야 한다 : %d", EffectiveBatchLimit(config.Default("t").GC, 20000))
	}
}

// TestDryRunChangesNothing 은 미리보기가 아무것도 안 쓰는지 본다.
func TestDryRunChangesNothing(t *testing.T) {
	opened := newStore(t, oldMemory(idAt(0), 200), oldMemory(idAt(1), 200))
	before := readFile(t, opened, idAt(0))
	result := runGC(t, opened, testConfig(), now, true)
	if len(result.Items) == 0 {
		t.Fatal("미리보기가 계획을 못 냈다")
	}
	if readFile(t, opened, idAt(0)) != before {
		t.Fatal("미리보기가 파일을 고쳤다")
	}
	if len(store.ArchiveFiles(opened.ArchiveDir())) != 0 {
		t.Fatal("미리보기가 아카이브를 썼다")
	}
}

// exemptCase 는 면제 하나와 그렇게 만든 기억이다.
type exemptCase struct {
	Name   string
	Change func(memory *model.Memory)
	Extra  *model.Memory
}

func exemptCases() []exemptCase {
	return []exemptCase{
		{Name: "고정", Change: func(m *model.Memory) { m.Pinned = true }},
		{Name: "decision", Change: func(m *model.Memory) { m.Type = model.TypeDecision }},
		{Name: "howto", Change: func(m *model.Memory) { m.Type = model.TypeHowto }},
		{Name: "fact", Change: func(m *model.Memory) { m.Type = model.TypeFact }},
		{Name: "열린todo", Change: func(m *model.Memory) {
			m.Type = model.TypeTodo
			m.TodoStatus = model.StatusOpen
		}},
		{Name: "importance5", Change: func(m *model.Memory) { m.Importance = 5 }},
		{Name: "caution-high", Change: func(m *model.Memory) {
			m.Type = model.TypeCaution
			m.Severity = model.SeverityHigh
		}},
		{Name: "links로지목됨", Change: func(m *model.Memory) {}, Extra: pointer(idAt(9), idAt(0))},
	}
}

// pointer 는 다른 기억을 links 로 가리키는 새 기억이다.
func pointer(id, target string) *model.Memory {
	memory := oldMemory(id, 5)
	memory.Links = []string{target}
	return memory
}

// TestExemptions 는 면제 표의 각 경우가 정말 안 걸리는지 본다 (설계 9-4).
func TestExemptions(t *testing.T) {
	for _, item := range exemptCases() {
		t.Run(item.Name, func(t *testing.T) {
			guarded := oldMemory(idAt(0), 200)
			item.Change(guarded)
			memories := []*model.Memory{guarded, oldMemory(idAt(1), 200)}
			if item.Extra != nil {
				memories = append(memories, item.Extra)
			}
			opened := newStore(t, memories...)
			result := runGC(t, opened, testConfig(), now, true)
			if len(result.Items) == 0 {
				t.Fatal("면제 아닌 기억까지 안 걸렸다. 시험이 헛돌고 있다")
			}
			for _, moved := range result.Items {
				if moved.ID == guarded.ID {
					t.Fatalf("면제인데 옮기려 든다 : %+v", moved)
				}
			}
		})
	}
}

// TestRecentHitExempt 는 30일 안에 조회된 기억이 면제인지 본다.
func TestRecentHitExempt(t *testing.T) {
	opened := newStore(t, oldMemory(idAt(0), 200), oldMemory(idAt(1), 200))
	if err := store.AppendHit(opened.Dir, "show", idAt(0)); err != nil {
		t.Fatal(err)
	}
	reindex(t, opened)
	result := runGC(t, opened, testConfig(), now, true)
	if len(result.Items) == 0 {
		t.Fatal("아무것도 안 걸렸다. 시험이 헛돌고 있다")
	}
	for _, moved := range result.Items {
		if moved.ID == idAt(0) {
			t.Fatal("최근에 본 기억을 옮기려 든다")
		}
	}
}

// TestColdKeepsFile 은 차가움이 요약만 남기고 파일은 남기는지 본다.
func TestColdKeepsFile(t *testing.T) {
	opened := newStore(t, oldMemory(idAt(0), 300), oldMemory(idAt(1), 300))
	settings := testConfig()
	settings.ColdCount = 1
	settings.ColdDays = 180
	result := runGC(t, opened, settings, now, false)
	if result.Cooled != 2 {
		t.Fatalf("두 건이 차가움으로 가야 한다 : %+v", result)
	}
	text := readFile(t, opened, idAt(0))
	if !strings.Contains(text, "state: cold") || strings.Contains(text, "줄 5") {
		t.Fatalf("본문이 안 접혔다 :\n%s", text)
	}
	if countFiles(t, opened.StoreDir()) != 2 {
		t.Fatal("파일이 없어졌다")
	}
}

// TestLockedSkips 는 남이 색인 중이면 조용히 건너뛰는지 본다.
func TestLockedSkips(t *testing.T) {
	opened := newStore(t, oldMemory(idAt(0), 200), oldMemory(idAt(1), 200))
	release, taken, err := index.TryLock(opened.Dir)
	if err != nil || !taken {
		t.Fatal("락을 못 잡았다")
	}
	defer release()
	result := runGC(t, opened, testConfig(), now, false)
	if !result.Locked {
		t.Fatalf("락이 잡혀 있는데 돌았다 : %+v", result)
	}
}
