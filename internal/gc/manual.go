package gc

import (
	"fmt"
	"os"

	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// 사람이 손으로 시킨 한 건의 결과다.
const (
	StepFold    = "fold"
	StepRestore = "restore"
)

// Manual 은 `--fold` · `--restore` 가 기억 하나에 한 일이다.
type Manual struct {
	ID   string `json:"id"`
	Step string `json:"step"`
	Path string `json:"path,omitempty"`
	// Why 는 못 한 이유다. 빈 값이면 한 것이다.
	Why string `json:"why,omitempty"`
}

// Done 은 실제로 한 것인지다.
func (m Manual) Done() bool { return m.Why == "" }

// manualWanted 는 손으로 시킨 것이 있는지다. 있으면 나이 규칙으로 고르는
// 자동 계획은 아예 안 돈다 — 사람이 집은 것만 건드린다.
func manualWanted(options Options) bool {
	return len(options.Fold) > 0 || len(options.Restore) > 0
}

// runManual 은 사람이 집은 기억만 접거나 되돌린다. 파일은 하나도 안 지운다.
func runManual(database *index.DB, options Options, result *Result) error {
	worker := mover{db: database, store: options.Store, now: options.Now}
	for _, id := range options.Fold {
		result.Manual = append(result.Manual, foldOne(&worker, options, id))
	}
	for _, id := range options.Restore {
		result.Manual = append(result.Manual, restoreOne(&worker, options, id))
	}
	for _, one := range result.Manual {
		if one.Done() {
			result.Cooled++
		}
	}
	return nil
}

// foldOne 은 기억 하나의 본문을 접는다. 머리말은 남고 원본은 아카이브로 간다.
func foldOne(worker *mover, options Options, id string) Manual {
	done := Manual{ID: id, Step: StepFold}
	path, err := pathOf(worker.db, options.Store, id)
	if err != nil {
		done.Why = err.Error()
		return done
	}
	done.Path = path
	read, err := options.Store.ReadMemory(path)
	if err != nil {
		done.Why = err.Error()
		return done
	}
	if read.Memory.State == index.StateCold {
		done.Why = "이미 접혀 있다"
		return done
	}
	if options.DryRun {
		return done
	}
	if err := worker.applyItem(Item{ID: id, Type: read.Memory.Type, Step: StepCold, Path: path}); err != nil {
		done.Why = err.Error()
		return done
	}
	store.AppendLog(options.Store.Dir, options.Now, store.LogFolded, fmt.Sprintf("%s (gc --fold)", id))
	return done
}

// restoreOne 은 접은 본문을 아카이브에서 되살린다 (설계 3-4 · 결정 26).
// 되돌릴 수 없는 자동 조치를 안 만들겠다는 약속이 이 함수다.
func restoreOne(worker *mover, options Options, id string) Manual {
	done := Manual{ID: id, Step: StepRestore}
	path, err := pathOf(worker.db, options.Store, id)
	if err != nil {
		done.Why = err.Error()
		return done
	}
	done.Path = path
	saved, err := options.Store.ArchivedMemory(id)
	if err != nil {
		done.Why = err.Error()
		return done
	}
	if options.DryRun {
		return done
	}
	// 접기 전 판을 그대로 되돌린다. 접힘 표시만 지운다.
	saved.State = index.StateHot
	saved.Archived = ""
	if err := options.Store.WriteMemory(saved); err != nil {
		done.Why = err.Error()
		return done
	}
	if err := worker.refreshRow(Item{ID: id, Path: path}, saved); err != nil {
		done.Why = err.Error()
		return done
	}
	store.AppendLog(options.Store.Dir, options.Now, store.LogFolded, fmt.Sprintf("%s 되돌림 (gc --restore)", id))
	return done
}

// pathOf 는 기억 파일 자리다. 색인에 물어보고, 없으면 id 로 자리를 계산한다 —
// 색인은 파생물이라 아직 안 돌았을 수 있다.
func pathOf(database *index.DB, opened *store.Store, id string) (string, error) {
	if !model.IsID(id) {
		return "", fmt.Errorf("`%s` 는 기억 id 꼴이 아니다", id)
	}
	row, err := database.ByID(id)
	if err == nil && row != nil && row.Path != "" {
		return row.Path, nil
	}
	path := model.StorePath(id)
	abs, err := opened.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("`%s` 기억을 저장소에서 못 찾았다", id)
	}
	return path, nil
}

// ManualLines 는 --fold · --restore 결과를 사람이 읽는 줄로 바꾼다. cmd 가
// 한 줄로 찍는 자리다.
func ManualLines(result *Result) []string {
	lines := []string{}
	for _, one := range result.Manual {
		what := "접었다"
		if one.Step == StepRestore {
			what = "되돌렸다"
		}
		if result.DryRun {
			what += " (미리보기)"
		}
		if !one.Done() {
			lines = append(lines, fmt.Sprintf("  %s — 못 했다 : %s", one.ID, one.Why))
			continue
		}
		lines = append(lines, fmt.Sprintf("  %s — %s", one.ID, what))
	}
	return lines
}
