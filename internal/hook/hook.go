package hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// EventName 은 Claude Code 와 Gemini CLI 가 같이 쓰는 이름이다 (설계 7-6).
const EventName = "SessionStart"

// SubagentEventName 은 서브에이전트가 뜰 때 Claude Code 가 주는 이름이다.
// Gemini CLI 에는 이 이벤트가 없다 (서브에이전트훅설계 6-5).
const SubagentEventName = "SubagentStart"

// Event 는 훅 이벤트 이름을 우리가 아는 갈래로 접은 것이다. 모르는 이름은
// EventUnknown 이고, 그때 훅은 아무 말 없이 0 이다 (서브에이전트훅설계 3-1).
type Event int

const (
	EventUnknown Event = iota
	EventSession
	EventSubagentStart
)

// debugEnv 는 stderr 알림을 켠다. 켜든 말든 stdout 은 깨끗해야 한다.
const debugEnv = "MEM_DEBUG"

// maxInput 은 stdin 에서 읽을 상한이다. 훅 JSON 은 원래 작다.
const maxInput = 1 << 20

// catchUpBudget 은 훅 안에서 승격+증분 색인에 줄 수 있는 시간이다. 훅 한 번은
// 벽시계 1초 안에 끝나야 하고, 블록을 만들고 쓰는 몫을 남겨야 한다
// (불변조건 4 · 리뷰 A #5).
const catchUpBudget = 600 * time.Millisecond

// projectDirEnv 는 Gemini CLI 도 주는 별칭이다 (설계 7-6).
const projectDirEnv = "CLAUDE_PROJECT_DIR"

// Input 은 훅이 stdin 으로 받는 JSON 이다. 이름이 바뀐 칸은 둘 다 읽는다 (설계 7-2).
type Input struct {
	HookEventName    string `json:"hook_event_name"`
	Source           string `json:"source"`
	SessionStartKind string `json:"session_start_kind"`
	Cwd              string `json:"cwd"`
	SessionID        string `json:"session_id"`
	// AgentType 은 SubagentStart 가 주는 에이전트 종류다 (`general-purpose` ·
	// `Explore` …). mem.toml 의 subagent_skip 이 이 값을 본다.
	AgentType string `json:"agent_type"`
	AgentID   string `json:"agent_id"`
	// sawJSON 은 stdin 으로 훅 JSON 이 실제로 들어왔다는 표다. 들어왔는데
	// `cwd` 가 비면 **어느 저장소인지 모르는 것**이고, 그때 지금 폴더로
	// 떨어지면 전혀 다른 저장소를 열어 색인까지 다시 만든다 (스트레스 V14).
	sawJSON bool
}

// payload 는 Claude 가 읽는 안쪽 객체다.
type payload struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

// output 은 통째로 만들어 한 번에 쓰는 바깥 객체다.
type output struct {
	HookSpecificOutput payload `json:"hookSpecificOutput"`
}

// Run 은 훅 한 번이다. 무슨 일이 있어도 0 을 돌려주고, 못 하겠으면 아무것도
// 안 쓴다 (불변조건 3 · 설계 5-1).
func Run(argv []string, stdin io.Reader, stdout io.Writer) (code int) {
	defer func() {
		// 터진 것은 삼키고 0 이다. 훅은 무슨 일이 있어도 세션을 안 막는다.
		if trouble := recover(); trouble != nil {
			note(trouble)
			code = 0
		}
	}()
	defer config.Silence()()
	started := time.Now()
	flags := parseArgs(argv)
	if flags.help {
		// 도움말은 stderr 로 간다. 훅 stdout 은 JSON 아니면 빈 것이어야 하고,
		// 깨진 JSON 은 그 글자가 그대로 컨텍스트에 들어간다 (설계 7-3 · 리뷰 A #6).
		if text, found := i18n.CommandHelp("hook"); found {
			fmt.Fprintln(os.Stderr, text)
		}
		return 0
	}
	input := inputOf(stdin)
	event := flags.event
	if input.HookEventName != "" {
		event = input.HookEventName
	}
	kind := eventKind(event)
	if kind == EventUnknown {
		// 등록된 훅 경로(`hook session-start` · stdin 의 hook_event_name)는
		// 여기 안 온다. 사람이 `mem hook startup` 처럼 잘못 친 것만 온다 —
		// 말없이 빈 출력이면 「훅이 죽었나」로 오해한다 (보안시험 낮음 표).
		if flags.event != "" && input.HookEventName == "" {
			fmt.Fprintln(os.Stderr, i18n.T(i18n.HookBadEvent, flags.event))
			return 1
		}
		return 0
	}
	text, stats := build(input, flags.budget, kind)
	stats.MS = time.Since(started).Milliseconds()
	if flags.json {
		// 재는 모드다. 훅 JSON 이 아니라 잰 값을 찍는다 (설계 G3).
		writeStats(stdout, stats)
		return 0
	}
	if text == "" {
		return 0
	}
	if flags.dryRun {
		fmt.Fprint(stdout, text)
		return 0
	}
	write(stdout, text, eventNameOf(kind))
	return 0
}

// eventNameOf 는 출력 JSON 에 적을 이벤트 이름이다. Claude 가 이 이름으로
// 어느 훅이 낸 값인지 가른다.
func eventNameOf(kind Event) string {
	if kind == EventSubagentStart {
		return SubagentEventName
	}
	return EventName
}

// flagSet 은 훅이 받는 깃발이다. 사용법이 틀려도 세션은 그냥 산다.
type flagSet struct {
	dryRun bool
	json   bool
	help   bool
	event  string
	// budget 은 이 한 번만 쓰는 토큰 예산이다. 0 이면 계기별 기본값이다
	// (설계 6-1). 사람이 상한을 재 볼 때 쓴다.
	budget int
}

// parseArgs 는 인자를 읽되 절대 실패하지 않는다.
func parseArgs(argv []string) flagSet {
	flags := flagSet{}
	wantBudget := false
	wantSkip := false
	for _, word := range argv {
		switch {
		case wantSkip:
			wantSkip = false
		case wantBudget:
			wantBudget = false
			flags.budget = number(word)
		case word == "--dry-run":
			flags.dryRun = true
		case word == "--json":
			flags.json = true
		// `mem hook help` 도 도움말이다. 이벤트 이름으로 읽으면 「모르는
		// 이벤트」가 되고, 훅은 그 자리에서 0 이어야 한다 (리뷰 A #6).
		case word == "--help" || word == "-h" || word == "help":
			flags.help = true
		case word == "--budget":
			wantBudget = true
		case strings.HasPrefix(word, "--budget="):
			flags.budget = number(strings.TrimPrefix(word, "--budget="))
		// 훅은 `--repo` 를 안 쓴다. 값까지 삼켜야 그 값이 이벤트 이름으로
		// 읽히지 않는다 (보안연동 시험 L-5).
		case word == "--repo":
			wantSkip = true
			fmt.Fprintln(os.Stderr, i18n.T(i18n.HookRepoIgnored))
		case strings.HasPrefix(word, "--repo="):
			fmt.Fprintln(os.Stderr, i18n.T(i18n.HookRepoIgnored))
		// 모르는 옵션은 조용히 먹지 않는다. 훅은 종료 0 규약이라 막지는 않고
		// stderr 로만 알린다 — stdout 은 훅 JSON 말고 아무것도 못 싣는다.
		case strings.HasPrefix(word, "-"):
			fmt.Fprintln(os.Stderr, i18n.T(i18n.HookUnknownOption, word))
		case flags.event == "":
			flags.event = word
		}
	}
	return flags
}

// number 는 못 읽는 값을 0 으로 본다. 훅은 인자가 틀려도 안 죽는다.
func number(text string) int {
	value, err := strconv.Atoi(text)
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

// writeStats 는 잰 값을 한 줄 JSON 으로 찍는다. 이 모드는 사람과 시험이
// 부르는 것이라 훅 stdout 규칙(빈 것 아니면 훅 JSON)과 상관없다.
func writeStats(stdout io.Writer, stats Stats) {
	raw, err := json.Marshal(stats)
	if err != nil {
		note(err)
		return
	}
	fmt.Fprintln(stdout, string(raw))
}

// eventKind 는 이름 표 하나다. `-`·`_` 를 지우고 소문자로 견주니 Gemini 꼴
// (`session-start`)도 같이 걸린다 (설계 7-6).
//
// SubagentStop 은 **일부러 표에 없다** — 누가 settings.json 에 걸어 놔도 조용히
// 0 이다 (서브에이전트훅설계 5-1).
func eventKind(event string) Event {
	switch strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(event)) {
	case "sessionstart":
		return EventSession
	case "subagentstart":
		return EventSubagentStart
	}
	return EventUnknown
}

// inputOf 는 훅 JSON 을 읽는다. 못 읽으면 빈 입력이고, 그것은 폴더를 모르는
// 평범한 startup 으로 읽힌다 — 제일 안전한 쪽이다.
func inputOf(stdin io.Reader) Input {
	if stdin == nil || isTerminal(stdin) {
		return Input{}
	}
	raw, err := io.ReadAll(io.LimitReader(stdin, maxInput))
	if err != nil {
		return Input{}
	}
	input := Input{}
	if err := json.Unmarshal(bytes.TrimSpace(raw), &input); err != nil {
		note(err)
		return Input{}
	}
	input.sawJSON = true
	return input
}

// isTerminal 은 표준입력이 사람이 앉은 콘솔인지다. 콘솔이면 EOF 가 안 와서
// 읽기가 멎는다 — 사람이 손으로 `mem hook session-start` 를 치면 도구가
// 매달린 것처럼 보인다. 훅은 늘 파이프로 들어온다.
func isTerminal(stdin io.Reader) bool {
	file, ok := stdin.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// kindOf 는 계기를 읽는다. 두 이름 다 본다 (설계 7-2).
func kindOf(input Input) string {
	if KnownKind(input.Source) {
		return input.Source
	}
	if KnownKind(input.SessionStartKind) {
		return input.SessionStartKind
	}
	return KindStartup
}

// build 는 저장소를 찾아 블록을 만든다. 넣을 것이 없으면 빈 글월이다.
// 두 이벤트가 같은 길을 타고, 갈리는 것은 예산·상한과 맨 위 세 줄뿐이다.
func build(input Input, budget int, kind Event) (string, Stats) {
	dir, ok := startDir(input)
	if !ok {
		// 어느 폴더에서 세션이 열렸는지 모른다. 아무 저장소도 열지 않는다 —
		// 여기서 지금 폴더로 떨어지면 남의 저장소를 열고 색인까지 고친다
		// (스트레스 V14). 훅 규약대로 빈 출력 · 종료 0 이다.
		note(errors.New(i18n.T(i18n.HookNoCwd)))
		return "", Stats{}
	}
	project, err := config.Resolve("", dir)
	if err != nil {
		note(err)
		return "", Stats{}
	}
	if kind == EventSubagentStart && !subagentWanted(project, input) {
		return "", Stats{}
	}
	// 따라잡기 색인도 `mem index` 와 **같은 정규화기**를 타야 한다. 안 꽂으면
	// 훅이 만든 fts_norm 행만 대표말 치환이 빠져 같은 낱말이 다른 글자가 된다
	// (2A 넘김). 표를 읽는 값은 이미 메모리에 있어 예산에 안 걸린다.
	index.WireNormalize(project)
	options := Options{Kind: kindOf(input)}
	if kind == EventSubagentStart {
		options.Kind = KindSubagent
	}
	closers := []*index.DB{}
	defer func() {
		for _, database := range closers {
			database.Close()
		}
	}()
	options.Project, closers = openOne(project, closers)
	if options.Project == nil {
		return "", Stats{}
	}
	fill(&options, project, dir)
	if kind == EventSubagentStart {
		options.Lead = subagentLead(project)
	}
	// 손으로 준 예산이 계기별 기본값을 이긴다 (설계 6-1 `--budget`).
	if budget > 0 {
		options.BudgetOverride = budget
	}
	return Build(options)
}

// subagentWanted 는 이 서브에이전트에 블록을 넣을지다. 저장소가 꺼 뒀거나
// agent_type 이 건너뛸 목록에 있으면 아무것도 안 넣는다 (서브에이전트훅설계 3-4).
func subagentWanted(project *config.Repository, input Input) bool {
	if project == nil || !project.Config.Hook.SubagentStart {
		return false
	}
	for _, name := range project.Config.Hook.SubagentSkip {
		if strings.EqualFold(strings.TrimSpace(name), input.AgentType) {
			return false
		}
	}
	return true
}

// subagentLead 는 guard 바깥 맨 위에 붙는 줄들이다. 우리가 만든 상수라 살균이
// 필요 없다. scope 표가 비면 그 줄은 통째로 뺀다 (서브에이전트훅설계 3-3).
func subagentLead(project *config.Repository) string {
	lead := i18n.T(i18n.HookSubagentStart) + "\n" + i18n.T(i18n.HookSubagentEnd) + "\n"
	scopes := knownScopes(project)
	if scopes == "" {
		return lead
	}
	return lead + i18n.T(i18n.HookSubagentScopes, scopes) + "\n"
}

// knownScopes 는 이 저장소의 표준 scope 를 한 줄로 잇는다. 훅은 어느 scope 에
// 남길지 고르지 않는다 — 목록까지만 보여 준다 (서브에이전트훅설계 3-3).
//
// 이 줄은 가드 위 신뢰 구역에 실린다. vocab 로딩이 이미 규격 밖 키를 버리지만,
// 여기서도 한 번 더 씻는다 — 남이 고칠 수 있는 파일에서 온 글이라 살균이 한
// 겹뿐이면 안 된다 (불변조건 I3).
func knownScopes(project *config.Repository) string {
	if project == nil {
		return ""
	}
	vocab, err := config.LoadVocab(filepath.Join(project.Dir, config.VocabFileName))
	if err != nil {
		return ""
	}
	safeScopes := []string{}
	for _, scope := range vocab.StandardScopes() {
		if model.IsScope(scope) {
			safeScopes = append(safeScopes, scope)
		}
	}
	return safeLine(strings.Join(safeScopes, " · "))
}

// openOne 은 색인이 이미 있을 때만 연다. 없거나 깨졌으면 그 저장소는 없는
// 셈 친다 — 훅은 전수 스캔으로 안 떨어진다 (설계 7-7).
func openOne(repository *config.Repository, closers []*index.DB) (*index.DB, []*index.DB) {
	if repository == nil || !index.Usable(repository.Dir) {
		return nil, closers
	}
	database, err := index.Open(repository.Dir)
	if err != nil {
		note(err)
		return nil, closers
	}
	return database, append(closers, database)
}

// fill 은 프로젝트 저장소 쪽 값을 채운다 — 색인 따라잡기 · scope · 예산 · 알림.
func fill(options *Options, project *config.Repository, cwd string) {
	if project != nil {
		options.Budget = project.Config.Budget
		options.Hook = project.Config.Hook
		options.Secret = project.Config.Secret
		options.Scope = ScopeFor(project.Config.Scope, cwd)
		options.Types = config.TypesIn(project.Dir)
		options.HasRules = hasRules(project.Dir)
	}
	result := catchUp(options.Project, project)
	options.Total = totalOf(options)
	options.Notices = noticesOf(options, project, result)
}

// catchUp 은 락을 기다리지 않고 한 번만 잡아 본다. 못 잡으면 지금 있는 색인으로
// 그냥 읽는다 (설계 7-7).
func catchUp(database *index.DB, project *config.Repository) *index.Result {
	if database == nil || project == nil {
		return nil
	}
	return database.CatchUpBy(store.New(project), project.Config.GC, project.Config.Secret,
		time.Now().Add(catchUpBudget))
}

func totalOf(options *Options) int {
	total := 0
	if options.Project == nil {
		return total
	}
	counted, err := options.Project.Count()
	if err == nil {
		total += counted
	}
	return total
}

// noticesOf 는 mem 자신이 할 말이다. 자리는 둘뿐이라 급한 것부터 담는다 (설계 7-4).
func noticesOf(options *Options, project *config.Repository, result *index.Result) []string {
	notes := []string{}
	if bad := badCount(project); bad > 0 {
		notes = append(notes, i18n.T(i18n.HookNoticeBad, bad))
	}
	if result != nil && len(result.Unindexed) > 0 {
		notes = append(notes, i18n.T(i18n.HookNoticeUnindexed, len(result.Unindexed)))
	}
	if result != nil && result.OverBudget {
		notes = append(notes, i18n.T(i18n.HookOverBudget))
	}
	if gcReady(options.Project, project) {
		notes = append(notes, i18n.T(i18n.HookNoticeGC, options.Total))
	}
	return notes
}

// gcReady 는 정리할 때가 됐는지만 본다. 훅 안에서는 gc 가 절대 안 돈다 (설계 9-4).
func gcReady(database *index.DB, project *config.Repository) bool {
	if database == nil || project == nil {
		return false
	}
	total, old, err := database.Totals(project.Config.GC.WarmDays)
	if err != nil {
		return false
	}
	return index.GCReady(project.Config.GC, total, old)
}

// badCount 는 저장에 실패해 inbox/bad 에 남은 건수다. 아무도 그 폴더를 안 열어서
// 블록이 소리 내어 말해 준다.
func badCount(project *config.Repository) int {
	if project == nil {
		return 0
	}
	entries, err := os.ReadDir(store.New(project).InboxBadDir())
	if err != nil {
		return 0
	}
	return len(entries)
}

func hasRules(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, i18n.InstallUsageName))
	return err == nil
}

// startDir 는 세션이 시작한 폴더다. Gemini 는 같은 이름의 환경변수를 준다 (설계 7-6).
// 둘째 값이 거짓이면 「어디서 열렸는지 모른다」 는 뜻이고, 그때는 아무 저장소도
// 열면 안 된다 (스트레스 V14).
func startDir(input Input) (string, bool) {
	if input.Cwd != "" {
		return input.Cwd, true
	}
	if fromEnv := os.Getenv(projectDirEnv); fromEnv != "" {
		return fromEnv, true
	}
	// 훅 JSON 이 들어왔는데 `cwd` 가 비어 있으면 지금 폴더로 떨어지지 않는다.
	// 훅은 저장소 밖 어디서든 불릴 수 있어서 지금 폴더가 근거가 못 된다.
	if input.sawJSON {
		return "", false
	}
	working, err := os.Getwd()
	if err != nil {
		return ".", true
	}
	return working, true
}

// ScopeFor 는 세션이 시작한 폴더로 scope 를 짐작한다. mem.toml `[scope]` 표가
// 이기고, 없으면 폴더 이름 소문자다 (설계 7-2).
func ScopeFor(table map[string]string, cwd string) string {
	if cwd == "" {
		return ""
	}
	folder := filepath.Base(filepath.Clean(cwd))
	if folder == "." || folder == string(filepath.Separator) {
		return ""
	}
	if scope, found := table[folder]; found && scope != "" {
		return scope
	}
	for name, scope := range table {
		if strings.EqualFold(name, folder) && scope != "" {
			return scope
		}
	}
	return strings.ToLower(folder)
}

// write 는 완성된 JSON 을 한 번에 쓴다. 조각내서 쓰면 깨진 문자열이 그대로
// 컨텍스트에 들어간다 (설계 7-3).
func write(stdout io.Writer, text, event string) {
	raw := encode(text, event)
	if raw == nil {
		return
	}
	if _, err := stdout.Write(raw); err != nil {
		note(err)
	}
}

func encode(text, event string) []byte {
	if text == "" {
		return nil
	}
	buffer := bytes.Buffer{}
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	answer := output{HookSpecificOutput: payload{HookEventName: event, AdditionalContext: text}}
	if err := encoder.Encode(answer); err != nil {
		note(err)
		return nil
	}
	return buffer.Bytes()
}

func note(trouble any) {
	if trouble == nil || os.Getenv(debugEnv) != "1" {
		return
	}
	fmt.Fprintln(os.Stderr, trouble)
}
