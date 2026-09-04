// Package hook 은 세션이 시작할 때 넣을 기억 블록을 만들고 안전하게 쓴다.
// 어떤 일이 있어도 세션을 막지 않는다 — 못 하겠으면 아무 말도 안 한다 (불변조건 3).
package hook

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mirusona/officina-ai-memory-tool/internal/budget"
	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/secret"
)

// 세션이 시작하는 계기다 (설계 5-1).
const (
	KindStartup = "startup"
	KindClear   = "clear"
	KindResume  = "resume"
	KindCompact = "compact"
	KindFork    = "fork"
	// KindSubagent 는 세션이 아니라 서브에이전트가 뜬 것이다. KnownKind 에는
	// 일부러 안 넣는다 — 세션 입력의 source 칸으로 이 예산을 고를 수 없게 한다.
	KindSubagent = "subagent"
)

// noticeLines 는 알림 절의 상한이다. 기억 줄과 달리 mem 자신이 하는 말이라
// mem.toml 의 lines_per_section 을 안 따른다 (설계 5-1).
const noticeLines = 2

// 예산과 상한 기본값은 config.Default 한 곳에만 있다. 두 곳에 적어 뒀더니
// mem.toml 이 말하는 값과 훅이 쓰는 값이 서로 달랐다 (설계 6-7 · 불일치 ②).
var (
	defaultBudget = config.Default("").Budget
	defaultHook   = config.Default("").Hook
)

// 하드 상한은 예산의 1.5배, 글자 하드컷은 Claude 공식 상한이다 (설계 5-1).
const (
	hardCapNumer = 3
	hardCapDenom = 2
	openHardCap  = 4000
	hardCharCap  = 10000
)

// byteRounds 는 「바이트 수를 적은 줄이 다시 바이트 수를 바꾼다」 를 푸는
// 되풀이 횟수다. 자릿수가 한 자리 늘고 마는 문제라 두세 번이면 맞는다.
const byteRounds = 8

// Options 는 블록 한 번을 만드는 데 필요한 전부다. 색인 핸들은 이미 열린 것을
// 받는다 — 훅은 어디서도 색인을 다시 열지 않는다 (설계 5-1).
type Options struct {
	Project *index.DB
	Kind    string
	// Scope 는 cwd 로 짐작한 이 세션의 관심사다. 절마다 이 scope 를 먼저 채운다.
	Scope string
	// Lead 는 guard 바깥 맨 위에 붙는 줄들이다. 서브에이전트 블록만 쓴다.
	Lead   string
	Budget config.BudgetConfig
	// BudgetOverride 는 `--budget` 이 준 값이다. 0 이면 계기별 기본값을 쓴다.
	BudgetOverride int
	Hook           config.HookConfig
	Total          int
	HasRules       bool
	Notices        []string
	// Types 는 이 저장소의 기억 종류 표다. 절 이름과 차례를 이것이 정한다.
	Types model.TypeTable
	// Secret 은 마지막 방어선이 쓸 패턴이다. 손으로 고친 md 가 색인을 뚫고
	// 들어와도 훅 블록에는 못 실리게 한다 (보안시험 H-1).
	Secret config.SecretConfig
	// scanner 는 Secret 을 컴파일한 것이다. collect 가 한 번만 만든다.
	scanner *secret.Scanner
}

// secretScanner 는 빈 설정도 기본 패턴으로 떨어뜨린다.
func secretScanner(settings config.SecretConfig) *secret.Scanner {
	if len(settings.Patterns) == 0 {
		settings = config.Default("").Secret
	}
	return secret.New(settings.Patterns)
}

// Stats 는 훅 한 번을 잰 값이다. `mem hook --json` 과 `status --doctor` 가
// 이것을 읽는다 — 상한이 자인지 바이트인지는 우리가 재야 안다 (설계 G3).
type Stats struct {
	// Bytes 는 주입 글월의 UTF-8 바이트 수다. 상한을 재는 진짜 자다.
	Bytes int `json:"bytes"`
	// Tokens 는 근사 토큰 수다 (budget 패키지 한 곳에서만 센다).
	Tokens int `json:"tokens"`
	// MS 는 훅 한 번의 벽시계 시간이다. Build 는 안 채운다 — Run 이 채운다.
	MS int64 `json:"ms"`
	// Truncated 는 우리가 먼저 잘랐는지다.
	Truncated bool `json:"truncated"`
	// Shown 은 실제로 실린 기억 줄 수, Total 은 저장소 전체 건수다.
	Shown int `json:"shown"`
	Total int `json:"total"`
	// Budget 은 이 계기의 토큰 예산, MaxBytes 는 바이트 상한이다.
	Budget   int `json:"budget"`
	MaxBytes int `json:"max_bytes"`
}

// section 은 제목 하나에 줄 몇 개다. notes 는 기억이 아니라 mem 자신의 알림이라
// 주입 건수에 안 센다.
type section struct {
	key i18n.Key
	// name 은 종류 표가 만든 절 이름이다. 비면 key 로 제목을 만든다.
	name      string
	lines     []string
	notes     []string
	protected bool
}

// title 은 절 제목 한 줄이다. 종류 표가 만든 절은 이름이 곧 제목이라 i18n 표에
// 넣을 한글 문장이 없다 — 서식만 여기서 만든다.
func (s *section) title(count int) string {
	if s.name != "" {
		return fmt.Sprintf("## %s (%d)", s.name, count)
	}
	return i18n.T(s.key, count)
}

// Build 는 블록 글월과 잰 값을 돌려준다.
func Build(options Options) (string, Stats) {
	limit := budgetFor(options)
	date := time.Now().Format(model.DayLayout)
	whole, _ := shrink(collect(options), options, limit, date)
	// 머리말의 「N건 중 M건」과 shown 은 여기서 잘리기 전 값이 아니라, 바이트·
	// 글자 상한까지 다 지난 뒤 실제로 실린 줄 수로 render 가 다시 맞춘다 (U2).
	return render(whole, limit, maxBytesOf(options), options.Total, date)
}

// shrink 는 토큰 예산에 맞을 때까지 긴 절부터 한 줄씩 뗀다.
func shrink(sections []*section, options Options, limit int, date string) (block, int) {
	for {
		whole, shown := assemble(sections, options, date)
		if limit <= 0 || budget.Estimate(whole.text()) <= limit {
			return whole, shown
		}
		if !cutLongest(sections, false) && !cutLongest(sections, true) {
			return whole, shown
		}
	}
}

// block 은 완성된 글월을 셋으로 나눠 든다. 하드컷이 자료 표시 두 줄을 안 먹고
// 가운데만 자를 수 있게 하려는 것이다 (설계 5-1).
type block struct {
	head string
	body string
	foot string
}

func (b block) text() string {
	return b.head + b.body + b.foot
}

// render 는 토큰 하드 상한과 바이트 상한을 다 씌우고 글월을 완성한다.
// 꼬리에 적는 바이트 수가 다시 바이트 수를 바꾸므로 값이 멎을 때까지 되푼다.
func render(base block, limit, maxBytes, total int, date string) (string, Stats) {
	base, cut := capped(base, limit)
	notice, guess, text := "", len(base.text()), ""
	for round := 0; round < byteRounds; round++ {
		trial := base
		trial.head = base.head + notice
		trial.foot = base.foot + i18n.T(i18n.HookFootStats, guess, maxBytes) + "\n"
		trimmed, byteCut := byteCapped(trial, maxBytes)
		if (cut || byteCut) && notice == "" {
			notice = i18n.T(i18n.HookByteCut, guess, maxBytes) + "\n"
			cut = true
			continue
		}
		text = trimmed.text()
		if len(text) == guess {
			break
		}
		guess = len(text)
	}
	shown := countEntries(text)
	text = restampHeader(text, date, total, shown)
	return text, Stats{Bytes: len(text), Tokens: budget.Estimate(text),
		Truncated: cut, Budget: limit, MaxBytes: maxBytes, Shown: shown, Total: total}
}

// entryPrefix 는 기억 한 줄의 앞머리다. markOf 가 붙든 안 붙든 lineOf 가 만드는
// 줄은 늘 "- [" 로 시작한다 — 알림 절 문장(hookMessages 의 Notice 표)은 이
// 모양을 안 쓴다. 바뀌면 여기와 lineOf 를 같이 본다.
const entryPrefix = bullet + "["

// countEntries 는 완성된 글월에서 실제로 실린 기억 줄 수를 센다. shrink 가 센
// shown 은 토큰 예산까지만 반영한 값이라, 그 뒤 바이트·글자 상한이 한 번 더
// 자르면 머리말 숫자와 어긋난다 (U2). 그래서 다 자른 뒤 여기서 다시 센다.
func countEntries(text string) int {
	count := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, entryPrefix) {
			count++
		}
	}
	return count
}

// headerPrefix 는 headOf 가 쓰는 i18n.HookHeader 서식의 고정 앞부분이다.
// i18n/hook.go 의 서식을 바꾸면 여기도 같이 본다.
const headerPrefix = "# 기억 (mem · "

// restampHeader 는 다 자른 뒤 실제 shown 으로 머리말 줄만 다시 찍는다. shown 은
// 자르기 전보다 줄지 않을 일이 없어 숫자 자릿수도 같거나 줄어든다 — 그래서
// 바이트 상한을 다시 넘길 걱정 없이 문자열만 바꿔 끼워도 된다.
func restampHeader(text, date string, total, shown int) string {
	tokens := budget.Estimate(text)
	fresh := i18n.T(i18n.HookHeader, date, total, shown, tokens) + "\n"
	replaced := replaceLine(text, headerPrefix, fresh)
	// 머리 길이가 줄면 토큰 어림값도 한두 개 달라질 수 있어 한 번 더 맞춘다.
	if again := budget.Estimate(replaced); again != tokens {
		fresh = i18n.T(i18n.HookHeader, date, total, shown, again) + "\n"
		replaced = replaceLine(replaced, headerPrefix, fresh)
	}
	return replaced
}

// replaceLine 은 prefix 로 시작하는 한 줄만 통째로 새 줄로 바꾼다.
func replaceLine(text, prefix, newLine string) string {
	lines := strings.SplitAfter(text, "\n")
	for at, line := range lines {
		if strings.HasPrefix(line, prefix) {
			lines[at] = newLine
			break
		}
	}
	return strings.Join(lines, "")
}

// capped 은 토큰 하드 상한과 글자 하드컷 안으로 자른다. 고정 줄도 여기서는
// 잘린다 (불변조건 I3 의 자료 표시 두 줄만 남는다).
func capped(whole block, limit int) (block, bool) {
	room := hardCap(limit) - budget.Estimate(whole.head+whole.foot)
	if room <= 0 {
		whole.body = i18n.T(i18n.HookTruncated) + "\n"
		return whole, true
	}
	cut, clipped := budget.Clip(whole.body, room)
	if clipped {
		whole.body = trimPartialLine(cut) + i18n.T(i18n.HookTruncated) + "\n"
	}
	return charCapped(whole, clipped)
}

// charCapped 은 근사식 뒤에 있는 진짜 안전장치다 — 세어서 자른다.
func charCapped(whole block, cut bool) (block, bool) {
	if len([]rune(whole.text())) <= hardCharCap {
		return whole, cut
	}
	notice := i18n.T(i18n.HookTruncated) + "\n"
	room := hardCharCap - len([]rune(whole.head+whole.foot+notice))
	if room < 0 {
		room = 0
	}
	body := []rune(whole.body)
	if len(body) > room {
		body = body[:room]
	}
	whole.body = trimPartialLine(string(body)) + notice
	return whole, true
}

// byteCapped 은 UTF-8 바이트 상한을 씌운다. 상한이 자인지 바이트인지 갈리고
// 한글은 1글자가 3바이트라, 글자 수만 보면 3배 위험하다 (설계 결정 23).
func byteCapped(whole block, maxBytes int) (block, bool) {
	if maxBytes <= 0 || len(whole.text()) <= maxBytes {
		return whole, false
	}
	room := maxBytes - len(whole.head) - len(whole.foot)
	if room < 0 {
		room = 0
	}
	whole.body = cutBytes(whole.body, room)
	return whole, true
}

// cutBytes 는 뒤에서 줄 단위로 덜어 낸다. 줄 하나가 그래도 넘치면 그때만
// 룬 경계에서 자른다 — 한글 한 글자를 반으로 쪼개면 글월 전체가 깨진다.
func cutBytes(body string, room int) string {
	if len(body) <= room {
		return body
	}
	kept := strings.Builder{}
	for _, line := range strings.SplitAfter(body, "\n") {
		if kept.Len()+len(line) > room {
			break
		}
		kept.WriteString(line)
	}
	if kept.Len() > 0 {
		return kept.String()
	}
	return clipBytes(body, room)
}

// clipBytes 는 룬 가운데를 안 자르고 바이트 수를 맞춘다.
func clipBytes(text string, room int) string {
	if len(text) <= room {
		return text
	}
	cut := room
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

// trimPartialLine 은 잘리다 만 마지막 줄을 아예 버린다. 반쪽 줄이 남으면
// 그것이 그대로 기억처럼 읽힌다.
func trimPartialLine(text string) string {
	if text == "" {
		return ""
	}
	at := strings.LastIndex(text, "\n")
	if at < 0 {
		return ""
	}
	return text[:at+1]
}

func hardCap(limit int) int {
	if limit <= 0 {
		return openHardCap
	}
	return limit * hardCapNumer / hardCapDenom
}

// KnownKind 는 우리가 아는 계기인지다.
func KnownKind(kind string) bool {
	switch kind {
	case KindStartup, KindClear, KindResume, KindCompact, KindFork:
		return true
	}
	return false
}

// budgetFor 는 계기에 맞는 예산이다. mem.toml 값이 0 이면 설계 기본값을 쓴다.
func budgetFor(options Options) int {
	if options.BudgetOverride > 0 {
		return options.BudgetOverride
	}
	switch options.Kind {
	case KindClear:
		return orDefault(options.Budget.Clear, defaultBudget.Clear)
	case KindResume:
		return orDefault(options.Budget.Resume, defaultBudget.Resume)
	case KindCompact:
		return orDefault(options.Budget.Compact, defaultBudget.Compact)
	case KindFork:
		return orDefault(options.Budget.Fork, defaultBudget.Fork)
	case KindSubagent:
		return orDefault(options.Budget.Subagent, defaultBudget.Subagent)
	}
	return orDefault(options.Budget.Startup, defaultBudget.Startup)
}

// types 는 이 저장소의 종류 표다. 안 받았으면 기본 7종이다.
func (o Options) types() model.TypeTable {
	if len(o.Types) == 0 {
		return model.DefaultTypes()
	}
	return o.Types
}

func maxBytesOf(options Options) int {
	if options.Kind == KindSubagent {
		return orDefault(options.Hook.SubagentMaxBytes, defaultHook.SubagentMaxBytes)
	}
	return orDefault(options.Hook.MaxBytes, defaultHook.MaxBytes)
}

func linesOfSection(options Options) int {
	return orDefault(options.Hook.LinesPerSection, defaultHook.LinesPerSection)
}

func orDefault(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

// collect 는 고정 절 · 종류 표가 만든 절들 · 알림 절을 만든다. 한 기억이 두 번
// 들어가지 않게 seen 으로 센다. 줄이 하나도 없는 절은 assemble 이 통째로
// 건너뛰므로 「(0)」 짜리 절은 안 생긴다.
func collect(options Options) []*section {
	seen := map[string]bool{}
	options.scanner = secretScanner(options.Secret)
	want := linesOfSection(options)
	table := options.types()
	made := []*section{{key: i18n.HookSectionPinned, protected: true,
		lines: pick(options, index.HookPick{Kind: index.PickPinned}, want, seen)}}
	for _, name := range table.HookSections() {
		one := section{name: name}
		for _, ask := range index.HookPicksFor(table, name) {
			if len(one.lines) >= want {
				break
			}
			one.lines = append(one.lines, pick(options, ask, want-len(one.lines), seen)...)
		}
		made = append(made, &one)
	}
	notice := section{key: i18n.HookSectionNotice, notes: options.Notices, protected: true}
	if len(notice.notes) > noticeLines {
		notice.notes = notice.notes[:noticeLines]
	}
	return append(made, &notice)
}

// source 는 어느 저장소를 어떤 scope 로 볼 차례인지다.
type source struct {
	database *index.DB
	scope    string
}

// sourceOrder 는 자기 scope 를 두 저장소에서 먼저 훑고, 남는 자리를 무필터로
// 채운다. scope 는 필터가 아니라 가산이다 (설계 4-1b).
func sourceOrder(options Options) []source {
	order := []source{}
	for _, scope := range scopeOrder(options.Scope) {
		if options.Project != nil {
			order = append(order, source{database: options.Project, scope: scope})
		}
	}
	return order
}

func scopeOrder(scope string) []string {
	if scope == "" {
		return []string{""}
	}
	return []string{scope, ""}
}

// pick 은 한 절 몫을 채운다. 색인 질의가 실패하면 그 자리만 건너뛴다.
func pick(options Options, ask index.HookPick, want int, seen map[string]bool) []string {
	lines := []string{}
	for _, from := range sourceOrder(options) {
		if len(lines) >= want {
			break
		}
		ask.Scope, ask.Limit = from.scope, want
		rows, err := from.database.HookRows(ask)
		if err != nil {
			continue
		}
		lines = append(lines, linesOf(rows, seen, want-len(lines), options.scanner)...)
	}
	return lines
}

func linesOf(rows []index.HookRow, seen map[string]bool, room int,
	scanner *secret.Scanner) []string {
	lines := []string{}
	for _, row := range rows {
		if len(lines) >= room || seen[row.ID] {
			continue
		}
		seen[row.ID] = true
		// 마지막 방어선이다. 걸린 기억은 줄을 아예 안 만든다 — 규칙 이름도
		// 안 적는다. 훅 블록은 사람이 안 보는 자리라 알릴 데가 없다.
		if scanner != nil && scanner.ScanLine(row.Summary) != nil {
			continue
		}
		lines = append(lines, lineOf(row))
	}
	return lines
}

// lineOf 는 기억 한 건을 한 줄로 쓴다. 요약만 따로 살균하고 앞뒤는 우리가 만든다.
func lineOf(row index.HookRow) string {
	head := markOf(row) + "[" + safeID(row.ID) + "]"
	tail := " (" + safeHead(row.Scope) + " · " + safeHead(row.Date) + ")"
	room := lineMaxRunes - len([]rune(bullet+head+" "+tail))
	line := bullet + safeHead(head) + " " + safeSummary(row.Summary, room) + tail
	return clip(line, lineMaxRunes)
}

// markOf 는 줄 앞 표식이다. 어디서 온 말인지 줄에 적는다 (불변조건 I3).
func markOf(row index.HookRow) string {
	marks := []string{}
	if row.Pinned {
		marks = append(marks, i18n.T(i18n.HookMarkPinned))
	}
	if row.Type == model.TypeTodo {
		marks = append(marks, i18n.T(i18n.HookMarkTodo))
	}
	if row.Severity == model.SeverityHigh {
		marks = append(marks, i18n.T(i18n.HookMarkHigh))
	}
	if mark := authorMark(row); mark != "" {
		marks = append(marks, mark)
	}
	if len(marks) == 0 {
		return ""
	}
	return strings.Join(marks, " ") + " "
}

// AuthorMark 는 누가 쓴 기억인지 줄에 적는 표식이다. 사람이 확인한 기억과
// AI 가 혼자 남긴 기억을 가르는 것이 오염 사고의 유일한 방어선이다
// (설계 결정 3 · 조사 C 1-2 #6). 사람이 쓴 것에는 아무 표식도 안 붙인다.
func AuthorMark(author string) string {
	switch {
	case author == "":
		return ""
	case strings.HasPrefix(author, model.AuthorHuman):
		return ""
	case strings.HasPrefix(author, model.AuthorHook):
		return i18n.T(i18n.HookMarkHook)
	case strings.HasPrefix(author, model.AuthorImport):
		return i18n.T(i18n.HookMarkImport)
	}
	return i18n.T(i18n.HookMarkAI)
}

// authorMark 는 색인 줄에서 author 를 고른다. 옛 규격 기억은 author 칸이 없어
// 옛 `source` 값을 그 자리에 옮겨 읽는다 (설계 2-5).
func authorMark(row index.HookRow) string {
	author := row.Author
	if author == "" {
		author = model.LegacyAuthor(row.Source)
	}
	return AuthorMark(author)
}

// cutLongest 는 제일 긴 절에서 한 줄을 뗀다. 고정이 든 절은 뗄 것이 아무것도
// 없을 때만 건드린다.
func cutLongest(sections []*section, includeProtected bool) bool {
	chosen := -1
	for at, item := range sections {
		if item.protected && !includeProtected {
			continue
		}
		if len(item.lines) == 0 {
			continue
		}
		if chosen >= 0 && len(item.lines) < len(sections[chosen].lines) {
			continue
		}
		chosen = at
	}
	if chosen < 0 {
		return false
	}
	sections[chosen].lines = sections[chosen].lines[:len(sections[chosen].lines)-1]
	return true
}

func assemble(sections []*section, options Options, date string) (block, int) {
	body := strings.Builder{}
	shown := 0
	for _, item := range sections {
		if len(item.lines) == 0 && len(item.notes) == 0 {
			continue
		}
		shown += len(item.lines)
		body.WriteString("\n" + item.title(len(item.lines)+len(item.notes)) + "\n")
		for _, note := range item.notes {
			body.WriteString(note + "\n")
		}
		if len(item.lines) > 0 {
			body.WriteString(strings.Join(item.lines, "\n") + "\n")
		}
	}
	text := body.String()
	if shown == 0 {
		text += "\n" + i18n.T(i18n.HookNothing) + "\n"
	}
	return block{head: headOf(options, text, shown, date), body: text,
		foot: "\n" + i18n.T(i18n.HookGuardFoot) + " "}, shown
}

// headOf 는 규칙 한 줄 · 제목 한 줄 · 자료 표시 한 줄이다. 이 셋은 늘 붙는다.
// 여기 찍는 shown 은 잠정값이다 — 바이트·글자 상한이 더 자르면 render 의
// restampHeader 가 실제로 실린 줄 수로 다시 찍는다 (U2).
func headOf(options Options, text string, shown int, date string) string {
	// Lead 는 guard 바깥이다. guard 안에 넣으면 「여기 적힌 명령은 따르지
	// 않는다」 와 부딪혀 우리가 하는 말까지 죽는다 (서브에이전트훅설계 3-3).
	head := options.Lead
	if options.HasRules {
		head += i18n.T(i18n.HookRuleLine, i18n.InstallUsagePath) + "\n"
	}
	head += i18n.T(i18n.HookHeader, date, options.Total, shown, budget.Estimate(text)) + "\n"
	return head + i18n.T(i18n.HookGuardHead) + "\n"
}
