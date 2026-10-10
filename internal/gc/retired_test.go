package gc

import (
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// 시험 C18 (기억점검정리설계 9절) 의 gc 쪽이다.

func runRetiredGC(t *testing.T, opened *store.Store, apply bool) *Result {
	t.Helper()
	result, err := Run(Options{Store: opened, GC: testConfig(), Now: now,
		DryRun: !apply, Retired: true})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// superseded 는 다른 기억이 덮은 기억이다. 갓 만들어 나이 문턱에는 안 걸린다.
func superseded(id, by string) *model.Memory {
	memory := oldMemory(id, 1)
	memory.SupersededBy = by
	return memory
}

// expired 는 invalid_at 이 어제로 지난 기억이다.
func expired(id string) *model.Memory {
	memory := oldMemory(id, 1)
	memory.InvalidAt = now.AddDate(0, 0, -1).Format("2006-01-02")
	return memory
}

func retiredIDs(result *Result) map[string]bool {
	picked := map[string]bool{}
	for _, one := range result.Retired {
		picked[one.ID] = true
	}
	return picked
}

// TestRetiredPicksDeadOnly 는 덮임·무효 지남만 고르고 산 것·무효 날짜 전인 것은
// 안 고르는지 본다. 나이 1일이라 다른 gc 의 문턱으로는 하나도 안 걸린다.
func TestRetiredPicksDeadOnly(t *testing.T) {
	future := oldMemory(idAt(3), 1)
	future.InvalidAt = now.AddDate(0, 0, 10).Format("2006-01-02")
	opened := newStore(t, superseded(idAt(0), idAt(2)), expired(idAt(1)),
		oldMemory(idAt(2), 1), future)
	result := runRetiredGC(t, opened, false)
	picked := retiredIDs(result)
	if len(picked) != 2 || !picked[idAt(0)] || !picked[idAt(1)] {
		t.Fatalf("덮임·무효 둘만 골라야 한다 : %+v", result.Retired)
	}
	for _, one := range result.Retired {
		if one.ID == idAt(0) && one.SupersededBy != idAt(2) {
			t.Fatalf("덮은 id 가 안 적혔다 : %+v", one)
		}
		if one.ID == idAt(1) && one.InvalidAt != expired(idAt(1)).InvalidAt {
			t.Fatalf("invalid_at 이 안 적혔다 : %+v", one)
		}
	}
}

// TestRetiredSkipsGuarded 는 cold · pinned · 산 카드의 근거는 안 고르는지 본다.
func TestRetiredSkipsGuarded(t *testing.T) {
	cases := map[string]func(t *testing.T) *store.Store{
		"cold": func(t *testing.T) *store.Store {
			opened := newStore(t, superseded(idAt(0), idAt(2)), expired(idAt(1)), oldMemory(idAt(2), 1))
			runManualGC(t, opened, []string{idAt(0)}, nil, false)
			return opened
		},
		"pinned": func(t *testing.T) *store.Store {
			guarded := superseded(idAt(0), idAt(2))
			guarded.Pinned = true
			return newStore(t, guarded, expired(idAt(1)), oldMemory(idAt(2), 1))
		},
		"카드근거": func(t *testing.T) *store.Store {
			return newStore(t, superseded(idAt(0), idAt(2)), expired(idAt(1)),
				oldMemory(idAt(2), 1), card(idAt(9), idAt(0)))
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			result := runRetiredGC(t, build(t), false)
			picked := retiredIDs(result)
			if !picked[idAt(1)] {
				t.Fatalf("막히지 않은 무효 기억도 안 골랐다. 시험이 헛돌고 있다 : %+v", result.Retired)
			}
			if picked[idAt(0)] {
				t.Fatalf("%s 인데 골랐다 : %+v", name, result.Retired)
			}
		})
	}
}

// TestRetiredPreviewChangesNothing 은 기본(미리보기)이 파일·아카이브·log 를 안
// 건드리는지 본다.
func TestRetiredPreviewChangesNothing(t *testing.T) {
	opened := newStore(t, superseded(idAt(0), idAt(1)), oldMemory(idAt(1), 1))
	before := readFile(t, opened, idAt(0))
	files := countFiles(t, opened.Dir)
	result := runRetiredGC(t, opened, false)
	if len(result.Retired) != 1 || len(result.Manual) != 0 || result.Cooled != 0 {
		t.Fatalf("미리보기가 접으려 들었다 : %+v", result)
	}
	if readFile(t, opened, idAt(0)) != before {
		t.Fatal("미리보기가 파일을 바꿨다")
	}
	if countFiles(t, opened.Dir) != files {
		t.Fatal("미리보기가 파일을 새로 만들었다 (아카이브·log)")
	}
	lines := RetiredLines(result)
	if !strings.Contains(lines[len(lines)-1], "1건") || !strings.Contains(lines[len(lines)-1], "--apply") {
		t.Fatalf("마지막 줄에 건수·명령이 없다 : %q", lines[len(lines)-1])
	}
}

// TestRetiredApplyThenRestore 는 --apply 가 접고 log 에 `(gc --retired)` 를 적으며,
// --restore 하면 바이트까지 같게 돌아오는지 본다 (결정 10 — 실수는 되돌린다).
func TestRetiredApplyThenRestore(t *testing.T) {
	opened := newStore(t, superseded(idAt(0), idAt(1)), oldMemory(idAt(1), 1))
	before := readFile(t, opened, idAt(0))
	result := runRetiredGC(t, opened, true)
	if result.Cooled != 1 || len(result.Manual) != 1 || !result.Manual[0].Done() {
		t.Fatalf("접기가 안 됐다 : %+v", result.Manual)
	}
	if strings.Contains(readFile(t, opened, idAt(0)), "마지막 줄이다") {
		t.Fatal("본문이 안 접혔다")
	}
	logText, err := store.ReadLog(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logText, idAt(0)+" (gc --retired)") {
		t.Fatalf("log.md 에 (gc --retired) 줄이 없다 :\n%s", logText)
	}
	if again := runRetiredGC(t, opened, false); len(again.Retired) != 0 {
		t.Fatalf("접은 것을 또 고른다 : %+v", again.Retired)
	}
	restored := runManualGC(t, opened, nil, []string{idAt(0)}, false)
	if len(restored.Manual) != 1 || !restored.Manual[0].Done() {
		t.Fatalf("되돌리기가 안 됐다 : %+v", restored.Manual)
	}
	if after := readFile(t, opened, idAt(0)); after != before {
		t.Fatalf("되돌린 파일이 바이트까지 같지 않다 :\n--- 전\n%s\n--- 후\n%s", before, after)
	}
	// 되돌린 뒤 색인도 살아 있는 꼴(hot)이라 다시 고를 수 있어야 한다.
	if back := runRetiredGC(t, opened, false); len(back.Retired) != 1 {
		t.Fatalf("되돌린 뒤 다시 안 고른다 : %+v", back.Retired)
	}
}
