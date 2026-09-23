package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
	"github.com/mirusona/officina-ai-memory-tool/internal/safe"
	"github.com/mirusona/officina-ai-memory-tool/internal/secret"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// addBools 는 값을 안 받는 옵션이다.
var addBools = []string{"stdin", "pin", "check", "jsonl", "new", "json", "hold"}
var addValues = []string{"type", "scope", "summary", "tags", "body", "status", "todo-status",
	"severity", "title", "author", "sources", "importance", "links", "link", "by",
	"stale-after", "invalid-at", "source", "date", "repo"}

// defaultAuthor 는 `--author` 를 안 줬을 때 쓰는 값이다. `mem` 이 스스로 찍는
// 도구 표기다 — add 를 부르는 것이 훅인지 사람인지 여기서는 알 수 없고, 사람
// 아이디를 도구가 지어내면 「사람이 확인했다」 는 신호가 거짓말이 된다
// (설계 2-2 · 결정 3). 사람은 `--author human:<아이디>` 를 준다.
var defaultAuthor = "mem/" + i18n.Version

func init() {
	register(command{name: "add", run: runAdd, bools: addBools, values: addValues})
}

// runAdd 는 관문 여섯 단계를 지난 기억 한 건을 inbox/new 에 넣고 최종 id 를
// 찍는다. store/ 는 색인이 락을 잡고 승격할 때만 만진다 (설계 3-1).
func runAdd(argv []string) int {
	parsed, err := parseOptions(argv, addBools, addValues)
	if err != nil {
		return fail(err.Error())
	}
	if parsed.flags["jsonl"] {
		return runAddJSONL(parsed)
	}
	// 옵션 값이 아닌 낱말은 대개 `--sources a b` 처럼 공백으로 이은 것이다.
	// 조용히 먹으면 뒤 토막이 통째로 사라진다 (사용 피드백 2026-09-20).
	if len(parsed.rest) > 0 {
		return fail(i18n.T(i18n.AddStrayWords, safe.Summary(strings.Join(parsed.rest, " "), strayRoom)))
	}
	// `--by` 는 큐에 넣기 **전에** 본다. 넣고 나서 걸리면 「저장됨」과 사용법
	// 오류가 한 화면에 같이 뜬다 (리뷰 2026-09-21).
	if old := parsed.text("by"); old != "" && !model.IsID(old) {
		return fail(i18n.T(i18n.BadID, old))
	}
	body, err := bodyOf(parsed)
	if err != nil {
		return fail(err.Error())
	}
	request := requestOf(parsed, body)
	repository, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	noteSeverityFix(parsed.text("severity"), request.Severity)
	if defaultTodoStatus(&request, vocabOf(repository)) {
		fmt.Fprintln(os.Stderr, i18n.T(i18n.AddStatusDefault, request.Status))
	}
	verdict := quality.Gate(memoryOf(request), gateOptions(repository, opened, parsed))
	if parsed.flags["json"] {
		code := printVerdictJSON(verdict)
		if parsed.flags["check"] || verdict.Rejected() {
			return code
		}
		return queueAdd(parsed, opened, applyVerdict(request, verdict))
	}
	// --check 는 미리보기다. 관문만 돌리고 무슨 일이 있어도 저장하지 않는다.
	if parsed.flags["check"] {
		return reportCheck(verdict)
	}
	if verdict.Rejected() {
		printVerdict(verdict)
		// 화면 끝 한 줄만 봐도 들어갔는지 알아야 한다 (사용 피드백 2026-09-20).
		fmt.Println(i18n.T(i18n.AddRejectedNote, verdict.RejectCount()))
		noteReject(opened, verdict)
		return verdict.ExitCode()
	}
	printVerdict(verdict)
	warnNote(verdict)
	fixNote(verdict)
	return queueAdd(parsed, opened, applyVerdict(request, verdict))
}

// applyVerdict 는 관문이 스스로 고친 것(별칭 치환)을 큐 항목에 되돌려 담는다.
func applyVerdict(request store.AddRequest, verdict quality.Verdict) store.AddRequest {
	if verdict.Memory == nil {
		return request
	}
	request.Tags = verdict.Memory.Tags
	request.Scope = verdict.Memory.Scope
	return request
}

// reportCheck 는 --check 화면이다. 통과면 넣어도 된다고 말한다.
// 무슨 일이 있어도 저장하지 않는다 — 그 말을 화면에도 늘 적는다.
func reportCheck(verdict quality.Verdict) int {
	if len(verdict.Findings) == 0 {
		fmt.Println(i18n.T(i18n.GateCheckClean))
		fmt.Println(i18n.T(i18n.GateCheckOnly))
		return exitOK
	}
	printVerdict(verdict)
	fmt.Println(i18n.T(i18n.GateCheckOnly))
	return verdict.ExitCode()
}

// noteReject 는 거절도 log.md 에 남긴다. 설계 3-5 가 「거절도 적는다 — 무엇이
// 얼마나 막혔는지가 G6 ④ 를 재는 재료다」 라고 적었는데 들어온 것만 적고
// 있었다. 그래서 S05(add-reject-rate)가 늘 「—」 였다 (실데이터 시험 7절).
func noteReject(opened *store.Store, verdict quality.Verdict) {
	rules := strings.Join(verdict.Rules(), " ")
	title := ""
	if verdict.Memory != nil {
		title = verdict.Memory.Title
	}
	noteLog(opened, store.LogRejected, fmt.Sprintf("%s `%s`", rules, title))
}

// warnNote 는 경고만 나고 저장된 경우를 분명히 말한다. 경고 줄만 흘러가면
// 1년차는 거절인지 통과인지 모른다 (실데이터 시험 4-3).
//
// 경고 본문(규칙 이름 + 까닭)을 이 줄 바로 밑에 한 번 더 찍는다. 위쪽 판정
// 덩어리는 관련 id·다음에 할 것까지 붙어 길어서, 끝줄 근처만 보는 사람은
// 「몇 가지가 경고다」만 읽고 무엇인지는 놓쳤다 (사용 피드백 2026-09-21 손님그림).
func warnNote(verdict quality.Verdict) {
	if verdict.Kind != quality.KindWarn {
		return
	}
	fmt.Fprintln(os.Stderr, i18n.T(i18n.GateWarnStored, len(verdict.Findings)))
	for _, one := range verdict.Findings {
		fmt.Fprintf(os.Stderr, warnRowFormat+"\n", one.Rule, one.Reason)
	}
}

func queueAdd(parsed *options, opened *store.Store, request store.AddRequest) int {
	if err := opened.EnsureDirs(); err != nil {
		return fail(err.Error())
	}
	name, err := opened.WriteAdd(request)
	if err != nil {
		return fail(err.Error())
	}
	// 승격이 어디서 멈춰도 이 id 로 같은 자리를 가리킨다. 다만 닮은 기억에
	// 합쳐지면 그 기억의 id 가 남는다 (설계 3-1 ⑥).
	id := model.QueueID(name, request.Body, request.Date)
	fmt.Println(id)
	noteLog(opened, store.LogAdded, fmt.Sprintf("%s `%s` (%s)%s%s", id, request.Title, request.Author,
		forcedMark(parsed), heldMark(request)))
	if request.Review {
		fmt.Fprintln(os.Stderr, i18n.T(i18n.AddHeldNote, id))
	}
	code := supersedeOld(parsed, opened, id)
	// 저장됐다는 말이 늘 **맨 마지막 줄**이라야 화면 끝만 보고도 안다.
	// 덮기가 실패했으면 「저장됨」만 찍으면 안 된다 — 옛 기억은 그대로다.
	if code != exitOK {
		fmt.Fprintln(os.Stderr, i18n.T(i18n.AddQueuedNoBy, id, parsed.text("by")))
		return code
	}
	fmt.Fprintln(os.Stderr, i18n.T(i18n.AddQueuedNote, id))
	return code
}

// warnRowFormat 은 경고 한 줄(규칙 이름 : 까닭)이다. 한글이 없는 짜임새라
// i18n 표에 안 둔다 (tagsPlanFormat 과 같은 규칙).
const warnRowFormat = "  - %s : %s"

// strayRoom 은 되돌려 찍는 낱말 자리의 룬 수다.
const strayRoom = 60

// severityOf 는 `--severity medium` 처럼 흔한 다른 말을 표준 값으로 바꾼다.
// 바꿨다고 알리는 것은 부르는 쪽 몫이다 — 값을 옮기는 함수가 화면까지 만지면
// JSONL 묶음이 줄마다 같은 말을 찍는다 (리뷰 2026-09-21).
func severityOf(value string) string {
	fixed, _ := model.NormalizeSeverity(value)
	return fixed
}

// noteSeverityFix 는 한 건짜리 add 에서 바뀐 severity 를 알린다.
func noteSeverityFix(given, fixed string) {
	if given == "" || given == fixed {
		return
	}
	fmt.Fprintln(os.Stderr, i18n.T(i18n.GateFixed, "severity", given, fixed))
}

// defaultTodoStatus 는 todo_status 를 안 주면 open 으로 둔다. 새 할 일은 거의 다
// 열린 상태라 매번 적는 것이 군더더기였다 (사용 피드백 2026-09-20).
// 글자 `todo` 를 박지 않고 종류 표의 todo_status 칸을 본다 — 표로 늘린 종류도 같다.
// 돌려주는 값은 채웠는지다. 알리는 것은 부르는 쪽이 한다.
func defaultTodoStatus(request *store.AddRequest, vocab config.Vocab) bool {
	if request.Status != "" || !vocab.TypeTable().Spec(request.Type).TodoStatus {
		return false
	}
	request.Status = model.StatusOpen
	return true
}

// forcedMark 는 `--new` 로 중복 관문을 밀고 들어왔다는 표시다. status --quality
// 가 이 표시를 세어 C04 되돌림 신호로 쓴다 (결정 39). JSONL 묶음은 한 줄마다
// 옵션을 못 주므로 묶음 전체에 같은 표시가 붙는다.
func forcedMark(parsed *options) string {
	if parsed.flags["new"] {
		return newMark
	}
	return ""
}

// holdMark 는 `--hold` 로 들어와 사람 승격을 기다리는 기억이라는 표시다.
// log.md 만 보고도 어느 기억이 멈춰 있는지 알 수 있어야 한다 (결정 6).
const holdMark = " --hold"

func heldMark(request store.AddRequest) string {
	if request.Review {
		return holdMark
	}
	return ""
}

// supersedeOld 는 `add --by <옛id>` 가 옛 결정에 덮임 표시를 다는 자리다.
// 도구만 쓰는 칸이라 사람이 손으로 못 적는다 (설계 2-2 · 3-4).
func supersedeOld(parsed *options, opened *store.Store, newID string) int {
	old := parsed.text("by")
	if old == "" {
		return exitOK
	}
	if !model.IsID(old) {
		return fail(i18n.T(i18n.BadID, old))
	}
	set := map[string]any{"superseded_by": newID, "invalid_at": time.Now().Format(model.DayLayout)}
	if _, err := opened.WritePatch(old, set); err != nil {
		return fail(err.Error())
	}
	fmt.Fprintln(os.Stderr, i18n.T(i18n.SetSuperseded, old, newID))
	noteLog(opened, store.LogSuper, old+" → "+newID)
	noteUsedBy(parsed, opened, old, os.Stderr)
	return exitOK
}

// noteLog 는 log.md 에 한 줄 남긴다. 못 남겨도 하던 일은 그대로다 — 기록은
// 곁다리라 오류를 삼키는 세 자리 중 하나다 (설계 6-5).
func noteLog(opened *store.Store, kind, line string) {
	if err := store.AppendLog(opened.Dir, time.Now(), kind, line); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
	}
}

// scannerFor 는 이 저장소의 비밀정보 패턴을 컴파일한다. 설정이 비면 기본
// 패턴으로 떨어진다 — 검사 없이 지나가는 길은 두지 않는다.
func scannerFor(settings config.SecretConfig) *secret.Scanner {
	if len(settings.Patterns) == 0 {
		settings = config.Default("").Secret
	}
	return secret.New(settings.Patterns)
}

// refuseSecret 은 body·summary·title 을 훑는다. 맞은 값은 절대 안 찍고 규칙
// 이름과 줄 번호만 알린다.
func refuseSecret(repository *config.Repository, request store.AddRequest) int {
	scanner := scannerFor(repository.Config.Secret)
	if found := scanner.ScanText(request.Body); found != nil {
		return refuse(i18n.T(i18n.SecretFound, found.Line, found.Rule))
	}
	for _, pair := range [][2]string{{"summary", request.Summary}, {"title", request.Title}} {
		if found := scanner.ScanLine(pair[1]); found != nil {
			return refuse(i18n.T(i18n.SecretFoundIn, pair[0], found.Rule))
		}
	}
	// sources 도 사람이 쓰는 칸이다. url: 안에 토큰을 끼워 넣는 것이 흔하다.
	for _, source := range request.Sources {
		if found := scanner.ScanLine(source); found != nil {
			return refuse(i18n.T(i18n.SecretFoundIn, "sources", found.Rule))
		}
	}
	return exitOK
}

// refuse 는 보안 차단이다. 설계 6-1 이 보안에 종료 코드 4 를 줬다 — 품질
// 거절(2)과 숫자를 갈라야 스크립트가 둘을 다르게 다룰 수 있다.
func refuse(message string) int {
	fmt.Fprintln(os.Stderr, message)
	fmt.Fprintln(os.Stderr, i18n.T(i18n.SecretFix))
	return exitSecurity
}

// runAddJSONL 은 표준입력에서 한 줄에 한 건씩 받는다. 한 줄이라도 관문에
// 걸리면 **하나도 안 넣는다** — 반만 들어간 묶음은 사람이 어디까지 됐는지
// 못 안다 (불변조건 I4).
func runAddJSONL(parsed *options) int {
	repository, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fail(err.Error())
	}
	requests, fixes, code := decodeJSONL(string(raw), repository, opened, parsed)
	if code != exitOK {
		return code
	}
	if err := opened.EnsureDirs(); err != nil {
		return fail(err.Error())
	}
	for _, request := range requests {
		name, err := opened.WriteAdd(request)
		if err != nil {
			return fail(err.Error())
		}
		id := model.QueueID(name, request.Body, request.Date)
		fmt.Println(id)
		noteLog(opened, store.LogAdded, fmt.Sprintf("%s `%s` (%s)%s", id, request.Title, request.Author,
			forcedMark(parsed)))
	}
	fixes.note()
	fmt.Fprintln(os.Stderr, i18n.T(i18n.JSONLDone, len(requests)))
	return exitOK
}

// addFixes 는 도구가 조용히 바꾸거나 채운 칸을 센다. 줄마다 찍으면 묶음
// 화면이 알림으로 덮여 정작 결과가 안 보인다 (리뷰 2026-09-21).
type addFixes struct {
	Severity int
	Status   int
}

// note 는 묶음 한 판을 끝내고 몇 건을 고쳐 넣었는지 한 줄로 알린다.
func (f addFixes) note() {
	if f.Severity == 0 && f.Status == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, i18n.T(i18n.JSONLFixed, f.Severity, f.Status))
}

func decodeJSONL(text string, repository *config.Repository, opened *store.Store, parsed *options) ([]store.AddRequest, addFixes, int) {
	requests := []store.AddRequest{}
	fixes := addFixes{}
	shared := gateOptions(repository, opened, parsed)
	for number, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		request := store.AddRequest{}
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			return nil, fixes, fail(i18n.T(i18n.JSONLBadLine, number+1, err.Error()))
		}
		// JSONL 길은 `--by` 를 안 받는다. 줄에 적힌 supersedes 는 id 검사도,
		// 옛 기억 덮임 표시도 없이 들어오니 비운다 (리뷰 2026-09-23).
		request.Supersedes = ""
		given := request.Severity
		fillDefaults(&request)
		if request.Severity != given {
			fixes.Severity++
		}
		if defaultTodoStatus(&request, shared.Vocab) {
			fixes.Status++
		}
		verdict := quality.Gate(memoryOf(request), shared)
		if verdict.Rejected() {
			fmt.Fprintln(os.Stderr, i18n.T(i18n.JSONLAt, number+1))
			printVerdict(verdict)
			fmt.Fprintln(os.Stderr, i18n.T(i18n.AddRejectedNote, verdict.RejectCount()))
			return nil, fixes, verdict.ExitCode()
		}
		requests = append(requests, applyVerdict(request, verdict))
	}
	if len(requests) == 0 {
		return nil, fixes, fail(i18n.T(i18n.JSONLEmpty))
	}
	return requests, fixes, exitOK
}

// fillDefaults 는 한 줄 JSON 이 안 적은 칸을 명령줄과 같은 기본값으로 채운다.
func fillDefaults(request *store.AddRequest) {
	request.Op = store.OpAdd
	if !model.IsDate(request.Date) {
		request.Date = time.Now().Format(model.DayLayout)
	}
	if request.Author == "" {
		request.Author = defaultAuthor
	}
	request.Severity = severityOf(request.Severity)
	request.Body = trimTail(request.Body)
}

// bodyOf 는 --body 를 쓰거나, 없으면 표준입력을 통째로 읽는다.
func bodyOf(parsed *options) (string, error) {
	if text := parsed.text("body"); text != "" {
		return trimTail(text), nil
	}
	return readStdin()
}

// readStdin 은 표준입력을 통째로 읽는다. add 와 set 이 같이 쓴다.
func readStdin() (string, error) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return trimTail(string(data)), nil
}

func trimTail(text string) string {
	return strings.TrimRight(text, "\r\n")
}

func requestOf(parsed *options, body string) store.AddRequest {
	date := parsed.text("date")
	if !model.IsDate(date) {
		date = time.Now().Format(model.DayLayout)
	}
	author := parsed.text("author")
	if author == "" {
		author = defaultAuthor
	}
	importance, err := strconv.Atoi(parsed.text("importance"))
	if err != nil {
		importance = 0
	}
	return store.AddRequest{
		Op: store.OpAdd, Type: parsed.text("type"), Date: date,
		Summary: parsed.text("summary"), Tags: parsed.list("tags"),
		Source: parsed.text("source"), Scope: parsed.text("scope"), Title: parsed.text("title"),
		Status: todoStatusOf(parsed), Severity: severityOf(parsed.text("severity")),
		Pinned: parsed.flags["pin"], Importance: importance,
		InvalidAt: parsed.text("invalid-at"), Links: linksOf(parsed),
		Author: author, Sources: parsed.list("sources"), StaleAfter: parsed.text("stale-after"),
		Review: parsed.flags["hold"], Supersedes: parsed.text("by"), Body: body,
	}
}

// todoStatusOf 는 새 이름(--todo-status)을 먼저 보고 없으면 옛 이름을 본다.
func todoStatusOf(parsed *options) string {
	if parsed.has("todo-status") {
		return parsed.text("todo-status")
	}
	return parsed.text("status")
}

// linksOf 는 설계 표의 `--link` 와 v0.1 의 `--links` 를 같이 받는다.
func linksOf(parsed *options) []string {
	return append(parsed.list("links"), parsed.list("link")...)
}

// memoryOf 는 관문에 걸기 위해 큐 항목을 기억 꼴로 옮긴다. id 는 승격할 때
// 정해지니 여기서는 검사에 통과할 임시 값을 넣는다.
func memoryOf(request store.AddRequest) *model.Memory {
	memory := &model.Memory{
		ID: model.NewID(request.Body, time.Now()), Type: request.Type, Date: request.Date,
		Summary: request.Summary, Tags: request.Tags,
		Scope: request.Scope, Title: request.Title, Pinned: request.Pinned,
		Importance: request.Importance, Severity: request.Severity,
		InvalidAt: request.InvalidAt, Links: request.Links, Body: request.Body,
		Author: request.Author, Sources: request.Sources, StaleAfter: request.StaleAfter,
		TodoStatus: request.Status, Review: request.Review, Spec: model.SpecV2,
	}
	if memory.Author == "" {
		// 옛 규격으로 들어온 큐 파일이다. 그 자를 그대로 쓴다 (설계 2-5).
		memory.Spec = model.SpecV1
		memory.LegacySource = request.Source
		memory.Author = model.LegacyAuthor(request.Source)
		memory.LegacyStatus = request.Status
	}
	return memory
}
