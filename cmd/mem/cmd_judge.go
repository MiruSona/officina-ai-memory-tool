package main

// mem judge config|support — 바깥 LLM 판정(SemIf)을 사람이 직접 불러 본다 (자동쌓기설계
// 5절 K). add 관문의 R3 와 **같은 판정·같은 기록 파일**을 쓴다. K 의 통과선
// 「실제 자료로 다시 잰다」를 이 명령의 --file 로 잰다.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/llm"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

var judgeBools = []string{"json", "fresh", "apply"}
var judgeValues = []string{"evidence", "claim", "file", "repo", "older", "prompt"}

// judgeDirName 은 판정 기록 폴더다. Memory/local 아래라 git 에 안 들어간다.
const judgeDirName = "judge"

// judgeLineLimit 은 --file 한 줄의 상한이다. 근거·주장 한 쌍이라 넉넉하다.
const judgeLineLimit = 1 << 20

func init() {
	register(command{name: "judge", run: runJudge, bools: judgeBools, values: judgeValues})
}

func runJudge(argv []string) int {
	if len(argv) == 0 {
		return fail(i18n.T(i18n.JudgeUsage))
	}
	parsed, err := parseOptions(argv[1:], judgeBools, judgeValues)
	if err != nil {
		return fail(err.Error())
	}
	if len(parsed.rest) > 0 {
		return fail(i18n.T(i18n.JudgeUsage))
	}
	switch argv[0] {
	case "config":
		return judgeConfig(parsed)
	case "support":
		return judgeSupport(parsed)
	case "clean":
		return judgeClean(parsed)
	}
	return fail(i18n.T(i18n.JudgeUsage))
}

// loadLLM 은 이 기계의 llm.toml 을 읽고, 틀린 칸이 있으면 stderr 에 한 줄씩 알린다.
func loadLLM() config.LLMConfig {
	settings, problems := config.LoadLLM(config.LLMPath())
	for _, problem := range problems {
		fmt.Fprintln(os.Stderr, problem)
	}
	return settings
}

// judgeOf 는 저장소 하나의 판정기다. llm.toml 이 꺼져 있으면 nil 이다 — 부르는 쪽은
// nil 이면 판정을 건너뛴다. 비밀 꼴이 든 글은 서버로 안 보낸다.
func judgeOf(repository *config.Repository, settings config.LLMConfig) *llm.Judge {
	client := llm.New(settings)
	if client == nil {
		return nil
	}
	scanner := scannerFor(repository.Config.Secret)
	return &llm.Judge{Client: client, Dir: judgeDir(repository),
		Refuse: func(text string) bool { return scanner.ScanText(text) != nil }}
}

// judgeDir 는 판정 기록 폴더다. 판정기와 clean 이 **같은 식 하나**로만 구한다.
func judgeDir(repository *config.Repository) string {
	return filepath.Join(store.LocalDir(repository.Dir), judgeDirName)
}

// judgeCleanDays 는 clean 의 기본 나이(일)다.
const judgeCleanDays = 30

// judgeRecordName 은 판정 기록 파일 이름이다 — sha256 hex 64자 + .json (llm.hashOf).
var judgeRecordName = regexp.MustCompile(`^[0-9a-f]{64}\.json$`)

// judgeCleanJSON 은 `judge clean --json` 이다.
type judgeCleanJSON struct {
	Dir     string `json:"dir"`
	Older   int    `json:"older_days"`
	Apply   bool   `json:"apply"`
	Count   int    `json:"count"`
	Bytes   int64  `json:"bytes"`
	Removed int    `json:"removed"`
	Failed  int    `json:"failed"`
}

// judgeClean 은 오래된 판정 기록을 고르고, --apply 일 때만 지운다.
//
// 경로 감옥 : 폴더가 링크면 멈춘다 · 한 단계만 읽는다 · 이름이 해시 꼴인 일반
// 파일만 고른다 · 지우는 경로는 filepath.Join(dir, 이름) 하나뿐이다.
func judgeClean(parsed *options) int {
	days := judgeCleanDays
	if parsed.has("older") {
		value, err := strconv.Atoi(strings.TrimSpace(parsed.text("older")))
		if err != nil || value < 1 {
			return fail(i18n.T(i18n.JudgeCleanUsage))
		}
		days = value
	}
	repository, _, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	dir := judgeDir(repository)
	row := judgeCleanJSON{Dir: dir, Older: days, Apply: parsed.flags["apply"]}
	info, err := os.Lstat(dir)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		fmt.Fprintln(os.Stderr, i18n.T(i18n.JudgeCleanLinked, dir))
		return exitSecurity
	}
	targets := []string{}
	if err == nil && info.IsDir() {
		entries, _ := os.ReadDir(dir)
		cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
		for _, entry := range entries {
			if !entry.Type().IsRegular() || !judgeRecordName.MatchString(entry.Name()) {
				continue
			}
			fileInfo, err := entry.Info()
			if err != nil || !fileInfo.ModTime().Before(cutoff) {
				continue
			}
			targets = append(targets, filepath.Join(dir, entry.Name()))
			row.Bytes += fileInfo.Size()
		}
	}
	row.Count = len(targets)
	if row.Apply {
		for _, target := range targets {
			if os.Remove(target) != nil {
				row.Failed++
				continue
			}
			row.Removed++
		}
	}
	if parsed.flags["json"] {
		printJSON(row)
	} else {
		printJudgeClean(row)
	}
	if row.Failed > 0 {
		return exitCheck
	}
	return exitOK
}

func printJudgeClean(row judgeCleanJSON) {
	switch {
	case row.Count == 0:
		fmt.Println(i18n.T(i18n.JudgeCleanNothing, row.Older))
	case !row.Apply:
		fmt.Println(i18n.T(i18n.JudgeCleanPlan, row.Count, row.Older, humanBytes(row.Bytes)))
	default:
		fmt.Println(i18n.T(i18n.JudgeCleanDone, row.Removed))
		if row.Failed > 0 {
			fmt.Fprintln(os.Stderr, i18n.T(i18n.JudgeCleanFailed, row.Failed))
		}
	}
}

// judgeConfigJSON 은 `judge config --json` 이다. 키는 있는지만 알린다.
type judgeConfigJSON struct {
	Path            string `json:"path"`
	Enabled         bool   `json:"enabled"`
	URL             string `json:"url,omitempty"`
	Key             bool   `json:"key"`
	JudgeProfile    string `json:"judge_profile,omitempty"`
	GenerateProfile string `json:"generate_profile,omitempty"`
	TimeoutMS       int    `json:"timeout_ms"`
}

// judgeConfig 는 llm.toml 자리와 읽은 값을 보인다. 서버에는 아무것도 안 보낸다.
func judgeConfig(parsed *options) int {
	settings := loadLLM()
	row := judgeConfigJSON{Path: settings.Path, Enabled: settings.Enabled(), URL: settings.URL,
		Key: settings.Key != "", JudgeProfile: settings.JudgeProfile,
		GenerateProfile: settings.GenerateProfile, TimeoutMS: settings.TimeoutMS}
	if parsed.flags["json"] {
		return printJSON(row)
	}
	if !row.Enabled {
		fmt.Println(i18n.T(i18n.JudgeOff, row.Path))
		return exitOK
	}
	fmt.Println(i18n.T(i18n.JudgeConfigLine, row.Path, row.URL, orDash(row.JudgeProfile),
		orDash(row.GenerateProfile), row.TimeoutMS, yesNo(row.Key)))
	return exitOK
}

// judgeSupport 는 (근거, 주장) 한 쌍 또는 --file 의 여러 쌍을 판정한다.
func judgeSupport(parsed *options) int {
	single := parsed.has("evidence") || parsed.has("claim")
	if single == parsed.has("file") {
		return fail(i18n.T(i18n.JudgeSupportUsage))
	}
	if single && (strings.TrimSpace(parsed.text("evidence")) == "" || strings.TrimSpace(parsed.text("claim")) == "") {
		return fail(i18n.T(i18n.JudgeSupportUsage))
	}
	repository, _, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	judge := judgeOf(repository, loadLLM())
	if judge == nil {
		fmt.Println(i18n.T(i18n.JudgeOff, config.LLMPath()))
		return exitOK
	}
	judge.Fresh = parsed.flags["fresh"]
	// --prompt 는 측정용이다 — 물음 글 판(v2 · v3 · v3-strict)을 골라 나란히 잰다. 안 주면 기본 판.
	if parsed.has("prompt") {
		prompt, ok := llm.PromptNamed(strings.TrimSpace(parsed.text("prompt")))
		if !ok {
			return fail(i18n.T(i18n.JudgePromptUsage))
		}
		judge.Prompt = &prompt
	}
	if single {
		return judgeOne(parsed, judge)
	}
	return judgeFile(parsed.text("file"), judge)
}

// judgeOne 은 한 쌍이다. 판정을 못 받으면 까닭을 알리고 종료 2 다.
func judgeOne(parsed *options, judge *llm.Judge) int {
	verdict, err := judge.Support(parsed.text("evidence"), parsed.text("claim"))
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.T(i18n.JudgeFailed, err.Error()))
		return exitCheck
	}
	if parsed.flags["json"] {
		return printJSON(verdict)
	}
	fmt.Println(i18n.T(i18n.JudgeVerdictLine, verdict.Letter, labelOf(verdict.Letter), verdict.Prob,
		probsText(verdict.Probs), verdict.MS, unsureMark(verdict.Unsure)+cachedMark(verdict.Cached)))
	return exitOK
}

// judgePair 는 --file 한 줄이다. want 는 글자(A·B·C)나 이름(support·contradict·unrelated).
type judgePair struct {
	ID       string `json:"id"`
	Evidence string `json:"evidence"`
	Claim    string `json:"claim"`
	Want     string `json:"want"`
}

// judgeRow 는 --file 결과 한 줄이다.
type judgeRow struct {
	ID      string `json:"id"`
	Want    string `json:"want,omitempty"`
	Right   *bool  `json:"right,omitempty"`
	Problem string `json:"problem,omitempty"`
	llm.Verdict
}

// judgeFile 은 jsonl 을 한 줄씩 **순차로** 판정한다 (로컬 서버는 한 번에 한 판).
// 줄마다 JSON 한 줄을 stdout 에, 요약을 stderr 에 낸다.
func judgeFile(path string, judge *llm.Judge) int {
	file, err := os.Open(path)
	if err != nil {
		return fail(i18n.T(i18n.JudgeFileUnreadable, path))
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), judgeLineLimit)
	total, failed, right, graded, unsure := 0, 0, 0, 0, 0
	asked := []int64{}
	for number := 1; scanner.Scan(); number++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var pair judgePair
		if json.Unmarshal([]byte(line), &pair) != nil || pair.Evidence == "" || pair.Claim == "" {
			return fail(i18n.T(i18n.JudgeBadLine, number))
		}
		total++
		row := judgeRow{ID: pair.ID, Want: letterOf(pair.Want)}
		verdict, err := judge.Support(pair.Evidence, pair.Claim)
		if err != nil {
			failed++
			row.Problem = err.Error()
		} else {
			row.Verdict = verdict
			if !verdict.Cached {
				asked = append(asked, verdict.MS)
			}
			if verdict.Unsure {
				unsure++
			}
			if row.Want != "" {
				graded++
				hit := row.Want == verdict.Letter
				row.Right = &hit
				if hit {
					right++
				}
			}
		}
		printJSON(row)
	}
	if scanner.Err() != nil {
		return fail(i18n.T(i18n.JudgeFileUnreadable, path))
	}
	fmt.Fprintln(os.Stderr, i18n.T(i18n.JudgeFileSummary, total, total-failed, failed, right, graded, unsure,
		percentile(asked, 50), percentile(asked, 95), len(asked)))
	if failed > 0 {
		return exitCheck
	}
	return exitOK
}

// letterOf 는 정답 칸을 글자로 맞춘다. 모르는 값은 빈 글(채점 안 함)이다.
func letterOf(want string) string {
	switch strings.ToLower(strings.TrimSpace(want)) {
	case "a", "support", "지지":
		return llm.LetterSupport
	case "b", "contradict", "반대":
		return llm.LetterContradict
	case "c", "unrelated", "무관":
		return llm.LetterUnrelated
	}
	return ""
}

func labelOf(letter string) string {
	switch letter {
	case llm.LetterSupport:
		return i18n.T(i18n.JudgeSupport)
	case llm.LetterContradict:
		return i18n.T(i18n.JudgeContradict)
	}
	return i18n.T(i18n.JudgeUnrelated)
}

func probsText(probs map[string]float64) string {
	letters := make([]string, 0, len(probs))
	for letter := range probs {
		letters = append(letters, letter)
	}
	sort.Strings(letters)
	parts := []string{}
	for _, letter := range letters {
		parts = append(parts, fmt.Sprintf("%s=%.3f", letter, probs[letter]))
	}
	return strings.Join(parts, " ")
}

// unsureMark 는 「모른다」 꼬리표다. 글자는 그대로 찍고 경고만 붙인다.
func unsureMark(unsure bool) string {
	if unsure {
		return i18n.T(i18n.JudgeUnsure)
	}
	return ""
}

func cachedMark(cached bool) string {
	if cached {
		return i18n.T(i18n.JudgeCached)
	}
	return ""
}

// percentile 은 가장 가까운 순위 꼴이다. 값이 없으면 0 이다.
func percentile(values []int64, rank int) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64{}, values...)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a] < sorted[b] })
	at := (rank*len(sorted)+99)/100 - 1
	if at < 0 {
		at = 0
	}
	return sorted[at]
}

func orDash(text string) string {
	if text == "" {
		return "-"
	}
	return text
}

func yesNo(flag bool) string {
	if flag {
		return i18n.T(i18n.JudgeYes)
	}
	return i18n.T(i18n.JudgeNo)
}
