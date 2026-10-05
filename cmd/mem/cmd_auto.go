package main

// mem auto list|undo|redo — 자동으로 들어온 기억(origin 칸이 있는 것)을 보고,
// 한 번에 보류로 돌리고, 되살린다 (자동쌓기설계 2-4).
//
// undo 는 파일을 안 지운다. `review: true` 를 다는 덮어쓰기 표를 큐에 넣어 검색·훅에서
// 뺄 뿐이다. 그래서 redo 는 같은 칸을 떼면 끝이다. 무엇을 돌렸는지는
// Memory/local/auto-undo/ 에 기록으로 남긴다.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// dry-run 은 받기만 한다 — 미리보기가 기본이라 --apply 가 없으면 늘 미리보기다.
var autoBools = []string{"json", "apply", "dry-run"}
var autoValues = []string{"origin", "since", "until", "session", "repo"}

// autoListFormat 은 list 한 줄이다 — id · origin · 세션 · 날짜 · 제목.
const autoListFormat = "%s  %-16s %-8s %s  %s"

// undoDirName 은 undo 기록 폴더다. Memory/local 아래라 git 에 안 들어간다.
const undoDirName = "auto-undo"

func init() {
	register(command{name: "auto", run: runAuto, bools: autoBools, values: autoValues})
}

// runAuto 는 첫 낱말로 갈래를 고른다.
func runAuto(argv []string) int {
	if len(argv) == 0 {
		return fail(i18n.T(i18n.AutoUsage))
	}
	parsed, err := parseOptions(argv[1:], autoBools, autoValues)
	if err != nil {
		return fail(err.Error())
	}
	if len(parsed.rest) > 0 {
		return fail(i18n.T(i18n.AutoUsage))
	}
	filter, code := autoFilterOf(parsed)
	if code != exitOK {
		return code
	}
	switch argv[0] {
	case "list":
		return autoList(parsed, filter)
	case "undo":
		return autoUndo(parsed, filter)
	case "redo":
		return autoRedo(parsed, filter)
	}
	return fail(i18n.T(i18n.AutoUsage))
}

// autoFilter 는 거름이다. 날짜는 기억의 date 칸(하루 단위)으로 견준다.
type autoFilter struct {
	Origin  string `json:"origin,omitempty"`
	Since   string `json:"since,omitempty"`
	Until   string `json:"until,omitempty"`
	Session string `json:"session,omitempty"`
}

func autoFilterOf(parsed *options) (autoFilter, int) {
	filter := autoFilter{Origin: parsed.text("origin"), Since: parsed.text("since"),
		Until: parsed.text("until"), Session: parsed.text("session")}
	for _, day := range []string{filter.Since, filter.Until} {
		if day != "" && !model.IsDate(day) {
			return filter, fail(i18n.T(i18n.AutoBadDate, day))
		}
	}
	return filter, checkSessionPrefix(filter.Session)
}

func (f autoFilter) empty() bool {
	return f.Origin == "" && f.Since == "" && f.Until == "" && f.Session == ""
}

// matches 는 자동 기억 하나가 거름에 맞는지다. `--origin retain` 은 retain:* 를 다 고른다.
func (f autoFilter) matches(memory *model.Memory) bool {
	if memory.Origin == "" {
		return false
	}
	if f.Origin != "" && memory.Origin != f.Origin && !strings.HasPrefix(memory.Origin, f.Origin+":") {
		return false
	}
	if f.Since != "" && memory.Date < f.Since {
		return false
	}
	if f.Until != "" && memory.Date > f.Until {
		return false
	}
	if f.Session == "" {
		return true
	}
	// 기억에는 앞 8자만 남는다. 거름은 8자로도, 전체 id 로도 줄 수 있다.
	short := memory.OriginSession
	return short != "" && (strings.HasPrefix(f.Session, short) || strings.HasPrefix(short, f.Session))
}

func (f autoFilter) String() string {
	parts := []string{}
	for _, pair := range [][2]string{{"origin", f.Origin}, {"since", f.Since}, {"until", f.Until}, {"session", f.Session}} {
		if pair[1] != "" {
			parts = append(parts, pair[0]+"="+pair[1])
		}
	}
	return strings.Join(parts, ",")
}

// autoMemories 는 저장소의 기억을 다 읽는다. 색인을 안 연다 — 보류된 것도 봐야 한다.
func autoMemories(opened *store.Store) ([]*model.Memory, error) {
	files, err := opened.ListMemories()
	if err != nil {
		return nil, err
	}
	memories := []*model.Memory{}
	for _, file := range files {
		one, err := opened.ReadListed(file)
		if err != nil || one.Memory == nil {
			continue
		}
		memories = append(memories, one.Memory)
	}
	sort.Slice(memories, func(a, b int) bool { return memories[a].ID < memories[b].ID })
	return memories, nil
}

// autoRow 는 list `--json` 한 줄이다.
type autoRow struct {
	ID      string `json:"id"`
	Origin  string `json:"origin"`
	Session string `json:"origin_session,omitempty"`
	Date    string `json:"date"`
	Type    string `json:"type"`
	Title   string `json:"title"`
	Held    bool   `json:"held"`
}

func autoList(parsed *options, filter autoFilter) int {
	_, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	memories, err := autoMemories(opened)
	if err != nil {
		return fail(err.Error())
	}
	rows := []autoRow{}
	for _, memory := range memories {
		if filter.matches(memory) {
			rows = append(rows, autoRow{ID: memory.ID, Origin: memory.Origin, Session: memory.OriginSession,
				Date: memory.Date, Type: memory.Type, Title: memory.Title, Held: memory.Review})
		}
	}
	if parsed.flags["json"] {
		return printJSON(rows)
	}
	if len(rows) == 0 {
		fmt.Println(i18n.T(i18n.AutoListEmpty))
		return exitOK
	}
	for _, row := range rows {
		title := row.Title
		if row.Held {
			title = heldTag + " " + title
		}
		fmt.Printf(autoListFormat+"\n", row.ID, row.Origin, row.Session, row.Date, title)
	}
	fmt.Println(i18n.T(i18n.AutoListTotal, len(rows)))
	return exitOK
}

// heldTag 는 list 에서 이미 보류된 기억 앞에 붙는다.
const heldTag = "[보류]"

// undoRecord 는 undo 한 번의 기록이다. redo 가 이것만 보고 되살린다.
type undoRecord struct {
	At     string     `json:"at"`
	Filter autoFilter `json:"filter"`
	IDs    []string   `json:"ids"`
	Redone string     `json:"redone,omitempty"`
	// RedoneIDs 는 거름을 준 redo 가 일부만 되살린 id 다. IDs 가 다 여기 들면 Redone 을 적어 닫는다.
	RedoneIDs []string `json:"redone_ids,omitempty"`
}

// redone 은 이 기록에서 id 가 이미 되살아났는지다.
func (r undoRecord) redone(id string) bool {
	for _, one := range r.RedoneIDs {
		if one == id {
			return true
		}
	}
	return false
}

func autoUndo(parsed *options, filter autoFilter) int {
	if filter.empty() {
		return fail(i18n.T(i18n.AutoNoFilter))
	}
	repository, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	memories, err := autoMemories(opened)
	if err != nil {
		return fail(err.Error())
	}
	targets := []*model.Memory{}
	for _, memory := range memories {
		if filter.matches(memory) && !memory.Review {
			targets = append(targets, memory)
		}
	}
	if len(targets) == 0 {
		fmt.Println(i18n.T(i18n.AutoUndoNothing))
		return exitOK
	}
	fmt.Println(i18n.T(i18n.AutoUndoPlan, len(targets)))
	for _, memory := range targets {
		fmt.Printf(autoListFormat+"\n", memory.ID, memory.Origin, memory.OriginSession, memory.Date, memory.Title)
		if linked := linkedTo(memories, memory.ID); len(linked) > 0 {
			fmt.Println(i18n.T(i18n.AutoUndoLinked, memory.ID, len(linked), strings.Join(linked, " ")))
		}
	}
	if !parsed.flags["apply"] || parsed.flags["dry-run"] {
		fmt.Println(i18n.T(i18n.AutoUndoDryRun))
		return exitOK
	}
	ids := []string{}
	for _, memory := range targets {
		ids = append(ids, memory.ID)
	}
	if code := patchReview(repository, opened, ids, true); code != exitOK {
		return code
	}
	record := undoRecord{At: time.Now().Format(time.RFC3339), Filter: filter, IDs: ids}
	path, err := writeUndoRecord(opened.Dir, record)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
	}
	noteLog(opened, store.LogAutoUndo, i18n.T(i18n.AutoLogUndo, len(ids), filter.String(), filepath.Base(path)))
	fmt.Println(i18n.T(i18n.AutoUndoDone, len(ids), filepath.Base(path)))
	return exitOK
}

// linkedTo 는 id 를 근거(mem:)나 링크로 가리키는 다른 기억이다. undo 해도 그
// 기억들은 그대로라, 사람이 끊긴 근거를 알고 넘어가게 보여 준다.
func linkedTo(memories []*model.Memory, id string) []string {
	found := []string{}
	for _, memory := range memories {
		if memory.ID == id {
			continue
		}
		if refersTo(memory, id) {
			found = append(found, memory.ID)
		}
	}
	return found
}

func refersTo(memory *model.Memory, id string) bool {
	for _, source := range memory.Sources {
		if strings.HasPrefix(source, model.SourceMem) && strings.TrimSpace(strings.TrimPrefix(source, model.SourceMem)) == id {
			return true
		}
	}
	for _, link := range memory.Links {
		if strings.Contains(link, id) {
			return true
		}
	}
	return false
}

// patchReview 는 review 칸을 바꾸는 덮어쓰기 표를 넣고 그 자리에서 승격한다 —
// 끝나면 검색에서 바로 빠진다(또는 돌아온다). 락을 못 잡으면 다음 색인 때 먹는다.
func patchReview(repository *config.Repository, opened *store.Store, ids []string, held bool) int {
	if err := opened.EnsureDirs(); err != nil {
		return fail(err.Error())
	}
	names := []string{}
	for _, id := range ids {
		name, err := opened.WritePatch(id, map[string]any{"review": held})
		if err != nil {
			return fail(err.Error())
		}
		names = append(names, name)
	}
	promoteNow(repository, opened, names)
	return exitOK
}

func undoDir(dir string) string { return filepath.Join(store.LocalDir(dir), undoDirName) }

func writeUndoRecord(dir string, record undoRecord) (string, error) {
	if err := os.MkdirAll(undoDir(dir), 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(undoDir(dir), time.Now().Format("20060102-150405.000000000")+".json")
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o644)
}

// undoFile 은 읽어 둔 undo 기록 하나와 그 자리다.
type undoFile struct {
	Path   string
	Record undoRecord
}

// openUndoRecords 는 아직 되살리지 않은 기록이다. `--since` 를 주면 그 날 이후 기록만이다.
func openUndoRecords(dir string, since string) []undoFile {
	entries, err := os.ReadDir(undoDir(dir))
	if err != nil {
		return nil
	}
	files := []undoFile{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(undoDir(dir), entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		record := undoRecord{}
		if json.Unmarshal(data, &record) != nil || record.Redone != "" {
			continue
		}
		if since != "" && record.At < since {
			continue
		}
		files = append(files, undoFile{Path: path, Record: record})
	}
	return files
}

// autoRedo 는 undo 기록의 기억을 되살린다. `--since` 는 undo 기록 시각으로, `--origin`·
// `--session`·`--until` 은 undo 와 같은 뜻으로 기억마다 거른다 — 받아 놓고 무시하면
// `--apply` 가 거름과 상관없이 다 되살린다 (리뷰 2026-10-05). 버린(cold) 기억은 건너뛴다.
func autoRedo(parsed *options, filter autoFilter) int {
	repository, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	files := openUndoRecords(opened.Dir, filter.Since)
	memories, err := autoMemories(opened)
	if err != nil {
		return fail(err.Error())
	}
	byID := map[string]*model.Memory{}
	for _, memory := range memories {
		byID[memory.ID] = memory
	}
	perMemory := filter
	perMemory.Since = ""
	ids, cold := redoTargets(files, byID, perMemory)
	for _, id := range cold {
		fmt.Println(i18n.T(i18n.AutoRedoCold, id, id))
	}
	if len(ids) == 0 {
		fmt.Println(i18n.T(i18n.AutoRedoNothing))
		return exitOK
	}
	fmt.Println(i18n.T(i18n.AutoRedoPlan, len(ids), len(files)))
	for _, id := range ids {
		fmt.Println("  " + id)
	}
	if !parsed.flags["apply"] || parsed.flags["dry-run"] {
		fmt.Println(i18n.T(i18n.AutoRedoDryRun))
		return exitOK
	}
	if code := patchReview(repository, opened, ids, false); code != exitOK {
		return code
	}
	closeUndoRecords(opened, files, ids)
	fmt.Println(i18n.T(i18n.AutoRedoDone, len(ids)))
	return exitOK
}

// redoTargets 는 거름에 맞는 되살릴 id 와 건너뛸 cold id 다. 기억 파일을 못 찾은 id 는
// 기억 칸으로 거를 수 없으니 거름이 없을 때만 되살린다.
func redoTargets(files []undoFile, byID map[string]*model.Memory, filter autoFilter) ([]string, []string) {
	ids, cold := []string{}, []string{}
	seen := map[string]bool{}
	for _, file := range files {
		for _, id := range file.Record.IDs {
			if seen[id] || file.Record.redone(id) {
				continue
			}
			seen[id] = true
			memory := byID[id]
			switch {
			case memory == nil && !filter.empty():
				continue
			case memory != nil && !filter.empty() && !filter.matches(memory):
				continue
			case memory != nil && memory.State == index.StateCold:
				cold = append(cold, id)
				continue
			}
			ids = append(ids, id)
		}
	}
	return ids, cold
}

// closeUndoRecords 는 되살린 id 를 기록에서 지운다. 기록의 id 가 다 되살아났으면
// 「되살림」 시각을 적어 닫는다 — 거름으로 일부만 되살렸으면 나머지는 다음 redo 몫이다.
func closeUndoRecords(opened *store.Store, files []undoFile, redone []string) {
	done := map[string]bool{}
	for _, id := range redone {
		done[id] = true
	}
	now := time.Now().Format(time.RFC3339)
	for _, file := range files {
		count, left := 0, 0
		for _, id := range file.Record.IDs {
			switch {
			case done[id]:
				count++
				file.Record.RedoneIDs = append(file.Record.RedoneIDs, id)
			case !file.Record.redone(id):
				left++
			}
		}
		if count == 0 {
			continue
		}
		if left == 0 {
			file.Record.Redone = now
		}
		data, err := json.MarshalIndent(file.Record, "", "  ")
		if err == nil {
			err = os.WriteFile(file.Path, data, 0o644)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
		}
		noteLog(opened, store.LogAutoRedo, i18n.T(i18n.AutoLogRedo, count, filepath.Base(file.Path)))
	}
}
