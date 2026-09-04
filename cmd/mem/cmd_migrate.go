package main

// mem migrate — 옛 규격(v0.1) 기억을 v0.2 규격으로 옮긴다 (설계 2-5).
//
// 규격을 올리면 옛 기억이 한 건도 Validate 를 못 지나고, 불변조건 I7 이
// 「통과한 것만 색인한다」라 저장소가 통째로 안 보인다. 그래서 명령 하나로
// 한 번에 올린다.
//
//	기본        --dry-run (무엇을 고칠지만 보여준다)
//	--apply     진짜로 고친다. 고치기 전에 원본을 아카이브에 통째로 남긴다
//	--restore   그 아카이브에서 되돌린다
//
// 어떻게 쓰나 : store/ 를 직접 안 고치고 inbox 큐에 patch 를 넣는다. store/ 를
// 쓰는 것은 락을 잡은 `mem index` 하나뿐이다 (불변조건 2).

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/safe"
	"github.com/mirusona/officina-ai-memory-tool/internal/secret"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

var migrateBools = []string{"apply", "dry-run", "restore", "json"}
var migrateValues = []string{"repo", "author"}

// 아래 둘은 한글이 없는 짜임새용 서식이라 i18n 표에 안 둔다.
const (
	migrateRowFormat  = "  %s : %d건"
	migrateHandFormat = "  %s  %s"
)

// migrateDirSuffix 는 원본을 남기는 아카이브 폴더 꼬리다.
const migrateDirSuffix = "-migrate"

// noteMigrated 는 근거를 못 찾은 기억에 넣는 최소값이다. `note:` 뿐이면
// 규칙 F18 이 경고를 내는데, 그것이 4번 결정이 겨눈 바다 (설계 2-5).
// 글은 `model` 에 있다 — 이웃 링크가 이 값을 「근거 없음」으로 알아봐야 한다
// (v0.4 리뷰 A R5).
const noteMigrated = model.NoteMigrated

func init() {
	register(command{name: "migrate", run: runMigrate, bools: migrateBools, values: migrateValues})
}

// plan 은 기억 한 건을 어떻게 옮길지다.
type plan struct {
	ID    string         `json:"id"`
	Path  string         `json:"path"`
	Set   map[string]any `json:"set"`
	Kinds []string       `json:"kinds"`
	// Todo 는 옮기고 나서도 **사람이 마저 써야 하는 칸**이다. 도구가 지어내면
	// 안 되는 것만 여기 온다 (지금은 title 하나).
	Todo []string `json:"todo,omitempty"`
	// Left 는 이번에 **못 옮긴 칸**이다. 한 칸이 막혔다고 나머지를 버리지
	// 않는다 — 옮길 수 있는 칸은 옮기고 막힌 칸만 여기 적는다 (조사 E #6).
	Left   []string `json:"left,omitempty"`
	Reason string   `json:"reason,omitempty"`
}

// Partial 은 칸별로만 옮기는 계획인지다. 막힌 칸이 있으면 `migrated` 를 안
// 켠다 — 켜면 v0.2 자로 검사해서 승격이 통째로 거절된다 (불변조건 I7).
func (p plan) Partial() bool { return len(p.Left) > 0 }

// migrateReport 는 --json 이 내는 것 전부다.
type migrateReport struct {
	Total int `json:"total"`
	Auto  int `json:"auto"`
	// Part 는 칸별로만 옮긴 건수다 (설계 6절 · 조사 E #6).
	Part   int            `json:"part"`
	ByHand int            `json:"by_hand"`
	Kinds  map[string]int `json:"kinds"`
	// Todos 는 이전 뒤 사람이 마저 쓸 칸의 건수다.
	Todos  map[string]int `json:"todos"`
	Plans  []plan         `json:"plans"`
	Parts  []plan         `json:"parts"`
	Hands  []plan         `json:"hands"`
	Backup string         `json:"backup,omitempty"`
}

func runMigrate(argv []string) int {
	parsed, err := parseOptions(argv, migrateBools, migrateValues)
	if err != nil {
		return fail(err.Error())
	}
	repository, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	if parsed.flags["restore"] {
		return restoreMigrate(opened)
	}
	report, err := planMigrate(repository, opened, parsed)
	if err != nil {
		return exitFor(err)
	}
	// 이전은 기억 파일을 통째로 다시 쓴다. 비밀정보가 든 채로 다시 쓰면 그
	// 값이 색인과 훅 블록까지 흘러간다 — 세 방어선 중 하나다 (설계 10절 · P8).
	if code := refuseSecretMigrate(repository, opened, report); code != exitOK {
		return code
	}
	if parsed.flags["json"] && !parsed.flags["apply"] {
		return printMigrateJSON(report)
	}
	if report.Total == 0 {
		fmt.Println(i18n.T(i18n.MigrateNothing))
		return exitOK
	}
	printMigrate(report)
	if !parsed.flags["apply"] {
		fmt.Println(i18n.T(i18n.MigrateDryRun))
		return exitOK
	}
	return applyMigrate(opened, report, parsed)
}

// planMigrate 는 옛 규격 기억을 다 훑어 계획을 세운다. 아무것도 안 고친다.
func planMigrate(repository *config.Repository, opened *store.Store, parsed *options) (*migrateReport, error) {
	vocab := vocabOf(repository)
	human := humanAuthor(parsed.text("author"))
	files, err := opened.ListMemories()
	if err != nil {
		return nil, err
	}
	report := &migrateReport{Kinds: map[string]int{}, Todos: map[string]int{}}
	for _, file := range files {
		one, err := opened.ReadListed(file)
		if err != nil {
			continue
		}
		memory := one.Memory
		if !memory.IsLegacy() || memory.Migrated {
			continue
		}
		report.Total++
		item := planOne(memory, file.Path, vocab, human)
		switch {
		case len(item.Set) == 0:
			// 옮길 칸이 하나도 없다. 사람이 손대야 한다.
			report.ByHand++
			report.Hands = append(report.Hands, item)
			continue
		case item.Partial():
			report.Part++
			report.Parts = append(report.Parts, item)
		default:
			report.Auto++
			report.Plans = append(report.Plans, item)
		}
		for _, kind := range item.Kinds {
			report.Kinds[kind]++
		}
		for _, kind := range item.Todo {
			report.Todos[kind]++
		}
	}
	return report, nil
}

// planOne 은 기억 한 건의 계획이다. **한 칸이 막혀도 나머지는 옮긴다.**
//
// 옛 판은 태그<2·scope 미등록에서 바로 `return` 해 이미 만든 `author`·
// `sources`·`todo_status` 를 통째로 버렸다 (실기억 206건에서 49건이 이렇게
// 사라졌다 · 조사 E #6). 이제 막힌 칸은 `Left` 에 적고 나머지는 그대로 옮긴다.
func planOne(memory *model.Memory, path string, vocab config.Vocab, human string) plan {
	item := plan{ID: memory.ID, Path: path, Set: map[string]any{}}
	next := *memory
	next.Spec = model.SpecV2
	next.Migrated = true

	next.Author = migratedAuthor(memory.LegacySource, human)
	item.Set["author"] = next.Author
	item.Kinds = append(item.Kinds, "author")

	// **제목은 지어내지 않는다** (리뷰 B). 요약 앞 40자를 베끼면 head 열에 새
	// 신호가 하나도 안 들어가 검색이 오히려 나빠지고(0.716→0.701) title-shape
	// 가 이전분 전량에 걸린다. 비워 두고 사람 몫으로 넘긴다 — 색인 head 열은
	// title 이 비면 요약으로 대신 채운다(model.DisplayTitle).
	if next.Title == "" {
		item.Todo = append(item.Todo, "title")
	}
	if memory.LegacyStatus != "" && memory.Type == model.TypeTodo {
		next.TodoStatus = memory.LegacyStatus
		item.Set["todo_status"] = next.TodoStatus
		item.Kinds = append(item.Kinds, "todo_status")
	}
	if len(next.Sources) == 0 {
		if found := sourcesFromBody(memory.Body); len(found) > 0 {
			next.Sources = found
			item.Set["sources"] = toAny(found)
			item.Kinds = append(item.Kinds, "sources")
		} else if vocab.TypeTable().Spec(memory.Type).Sources == model.SourcesRequired {
			next.Sources = []string{noteMigrated}
			item.Set["sources"] = toAny(next.Sources)
			item.Kinds = append(item.Kinds, "sources-note")
		}
	}
	next.Tags = planTags(memory, vocab, &item)
	planScope(memory, vocab, &next, &item)
	// 규격 검사는 마지막이다. 앞에서 이미 막힌 칸을 찾았으면 그 까닭을 덮지
	// 않는다 — 「태그가 1개뿐이다」가 「tags 칸이 없다」로 바뀌면 사람이 뭘
	// 해야 하는지 모른다.
	if problems := model.ValidateAs(&next, model.SpecV2); len(problems) > 0 && !item.Partial() {
		item.Reason = problems[0].Error()
		item.Left = append(item.Left, "규격")
	}
	return finishPlan(item)
}

// planTags 는 태그 칸이다. 출처 태그를 떼면 2개 미만이 되는 기억은 **안 뗀다**
// — 떼면 규격 미달이라 이전이 통째로 막히고, 붙어 있는 출처 태그는 F10 이
// 따로 말해 사람이 `mem tags --rename` 으로 고칠 수 있다. 도구가 태그를
// 지어내지 않는다는 원칙은 그대로다.
func planTags(memory *model.Memory, vocab config.Vocab, item *plan) []string {
	cleaned, changed := migrateTags(memory.Tags, vocab)
	if len(cleaned) >= tagsNeeded {
		if changed {
			item.Set["tags"] = toAny(cleaned)
			item.Kinds = append(item.Kinds, "tags")
		}
		return cleaned
	}
	if len(memory.Tags) >= tagsNeeded {
		item.Todo = append(item.Todo, "tags")
		return memory.Tags
	}
	item.Reason = i18n.T(i18n.MigrateReasonTag, len(cleaned))
	item.Left = append(item.Left, "tags")
	return cleaned
}

// tagsNeeded 는 v0.2 규격의 태그 하한이다.
const tagsNeeded = 2

// planScope 는 scope 칸이다. 표준 목록 밖이라는 것은 **규격 위반이 아니라
// 어휘 문제**(F12)라 이전을 막지 않는다. 별칭이면 조용히 치환한다.
func planScope(memory *model.Memory, vocab config.Vocab, next *model.Memory, item *plan) {
	scope, known, alias := vocab.NormalizeScope(memory.Scope)
	if alias {
		next.Scope = scope
		item.Set["scope"] = scope
		item.Kinds = append(item.Kinds, "scope")
	}
	if !known && !vocab.Learning() {
		item.Todo = append(item.Todo, "scope")
	}
}

// legacyOnlyFields 는 **옛 규격 파일에는 못 쓰는 칸**이다. `migrated` 를 못
// 켠 기억은 머리말이 `source:` 꼴로 남아서 `author` 를 써도 다시 읽을 때
// 사라진다 (model.Encode). 헛일을 큐에 넣지 않는다.
var legacyOnlyFields = []string{"author", "migrated"}

// finishPlan 은 막힌 칸이 있으면 `migrated` 를 빼고, 옛 규격에서 못 쓰는 칸도
// 같이 뺀다. 막힌 것이 없으면 규격을 올린다.
func finishPlan(item plan) plan {
	if !item.Partial() {
		item.Set["migrated"] = true
		return item
	}
	for _, key := range legacyOnlyFields {
		if _, found := item.Set[key]; found {
			delete(item.Set, key)
			item.Left = append(item.Left, key)
			item.Kinds = dropKind(item.Kinds, key)
		}
	}
	return item
}

func dropKind(kinds []string, name string) []string {
	out := kinds[:0]
	for _, kind := range kinds {
		if kind != name {
			out = append(out, kind)
		}
	}
	return out
}

// migrateTags 는 별칭을 치환하고 못 쓰는 태그(출처 태그)를 뗀다.
func migrateTags(tags []string, vocab config.Vocab) ([]string, bool) {
	out := []string{}
	changed := false
	for _, tag := range tags {
		if vocab.TagDenied(tag) {
			changed = true
			continue
		}
		fixed, _, alias := vocab.NormalizeTag(tag)
		if alias {
			changed = true
		}
		if !contains(out, fixed) {
			out = append(out, fixed)
			continue
		}
		changed = true
	}
	return out, changed
}

// migratedAuthor 는 옛 `source` 를 `author` 꼴로 옮긴다. `user` 만 사람
// 아이디가 필요해서 따로 받는다 (설계 2-5).
func migratedAuthor(source, human string) string {
	if source == model.LegacySourceUser {
		return human
	}
	return model.LegacyAuthor(source)
}

// humanAuthor 는 `--author` 나 git user.name 으로 사람 아이디를 만든다.
func humanAuthor(given string) string {
	if given != "" {
		if model.IsAuthor(given) {
			return given
		}
		return model.AuthorHuman + safeAuthorID(given)
	}
	out, err := exec.Command("git", "config", "user.name").Output()
	name := safeAuthorID(strings.TrimSpace(string(out)))
	if err != nil || name == "" {
		return model.AuthorHuman + "unknown"
	}
	return model.AuthorHuman + name
}

// safeAuthorID 는 `human:` 뒤에 들어갈 수 있는 글자만 남긴다.
func safeAuthorID(name string) string {
	out := strings.Builder{}
	for _, letter := range name {
		switch {
		case letter >= 'a' && letter <= 'z', letter >= 'A' && letter <= 'Z',
			letter >= '0' && letter <= '9', letter == '.', letter == '_', letter == '-':
			out.WriteRune(letter)
		default:
			out.WriteByte('-')
		}
	}
	trimmed := strings.Trim(out.String(), "-")
	if len(trimmed) > 64 {
		trimmed = trimmed[:64]
	}
	return trimmed
}

// sourcesFromBody 는 본문의 「근거 : …」 줄을 sources 로 옮긴다. v0.1 이
// 근거를 본문에 글로만 적어 둔 자리다 (조사 D 2-8).
func sourcesFromBody(body string) []string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "-*> \t"))
		if !strings.HasPrefix(line, "근거") {
			continue
		}
		_, rest, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		out := []string{}
		for _, piece := range strings.FieldsFunc(rest, func(r rune) bool { return r == '·' || r == ',' }) {
			if one := sourceEntry(strings.TrimSpace(piece)); one != "" {
				out = append(out, one)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

// sourceEntry 는 근거 한 조각에 접두를 붙인다. 꼴을 못 맞추면 note: 다.
func sourceEntry(text string) string {
	if text == "" {
		return ""
	}
	if model.SourceKind(text) != "" {
		if model.IsSource(text) {
			return text
		}
		return model.SourceNote + text
	}
	switch {
	case strings.HasPrefix(text, "http://"), strings.HasPrefix(text, "https://"):
		return model.SourceURL + text
	case strings.Contains(text, "/") && strings.Contains(text, "."):
		return model.SourceFile + text
	}
	return model.SourceNote + text
}

func printMigrate(report *migrateReport) {
	fmt.Println(i18n.T(i18n.MigrateHead, report.Total, report.Auto, report.ByHand))
	for _, kind := range sortedCountKeys(report.Kinds) {
		fmt.Printf(migrateRowFormat+"\n", kind, report.Kinds[kind])
	}
	printMigrateTodos(report)
	printMigrateParts(report)
	if len(report.Hands) == 0 {
		return
	}
	fmt.Println(i18n.T(i18n.MigrateHandHead))
	for _, item := range report.Hands {
		fmt.Printf(migrateHandFormat+"\n", item.ID, item.Reason)
	}
}

// printMigrateTodos 는 옮기고 나서도 사람이 써야 할 칸이다. 도구가 지어내면
// 안 되는 것이라 건수와 다음 명령만 말한다.
func printMigrateTodos(report *migrateReport) {
	if report.Todos["title"] == 0 {
		return
	}
	fmt.Printf("  제목은 안 채웠다 : %d건 — 요약을 베낀 제목은 검색을 나쁘게 한다\n", report.Todos["title"])
	fmt.Println("  → mem review --kind title   (사람이 제목을 쓴다)")
}

// printMigrateParts 는 칸별로만 옮기는 것이다. 무엇이 남는지 안 말하면 사람이
// 「다 됐다」고 읽는다.
func printMigrateParts(report *migrateReport) {
	if len(report.Parts) == 0 {
		return
	}
	fmt.Println(i18n.T(i18n.MigratePartHead, len(report.Parts)))
	for at, item := range report.Parts {
		if at >= migratePartMax {
			fmt.Println("  … 외", len(report.Parts)-migratePartMax, "건")
			break
		}
		fmt.Println(i18n.T(i18n.MigratePartRow, item.ID,
			strings.Join(item.Left, "·"), item.Reason))
	}
}

// migratePartMax 는 부분 이전을 몇 줄까지 찍을지다. 200건을 다 찍으면 아무도
// 안 읽는다.
const migratePartMax = 10

func printMigrateJSON(report *migrateReport) int {
	data, err := json.Marshal(report)
	if err != nil {
		return fail(err.Error())
	}
	fmt.Println(string(data))
	return exitOK
}

// applyMigrate 는 원본을 아카이브에 남기고 고칠 것을 큐에 넣는다.
func applyMigrate(opened *store.Store, report *migrateReport, parsed *options) int {
	release, got, err := index.TryLock(opened.Dir)
	if err != nil {
		return exitFor(err)
	}
	if !got {
		fmt.Fprintln(os.Stderr, i18n.T(i18n.MigrateLocked))
		return exitLocked
	}
	backup, err := backupStore(opened, time.Now())
	release()
	if err != nil {
		return exitFor(err)
	}
	report.Backup = backup
	fmt.Println(i18n.T(i18n.MigrateBackedUp, backup))
	if err := opened.EnsureDirs(); err != nil {
		return exitFor(err)
	}
	queued := 0
	for _, item := range append(append([]plan{}, report.Plans...), report.Parts...) {
		if len(item.Set) == 0 {
			continue
		}
		if _, err := opened.WritePatch(item.ID, item.Set); err != nil {
			return exitFor(err)
		}
		queued++
	}
	fmt.Println(i18n.T(i18n.MigrateApplied, queued))
	if queued >= queueNoticeMin {
		fmt.Println(safe.Text(i18n.T(i18n.QueueWait, queued)))
	}
	noteLog(opened, store.LogMigrated, fmt.Sprintf("%d건 (칸별 %d건, 사람 손 %d건, 원본 %s)",
		len(report.Plans), report.Part, report.ByHand, filepath.Base(backup)))
	if parsed.flags["json"] {
		return printMigrateJSON(report)
	}
	return exitOK
}

// refuseSecretMigrate 는 옮길 기억을 훑는다. 맞은 값은 절대 안 찍고 어느
// 기억의 어느 칸인지와 규칙 이름만 알린다. 하나라도 걸리면 **한 건도 안 옮긴다**
// — 반만 옮겨진 저장소는 사람이 어디까지 됐는지 못 안다 (불변조건 I4).
func refuseSecretMigrate(repository *config.Repository, opened *store.Store, report *migrateReport) int {
	scanner := scannerFor(repository.Config.Secret)
	for _, item := range append(append([]plan{}, report.Plans...), report.Parts...) {
		one, err := opened.ReadMemory(item.Path)
		if err != nil || one.Memory == nil {
			continue
		}
		memory := one.Memory
		// 훑는 칸은 `secret.MemoryText` 하나로 모은다. 칸 목록을 여기 손으로
		// 또 적으면 `sources` 처럼 뒤에 늘어난 칸이 빠진다 — migrate 가 채우는
		// 것이 바로 그 칸이다 (보안연동 시험 M-1).
		if scanner.ScanText(secret.MemoryText(memory.Title, memory.Summary,
			memory.Sources, memory.Body)) == nil {
			continue
		}
		// 걸린 뒤에야 어느 칸인지 가른다. 값은 어느 길로도 안 찍는다.
		if found := scanner.ScanText(memory.Body); found != nil {
			return refuse(i18n.T(i18n.MigrateSecretStop, memory.ID, "본문", found.Rule))
		}
		for _, pair := range [][2]string{{"summary", memory.Summary}, {"title", memory.Title}} {
			if found := scanner.ScanLine(pair[1]); found != nil {
				return refuse(i18n.T(i18n.MigrateSecretStop, memory.ID, pair[0], found.Rule))
			}
		}
		for _, source := range memory.Sources {
			if found := scanner.ScanLine(source); found != nil {
				return refuse(i18n.T(i18n.MigrateSecretStop, memory.ID, "sources", found.Rule))
			}
		}
		// 세 칸 어디에도 안 걸렸는데 모아 놓으니 걸렸다 = 칸 경계에서만 보이는
		// 꼴이다. 자리를 못 짚어도 막는 것이 먼저다.
		return refuse(i18n.T(i18n.MigrateSecretStop, memory.ID, "여러 칸", "secret-pattern"))
	}
	return exitOK
}

// backupStore 는 store/ 를 통째로 archive/<날짜>-migrate 아래로 복사한다.
// 자동 삭제가 없는 도구라 되돌리기는 늘 있어야 한다 (불변조건 I5).
func backupStore(opened *store.Store, at time.Time) (string, error) {
	target := filepath.Join(opened.ArchiveDir(), at.Format("2006-01-02")+migrateDirSuffix)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", err
	}
	source := opened.StoreDir()
	err := filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		// 심볼릭 링크·정션은 보통 파일이 아니다. 따라가면 저장소 밖의 글이
		// archive/ 로 복사돼 git 에 들어간다 (불변조건 I1).
		if err != nil || !info.Mode().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		return copyInto(path, filepath.Join(target, rel))
	})
	return target, err
}

func copyInto(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.Create(to)
	if err != nil {
		return err
	}
	defer target.Close()
	_, err = io.Copy(target, source)
	return err
}

// restoreMigrate 는 마지막 이전 아카이브를 store/ 로 되돌린다. store/ 를 직접
// 고치므로 락을 잡은 채로 한다 (불변조건 2·6-5).
func restoreMigrate(opened *store.Store) int {
	backup := latestBackup(opened.ArchiveDir())
	if backup == "" {
		fmt.Fprintln(os.Stderr, i18n.T(i18n.MigrateNoBackup))
		return exitCheck
	}
	release, got, err := index.TryLock(opened.Dir)
	if err != nil {
		return exitFor(err)
	}
	if !got {
		fmt.Fprintln(os.Stderr, i18n.T(i18n.MigrateLocked))
		return exitLocked
	}
	defer release()
	count := 0
	err = filepath.Walk(backup, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.Mode().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(backup, path)
		if err != nil {
			return err
		}
		count++
		return copyInto(path, filepath.Join(opened.StoreDir(), rel))
	})
	if err != nil {
		return exitFor(err)
	}
	fmt.Println(i18n.T(i18n.MigrateRestored, count, backup))
	return exitOK
}

// latestBackup 은 가장 최근 이전 아카이브 폴더다.
func latestBackup(archiveDir string) string {
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		return ""
	}
	names := []string{}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasSuffix(entry.Name(), migrateDirSuffix) {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return filepath.Join(archiveDir, names[len(names)-1])
}
