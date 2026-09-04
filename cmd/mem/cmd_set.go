package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/review"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

var setBools = []string{"stdin", "pin", "unpin", "done", "by-new"}
var setValues = []string{"summary", "scope", "title", "severity", "status", "todo-status",
	"invalid-at", "by", "stale-after", "tags", "links", "link", "body", "importance", "repo"}

func init() {
	register(command{name: "set", run: runSet, bools: setBools, values: setValues})
}

// textFields 는 `--이름 값` 이 그대로 머리말 칸으로 가는 것들이다.
var textFields = map[string]string{
	"summary":     "summary",
	"scope":       "scope",
	"title":       "title",
	"severity":    "severity",
	"status":      "todo_status",
	"todo-status": "todo_status",
	"invalid-at":  "invalid_at",
	"stale-after": "stale_after",
}

// runSet 은 머리말이나 본문을 고칠 것을 큐에 넣는다. store/ 를 직접 고치는 것은
// 락을 잡은 mem index 뿐이다 (설계 5-2).
func runSet(argv []string) int {
	parsed, err := parseOptions(argv, setBools, setValues)
	if err != nil {
		return fail(err.Error())
	}
	if len(parsed.rest) == 0 {
		return fail(i18n.T(i18n.NeedArgument, "set <id>"))
	}
	id := parsed.rest[0]
	if !model.IsID(id) {
		return fail(i18n.T(i18n.BadID, id))
	}
	set, err := changesOf(parsed, id)
	if err != nil {
		return failSet(err)
	}
	body, wantBody, err := newBody(parsed)
	if err != nil {
		return fail(err.Error())
	}
	if len(set) == 0 && !wantBody && !parsed.has("link") {
		return fail(i18n.T(i18n.SetNothing))
	}
	if wantBody {
		if err := checkList(model.Memory{Body: body}); err != nil {
			return failSet(err)
		}
	}
	return queueSet(parsed, id, set, body, wantBody)
}

func queueSet(parsed *options, id string, set map[string]any, body string, wantBody bool) int {
	repository, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	// --link 는 지금 파일에 적힌 목록을 읽어야 해서 저장소를 연 뒤에 푼다.
	if err := addLink(parsed, opened, id, set); err != nil {
		return fail(err.Error())
	}
	// 설계 10절의 3중 차단은 set 에도 걸린다. 여기가 비면 `--summary` 로 넣은
	// 토큰이 훅 주입 블록에 그대로 실려 나간다 (리뷰 A #1).
	if code := refuseSecretSet(repository, set, body, wantBody); code != exitOK {
		return code
	}
	if err := opened.EnsureDirs(); err != nil {
		return fail(err.Error())
	}
	if len(set) > 0 {
		if _, err := opened.WritePatch(id, set); err != nil {
			return fail(err.Error())
		}
	}
	if wantBody {
		if _, err := opened.WriteAmend(id, body); err != nil {
			return fail(err.Error())
		}
	}
	fmt.Println(i18n.T(i18n.SetQueued, id))
	if newID, ok := set["superseded_by"].(string); ok {
		fmt.Println(i18n.T(i18n.SetSuperseded, id, newID))
		noteLog(opened, store.LogSuper, id+" → "+newID)
		noteUsedBy(parsed, opened, id, os.Stdout)
	}
	if _, ok := set["stale_after"]; ok && parsed.flags["by-new"] {
		fmt.Println(i18n.T(i18n.SetByNewNext, id))
	}
	return exitOK
}

// noteUsedBy 는 방금 죽은 기억을 근거로 삼은 기억이 몇 건인지 알린다. 파일에도
// 큐에도 아무것도 안 쓴다 — 사람이 mem review --kind basis 로 하나씩 판정한다.
//
// **셈은 검토 큐와 같은 규칙이다** — 이미 죽었거나 다시 볼 날을 미뤄 둔 기억은
// 큐에 안 오르니 여기서도 안 센다. 「N건 있다 → review 하라」 해 놓고 큐가 비면
// 사람이 도구를 못 믿는다.
//
// 0건이면 아무 말도 안 하고, **못 셌을 때는 0건이라고 말하지 않는다.**
func noteUsedBy(parsed *options, opened *store.Store, id string, out io.Writer) {
	if parsed.flags["no-index"] {
		fmt.Fprintln(out, i18n.T(i18n.SetUsedByNoIndex))
		return
	}
	if !index.Exists(opened.Dir) {
		fmt.Fprintln(out, i18n.T(i18n.SetUsedByUnknown))
		return
	}
	// 판이 낮은 색인은 여기서 통째로 다시 만들어진다. 그러면 표가 비어 있어
	// 0건이 「없다」가 아니라 「아직 안 넣었다」다 (리뷰 #4).
	database, rebuilt, err := index.OpenRebuilding(opened.Dir)
	if err != nil {
		fmt.Fprintln(out, i18n.T(i18n.SetUsedByUnknown))
		return
	}
	defer database.Close()
	if rebuilt {
		fmt.Fprintln(out, i18n.T(i18n.SetUsedByRebuilt))
		return
	}
	rows, err := database.UsedBy(id)
	if err != nil {
		fmt.Fprintln(out, i18n.T(i18n.SetUsedByUnknown))
		return
	}
	tellUsedBy(out, rows)
}

// tellUsedBy 는 센 결과를 말한다. 큐에 오를 것이 하나도 없으면 review 로 보내지
// 않는다.
func tellUsedBy(out io.Writer, rows []index.UsedByRow) {
	if len(rows) == 0 {
		return
	}
	queued := queuedIDs(rows)
	if len(queued) == 0 {
		fmt.Fprintln(out, i18n.T(i18n.SetUsedByAllDead, len(rows)))
		return
	}
	fmt.Fprintln(out, i18n.T(i18n.SetUsedByFound, len(queued), review.IDLine(queued)))
	fmt.Fprintln(out, i18n.T(i18n.SetUsedByNext))
}

// queuedIDs 는 검토 큐(C14)가 실제로 잡을 기억의 id 다.
func queuedIDs(rows []index.UsedByRow) []string {
	ids := []string{}
	for _, row := range rows {
		if !row.Live || row.Snoozed {
			continue
		}
		ids = append(ids, row.ID)
	}
	return ids
}

// refuseSecretSet 은 이번에 바뀌는 글만 훑는다. 안 건드리는 칸은 볼 일이 없다.
func refuseSecretSet(repository *config.Repository, set map[string]any, body string, wantBody bool) int {
	scanner := scannerFor(repository.Config.Secret)
	if wantBody {
		if found := scanner.ScanText(body); found != nil {
			return refuse(i18n.T(i18n.SecretFound, found.Line, found.Rule))
		}
	}
	for _, field := range []string{"summary", "title", "scope", "superseded_by", "author"} {
		text, ok := set[field].(string)
		if !ok {
			continue
		}
		if found := scanner.ScanLine(text); found != nil {
			return refuse(i18n.T(i18n.SecretFoundIn, field, found.Rule))
		}
	}
	return exitOK
}

// changesOf 는 준 옵션만 모은다. 안 준 칸은 건드리지 않는다. 값은 여기서 바로
// 검사한다 — 나중에 inbox/bad 에서 조용히 실패하면 AI 는 잘 된 줄 안다 (리뷰 C #5).
func changesOf(parsed *options, id string) (map[string]any, error) {
	set := map[string]any{}
	for option, field := range textFields {
		if !parsed.has(option) {
			continue
		}
		value := parsed.text(option)
		if err := checkSetValue(option, value); err != nil {
			return nil, err
		}
		set[field] = value
	}
	if parsed.has("tags") {
		tags := parsed.list("tags")
		if err := checkList(model.Memory{Tags: tags}); err != nil {
			return nil, err
		}
		set["tags"] = toAny(tags)
	}
	// `--links` 는 통째 교체, `--link` 는 한 개 더하기다. 더하기는 지금 파일을
	// 읽어야 해서 여기서 안 하고 addLink 가 한다.
	for _, given := range append(parsed.list("links"), parsed.list("link")...) {
		if !model.IsID(given) {
			return nil, fmt.Errorf("%s", i18n.T(i18n.BadID, given))
		}
	}
	if parsed.has("links") {
		set["links"] = toAny(parsed.list("links"))
	}
	if parsed.flags["pin"] {
		set["pinned"] = true
	}
	if parsed.flags["unpin"] {
		set["pinned"] = false
	}
	if parsed.flags["done"] {
		set["todo_status"] = model.StatusDone
	}
	if err := addSupersede(parsed, id, set); err != nil {
		return nil, err
	}
	return withImportance(parsed, set)
}

// addSupersede 는 덮기 두 꼴을 푼다 (설계 3-4).
//
//	--by <새id>  이 기억을 그 기억이 덮었다. superseded_by 와 invalid_at 은
//	             한 짝이라 도구가 둘을 같이 채운다.
//	--by-new     덮을 새 기억이 아직 없다. **설계와 다른 자리다** — 설계는
//	             거절 안내에 이 옵션만 적었는데, 새 기억이 없는 상태에서
//	             superseded_by 를 채우면 짝이 깨진다. 그래서 여기서는 옛
//	             기억을 오늘부터 검토 큐(stale_after)에 올리고 다음에 칠
//	             명령을 알려준다.
func addSupersede(parsed *options, id string, set map[string]any) error {
	if parsed.has("by") {
		newID := parsed.text("by")
		if !model.IsID(newID) {
			return fmt.Errorf("%s", i18n.T(i18n.BadID, newID))
		}
		// 자기가 자기를 덮으면 그 기억은 살아 있는 채로 검색에서 사라진다.
		if newID == id {
			return fmt.Errorf("%s", i18n.T(i18n.SetSelfSupersede, id))
		}
		set["superseded_by"] = newID
		set["invalid_at"] = time.Now().Format(model.DayLayout)
	}
	if parsed.flags["by-new"] {
		set["stale_after"] = time.Now().Format(model.DayLayout)
	}
	return nil
}

// checkSetValue 는 model.Validate 와 같은 규칙을 한 칸에만 쓴다. 규칙이 두
// 벌이 되지 않게 검사는 model 이 하고 여기서는 부르기만 한다.
func checkSetValue(option, value string) error {
	// title 은 v0.2 칸이라 model.Validate 가 Spec 을 SpecV2 로 안 두면 그 검사를
	// 통째로 건너뛴다 (오늘 겪은 것 — 42자 제목이 그냥 통과했었다).
	one := model.Memory{Spec: model.SpecV2}
	switch option {
	case "summary":
		one.Summary = value
	case "scope":
		one.Scope = value
	case "severity":
		one.Severity = value
	case "status", "todo-status":
		one.TodoStatus = value
	case "invalid-at":
		one.InvalidAt = value
	case "stale-after":
		one.StaleAfter = value
	case "title":
		one.Title = value
	}
	return checkList(one)
}

// checkList 는 채운 칸만 든 임시 기억을 검사하고 첫 문제를 돌려준다. 빈 칸은
// model.Validate 가 「없는 칸」 으로 건너뛰므로 이 방식이 안전하다.
func checkList(one model.Memory) error {
	for _, problem := range model.Validate(&one) {
		if isMissingField(problem) {
			continue
		}
		return &checkFailError{problem}
	}
	return nil
}

// checkFailError 는 품질 관문(model.Validate) 거절을 usage 오류와 가른다.
// `add` 처럼 검사 실패는 종료 코드 2 다.
type checkFailError struct{ err error }

func (e *checkFailError) Error() string { return e.err.Error() }
func (e *checkFailError) Unwrap() error { return e.err }

// failSet 은 오류를 찍고 종료 코드를 고른다. 품질 관문 오류만 2, 나머지는 1.
func failSet(err error) int {
	var check *checkFailError
	if errors.As(err, &check) {
		fmt.Fprintln(os.Stderr, err.Error())
		return exitCheck
	}
	return fail(err.Error())
}

// isMissingField 는 「그 칸이 비었다」 는 문제인지 본다. set 은 준 칸만 보므로
// 안 준 칸의 「없다」 는 문제가 아니다.
func isMissingField(problem error) bool {
	for _, name := range []string{"id", "type", "date", "summary", "source", "scope", "tags",
		"status", "severity", "title", "author", "sources"} {
		if problem.Error() == i18n.T(i18n.MissingField, name) {
			return true
		}
	}
	return false
}

func withImportance(parsed *options, set map[string]any) (map[string]any, error) {
	if !parsed.has("importance") {
		return set, nil
	}
	text := parsed.text("importance")
	value, err := strconv.Atoi(text)
	if err != nil || value < 1 || value > 5 {
		return nil, fmt.Errorf("%s", i18n.T(i18n.BadImportanceValue, text))
	}
	set["importance"] = value
	return set, nil
}

// newBody 는 본문을 통째로 갈아 끼울지, 무엇으로 갈지 알려준다.
func newBody(parsed *options) (string, bool, error) {
	if parsed.has("body") {
		return parsed.text("body"), true, nil
	}
	if !parsed.flags["stdin"] {
		return "", false, nil
	}
	text, err := readStdin()
	return text, true, err
}

// toAny 는 문자열 목록을 큐 JSON 이 쓰는 꼴로 바꾼다.
func toAny(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

// addLink 는 `--link` 로 준 id 를 지금 links 뒤에 더한다. `--links` 를 같이
// 줬으면 그것으로 갈아 끼운 목록 뒤에 더한다. **사람이 적은 links 만
// 건드린다** — auto_links 는 색인이 만드는 파생 표라 여기서 안 본다.
func addLink(parsed *options, opened *store.Store, id string, set map[string]any) error {
	if !parsed.has("link") {
		return nil
	}
	base, replaced := set["links"].([]any)
	if !replaced {
		current, err := currentLinks(parsed, opened, id)
		if err != nil {
			return err
		}
		base = toAny(current)
	}
	set["links"] = withLinks(base, parsed.list("link"))
	return nil
}

// withLinks 는 뒤에 붙이되 이미 있는 것은 안 붙인다. 같은 id 가 두 벌이면
// gc 면제표와 이웃 셈이 그 기억을 두 번 센다.
func withLinks(base []any, adding []string) []any {
	seen := map[string]bool{}
	for _, one := range base {
		if text, ok := one.(string); ok {
			seen[text] = true
		}
	}
	for _, one := range adding {
		if seen[one] {
			continue
		}
		seen[one] = true
		base = append(base, one)
	}
	return base
}

// currentLinks 는 그 기억이 지금 적어 둔 links 다. 파일을 못 찾으면 오류다 —
// 없는 기억에 링크를 더하면 큐가 조용히 inbox/bad 로 간다.
func currentLinks(parsed *options, opened *store.Store, id string) ([]string, error) {
	path, err := showPath(parsed, opened, id)
	if err != nil {
		return nil, err
	}
	file, err := opened.ReadMemory(path)
	if err != nil {
		return nil, fmt.Errorf("%s", i18n.T(i18n.MemoryNotFound, id))
	}
	return file.Memory.Links, nil
}
