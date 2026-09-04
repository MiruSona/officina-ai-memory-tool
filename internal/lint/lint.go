// Package lint 은 저장소를 통째로 훑어 품질 규칙 카탈로그 42개를 검사한다.
//
// **규칙 자체는 여기 없다.** 검사 함수는 `internal/quality` 에 하나만 있고
// `add` 관문과 lint 가 같이 쓴다 — 같은 규칙이 두 곳에 따로 적히면 반드시
// 어긋난다 (설계 3절). lint 가 혼자 맡는 것은 「파일 바이트」와 「저장소 밖을
// 봐야 아는 것」 뿐이다 : 인코딩·BOM·줄끝 · 죽은 링크·경로·커밋 ·
// `inbox/bad` · 그림자 exe · git 으로 보는 낡음 · 조회 기록으로 보는 차가움.
//
// 자료가 아무리 닮아도 O(n²) 로 안 간다 — 중복 후보는 simhash 로 좁히고
// 모순 후보는 (scope, 태그) 묶음 안에서만 본다 (설계 3-3).
package lint

import (
	"sort"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
	"github.com/mirusona/officina-ai-memory-tool/internal/secret"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// Level 은 얼마나 나쁜지다. 오류만 종료 코드를 바꾼다.
type Level string

const (
	LevelError Level = "error"
	LevelWarn  Level = "warn"
	// LevelCandidate 는 사람이 판정할 자리만 좁힌 것이다. `mem review` 가
	// 이것만 모아 보여준다 (설계 3-4).
	LevelCandidate Level = "candidate"
)

// levelOf 는 규칙 등급을 lint 등급으로 옮긴다. 꺼진 규칙은 빈 값이라 안 찍는다.
func levelOf(grade quality.Grade) Level {
	switch grade {
	case quality.GradeReject:
		return LevelError
	case quality.GradeWarn:
		return LevelWarn
	case quality.GradeCandidate:
		return LevelCandidate
	}
	return ""
}

// Rules 는 보고 표에 나오는 차례다 — 규칙 카탈로그 그대로다.
// 건수가 0인 규칙은 표에서 빠진다.
var Rules = quality.RuleNames()

// Problem 은 걸린 것 하나다. 저장소 전체 규칙은 Path 가 빈다.
type Problem struct {
	Level Level  `json:"level"`
	Rule  string `json:"rule"`
	Path  string `json:"path,omitempty"`
	Line  int    `json:"line,omitempty"`
	ID    string `json:"id,omitempty"`
	Text  string `json:"text"`
	// Related 는 이 판정의 근거가 된 다른 기억이다 (중복·모순).
	Related []string `json:"related,omitempty"`
	// Score 는 닮음 점수 S 다. 중복 규칙에만 있다.
	Score float64 `json:"score,omitempty"`
	// Next 는 다음에 칠 명령이다.
	Next []string `json:"next,omitempty"`
}

// Options 는 lint 한 번이다.
type Options struct {
	Store  *store.Store
	Config config.Config
	Vocab  config.Vocab
	Fix    bool
	Now    time.Time
	// Rule 이 비지 않으면 그 규칙 하나만 본다 (`--rule`).
	Rule string
	// Synonym 이 참이면 miss.jsonl 로 동의어 후보를 뽑는다 (`--synonym`).
	Synonym bool
	// Demoted 는 `mem eval --quality` 가 정밀도를 재서 내린 등급이다
	// (`mem.toml [quality.demoted]` · 설계 결정 14).
	Demoted map[string]quality.Grade
	// NoGit 이 참이면 근거 파일 낡음(C06)·죽은 커밋(D03)을 안 본다.
	NoGit bool
	// Near 는 중복 후보의 셋째 신호(임베딩)다. nil 이면 안 쓴다 (결정 15).
	Near quality.Vectors
}

// Fix 는 --fix 가 실제로 고친 것 하나다. 되돌릴 수 있는 것만 여기 온다.
type Fix struct {
	Path string `json:"path"`
	What string `json:"what"`
	// Note 가 참이면 What 이 「규격에 없는 칸 이름」 이 아니라 그대로 읽는
	// 이유 한 마디다.
	Note bool `json:"note,omitempty"`
}

// Synonym 은 0건이 자꾸 나는 낱말 하나다 (`--synonym`).
type Synonym struct {
	Word  string `json:"word"`
	Count int    `json:"count"`
}

// Report 는 센 결과다. 한글 보고는 cmd 가 찍는다.
type Report struct {
	Problems []Problem `json:"problems"`
	Fixes    []Fix     `json:"fixes"`
	Held     []Fix     `json:"held"`
	Checked  int       `json:"checked"`
	// Synonyms 는 --synonym 을 켰을 때의 동의어 후보다.
	Synonyms []Synonym `json:"synonyms,omitempty"`
	// FixLocked 는 --fix 가 락을 못 잡아 검사만 했다는 뜻이다.
	FixLocked   bool          `json:"fix_locked"`
	FixReadOnly bool          `json:"fix_read_only"`
	Notes       []string      `json:"notes"`
	Elapsed     time.Duration `json:"-"`
	// ReadElapsed 는 그중 파일을 읽는 데 쓴 시간이다. 규칙이 느린 것과 디스크가
	// 느린 것을 갈라 봐야 어디를 고칠지 안다 — 바이러스 검사가 도는 기계에서는
	// 임시 폴더 읽기가 파일당 밀리초 단위로 튄다.
	ReadElapsed time.Duration `json:"-"`
}

// Errors 는 종료 코드를 2로 만드는 건수다.
func (r *Report) Errors() int { return r.count(LevelError) }

// Warnings 는 경고 건수다.
func (r *Report) Warnings() int { return r.count(LevelWarn) }

// Candidates 는 사람이 볼 후보 건수다. 종료 코드를 안 바꾼다.
func (r *Report) Candidates() int { return r.count(LevelCandidate) }

func (r *Report) count(level Level) int {
	total := 0
	for _, problem := range r.Problems {
		if problem.Level == level {
			total++
		}
	}
	return total
}

// note 는 「못 본 것」 한 줄을 남긴다. 안 본 것을 안 봤다고 말해야 한다.
func (r *Report) note(text string) { r.Notes = append(r.Notes, text) }

// CountByRule 은 규칙 이름마다 몇 건인지다.
func (r *Report) CountByRule() map[string]int {
	counts := map[string]int{}
	for _, problem := range r.Problems {
		counts[problem.Rule]++
	}
	return counts
}

// LevelOfRule 은 표에 적을 등급이다. 같은 규칙이 오류와 경고를 다 내면
// 나쁜 쪽을 적는다.
func (r *Report) LevelOfRule(rule string) Level {
	found := Level("")
	for _, problem := range r.Problems {
		if problem.Rule != rule {
			continue
		}
		if problem.Level == LevelError {
			return LevelError
		}
		if found == "" || problem.Level == LevelWarn {
			found = problem.Level
		}
	}
	return found
}

// Run 은 파일마다 한 번 훑고, 저장소 전체 규칙을 보고, 그다음에야 고친다.
func Run(options Options) (*Report, error) {
	started := time.Now()
	if options.Now.IsZero() {
		options.Now = started
	}
	report := Report{Problems: []Problem{}, Fixes: []Fix{}, Held: []Fix{}, Notes: []string{}}
	scans, err := scanAll(&options, &report)
	if err != nil {
		return nil, err
	}
	report.Problems = append(report.Problems, checkRepository(scans, &options, &report)...)
	if options.Synonym {
		report.Synonyms = synonymCandidates(&options, &report)
	}
	if err := runFixes(scans, &options, &report); err != nil {
		return nil, err
	}
	report.Problems = keepRule(report.Problems, options.Rule)
	sortProblems(report.Problems)
	report.Elapsed = time.Since(started)
	return &report, nil
}

// gateOptions 는 quality 관문에 넘길 조건이다. 저장소를 안 붙인다 — 중복은
// 저장소 전체를 한 번에 보는 CheckRepo 가 맡는다 (파일마다 견주면 O(n²)다).
//
// 비밀정보 패턴과 태그 표준은 여기서 한 번만 만든다. 파일마다 다시 컴파일하면
// 300건에 1.7초를 쓰던 자리로 되돌아간다.
func gateOptions(options *Options) quality.Options {
	vocab := options.Vocab
	if len(vocab.Tags) == 0 {
		vocab = config.DefaultVocab()
	}
	return quality.Options{Config: options.Config, Vocab: vocab,
		Now: options.Now, Demoted: options.Demoted, Near: options.Near,
		Secret:     secret.New(patternsOf(options.Config.Secret.Patterns, config.DefaultSecretPatterns())),
		SecretWarn: secret.New(patternsOf(options.Config.Secret.WarnPatterns, config.DefaultWarnPatterns())),
	}
}

func patternsOf(given, fallback []string) []string {
	if len(given) > 0 {
		return given
	}
	return fallback
}

// fromFinding 은 quality 가 낸 판정을 lint 문제 한 줄로 옮긴다. 꺼진 규칙은
// 빈 문제라 부르는 쪽이 버린다.
func fromFinding(found quality.Finding, path string) Problem {
	return Problem{Level: levelOf(found.Level), Rule: found.Rule, Path: path,
		Line: found.Line, ID: found.ID, Text: found.Reason,
		Related: found.Related, Score: found.Score, Next: found.Next}
}

func appendFindings(list []Problem, found []quality.Finding, path string) []Problem {
	for _, one := range found {
		problem := fromFinding(one, path)
		if problem.Level == "" {
			continue
		}
		list = append(list, problem)
	}
	return list
}

// scanAll 은 파일을 읽고 파일 하나만 보면 아는 규칙을 본다. 파일끼리는 서로를
// 안 보므로 자리 번호로 갈라 여럿이 같이 읽는다 — 결과는 자리 번호대로 모아
// 붙이니 한 갈래로 돈 것과 표가 같다. (읽기 시간은 일꾼들의 합이라 벽시계보다 크다)
func scanAll(options *Options, report *Report) ([]*scanned, error) {
	files, err := options.Store.ListMemories()
	if err != nil {
		return nil, err
	}
	gate := gateOptions(options)
	items := make([]*scanned, len(files))
	problems := make([][]Problem, len(files))
	reads := make([]time.Duration, len(files))
	checks := make([]time.Duration, len(files))
	scanStarted := time.Now()
	quality.InParallel(len(files), func(from, to int) {
		for at := from; at < to; at++ {
			started := time.Now()
			items[at] = read(files[at])
			middle := time.Now()
			reads[at] = middle.Sub(started)
			problems[at] = checkFile(items[at], gate)
			checks[at] = time.Since(middle)
		}
	})
	report.ReadElapsed = readShareOf(time.Since(scanStarted), reads, checks)
	scans := make([]*scanned, 0, len(files))
	for at, item := range items {
		report.Checked++
		report.Problems = append(report.Problems, problems[at]...)
		if item.Memory != nil {
			scans = append(scans, item)
		}
	}
	return scans, nil
}

// readShareOf 는 훑기에 쓴 벽시계 중 「파일 읽기」 몫이다. 여럿이 같이 읽으므로
// 일꾼 시간의 합을 그대로 적으면 벽시계보다 커진다 — 합의 비율로 벽시계를 가른다.
func readShareOf(wall time.Duration, reads, checks []time.Duration) time.Duration {
	readSum, checkSum := time.Duration(0), time.Duration(0)
	for at := range reads {
		readSum += reads[at]
		checkSum += checks[at]
	}
	if readSum+checkSum == 0 {
		return 0
	}
	return time.Duration(float64(wall) * float64(readSum) / float64(readSum+checkSum))
}

// keepRule 은 --rule 로 고른 규칙만 남긴다.
func keepRule(problems []Problem, rule string) []Problem {
	if rule == "" {
		return problems
	}
	kept := []Problem{}
	for _, problem := range problems {
		if problem.Rule == rule {
			kept = append(kept, problem)
		}
	}
	return kept
}

// runFixes 는 하나뿐인 쓰기 락 아래에서만 고친다. 못 잡으면 검사만 한 것이다
// (불변조건 2).
func runFixes(scans []*scanned, options *Options, report *Report) error {
	if !options.Fix {
		return nil
	}
	if options.Store.ReadOnly {
		report.FixReadOnly = true
		return nil
	}
	return applyFixes(scans, options, report)
}

func fail(rule, path string, line int, text string) Problem {
	return Problem{Level: LevelError, Rule: rule, Path: path, Line: line, Text: text}
}

func warn(rule, path string, line int, text string) Problem {
	return Problem{Level: LevelWarn, Rule: rule, Path: path, Line: line, Text: text}
}

// levelRank 는 표에 놓는 차례다 — 오류 · 경고 · 후보.
func levelRank(level Level) int {
	switch level {
	case LevelError:
		return 0
	case LevelWarn:
		return 1
	}
	return 2
}

// sortProblems 는 오류를 위에, 그다음 파일 차례로 놓는다.
func sortProblems(problems []Problem) {
	sort.SliceStable(problems, func(a, b int) bool {
		if problems[a].Level != problems[b].Level {
			return levelRank(problems[a].Level) < levelRank(problems[b].Level)
		}
		if problems[a].Path != problems[b].Path {
			return problems[a].Path < problems[b].Path
		}
		return problems[a].Line < problems[b].Line
	})
}

// LevelText 는 표에 적는 한글 등급이다.
func LevelText(level Level) string {
	switch level {
	case LevelError:
		return "오류"
	case LevelWarn:
		return "경고"
	case LevelCandidate:
		return "후보"
	}
	return string(level)
}

// memoriesOf 는 훑은 파일에서 기억만 뽑는다.
func memoriesOf(scans []*scanned) []*model.Memory {
	out := make([]*model.Memory, 0, len(scans))
	for _, item := range scans {
		out = append(out, item.Memory)
	}
	return out
}

// pathOf 는 기억 id 로 파일 경로를 찾는다. 저장소 전체 규칙이 걸린 기억을
// 파일로 되짚어 주는 자리다.
func pathOf(scans []*scanned) map[string]string {
	table := make(map[string]string, len(scans))
	for _, item := range scans {
		table[item.Memory.ID] = item.Path
	}
	return table
}

// KnownRule 은 --rule 값이 규칙표 안인지다. cmd 가 모르는 이름을 거절하는 데
// 쓴다 — 오타 하나로 「걸린 것 없음」 이 나오면 사람이 통과한 줄 안다.
func KnownRule(name string) bool {
	_, ok := quality.Lookup(name)
	return ok
}
