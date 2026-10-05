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
	"sort"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/llm"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

var judgeBools = []string{"json", "fresh"}
var judgeValues = []string{"evidence", "claim", "file", "repo"}

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
	return &llm.Judge{Client: client, Dir: filepath.Join(store.LocalDir(repository.Dir), judgeDirName),
		Refuse: func(text string) bool { return scanner.ScanText(text) != nil }}
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
		probsText(verdict.Probs), verdict.MS, cachedMark(verdict.Cached)))
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
	total, failed, right, graded := 0, 0, 0, 0
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
	fmt.Fprintln(os.Stderr, i18n.T(i18n.JudgeFileSummary, total, total-failed, failed, right, graded,
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
