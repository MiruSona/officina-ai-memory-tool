package main

// mem retain — 알림을 못 받은 세션의 대화 기록에서 기억 후보를 뽑아 자동 관문에 건다.
// 흐름 ①~⑤ 는 Docs/Design/2026-10-19-A2-retain설계.md 1절 그림 그대로다.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/llm"
	"github.com/mirusona/officina-ai-memory-tool/internal/retain"
)

var retainBools = []string{"dry-run", "prune", "apply", "json"}
var retainValues = []string{"session", "max", "repo"}

// retainDefaultMax 는 한 판에 볼 세션 수 기본값이다. 세션 하나에 수 분이 걸린다 (설계 9절).
const retainDefaultMax = 3

// healthTimeout 은 서버 둘을 확인할 때 기다리는 시간이다. 안 닿는 서버에 오래 묶이지 않게 짧다.
const healthTimeout = 5 * time.Second

func init() {
	register(command{name: "retain", run: runRetain, bools: retainBools, values: retainValues})
}

// retainSummary 는 끝 줄이다 (--json 이면 이 꼴 그대로).
type retainSummary struct {
	Sessions   int    `json:"sessions"`
	Candidates int    `json:"candidates"`
	Saved      int    `json:"saved"`
	Rejected   int    `json:"rejected"`
	ParseFail  int    `json:"parse_fail"`
	Dry        bool   `json:"dry"`
	Stopped    bool   `json:"stopped"`
	Skipped    string `json:"skipped,omitempty"`
}

func runRetain(argv []string) int {
	parsed, err := parseOptions(argv, retainBools, retainValues)
	if err != nil {
		return fail(err.Error())
	}
	max, ok := retainMax(parsed)
	if !ok || len(parsed.rest) > 0 || (parsed.flags["apply"] && !parsed.flags["prune"]) {
		return fail(i18n.T(i18n.RetainUsage))
	}
	if code := checkSessionPrefix(parsed.text("session")); code != exitOK {
		return code
	}
	repository, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	if parsed.flags["prune"] {
		return retainPrune(parsed, repository, opened)
	}
	summary := retainSummary{Dry: parsed.flags["dry-run"]}
	settings := loadLLM()
	generator, found := llm.NewGenerator(settings)
	if !found {
		summary.Skipped = i18n.T(i18n.RetainNoGenerate)
		return printRetainSummary(parsed, summary)
	}
	if why := serversDown(settings); why != "" {
		summary.Skipped = why
		return printRetainSummary(parsed, summary)
	}
	items, err := retain.ReadQueue(repository.Dir)
	if err != nil {
		return exitFor(err)
	}
	picked, code := pickRetain(parsed, repository, items, max)
	if code != exitOK {
		return code
	}
	if len(picked) == 0 {
		summary.Skipped = i18n.T(i18n.RetainNothingToDo)
		return printRetainSummary(parsed, summary)
	}
	worker := newRetainWorker(repository, generator, summary.Dry, settings.NLITimeoutMS)
	for _, item := range picked {
		line, stopped := worker.session(item)
		summary.add(line)
		if err := appendRetainLog(repository.Dir, line, time.Now()); err != nil {
			fmt.Fprintln(os.Stderr, i18n.T(i18n.RetainLogFailed, err.Error()))
		}
		if stopped {
			summary.Stopped = true
			fmt.Fprintln(os.Stderr, i18n.T(i18n.RetainStopped, line.Session, line.Error))
			break
		}
	}
	return printRetainSummary(parsed, summary)
}

// retainMax 는 --max 를 읽는다. 1 이상 정수만 받는다.
func retainMax(parsed *options) (int, bool) {
	if !parsed.has("max") {
		return retainDefaultMax, true
	}
	value, err := strconv.Atoi(strings.TrimSpace(parsed.text("max")))
	return value, err == nil && value >= 1
}

func (s *retainSummary) add(line retainLogLine) {
	s.Sessions++
	s.Candidates += line.Candidates
	s.Saved += line.Saved
	s.ParseFail += line.ParseFail
	for _, count := range line.Rejected {
		s.Rejected += count
	}
}

func printRetainSummary(parsed *options, summary retainSummary) int {
	if parsed.flags["json"] {
		data, err := json.Marshal(summary)
		if err != nil {
			return fail(err.Error())
		}
		fmt.Println(string(data))
		return exitOK
	}
	if summary.Skipped != "" {
		fmt.Println(summary.Skipped)
		return exitOK
	}
	if summary.Dry {
		fmt.Println(i18n.T(i18n.RetainDryNote))
	}
	fmt.Println(i18n.T(i18n.RetainSummary, summary.Sessions, summary.Candidates, summary.Saved, summary.Rejected))
	return exitOK
}

// serversDown 은 설계 1절 ① 이다. 판정 서버(R3 필수)와 생성 서버 중 하나라도 안 닿으면 그 한 줄을 준다.
func serversDown(settings config.LLMConfig) string {
	if settings.NLIURL == "" {
		return i18n.T(i18n.RetainNoNLI)
	}
	if !answers(strings.TrimRight(settings.NLIURL, "/")+"/health", "") {
		return i18n.T(i18n.RetainServerDown, "nli")
	}
	if !answers(modelsURL(settings.GenerateURL), settings.Key) {
		return i18n.T(i18n.RetainServerDown, "generate")
	}
	return ""
}

// answers 는 GET 이 200 으로 돌아오는지다. 주소·키는 어디에도 안 찍는다.
func answers(address, key string) bool {
	request, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return false
	}
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	response, err := llm.NewClient(healthTimeout).Do(request)
	if err != nil {
		return false
	}
	response.Body.Close()
	return response.StatusCode == http.StatusOK
}

// modelsURL 은 generate_url(뿌리 · /v1 · 끝 주소 셋 다)을 /v1/models 주소로 맞춘다.
func modelsURL(address string) string {
	address = strings.TrimRight(strings.TrimSpace(address), "/")
	address = strings.TrimSuffix(address, "/chat/completions")
	address = strings.TrimSuffix(address, "/v1")
	return address + "/v1/models"
}

// pickRetain 은 설계 1절 ③ 이다. --session 이면 그 세션 하나를 covered·done_at 과 상관없이 고른다.
func pickRetain(parsed *options, repository *config.Repository, items []retain.QueueItem, max int) ([]retain.QueueItem, int) {
	given := parsed.text("session")
	if given == "" {
		return retain.Pick(items, time.Now(), repository.Config.Retain.QueueDays, max), exitOK
	}
	for _, item := range items {
		if strings.HasPrefix(item.Entry.SessionID, given) {
			return []retain.QueueItem{item}, exitOK
		}
	}
	return nil, fail(i18n.T(i18n.RetainNoSession, given))
}
