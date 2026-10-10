package lint

// 근거 표류 갈고리 (설계 결정 36 ③).
//
// D02(없는 경로)·D03(없는 커밋)은 `lint` 안에 이미 있었지만 `quality.CheckRepo`
// 를 안 지나서 **`mem review` 의 STALE 큐에 한 건도 안 왔다**. 여기서
// `quality.RepoOptions.SourceMissing` 이 받을 함수 하나를 만들어 lint 와 review
// 가 **같은 판정**을 쓰게 한다.

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
)

// missing 은 경로 집합과 git 을 한 번만 만들어 되풀이해 쓴다. 근거(D02·D03)와
// 본문 경로(D05)가 같은 집합을 나눠 쓴다 — 프로젝트를 두 번 훑지 않는다.
type missing struct {
	root     string
	storeDir string
	known    map[string]bool
	full     bool
	watcher  *gitWatcher
	// scopeDirs 는 뿌리 바로 아래 폴더의 소문자 이름 → 실제 이름이다
	// (`aimemorytool` → `AIMemoryTool`). D05 의 세 번째 기준이다.
	scopeDirs map[string]string
}

// SourceMissing 은 「이 근거가 이제 없나」를 묻는 함수다. 저장소가 git 이
// 아니면 커밋은 안 보고, 프로젝트 파일이 너무 많으면 경로도 안 본다 — 반쯤
// 훑은 목록으로 「없다」고 말하면 거짓말이 된다.
//
// storeDir 은 저장소 폴더(`Memory/`)다. 프로젝트 뿌리는 그 위다.
func SourceMissing(storeDir string) quality.SourceMissing {
	return newMissing(storeDir, newGitWatcher(storeDir)).ask
}

// Missing 은 근거 갈고리(D02·D03)와 본문 경로 갈고리(D05)를 **한 경로 집합으로**
// 같이 만든다. `mem review` 처럼 둘 다 꽂는 쪽은 이것을 쓴다 — SourceMissing 과
// 따로 부르면 프로젝트를 두 번 훑는다.
func Missing(storeDir string) (quality.SourceMissing, quality.BodyMissing) {
	found := newMissing(storeDir, newGitWatcher(storeDir))
	return found.ask, found.bodyAsk
}

func newMissing(storeDir string, watcher *gitWatcher) *missing {
	root := filepath.Dir(storeDir)
	known, full := pathSet(root)
	return &missing{root: root, storeDir: storeDir, known: known, full: full,
		watcher: watcher, scopeDirs: topDirs(root)}
}

// topDirs 는 뿌리 바로 아래 폴더를 소문자 이름으로 찾게 만든다.
func topDirs(root string) map[string]string {
	dirs := map[string]string{}
	entries, err := os.ReadDir(root)
	if err != nil {
		return dirs
	}
	for _, entry := range entries {
		if entry.IsDir() {
			dirs[strings.ToLower(entry.Name())] = entry.Name()
		}
	}
	return dirs
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

// bodyAsk 는 본문에 적은 경로 하나를 본다 (D05 · 점검·정리 설계 2절 ②).
// 오탐을 먼저 막는다 — 걸러서 놓치는 것은 괜찮다, 후보만 올리는 검사다.
//
//  1. 확장자가 없으면(`internal/install`) 폴더 이름일 수 있어 안 본다.
//  2. `./`·`../` 로 시작하면 안 본다. 기억 파일 자리를 모르니 풀 기준이 없다.
//  3. 첫 마디가 세 기준 어디에도 없으면(`Assets/…`) 남의 저장소로 치고 멀쩡하다.
//  4. 세 기준(프로젝트 뿌리 · 저장소 폴더 · scope 폴더) 중 하나라도 있으면 멀쩡하다.
//
// linked 는 `[..](경로)` 링크에서 뽑은 것이다 — 그러면 D02 로, 아니면 D05 로 낸다.
func (m *missing) bodyAsk(memory *model.Memory, target string, linked bool) (string, string) {
	if !m.full {
		return "", ""
	}
	target = trimLineNumber(strings.TrimSpace(target))
	if !isRelativePath(target) || path.Ext(target) == "" {
		return "", ""
	}
	first, _, _ := strings.Cut(target, "/")
	if first == "." || first == ".." {
		return "", ""
	}
	bases := m.bodyBases(memory)
	if !m.anyHas(bases, first) || anyKnown(bases, m.root, target, m.known) {
		return "", ""
	}
	if linked {
		return quality.RuleDeadPath, fmt.Sprintf("본문 링크 `%s` 가 프로젝트에 없다. 옮겼는지 지웠는지 보고 본문을 고친다", target)
	}
	return quality.RuleDeadBodyPath, fmt.Sprintf("본문 경로 `%s` 가 프로젝트·저장소·scope 폴더 어디에도 없다. 옮겼는지 지웠는지 보고 본문을 고친다", target)
}

// bodyBases 는 본문 경로를 풀 세 기준이다. scope 폴더는 있을 때만 든다.
func (m *missing) bodyBases(memory *model.Memory) []string {
	bases := []string{m.root, m.storeDir}
	if dir, ok := m.scopeDirs[strings.ToLower(memory.Scope)]; ok && memory.Scope != "" {
		bases = append(bases, filepath.Join(m.root, dir))
	}
	return bases
}

// anyHas 는 첫 마디가 기준 중 하나 아래에 실제로 있는지다. 뿌리 밖으로 나가는
// 기준은 없다고 본다 — 여기선 「우리 것인가」만 묻는다.
func (m *missing) anyHas(bases []string, first string) bool {
	for _, base := range bases {
		rel, err := filepath.Rel(m.root, filepath.Join(base, first))
		if err != nil {
			continue
		}
		if m.known[filepath.ToSlash(rel)] {
			return true
		}
	}
	return false
}

// trimLineNumber 는 `file:경로:줄번호` 꼴에서 줄 번호를 뗀다.
func trimLineNumber(target string) string {
	if colon := strings.LastIndex(target, ":"); colon > 1 && isNumber(target[colon+1:]) {
		return target[:colon]
	}
	return target
}
