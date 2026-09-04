package lint

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// pathSetLimit 은 dead-path 가 경로를 몇 개까지 훑는지다. 넘으면 그 규칙을
// 아예 끈다 — 반쯤 훑은 목록으로 "없는 경로" 라고 말하면 거짓말이 된다.
const pathSetLimit = 200000

// MissFileName 은 search 가 0건 난 질의를 남기는 파일이다 (설계 6-3).
// 한 줄이 {"q":"…","words":["…"],"at":1755820800} 꼴이라고 보고 읽는다.
const MissFileName = "miss.jsonl"

// missMinCount 는 동의어 후보로 올릴 0건 횟수다.
const missMinCount = 3

// bodyLink 는 본문의 Markdown 링크다. lint 가 풀어 보는 경로는 이것과
// `sources` 의 `file:` 둘이다.
var bodyLink = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)\)`)

// missLine 은 local/miss.jsonl 한 줄이다.
type missLine struct {
	Query string   `json:"q"`
	Words []string `json:"words"`
	At    int64    `json:"at"`
}

// checkRepository 는 파일을 한꺼번에 봐야 아는 규칙들이다. 통계·모순·낡음·
// 차가움은 quality.CheckRepo 가, 링크·경로·큐·그림자 exe 는 lint 가 본다.
func checkRepository(scans []*scanned, options *Options, report *Report) []Problem {
	watcher := gitOf(options, report)
	memories := memoriesOf(scans)
	paths := pathOf(scans)
	problems := fromRepoReport(repoReport(memories, options, watcher, report), paths)
	problems = append(problems, checkLinks(scans)...)
	problems = append(problems, checkPaths(scans, options, report)...)
	problems = append(problems, checkCommits(scans, watcher)...)
	problems = append(problems, checkInboxBad(options)...)
	problems = append(problems, checkShadowExe(options)...)
	if watcher != nil {
		report.Notes = append(report.Notes, watcher.notes()...)
	}
	return problems
}

// gitOf 는 git 을 쓸 수 있으면 한 번만 만들어 C06·D03 이 같이 쓰게 한다.
func gitOf(options *Options, report *Report) *gitWatcher {
	if options.NoGit {
		return nil
	}
	watcher := newGitWatcher(options.Store.Dir)
	if watcher == nil {
		report.Notes = append(report.Notes, "git 저장소가 아니거나 git 이 없어 근거가 바뀌었는지(C06)·커밋이 있는지(D03)는 안 봤다")
	}
	return watcher
}

// repoReport 는 저장소 전체 규칙이다 (F11·F13·B10·C01~C11). 조회 기록과 git
// 은 lint 가 붙여 준다 — quality 는 파일 시스템을 모른다.
func repoReport(memories []*model.Memory, options *Options, watcher *gitWatcher, report *Report) quality.RepoReport {
	repo := quality.RepoOptions{Options: gateOptions(options)}
	repo.Hits = hitTimes(options, report.note)
	if watcher != nil {
		repo.SourceChanged = watcher.changed
	}
	// 근거 표류(D02·D03)를 CheckRepo 도 보게 한다. 안 꽂으면 `mem review` 의
	// STALE 큐에 한 건도 안 온다 (결정 36 ③).
	repo.SourceMissing = newMissing(options.Store.Dir, watcher).ask
	return quality.CheckRepo(memories, repo)
}

// fromRepoReport 는 기억마다 걸린 것과 저장소 전체 이야기를 문제 줄로 옮긴다.
func fromRepoReport(found quality.RepoReport, paths map[string]string) []Problem {
	problems := []Problem{}
	ids := make([]string, 0, len(found.ByID))
	for id := range found.ByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		problems = appendFindings(problems, dropOwnRules(found.ByID[id]), paths[id])
	}
	return appendFindings(problems, found.Wide, "")
}

// lintOwnRules 는 lint 가 **자기 눈으로 따로 보는** 규칙이다. CheckRepo 도
// 이제 같은 것을 보지만(결정 36 ③) lint 쪽이 경로마다·커밋마다 한 줄씩 내서
// 더 자세하다. 두 벌이 겹쳐 나오지 않게 여기서 뺀다 — review 는 CheckRepo 쪽을
// 그대로 쓴다.
var lintOwnRules = map[string]bool{quality.RuleDeadPath: true, quality.RuleDeadCommit: true}

func dropOwnRules(found []quality.Finding) []quality.Finding {
	kept := make([]quality.Finding, 0, len(found))
	for _, one := range found {
		if lintOwnRules[one.Rule] {
			continue
		}
		kept = append(kept, one)
	}
	return kept
}

// hitTimes 는 id 마다 마지막으로 읽힌 때다. 기록이 없으면 nil 이라 차가움·
// 고아(C09·C10)를 건너뛴다 — 기록도 없는데 「한 번도 안 잡혔다」고 말하면
// 저장소가 통째로 경고가 된다.
func hitTimes(options *Options, note func(string)) map[string]time.Time {
	totals, lines, err := store.ReadHits(options.Store.Dir)
	if err != nil || lines == 0 {
		note("조회 기록(hits.jsonl)이 없어 차가운 기억(C09)·고아(C10)는 안 봤다")
		return nil
	}
	when := make(map[string]time.Time, len(totals))
	for id, total := range totals {
		when[id] = time.Unix(total.LastAt, 0)
	}
	return when
}

// checkLinks 는 links·superseded_by·`mem:` 근거가 가리키는 기억이 실제로
// 있는지 본다 (D01).
func checkLinks(scans []*scanned) []Problem {
	known := map[string]bool{}
	for _, item := range scans {
		known[item.Memory.ID] = true
	}
	problems := []Problem{}
	for _, item := range scans {
		for _, link := range pointedBy(item.Memory) {
			if known[link] {
				continue
			}
			problems = append(problems, fail(quality.RuleDeadMemLink, item.Path, 0,
				fmt.Sprintf("없는 기억 `%s` 을 가리킨다", link)))
		}
	}
	return problems
}

// pointedBy 는 이 기억이 가리키는 기억 id 전부다.
func pointedBy(m *model.Memory) []string {
	out := append([]string{}, m.Links...)
	if m.SupersededBy != "" {
		out = append(out, m.SupersededBy)
	}
	for _, source := range m.Sources {
		if strings.HasPrefix(source, model.SourceMem) {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(source, model.SourceMem)))
		}
	}
	return out
}

// checkPaths 는 본문과 `file:` 근거가 가리키는 저장소 안 경로가 있는지 본다
// (D02). 파일마다 os.Stat 을 치지 않고 **한 번 훑어 만든 경로 집합**에 묻는다.
func checkPaths(scans []*scanned, options *Options, report *Report) []Problem {
	if !anyPathTarget(scans) {
		return nil
	}
	root := filepath.Dir(options.Store.Dir)
	known, full := pathSet(root)
	if !full {
		report.Notes = append(report.Notes,
			fmt.Sprintf("프로젝트 폴더에 파일이 %d개를 넘어 죽은 경로(D02)는 안 봤다", pathSetLimit))
		return nil
	}
	problems := []Problem{}
	for _, item := range scans {
		problems = append(problems, deadPathsOf(item, options.Store.Dir, root, known)...)
	}
	return problems
}

// pathTargets 는 이 기억이 가리키는 저장소 안 경로다.
func pathTargets(item *scanned) []string {
	found := []string{}
	for _, match := range bodyLink.FindAllStringSubmatch(item.Memory.Body, -1) {
		if isRelativePath(match[1]) {
			found = append(found, match[1])
		}
	}
	for _, source := range item.Memory.Sources {
		if !strings.HasPrefix(source, model.SourceFile) {
			continue
		}
		target := strings.TrimSpace(strings.TrimPrefix(source, model.SourceFile))
		// `file:경로:줄번호` 꼴에서 줄 번호를 뗀다.
		if colon := strings.LastIndex(target, ":"); colon > 1 && isNumber(target[colon+1:]) {
			target = target[:colon]
		}
		if isRelativePath(target) {
			found = append(found, target)
		}
	}
	return found
}

func isNumber(text string) bool {
	if text == "" {
		return false
	}
	for _, letter := range text {
		if letter < '0' || letter > '9' {
			return false
		}
	}
	return true
}

// anyPathTarget 은 풀어 볼 만한 경로가 하나라도 있는지 미리 본다. 없으면
// 프로젝트 폴더를 훑을 이유가 없다.
func anyPathTarget(scans []*scanned) bool {
	for _, item := range scans {
		if len(pathTargets(item)) > 0 {
			return true
		}
	}
	return false
}

func deadPathsOf(item *scanned, storeDir, root string, known map[string]bool) []Problem {
	problems := []Problem{}
	for _, target := range pathTargets(item) {
		bases := []string{root, storeDir, filepath.Dir(item.Abs)}
		if anyKnown(bases, root, target, known) {
			continue
		}
		problems = append(problems, warn(quality.RuleDeadPath, item.Path, 0,
			fmt.Sprintf("근거 경로 `%s` 가 프로젝트에 없다", target)))
	}
	return problems
}

// anyKnown 은 프로젝트 뿌리·저장소·그 파일이 있는 폴더 셋 중 하나를 기준으로
// 풀었을 때 있는 경로인지 본다. 밖으로 나가는 것은 "있다" 로 친다 — 남의
// 기계 얘기라 우리가 판정할 수 없다.
func anyKnown(bases []string, root, target string, known map[string]bool) bool {
	for _, base := range bases {
		rel, err := filepath.Rel(root, filepath.Join(base, filepath.FromSlash(target)))
		if err != nil {
			return true
		}
		slashed := filepath.ToSlash(rel)
		if slashed == ".." || strings.HasPrefix(slashed, "../") {
			return true
		}
		if known[slashed] {
			return true
		}
	}
	return false
}

func isRelativePath(target string) bool {
	if target == "" || strings.HasPrefix(target, "#") || strings.Contains(target, "://") {
		return false
	}
	if strings.HasPrefix(target, "mailto:") || filepath.IsAbs(target) {
		return false
	}
	return strings.Contains(target, "/") || strings.Contains(target, ".")
}

// pathSet 은 프로젝트 폴더의 경로를 한 번에 모은다. .git 은 건너뛰고, 너무
// 크면 못 다 봤다고 말한다.
func pathSet(root string) (map[string]bool, bool) {
	known := map[string]bool{}
	full := true
	filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		known[filepath.ToSlash(relative)] = true
		if len(known) > pathSetLimit {
			full = false
			return filepath.SkipAll
		}
		return nil
	})
	return known, full
}

// checkCommits 는 `commit:` 근거가 이 저장소에 있는 커밋인지 본다 (D03).
func checkCommits(scans []*scanned, watcher *gitWatcher) []Problem {
	if watcher == nil {
		return nil
	}
	problems := []Problem{}
	for _, item := range scans {
		for _, source := range item.Memory.Sources {
			if !strings.HasPrefix(source, model.SourceCommit) {
				continue
			}
			hash := strings.TrimSpace(strings.TrimPrefix(source, model.SourceCommit))
			if hash == "" || watcher.hasCommit(hash) {
				continue
			}
			problems = append(problems, warn(quality.RuleDeadCommit, item.Path, 0,
				fmt.Sprintf("근거 커밋 `%s` 를 이 저장소에서 못 찾았다", hash)))
		}
	}
	return problems
}

// checkInboxBad 는 승격에 실패한 파일이 쌓이는 것을 알린다 (E06).
func checkInboxBad(options *Options) []Problem {
	entries, err := os.ReadDir(options.Store.InboxBadDir())
	if err != nil || len(entries) == 0 {
		return nil
	}
	return []Problem{fail(quality.RuleInboxBad, "", 0,
		fmt.Sprintf("inbox/bad 에 규격 안 맞는 파일이 %d개 쌓였다. `mem index --clear-bad` 로 본다", len(entries)))}
}

// checkShadowExe 는 프로젝트 뿌리에 놓인 mem 을 찾는다 (E04). 윈도우는 그것을
// 설치된 것보다 먼저 잡는다.
func checkShadowExe(options *Options) []Problem {
	roots := []string{filepath.Dir(options.Store.Dir), options.Store.Dir}
	problems := []Problem{}
	for _, root := range roots {
		for _, name := range []string{"mem.exe", "mem"} {
			info, err := os.Lstat(filepath.Join(root, name))
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			problems = append(problems, fail(quality.RuleShadowExe, "", 0,
				fmt.Sprintf("저장소 자리에 `%s` 가 있다. 설치된 것 대신 이게 먼저 잡힌다", name)))
		}
	}
	return problems
}

// synonymCandidates 는 0건이 자꾸 나는 낱말이다 (`--synonym`). 파일이 없으면
// 그냥 건너뛴다 — search 가 아직 안 붙은 저장소가 흔하다.
func synonymCandidates(options *Options, report *Report) []Synonym {
	path := filepath.Join(store.LocalDir(options.Store.Dir), MissFileName)
	file, err := os.Open(path)
	if err != nil {
		report.Notes = append(report.Notes, "0건 기록(miss.jsonl)이 없어 동의어 후보를 못 뽑았다")
		return nil
	}
	defer file.Close()
	counts, lines := missCounts(file)
	if lines == 0 {
		return nil
	}
	report.Notes = append(report.Notes,
		fmt.Sprintf("0건 기록 %s 의 %d줄로 동의어 후보를 뽑았다", MissFileName, lines))
	found := []Synonym{}
	for _, word := range sortedNames(counts) {
		if counts[word] < missMinCount {
			continue
		}
		found = append(found, Synonym{Word: word, Count: counts[word]})
	}
	sort.SliceStable(found, func(a, b int) bool { return found[a].Count > found[b].Count })
	return found
}

func missCounts(file *os.File) (map[string]int, int) {
	counts := map[string]int{}
	lines := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := missLine{}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		lines++
		for _, word := range wordsOf(line) {
			counts[word]++
		}
	}
	return counts, lines
}

// wordsOf 는 낱말 칸을 쓰되, 없으면 질의를 띄어쓰기로 쪼갠다.
func wordsOf(line missLine) []string {
	if len(line.Words) > 0 {
		return line.Words
	}
	return strings.Fields(line.Query)
}

func sortedNames(counts map[string]int) []string {
	out := make([]string, 0, len(counts))
	for name := range counts {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// HitTimes 는 id 마다 마지막으로 읽힌 때다. 기록이 없으면 nil 이라 받는 쪽이
// 차가움(C09·C10)을 건너뛴다. `mem review` 가 같이 쓴다.
func HitTimes(dir string) map[string]time.Time {
	totals, lines, err := store.ReadHits(dir)
	if err != nil || lines == 0 {
		return nil
	}
	when := make(map[string]time.Time, len(totals))
	for id, total := range totals {
		when[id] = time.Unix(total.LastAt, 0)
	}
	return when
}
