package gc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

func runManualGC(t *testing.T, opened *store.Store, fold, restore []string, dry bool) *Result {
	t.Helper()
	result, err := Run(Options{Store: opened, GC: testConfig(), Now: now,
		DryRun: dry, Fold: fold, Restore: restore})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// TestFoldOneByHand 는 --fold 가 집은 기억 하나만 접는지 본다 (설계 3-4).
func TestFoldOneByHand(t *testing.T) {
	opened := newStore(t, oldMemory(idAt(0), 3), oldMemory(idAt(1), 3))
	result := runManualGC(t, opened, []string{idAt(0)}, nil, false)
	if len(result.Manual) != 1 || !result.Manual[0].Done() {
		t.Fatalf("접기가 안 됐다 : %+v", result.Manual)
	}
	folded := readFile(t, opened, idAt(0))
	if strings.Contains(folded, "마지막 줄이다") {
		t.Fatalf("본문이 안 접혔다 :\n%s", folded)
	}
	if !strings.Contains(readFile(t, opened, idAt(1)), "마지막 줄이다") {
		t.Fatal("안 시킨 기억까지 접었다")
	}
}

// TestFoldWritesLog 는 접힌 사실이 log.md 에 남는지 본다 (설계 3-5).
func TestFoldWritesLog(t *testing.T) {
	opened := newStore(t, oldMemory(idAt(0), 3))
	runManualGC(t, opened, []string{idAt(0)}, nil, false)
	text, err := store.ReadLog(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, idAt(0)) || !strings.Contains(text, store.LogFolded) {
		t.Fatalf("log.md 에 접힘 줄이 없다 :\n%s", text)
	}
}

// TestRestoreBringsBodyBack 은 --restore 가 접기 전 본문을 되살리는지 본다.
// 되돌릴 수 없는 자동 조치를 안 만들겠다는 약속이 이 시험이다 (결정 26).
func TestRestoreBringsBodyBack(t *testing.T) {
	opened := newStore(t, oldMemory(idAt(0), 3))
	before := readFile(t, opened, idAt(0))
	runManualGC(t, opened, []string{idAt(0)}, nil, false)
	result := runManualGC(t, opened, nil, []string{idAt(0)}, false)
	if len(result.Manual) != 1 || !result.Manual[0].Done() {
		t.Fatalf("되돌리기가 안 됐다 : %+v", result.Manual)
	}
	after := readFile(t, opened, idAt(0))
	if !strings.Contains(after, "마지막 줄이다") {
		t.Fatalf("본문이 안 돌아왔다 :\n%s", after)
	}
	if strings.Contains(after, index.StateCold) {
		t.Fatalf("접힘 표시가 남았다 :\n%s", after)
	}
	if len(after) < len(before)/2 {
		t.Fatalf("되살린 본문이 너무 짧다 : %d → %d", len(before), len(after))
	}
}

// TestFoldDryRunChangesNothing 은 미리보기가 파일을 안 건드리는지 본다.
func TestFoldDryRunChangesNothing(t *testing.T) {
	opened := newStore(t, oldMemory(idAt(0), 3))
	before := readFile(t, opened, idAt(0))
	result := runManualGC(t, opened, []string{idAt(0)}, nil, true)
	if len(result.Manual) != 1 || !result.Manual[0].Done() {
		t.Fatalf("미리보기가 계획을 안 보여줬다 : %+v", result.Manual)
	}
	if readFile(t, opened, idAt(0)) != before {
		t.Fatal("미리보기가 파일을 고쳤다")
	}
}

// TestFoldUnknownIDSaysWhy 는 없는 id 에 이유를 말하고 종료 0 을 지키는지 본다.
func TestFoldUnknownIDSaysWhy(t *testing.T) {
	opened := newStore(t, oldMemory(idAt(0), 3))
	result := runManualGC(t, opened, []string{"20260101-deadbeef"}, nil, false)
	if len(result.Manual) != 1 || result.Manual[0].Done() {
		t.Fatalf("없는 id 인데 했다고 한다 : %+v", result.Manual)
	}
	if result.Manual[0].Why == "" {
		t.Fatal("왜 못 했는지를 안 말했다")
	}
}

// TestManualDeletesNothing 은 손으로 시켜도 파일이 하나도 안 지워지는지 본다
// (불변조건 5).
func TestManualDeletesNothing(t *testing.T) {
	opened := newStore(t, oldMemory(idAt(0), 3))
	before := countFiles(t, opened.StoreDir())
	runManualGC(t, opened, []string{idAt(0)}, nil, false)
	if countFiles(t, opened.StoreDir()) != before {
		t.Fatal("파일이 지워졌다")
	}
	if _, err := os.Stat(filepath.Join(opened.Dir, filepath.FromSlash("store"))); err != nil {
		t.Fatal(err)
	}
}
