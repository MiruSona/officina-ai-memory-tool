package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/transform"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store := Open(filepath.Join(t.TempDir(), "Memory"), false)
	if err := store.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	return store
}

func sampleAdd() AddRequest {
	return AddRequest{
		Type: model.TypeIssue, Summary: strings.Repeat("가", 40), Tags: []string{"fts5"},
		Source: model.LegacySourceAI, Scope: "mem-search", Severity: model.SeverityHigh,
		Body: "## 증상\n\n두 글자 한글이 0건이었다.",
	}
}

func TestWriteAddLandsInNewWithoutEscaping(t *testing.T) {
	store := newTestStore(t)
	name, err := store.WriteAdd(sampleAdd())
	if err != nil {
		t.Fatal(err)
	}
	left, err := os.ReadDir(store.InboxTmpDir())
	if err != nil || len(left) != 0 {
		t.Fatalf("tmp must be empty after the rename: %v %v", left, err)
	}
	raw, err := os.ReadFile(filepath.Join(store.InboxNewDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `\u`) {
		t.Fatalf("Korean must stay as Korean: %s", raw)
	}
	if !strings.Contains(string(raw), `"op":"add"`) {
		t.Fatalf("unexpected payload: %s", raw)
	}
	item, err := store.ReadInbox(name)
	if err != nil {
		t.Fatal(err)
	}
	if item.Op != OpAdd || item.Add == nil || item.Add.Severity != model.SeverityHigh {
		t.Fatalf("unexpected item: %+v", item)
	}
}

func TestWriteAddRejectsMissingFields(t *testing.T) {
	store := newTestStore(t)
	request := sampleAdd()
	request.Scope = ""
	if _, err := store.WriteAdd(request); err == nil {
		t.Fatal("expected a missing scope error")
	}
}

func TestWritePatchLimitsFields(t *testing.T) {
	store := newTestStore(t)
	name, err := store.WritePatch("20260822-3f9a2c1b", map[string]any{"pinned": true})
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.ReadInbox(name)
	if err != nil {
		t.Fatal(err)
	}
	if item.Patch == nil || item.Patch.ID != "20260822-3f9a2c1b" || item.Patch.Set["pinned"] != true {
		t.Fatalf("unexpected patch: %+v", item.Patch)
	}
	// summary is patchable now (mem edit), but the body and anything that
	// decides the file path never are (설계검토 #14b).
	if _, err := store.WritePatch("20260822-3f9a2c1b", map[string]any{"body": "안 된다"}); err == nil {
		t.Fatal("the body must not be patchable")
	}
	if _, err := store.WritePatch("20260822-3f9a2c1b", map[string]any{"date": "2026-01-01"}); err == nil {
		t.Fatal("the date must not be patchable")
	}
	if _, err := store.WritePatch("", map[string]any{"pinned": true}); err == nil {
		t.Fatal("an empty id must fail")
	}
}

func TestConcurrentWritesLoseNothing(t *testing.T) {
	store := newTestStore(t)
	const writers, perWriter = 16, 200
	group := sync.WaitGroup{}
	for writer := 0; writer < writers; writer++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for i := 0; i < perWriter; i++ {
				if _, err := store.WriteAdd(sampleAdd()); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	group.Wait()
	names, err := store.ListInbox()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != writers*perWriter {
		t.Fatalf("expected %d files, got %d", writers*perWriter, len(names))
	}
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			t.Fatalf("duplicate name: %s", name)
		}
		seen[name] = true
	}
	left, err := os.ReadDir(store.InboxTmpDir())
	if err != nil || len(left) != 0 {
		t.Fatalf("tmp must be empty: %v %v", left, err)
	}
}

func TestBrokenJSONGoesToBad(t *testing.T) {
	store := newTestStore(t)
	name := "1.1.1.json"
	if err := os.WriteFile(filepath.Join(store.InboxNewDir(), name), []byte(`{"op":"add","summ`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadInbox(name); err == nil {
		t.Fatal("a broken file must be an error")
	}
	if err := store.MoveToBad(name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.InboxBadDir(), name)); err != nil {
		t.Fatalf("the file must survive in bad/: %v", err)
	}
	names, err := store.ListInbox()
	if err != nil || len(names) != 0 {
		t.Fatalf("new/ must be empty: %v %v", names, err)
	}
}

func TestUnknownOpIsAnError(t *testing.T) {
	store := newTestStore(t)
	name := "2.2.2.json"
	if err := os.WriteFile(filepath.Join(store.InboxNewDir(), name), []byte(`{"op":"drop","id":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadInbox(name); err == nil {
		t.Fatal("expected an unknown op error")
	}
}

// TestArchiveAppendsTwoLines: a promoted request is kept as a receipt under
// local/received/, which git never sees (design 2-3 #9).
func TestArchiveAppendsTwoLines(t *testing.T) {
	store := newTestStore(t)
	for i := 0; i < 2; i++ {
		name, err := store.WriteAdd(sampleAdd())
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Archive(name); err != nil {
			t.Fatal(err)
		}
	}
	names, err := store.ListInbox()
	if err != nil || len(names) != 0 {
		t.Fatalf("archived files must leave new/: %v %v", names, err)
	}
	lines, err := store.ReadReceived(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 archived lines, got %d", len(lines))
	}
	request := AddRequest{}
	if err := json.Unmarshal([]byte(lines[1]), &request); err != nil {
		t.Fatalf("archived line is not JSON: %v", err)
	}
	if request.Scope != "mem-search" {
		t.Fatalf("archived line lost fields: %+v", request)
	}
}

func writeMemoryFile(t *testing.T, store *Store, id string, data []byte) string {
	t.Helper()
	relative := model.StorePath(id)
	abs := filepath.Join(store.Dir, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return relative
}

func TestWriteAndListMemories(t *testing.T) {
	store := newTestStore(t)
	memory := model.Memory{
		ID: "20260822-3f9a2c1b", Type: model.TypeHistory, Date: "2026-08-22",
		Summary: strings.Repeat("가", 40), Tags: []string{"mem"}, LegacySource: model.LegacySourceAI,
		Scope: "mem", Body: "본문 한 줄",
	}
	if err := store.WriteMemory(&memory); err != nil {
		t.Fatal(err)
	}
	files, err := store.ListMemories()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "store/2026/08/20260822-3f9a2c1b.md" {
		t.Fatalf("unexpected listing: %+v", files)
	}
	if files[0].Size == 0 || files[0].ModTime.IsZero() {
		t.Fatalf("listing must carry size and mtime: %+v", files[0])
	}
	read, err := store.ReadMemory(files[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if read.Encoding != EncodingUTF8 || read.Memory.Body != memory.Body {
		t.Fatalf("unexpected read: %+v", read)
	}
	left, err := os.ReadDir(store.InboxTmpDir())
	if err != nil || len(left) != 0 {
		t.Fatalf("atomic write must clean tmp: %v %v", left, err)
	}
}

func TestReadMemoryStripsBOM(t *testing.T) {
	store := newTestStore(t)
	body := "---\nid: 20260822-aaaabbbb\ntype: history\ndate: 2026-08-22\nsummary: " + strings.Repeat("가", 40) + "\ntags: [mem]\nsource: ai\nscope: mem\n---\n\n본문\n"
	relative := writeMemoryFile(t, store, "20260822-aaaabbbb", append([]byte{0xEF, 0xBB, 0xBF}, body...))
	read, err := store.ReadMemory(relative)
	if err != nil {
		t.Fatal(err)
	}
	if read.Encoding != EncodingUTF8BOM || read.Memory.ID != "20260822-aaaabbbb" {
		t.Fatalf("unexpected read: %+v", read)
	}
}

func TestReadMemoryFallsBackToCP949AndRewrites(t *testing.T) {
	store := newTestStore(t)
	body := "---\nid: 20260822-ccccdddd\ntype: history\ndate: 2026-08-22\nsummary: " + strings.Repeat("가", 40) + "\ntags: [mem]\nsource: ai\nscope: mem\n---\n\n한글 본문\n"
	encoded, _, err := transform.Bytes(korean.EUCKR.NewEncoder(), []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	relative := writeMemoryFile(t, store, "20260822-ccccdddd", encoded)
	read, err := store.ReadMemory(relative)
	if err != nil {
		t.Fatal(err)
	}
	if read.Encoding != EncodingCP949 || read.Memory.Body != "한글 본문" {
		t.Fatalf("unexpected read: %+v", read)
	}
	again, err := store.ReadMemory(relative)
	if err != nil {
		t.Fatal(err)
	}
	if again.Encoding != EncodingUTF8 {
		t.Fatalf("a cp949 file must be rewritten as UTF-8, got %s", again.Encoding)
	}
}

func TestReadOnlyStoreRefusesWrites(t *testing.T) {
	writable := newTestStore(t)
	readOnly := Open(writable.Dir, true)
	if _, err := readOnly.WriteAdd(sampleAdd()); !IsReadOnly(err) {
		t.Fatalf("expected a read-only error, got %v", err)
	}
	if err := readOnly.EnsureDirs(); !IsReadOnly(err) {
		t.Fatalf("expected a read-only error, got %v", err)
	}
	memory := model.Memory{ID: "20260822-3f9a2c1b", Type: model.TypeHistory}
	if err := readOnly.WriteMemory(&memory); !IsReadOnly(err) {
		t.Fatalf("expected a read-only error, got %v", err)
	}
}

func TestRetryOnlyForSharingViolation(t *testing.T) {
	calls := 0
	err := withRetry(func() error {
		calls++
		if calls < 3 {
			return &os.PathError{Op: "open", Path: "x", Err: errSharingViolation}
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("expected 3 calls and no error, got %d %v", calls, err)
	}

	calls = 0
	err = withRetry(func() error {
		calls++
		return &os.LinkError{Op: "rename", Err: errLockViolation}
	})
	if err == nil || calls != retryAttempts {
		t.Fatalf("expected %d attempts, got %d %v", retryAttempts, calls, err)
	}

	calls = 0
	err = withRetry(func() error {
		calls++
		return &os.PathError{Op: "open", Path: "x", Err: syscall.Errno(2)}
	})
	if err == nil || calls != 1 {
		t.Fatalf("other errors must not be retried, got %d %v", calls, err)
	}
}

// --- 보안 강화 묶음 A : 경로 감옥 회귀 시험 ---

// isolateHome keeps a test away from the real home folder.
func isolateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
}

func TestAbsRefusesOutside(t *testing.T) {
	isolateHome(t)
	store := newTestStore(t)
	bad := []string{`..\AGENTS.md`, "../AGENTS.md", "store/../../AGENTS.md", "", filepath.Join(t.TempDir(), "x.md")}
	for _, path := range bad {
		if _, err := store.Abs(path); err == nil {
			t.Fatalf("must be refused: %q", path)
		}
	}
	inside, err := store.Abs("store/2026/08/20260822-3f9a2c1b.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(inside, store.Dir) {
		t.Fatalf("inside path left the root: %s", inside)
	}
}

func TestWriteMemoryRefusesOutside(t *testing.T) {
	isolateHome(t)
	store := newTestStore(t)
	outside := filepath.Dir(store.Dir)
	before, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{`\..\..\AGENTS`, "../../AGENTS", "20260822-XYZ"} {
		memory := model.Memory{ID: id, Type: model.TypeHistory, Date: "2026-08-22",
			Summary: strings.Repeat("가", 35), Tags: []string{"mem"}, LegacySource: model.LegacySourceAI, Scope: "mem"}
		if err := store.WriteMemory(&memory); err == nil {
			t.Fatalf("a bad id must not be written: %q", id)
		}
	}
	after, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("nothing may appear outside the root: %d -> %d", len(before), len(after))
	}
}

func TestReadMemoryRefusesSymlink(t *testing.T) {
	isolateHome(t)
	store := newTestStore(t)
	target := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(target, []byte("---\nid: x\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rel := "store/2026/08/20260822-3f9a2c1b.md"
	link := filepath.Join(store.Dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skip("이 환경에서는 심볼릭 링크를 못 만든다 :", err)
	}
	if _, err := store.ReadMemory(rel); err == nil {
		t.Fatal("a symlink must not be read")
	}
	files, err := store.ListMemories()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("a symlink must not be listed: %+v", files)
	}
}

func TestArchiveReadStopsAtCap(t *testing.T) {
	isolateHome(t)
	store := newTestStore(t)
	previous := MaxArchiveBytes
	MaxArchiveBytes = 512
	t.Cleanup(func() { MaxArchiveBytes = previous })
	at := time.Date(2026, 8, 22, 12, 0, 0, 0, time.Local)
	for i := 0; i < 40; i++ {
		if err := store.appendArchive(at, []byte(strings.Repeat("a", 100)+"\n")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.ReadReceived(at); err == nil {
		t.Fatal("reading past the cap must be an error")
	}
	MaxArchiveBytes = previous
	lines, err := store.ReadReceived(at)
	if err != nil || len(lines) != 40 {
		t.Fatalf("under the cap it must read everything: %d %v", len(lines), err)
	}
}

// TestHitsCompactKeepsTheSums is 설계검토 #5: squeezing the log must not change
// what the index reads back from it.
func TestHitsCompactKeepsTheSums(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 4; i++ {
		if err := AppendHit(dir, "show", "20260822-9e02d7f1"); err != nil {
			t.Fatal(err)
		}
	}
	if err := AppendHit(dir, "show", "20260822-0000000a"); err != nil {
		t.Fatal(err)
	}
	before, lines, err := ReadHits(dir)
	if err != nil {
		t.Fatal(err)
	}
	if lines != 5 || before["20260822-9e02d7f1"].Count != 4 {
		t.Fatalf("read %d lines, first count %d", lines, before["20260822-9e02d7f1"].Count)
	}
	if err := CompactHits(dir, before); err != nil {
		t.Fatal(err)
	}
	after, lines, err := ReadHits(dir)
	if err != nil {
		t.Fatal(err)
	}
	if lines != 2 {
		t.Fatalf("compacted file has %d lines, want one per memory", lines)
	}
	for id, total := range before {
		if after[id].Count != total.Count {
			t.Fatalf("%s: %d became %d", id, total.Count, after[id].Count)
		}
	}
}

// TestHitsSkipsTornLine keeps the counts in front of a broken tail.
func TestHitsSkipsTornLine(t *testing.T) {
	dir := t.TempDir()
	if err := AppendHit(dir, "show", "20260822-9e02d7f1"); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(HitsPath(dir), os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	file.WriteString(`{"id":"20260822-000`)
	file.Close()
	totals, _, err := ReadHits(dir)
	if err != nil {
		t.Fatal(err)
	}
	if totals["20260822-9e02d7f1"].Count != 1 {
		t.Fatal("a torn tail threw away the count in front of it")
	}
}
