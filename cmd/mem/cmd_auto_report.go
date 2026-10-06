package main

// `mem auto report` — 자동 쌓기 두 주 숫자를 한 번에 뽑는 읽기 전용 명령이다
// (자동쌓기A1후속 2절 표 · 7절). 통과선은 표시만 하고 판정하지 않는다. 종료 코드는 늘 0 이다.

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/retain"
	"github.com/mirusona/officina-ai-memory-tool/internal/safe"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// reportSampleDefault 는 잡음률 표본 기본 건수다. reportTitleClip 은 표본 제목 글자 상한이다.
const (
	reportSampleDefault = 30
	reportTitleClip     = 60
	// logSentinel 은 i18n 문구를 쪼개 줄 꼴을 얻는 자리표다 — 문구가 바뀌어도 같이 간다.
	logSentinel = "\x00"
)

// autoReportJSON 은 `--json` 출력이다. 분모가 0 이면 비율 칸은 null 이다.
type autoReportJSON struct {
	Since        string         `json:"since"`
	Until        string         `json:"until"`
	Repo         string         `json:"repo"`
	Nudges       int            `json:"nudges"`
	NudgeSession int            `json:"nudge_sessions"`
	AddSessions  int            `json:"sessions_with_auto_add"`
	AddRate      *float64       `json:"auto_add_rate"`
	Rejects      map[string]int `json:"rejects"`
	Undone       int            `json:"undone"`
	AutoCount    int            `json:"auto_memories"`
	UndoRate     *float64       `json:"undo_rate"`
	Queued       int            `json:"queued"`
	Missed       int            `json:"missed"`
	MissedBy     map[string]int `json:"missed_by_reason"`
	R3Rejects    int            `json:"r3_rejects"`
	R3Skipped    int            `json:"r3_skipped"`
	WarnSince    string         `json:"warn_since"`
	Seed         uint64         `json:"seed"`
	Sample       []autoRow      `json:"sample"`
}

func autoReport(parsed *options, filter autoFilter) int {
	sample, seed, code := reportKnobs(parsed)
	if code != exitOK {
		return code
	}
	_, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	state := retain.LoadState(opened.Dir)
	filter.Since, filter.Until = reportPeriod(filter, state)
	report := autoReportJSON{Since: filter.Since, Until: filter.Until, Repo: filepath.Base(opened.Dir), Seed: seed}
	report.Nudges, report.NudgeSession, report.AddSessions = reportNudges(state, filter)
	report.AddRate = ratio(report.AddSessions, report.NudgeSession)
	logText, _ := store.ReadLog(opened.Dir)
	report.Rejects, report.R3Skipped, report.WarnSince = reportLog(logText, filter)
	report.R3Rejects = report.Rejects[retain.RuleSupport]
	memories, _ := autoMemories(opened)
	matched := []*model.Memory{}
	for _, memory := range memories {
		if filter.matches(memory) {
			matched = append(matched, memory)
		}
	}
	report.Undone, report.AutoCount = reportUndo(opened.Dir, filter, matched)
	report.UndoRate = ratio(report.Undone, report.AutoCount)
	report.Queued, report.Missed, report.MissedBy = reportMissed(opened.Dir, filter)
	report.Sample = reportSample(matched, sample, seed)
	if parsed.flags["json"] {
		return printJSON(report)
	}
	printAutoReport(report)
	return exitOK
}

// reportKnobs 는 --sample · --seed 를 읽는다. --seed 가 없으면 시각으로 정하고 화면에 찍는다.
func reportKnobs(parsed *options) (int, uint64, int) {
	sample := reportSampleDefault
	if text := parsed.text("sample"); text != "" {
		number, err := strconv.Atoi(text)
		if err != nil || number < 0 {
			return 0, 0, fail(i18n.T(i18n.AutoUsage))
		}
		sample = number
	}
	seed := uint64(time.Now().UnixNano() % 1000000)
	if text := parsed.text("seed"); text != "" {
		number, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return 0, 0, fail(i18n.T(i18n.AutoUsage))
		}
		seed = uint64(number)
	}
	return sample, seed, exitOK
}

// reportPeriod 는 기간을 정한다. 안 주면 retain-state 의 가장 이른 날 ~ 오늘이다.
func reportPeriod(filter autoFilter, state retain.State) (string, string) {
	today := time.Now().Format("2006-01-02")
	since, until := filter.Since, filter.Until
	if until == "" {
		until = today
	}
	if since == "" {
		since = today
		for _, session := range state.Sessions {
			if day := dayOf(session.Seen); day != "" && day < since {
				since = day
			}
		}
	}
	return since, until
}

// dayOf 는 RFC3339 시각(또는 날짜로 시작하는 글)의 날짜 10자다. 못 읽으면 빈 글이다.
func dayOf(at string) string {
	if len(at) < 10 || !model.IsDate(at[:10]) {
		return ""
	}
	return at[:10]
}

func inPeriod(day string, filter autoFilter) bool {
	return day != "" && day >= filter.Since && day <= filter.Until
}

// reportNudges 는 기간 안에 본 세션의 알림 합 · 알림 받은 세션 수 · 그 가운데 자동 add 가 있는 세션 수다.
func reportNudges(state retain.State, filter autoFilter) (nudges, sessions, adds int) {
	for _, session := range state.Sessions {
		if session == nil || !inPeriod(dayOf(session.Seen), filter) || session.Nudges == 0 {
			continue
		}
		nudges += session.Nudges
		sessions++
		if session.AutoAdds > 0 {
			adds++
		}
	}
	return nudges, sessions, adds
}

// logShape 는 i18n 문구를 자리표로 쪼갠 조각이다. [0] 이 머리, [1] 이 칸 사이 구분이다.
func logShape(key i18n.Key, slots int) []string {
	args := make([]any, slots)
	for index := range args {
		args[index] = logSentinel
	}
	return strings.Split(i18n.T(key, args...), logSentinel)
}

// reportLog 는 log.md 에서 기간 안 자동 거절을 규칙별로, R3 건너뜀 경고를 센다.
// 경고를 처음 남긴 날도 돌려준다 — 그 전 몫은 stderr 로만 나가 되살릴 수 없다.
func reportLog(text string, filter autoFilter) (map[string]int, int, string) {
	rejected := logShape(i18n.AutoLogRejected, 3)
	warned := logShape(i18n.AutoLogWarned, 2)
	skipped := []string{i18n.T(i18n.AutoJudgeSkipped), logShape(i18n.AutoJudgeSkippedWhy, 1)[0]}
	rejects := map[string]int{}
	skips, warnSince, day := 0, "", ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "## ") {
			day = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		if body, ok := strings.CutPrefix(line, "- "+store.LogRejected+" "+rejected[0]); ok && inPeriod(day, filter) {
			fields := strings.SplitN(body, rejected[1], 3)
			if len(fields) >= 2 && originMatches(fields[0], filter) {
				rejects[fields[1]]++
			}
		}
		body, ok := strings.CutPrefix(line, "- "+store.LogWarned+" "+warned[0])
		if !ok {
			continue
		}
		if warnSince == "" {
			warnSince = day
		}
		fields := strings.SplitN(body, warned[1], 2)
		if len(fields) == 2 && inPeriod(day, filter) && originMatches(fields[0], filter) &&
			(strings.HasPrefix(fields[1], skipped[0]) || strings.HasPrefix(fields[1], skipped[1])) {
			skips++
		}
	}
	if warnSince == "" {
		warnSince = time.Now().Format("2006-01-02")
	}
	return rejects, skips, warnSince
}

// originMatches 는 log 줄의 origin 이 --origin 거름에 맞는지다 — autoFilter.matches 와 같은 뜻.
func originMatches(origin string, filter autoFilter) bool {
	return filter.Origin == "" || origin == filter.Origin || strings.HasPrefix(origin, filter.Origin+":")
}

// reportUndo 는 기간 안 undo 기록 **전부**(닫힌 것 포함)에서 되살리지 않은 id 수와 분모다.
// 분모는 기간 안 자동 기억에 되돌려 목록에서 빠진 id 를 더한 것이다.
func reportUndo(dir string, filter autoFilter, matched []*model.Memory) (int, int) {
	entries, err := os.ReadDir(undoDir(dir))
	if err != nil {
		return 0, len(matched)
	}
	known := map[string]bool{}
	for _, memory := range matched {
		known[memory.ID] = true
	}
	undone := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(undoDir(dir), entry.Name()))
		record := undoRecord{}
		if err != nil || json.Unmarshal(data, &record) != nil || !inPeriod(dayOf(record.At), filter) {
			continue
		}
		if record.Redone != "" {
			continue
		}
		for _, id := range record.IDs {
			if !record.redone(id) {
				undone[id] = true
			}
		}
	}
	total := len(known)
	for id := range undone {
		if !known[id] {
			total++
		}
	}
	return len(undone), total
}

// reportMissed 는 기간 안 큐 항목 수와 covered 아닌 것의 사유별 수다.
func reportMissed(dir string, filter autoFilter) (int, int, map[string]int) {
	byReason := map[string]int{}
	entries, err := os.ReadDir(retain.QueueDir(dir))
	if err != nil {
		return 0, 0, byReason
	}
	queued, missed := 0, 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(retain.QueueDir(dir), entry.Name()))
		one := retain.QueueEntry{}
		if err != nil || json.Unmarshal(data, &one) != nil || !inPeriod(dayOf(one.At), filter) {
			continue
		}
		queued++
		if !one.Covered {
			missed++
			byReason[one.Reason]++
		}
	}
	return queued, missed, byReason
}

// reportSample 은 id 순으로 줄 세운 뒤 씨앗으로 섞어 앞 N 건을 고른다. 같은 씨앗이면 같은 목록이다.
func reportSample(matched []*model.Memory, size int, seed uint64) []autoRow {
	pool := append([]*model.Memory(nil), matched...)
	sort.Slice(pool, func(a, b int) bool { return pool[a].ID < pool[b].ID })
	shuffler := rand.New(rand.NewPCG(seed, 0))
	shuffler.Shuffle(len(pool), func(a, b int) { pool[a], pool[b] = pool[b], pool[a] })
	if size < len(pool) {
		pool = pool[:size]
	}
	rows := []autoRow{}
	for _, memory := range pool {
		rows = append(rows, autoRow{ID: memory.ID, Origin: memory.Origin, Session: memory.OriginSession,
			Date: memory.Date, Type: memory.Type, Title: safe.Clip(safe.OneLine(memory.Title), reportTitleClip), Held: memory.Review})
	}
	return rows
}

func ratio(part, whole int) *float64 {
	if whole == 0 {
		return nil
	}
	value := float64(part) / float64(whole)
	return &value
}

// percent 는 비율을 글로 찍는다. 분모가 0 이면 「—」 다.
func percent(value *float64, digits int) string {
	if value == nil {
		return "—"
	}
	return strconv.FormatFloat(*value*100, 'f', digits, 64) + "%"
}

// countsLine 은 「이름 수 · 이름 수」 꼴이다. 비면 「없음」.
func countsLine(counts map[string]int) string {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := []string{}
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s %d", name, counts[name]))
	}
	if len(parts) == 0 {
		return i18n.T(i18n.AutoReportNone)
	}
	return strings.Join(parts, " · ")
}

func printAutoReport(report autoReportJSON) {
	fmt.Println(i18n.T(i18n.AutoReportPeriod, report.Since, report.Until, report.Repo))
	fmt.Println(i18n.T(i18n.AutoReportNudges, report.Nudges, report.NudgeSession, report.AddSessions,
		report.NudgeSession, percent(report.AddRate, 0)))
	fmt.Println(i18n.T(i18n.AutoReportRejects, countsLine(report.Rejects)))
	fmt.Println(i18n.T(i18n.AutoReportUndo, report.Undone, report.AutoCount, percent(report.UndoRate, 1)))
	fmt.Println(i18n.T(i18n.AutoReportMissed, report.Queued, report.Missed, countsLine(report.MissedBy)))
	fmt.Println(i18n.T(i18n.AutoReportR3, report.R3Rejects, report.R3Skipped, report.WarnSince))
	fmt.Println(i18n.T(i18n.AutoReportHook))
	fmt.Println(i18n.T(i18n.AutoReportSearch))
	fmt.Println(i18n.T(i18n.AutoReportSample, len(report.Sample), report.Seed))
	for index, row := range report.Sample {
		held := ""
		if row.Held {
			held = heldTag + " "
		}
		fmt.Println(i18n.T(i18n.AutoReportSampleRow, index+1, row.ID, held, safe.OneLine(row.Origin), safe.OneLine(row.Date), row.Title))
	}
}
