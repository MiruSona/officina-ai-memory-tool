package lint

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// gitCallLimit 은 한 번의 lint 가 git 을 몇 번까지 부르는지다. 근거가 붙은
// 기억이 많으면 여기서 시간이 다 간다 — 상한을 넘으면 그 뒤로는 안 보고
// 「다 못 봤다」고 말한다 (20k 10초 예산 · 설계 G4).
const gitCallLimit = 400

// gitWatcher 는 근거 파일이 언제 바뀌었나(C06)와 커밋이 있나(D03)를 git 에
// 묻는다. 같은 경로는 한 번만 묻는다.
type gitWatcher struct {
	root    string
	git     string
	lastAt  map[string]int64
	commits map[string]bool
	calls   int
	capped  bool
}

// newGitWatcher 는 git 이 있고 프로젝트가 git 저장소일 때만 만들어진다.
// 아니면 nil 이고, 부르는 쪽이 그 규칙을 건너뛴다.
func newGitWatcher(storeDir string) *gitWatcher {
	found, err := exec.LookPath("git")
	if err != nil {
		return nil
	}
	root := filepath.Dir(storeDir)
	watcher := gitWatcher{root: root, git: found,
		lastAt: map[string]int64{}, commits: map[string]bool{}}
	if _, err := watcher.run("rev-parse", "--git-dir"); err != nil {
		return nil
	}
	return &watcher
}

func (w *gitWatcher) run(args ...string) (string, error) {
	command := exec.Command(w.git, args...)
	command.Dir = w.root
	out, err := command.Output()
	return strings.TrimSpace(string(out)), err
}

// changed 는 근거 하나가 기억 날짜 뒤에 바뀌었는지다 (C06).
func (w *gitWatcher) changed(m *model.Memory, source string) bool {
	target := ""
	switch {
	case strings.HasPrefix(source, model.SourceFile):
		target = strings.TrimSpace(strings.TrimPrefix(source, model.SourceFile))
	default:
		return false
	}
	if colon := strings.LastIndex(target, ":"); colon > 1 && isNumber(target[colon+1:]) {
		target = target[:colon]
	}
	if target == "" {
		return false
	}
	when := w.lastChange(target)
	if when == 0 {
		return false
	}
	written, err := time.ParseInLocation("2006-01-02", m.Date, time.Local)
	if err != nil {
		return false
	}
	// 같은 날 안의 순서는 못 가른다. 하루를 얹어 준다 — 넘치게 알리면 사람이
	// 검토 큐 전체를 무시하기 시작한다 (설계 결정 14).
	return when > written.AddDate(0, 0, 1).Unix()
}

// lastChange 는 그 경로를 마지막으로 고친 커밋 시각이다. 못 물으면 0 이다.
func (w *gitWatcher) lastChange(path string) int64 {
	if when, asked := w.lastAt[path]; asked {
		return when
	}
	if w.calls >= gitCallLimit {
		w.capped = true
		return 0
	}
	w.calls++
	out, err := w.run("log", "-1", "--format=%ct", "--", filepath.FromSlash(path))
	when := int64(0)
	if err == nil {
		when, _ = strconv.ParseInt(out, 10, 64)
	}
	w.lastAt[path] = when
	return when
}

// hasCommit 은 그 커밋이 이 저장소에 있는지다 (D03).
func (w *gitWatcher) hasCommit(hash string) bool {
	if found, asked := w.commits[hash]; asked {
		return found
	}
	if w.calls >= gitCallLimit {
		w.capped = true
		// 못 물어본 것을 「없다」고 말하지 않는다.
		return true
	}
	w.calls++
	_, err := w.run("cat-file", "-e", hash+"^{commit}")
	found := err == nil
	w.commits[hash] = found
	return found
}

// notes 는 다 못 본 것이 있으면 그 사실 한 줄이다.
func (w *gitWatcher) notes() []string {
	if !w.capped {
		return nil
	}
	return []string{fmt.Sprintf("근거를 %d번까지만 git 에 물었다. 그 뒤 것은 안 봤다", gitCallLimit)}
}

// SourceChanged 는 「근거가 이 기억 날짜 뒤에 바뀌었나」를 묻는 함수다.
// git 을 못 쓰면 nil 이라, 받는 쪽이 그 규칙(C06)을 건너뛴다.
// `mem review` 가 lint 와 같은 판정을 쓰게 하려고 여기서 내준다.
func SourceChanged(storeDir string) func(*model.Memory, string) bool {
	watcher := newGitWatcher(storeDir)
	if watcher == nil {
		return nil
	}
	return watcher.changed
}
