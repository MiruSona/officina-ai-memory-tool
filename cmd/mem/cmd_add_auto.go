package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
	"github.com/mirusona/officina-ai-memory-tool/internal/retain"
	"github.com/mirusona/officina-ai-memory-tool/internal/safe"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// autoRejectFormat 은 자동 관문 거절 한 줄이다. 한글이 없는 짜임새라 i18n 표에 안 둔다.
const autoRejectFormat = "  [%s] %s"

// quoteSeparator 는 `--quotes` 한 값 안에서 근거 문장을 가르는 표다. 옵션은 이름당
// 값 하나라 여러 번 줄 수 없다.
const quoteSeparator = "||"

// checkOriginOptions 는 큐에 넣기 전에 자동 기억 옵션을 본다. 실패면 종료 코드다.
func checkOriginOptions(parsed *options) (string, int) {
	origin := parsed.text("origin")
	if origin == "" {
		if parsed.has("quotes") || parsed.has("session") {
			return "", fail(i18n.T(i18n.AddOriginOnly))
		}
		return "", exitOK
	}
	if parsed.flags["jsonl"] {
		return "", fail(i18n.T(i18n.AddOriginJSONL))
	}
	if !model.IsOrigin(origin) {
		return "", fail(i18n.T(i18n.BadOrigin, origin))
	}
	if code := checkSessionPrefix(parsed.text("session")); code != exitOK {
		return "", code
	}
	return origin, exitOK
}

// checkSessionPrefix 는 `--session` 앞머리가 8자 이상인지 본다. 1글자 앞머리는 아무
// 세션이나 골라 남의 기록과 대조하거나, undo 로 남의 기억을 거둔다 (리뷰 2026-10-05).
func checkSessionPrefix(given string) int {
	if given != "" && len(given) < retain.ShortLen {
		return fail(i18n.T(i18n.SessionTooShort, given))
	}
	return exitOK
}

// sessionWindow 는 `--session` 없이 「가장 최근 세션」 을 고를 때 다른 세션이 돌고
// 있는지 보는 창이다. 이 안에 본 세션이 둘 이상이면 고르지 않는다.
const sessionWindow = 30 * time.Minute

// ambiguousSession 은 `--session` 을 안 줬는데 최근에 돈 세션이 둘 이상인지다.
// 그러면 남의 세션 기록과 대조하고 origin_session 도 남의 것이 찍힌다.
func ambiguousSession(parsed *options, state retain.State, now time.Time) int {
	if parsed.text("session") != "" {
		return 0
	}
	if count := state.RecentSessions(now, sessionWindow); count > 1 {
		return count
	}
	return 0
}

// autoAdd 는 자동 관문 한 판에 필요한 것을 모은 것이다.
type autoAdd struct {
	Origin  string
	Session string
	Short   string
	Result  retain.Result
}

// quotesOf 는 `--quotes "가||나"` 를 문장 목록으로 편다. 빈 토막은 버린다.
func quotesOf(parsed *options) []string {
	quotes := []string{}
	for _, part := range strings.Split(parsed.text("quotes"), quoteSeparator) {
		if part = strings.TrimSpace(part); part != "" {
			quotes = append(quotes, part)
		}
	}
	return quotes
}

// checkAuto 는 R1~R8 을 돈다 (자동쌓기설계 2-3). 세션 상태와 대화 기록은 이
// 기계의 Memory/local 에서 찾는다 — 못 찾으면 (가)는 저장소 대조만, (나)는 거절이다.
// R3 는 이 기계의 llm.toml 이 켜져 있을 때만 돈다. 서버가 안 닿으면 경고 한 줄로 건너뛴다.
func checkAuto(parsed *options, repository *config.Repository, request store.AddRequest,
	verdict quality.Verdict, origin string) autoAdd {
	state := retain.LoadState(repository.Dir)
	sessionID, session := state.Find(parsed.text("session"))
	if session == nil && parsed.text("session") != "" {
		fmt.Fprintln(os.Stderr, i18n.T(i18n.AddSessionUnknown, parsed.text("session")))
	}
	context := retain.Context{
		Settings: repository.Config.Retain, Root: filepath.Dir(repository.Dir),
		StoreDir: repository.Dir, Scopes: vocabOf(repository).StandardScopes(),
		DayAdds: state.Days[time.Now().Format(model.DayLayout)],
	}
	if session != nil {
		context.SessionAdds = session.AutoAdds
		record, err := retain.ReadWhole(session.TranscriptPath)
		if err == nil {
			context.Record = record
		}
	}
	memory := memoryOf(request)
	if verdict.Memory != nil {
		memory = verdict.Memory
	}
	candidate := retain.Candidate{Memory: memory, Origin: origin, Quotes: quotesOf(parsed),
		Supersedes: request.Supersedes, GateRules: findingRules(verdict)}
	// R3 는 llm.toml 이 켜져 있고 근거 문장이 있고 기존 관문을 지났을 때만 묻는다.
	// nil 포인터를 인터페이스에 넣으면 nil 이 아니게 되므로 있을 때만 단다.
	if len(candidate.Quotes) > 0 && !verdict.Rejected() {
		if judge := judgeOf(repository, loadLLM()); judge != nil {
			context.Judge = judge
		}
	}
	short := retain.Short(sessionID)
	if short == "" {
		short = sessionHint(parsed.text("session"))
	}
	return autoAdd{Origin: origin, Session: sessionID, Short: short,
		Result: retain.Check(candidate, context)}
}

// findingRules 는 판정이 낸 규칙 이름 그대로다. 경고 등급도 빠짐없이 R4 에 넘긴다.
func findingRules(verdict quality.Verdict) []string {
	rules := []string{}
	for _, one := range verdict.Findings {
		rules = append(rules, one.Rule)
	}
	return rules
}

// sessionHint 는 상태 파일에 없는 세션이라도 부르는 쪽이 준 앞 8자를 남긴다.
func sessionHint(given string) string {
	if !retain.ValidSession(given) {
		return ""
	}
	return given[:retain.ShortLen]
}

// runAddAuto 는 `--origin` 이 붙은 add 다. 기존 관문과 자동 관문을 둘 다 지나야
// 저장한다. 걸리면 무엇에 걸렸는지 찍고 log.md 에 `자동 · <origin> · <규칙>` 으로 남긴다.
func runAddAuto(parsed *options, repository *config.Repository, opened *store.Store,
	request store.AddRequest, verdict quality.Verdict, origin string) int {
	auto := checkAuto(parsed, repository, request, verdict, origin)
	if parsed.flags["json"] && (parsed.flags["check"] || verdict.Rejected() || auto.Result.Rejected()) {
		return printAutoJSON(verdict, auto)
	}
	if parsed.flags["check"] {
		code := reportCheckAs(verdict, true)
		printAuto(auto)
		if code == exitOK && auto.Result.Rejected() {
			return exitCheck
		}
		return code
	}
	if verdict.Rejected() || auto.Result.Rejected() {
		return rejectAuto(opened, verdict, auto)
	}
	printVerdictAs(verdict, true)
	warnNote(verdict)
	fixNote(verdict)
	printAuto(auto)
	noteWarnings(opened, auto)
	request = markOrigin(applyVerdict(request, verdict), auto)
	var code int
	if parsed.flags["json"] {
		code = queueAddJSON(parsed, repository, opened, request, verdict)
	} else {
		code = queueAdd(parsed, repository, opened, request)
	}
	if code == exitOK {
		countAuto(repository.Dir, auto.Session)
	}
	return code
}

// markOrigin 은 통과한 기억에 출처 칸과 세션 근거를 단다.
func markOrigin(request store.AddRequest, auto autoAdd) store.AddRequest {
	request.Origin = auto.Origin
	request.OriginSession = auto.Short
	if auto.Short == "" {
		return request
	}
	note := model.SourceNote + "session " + auto.Short
	for _, source := range request.Sources {
		if source == note {
			return request
		}
	}
	request.Sources = append(request.Sources, note)
	return request
}

// rejectAuto 는 둘 중 하나라도 걸린 화면이다. 품질·보안 거절이면 그 종료 코드를 따른다.
func rejectAuto(opened *store.Store, verdict quality.Verdict, auto autoAdd) int {
	code := exitCheck
	if verdict.Rejected() {
		printVerdictAs(verdict, true)
		noteReject(opened, verdict)
		code = verdict.ExitCode()
	}
	printAuto(auto)
	rules := []string{}
	seen := map[string]bool{}
	for _, one := range auto.Result.Rejects {
		if !seen[one.Rule] {
			seen[one.Rule] = true
			rules = append(rules, one.Rule)
		}
		noteLog(opened, store.LogRejected, i18n.T(i18n.AutoLogRejected, auto.Origin, one.Rule, one.Reason))
	}
	noteWarnings(opened, auto)
	if verdict.Rejected() {
		rules = append(rules, verdict.Rules()...)
	}
	fmt.Println(i18n.T(i18n.AddAutoRejected, strings.Join(rules, " ")))
	return code
}

// warnClip 은 log.md 경고 한 줄의 글자 상한이다 — 판정기 오류 글이 길게 붙을 수 있다.
const warnClip = 160

// noteWarnings 는 막지 않은 알림(R3 건너뜀 등)을 log.md 에 `경고 자동 · <origin> · <알림>` 으로 남긴다.
// stderr 로만 나가면 `mem auto report` 가 셀 수 없다. 글은 한 줄로 누르고 잘라 비밀값·줄바꿈이 새지 않게 한다.
func noteWarnings(opened *store.Store, auto autoAdd) {
	for _, warning := range auto.Result.Warnings {
		noteLog(opened, store.LogWarned, i18n.T(i18n.AutoLogWarned, auto.Origin, safe.Clip(safe.OneLine(warning), warnClip)))
	}
}

// printAuto 는 자동 관문 결과를 찍는다. 통과면 stderr, 거절이면 stdout 이다 —
// 기존 관문 화면과 같은 규칙이다.
func printAuto(auto autoAdd) {
	out := os.Stderr
	if auto.Result.Rejected() {
		out = os.Stdout
	}
	for _, one := range auto.Result.Rejects {
		fmt.Fprintln(out, fmt.Sprintf(autoRejectFormat, one.Rule, one.Reason))
	}
	for _, warning := range auto.Result.Warnings {
		fmt.Fprintln(os.Stderr, i18n.T(i18n.AddAutoWarnLine, warning))
	}
	if !auto.Result.Rejected() {
		fmt.Fprintln(os.Stderr, i18n.T(i18n.AddAutoPassed, auto.Origin, auto.Short))
	}
}

// autoVerdictJSON 은 `--json` 의 자동 관문 판정이다.
type autoVerdictJSON struct {
	quality.Verdict
	Origin  string        `json:"origin"`
	Session string        `json:"origin_session,omitempty"`
	Auto    retain.Result `json:"auto"`
}

func printAutoJSON(verdict quality.Verdict, auto autoAdd) int {
	data, err := json.Marshal(autoVerdictJSON{Verdict: verdict, Origin: auto.Origin,
		Session: auto.Short, Auto: auto.Result})
	if err != nil {
		return fail(err.Error())
	}
	fmt.Println(string(data))
	if verdict.Rejected() {
		return verdict.ExitCode()
	}
	if auto.Result.Rejected() {
		return exitCheck
	}
	return exitOK
}

// countAuto 는 R6 상한을 세는 수를 올린다. 못 써도 기억은 이미 들어갔으니 알리기만 한다.
func countAuto(dir, sessionID string) {
	now := time.Now()
	err := retain.UpdateState(dir, now, func(state *retain.State) bool {
		state.Days[now.Format(model.DayLayout)]++
		if session := state.Sessions[sessionID]; session != nil {
			session.AutoAdds++
			session.Seen = now.Format(time.RFC3339)
		}
		return true
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
	}
}
