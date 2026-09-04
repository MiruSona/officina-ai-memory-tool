package lint

// 근거 표류 갈고리 (설계 결정 36 ③).
//
// D02(없는 경로)·D03(없는 커밋)은 `lint` 안에 이미 있었지만 `quality.CheckRepo`
// 를 안 지나서 **`mem review` 의 STALE 큐에 한 건도 안 왔다**. 여기서
// `quality.RepoOptions.SourceMissing` 이 받을 함수 하나를 만들어 lint 와 review
// 가 **같은 판정**을 쓰게 한다.

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
)

// missing 은 경로 집합과 git 을 한 번만 만들어 되풀이해 쓴다.
type missing struct {
	root    string
	known   map[string]bool
	full    bool
	watcher *gitWatcher
}

// SourceMissing 은 「이 근거가 이제 없나」를 묻는 함수다. 저장소가 git 이
// 아니면 커밋은 안 보고, 프로젝트 파일이 너무 많으면 경로도 안 본다 — 반쯤
// 훑은 목록으로 「없다」고 말하면 거짓말이 된다.
//
// storeDir 은 저장소 폴더(`Memory/`)다. 프로젝트 뿌리는 그 위다.
func SourceMissing(storeDir string) quality.SourceMissing {
	return newMissing(storeDir, newGitWatcher(storeDir)).ask
}

func newMissing(storeDir string, watcher *gitWatcher) *missing {
	root := filepath.Dir(storeDir)
	known, full := pathSet(root)
	return &missing{root: root, known: known, full: full, watcher: watcher}
}

// ask 는 근거 한 줄을 본다. 규칙 이름이 비면 멀쩡한 것이다.
func (m *missing) ask(_ *model.Memory, source string) (string, string) {
	switch {
	case strings.HasPrefix(source, model.SourceFile):
		return m.askPath(source)
	case strings.HasPrefix(source, model.SourceCommit):
		return m.askCommit(source)
	}
	return "", ""
}

func (m *missing) askPath(source string) (string, string) {
	if !m.full {
		return "", ""
	}
	target := trimLineNumber(strings.TrimSpace(strings.TrimPrefix(source, model.SourceFile)))
	if !isRelativePath(target) {
		return "", ""
	}
	if anyKnown([]string{m.root}, m.root, target, m.known) {
		return "", ""
	}
	return quality.RuleDeadPath, fmt.Sprintf("근거 경로 `%s` 가 프로젝트에 없다. 옮겼는지 지웠는지 보고 근거를 고친다", target)
}

func (m *missing) askCommit(source string) (string, string) {
	if m.watcher == nil {
		return "", ""
	}
	hash := strings.TrimSpace(strings.TrimPrefix(source, model.SourceCommit))
	if hash == "" || m.watcher.hasCommit(hash) {
		return "", ""
	}
	return quality.RuleDeadCommit, fmt.Sprintf("근거 커밋 `%s` 를 이 저장소에서 못 찾았다. 되감았거나 남의 저장소 것이다", hash)
}

// trimLineNumber 는 `file:경로:줄번호` 꼴에서 줄 번호를 뗀다.
func trimLineNumber(target string) string {
	if colon := strings.LastIndex(target, ":"); colon > 1 && isNumber(target[colon+1:]) {
		return target[:colon]
	}
	return target
}
