package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// 훅의 키와 값이다 (설계 7-1). args 가 있는 exec 꼴이라 셸을 안 거친다 —
// .cmd·.ps1 래퍼는 한글이 깨진다.
const (
	hooksKey        = "hooks"
	sessionKey      = "SessionStart"
	subagentKey     = "SubagentStart"
	subagentStopKey = "SubagentStop"
	commandKey      = "command"
	argsKey         = "args"
	hookVerb        = "hook"
	hookAction      = "session-start"
	subagentAction  = "subagent-start"
	// subagentStopAction 은 **붙이지 않는다.** 우리 액션 목록에만 남겨 둬서,
	// 사람이 시험 삼아 손으로 걸어 둔 줄을 --undo 가 걷을 수 있게 한다
	// (서브에이전트훅설계 6-2).
	subagentStopAction = "subagent-stop"
	hookMatcher        = "startup|resume|clear|compact|fork"
	settingsFile       = "settings.json"
	backupSuffix       = ".mem-bak"
	tempSuffix         = ".mem-tmp"
)

// hookSpec 은 우리가 settings.json 에 붙이는 훅 한 줄이다.
type hookSpec struct {
	event string
	// action 은 args 의 둘째 칸이다 : ["hook", action].
	action string
	// matcher 가 비면 matcher 키를 안 넣는다 = 모든 종류에 걸린다.
	matcher string
	// gemini 는 Gemini CLI 에도 붙일지다. 그 CLI 에는 SubagentStart 가 없다.
	gemini bool
}

// hookSpecs 는 붙일 훅 표다. 줄을 더하면 install 도 undo 도 같이 늘어난다.
var hookSpecs = []hookSpec{
	{event: sessionKey, action: hookAction, matcher: hookMatcher, gemini: true},
	{event: subagentKey, action: subagentAction},
}

// hookEventKeys 는 우리가 모양을 따지고 --undo 가 훑는 이벤트 키다.
var hookEventKeys = []string{sessionKey, subagentKey, subagentStopKey}

// ourActions 는 우리 훅으로 알아보는 액션이다. 붙이는 표보다 하나 넓다.
var ourActions = []string{hookAction, subagentAction, subagentStopAction}

// specsFor 는 이 도구·이 깃발에 실제로 붙일 훅만 고른다.
func specsFor(gemini, noSubagent bool) []hookSpec {
	chosen := []hookSpec{}
	for _, spec := range hookSpecs {
		if gemini && !spec.gemini {
			continue
		}
		if noSubagent && spec.event == subagentKey {
			continue
		}
		chosen = append(chosen, spec)
	}
	return chosen
}

// timeout 은 두 도구가 단위가 다르다 : Claude 는 초, Gemini 는 밀리초 (설계 7-6).
const (
	claudeTimeout = 10
	geminiTimeout = 60000
)

// errBadSettings 는 JSON 을 못 읽었다는 뜻이다. 이때는 파일에 손대지 않는다.
var errBadSettings = errors.New("bad settings json")

// errOddSettings 는 hooks 가 우리가 병합할 줄 아는 모양이 아니라는 뜻이다.
// 다시 쓰면 남의 훅을 버리게 된다.
var errOddSettings = errors.New("unknown settings shape")

// errSettingsChanged 는 우리가 생각하는 사이에 남이 파일을 고쳤다는 뜻이다.
// 남의 일을 덮느니 아무것도 안 한다 (설계 10절 : 백업 + 재대조).
var errSettingsChanged = errors.New("settings json changed under us")

// settingsWriter 는 시험이 경쟁 상황을 꾸미려고 갈아 끼운다.
var settingsWriter = writeSettingsAtomic

// ClaudeSettingsPath 는 프로젝트의 Claude Code 설정 파일이다.
func ClaudeSettingsPath(root string) string {
	return filepath.Join(root, ".claude", settingsFile)
}

// GeminiSettingsPath 는 Gemini CLI 설정 파일이다. --gemini 를 사람이 줄 때만 만든다.
func GeminiSettingsPath(root string) string {
	return filepath.Join(root, ".gemini", settingsFile)
}

// hookEntry 는 한 이벤트에 붙일 묶음 하나를 만든다. matcher 가 빈 spec 은
// matcher 키를 아예 안 넣는다 — 그것이 「모든 종류」다.
func hookEntry(spec hookSpec, timeout int) *jsonObject {
	inner := newObject()
	inner.set("type", "command")
	inner.set(commandKey, ExeName)
	inner.set(argsKey, []any{hookVerb, spec.action})
	inner.set("timeout", json.Number(strconv.Itoa(timeout)))
	inner.set("statusMessage", i18n.T(i18n.HookStatusMessage))
	group := newObject()
	if spec.matcher != "" {
		group.set("matcher", spec.matcher)
	}
	group.set(hooksKey, []any{inner})
	return group
}

// loadSettings 는 파싱한 나무와 그것이 나온 바로 그 바이트를 같이 준다.
// 쓰는 쪽이 「아직 우리가 읽은 그 파일인가」 를 다시 대조할 수 있어야 한다.
func loadSettings(path string) (*jsonObject, []byte, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return newObject(), nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(data, []byte("\ufeff")))
	if len(trimmed) == 0 {
		return newObject(), data, false, nil
	}
	value, err := decodeJSON(trimmed)
	if err != nil {
		return nil, data, true, errBadSettings
	}
	root, ok := value.(*jsonObject)
	if !ok {
		return nil, data, true, errBadSettings
	}
	if !knownShape(root) || !allowShape(root) {
		return nil, data, true, errOddSettings
	}
	return root, data, true, nil
}

// knownShape 는 hooks 가 객체이고 우리가 만지는 이벤트 키가 저마다 배열인지
// 본다. 하나라도 딴 모양이면 손대지 않는다 — 갈아 끼우면 거기 든 것을 버린다.
func knownShape(root *jsonObject) bool {
	hooks, found := root.get(hooksKey)
	if !found {
		return true
	}
	object, ok := hooks.(*jsonObject)
	if !ok {
		return false
	}
	for _, name := range hookEventKeys {
		list, found := object.get(name)
		if !found {
			continue
		}
		if _, ok := list.([]any); !ok {
			return false
		}
	}
	return true
}

// writeSettingsAtomic 은 파일이 우리 밑에서 바뀌었으면 안 쓰고, 백업을 남기고,
// rename 으로 내려놓아 반쯤 쓰인 파일이 보이지 않게 한다.
func writeSettingsAtomic(path string, root *jsonObject, before []byte) error {
	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if !bytes.Equal(current, before) {
		return errSettingsChanged
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if len(before) > 0 {
		if err := os.WriteFile(path+backupSuffix, before, 0o644); err != nil {
			return err
		}
	}
	data, err := encodeSettings(root)
	if err != nil {
		return err
	}
	// 남의 파일 모양(BOM·줄끝)은 우리 것이 아니다. 안 지키면 훅 한 줄 붙인
	// 것이 파일 전체 diff 로 뜬다 (리뷰 A #9).
	data = asBefore(data, before)
	temporary := path + tempSuffix
	if err := os.WriteFile(temporary, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	// rename 은 반쯤 쓰인 파일을 안 남긴다. 백업까지 git 이 추적하면 사람이
	// 넣어 둔 로컬 설정이 통째로 커밋된다 (리뷰 A #8).
	os.Remove(path + backupSuffix)
	return nil
}

// asBefore 는 새 바이트를 앞 판과 같은 모양으로 맞춘다 — BOM 유무와 줄끝.
func asBefore(data, before []byte) []byte {
	if bytes.Contains(before, []byte("\r\n")) {
		data = bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
	}
	if bytes.HasPrefix(before, []byte("\ufeff")) {
		data = append([]byte("\ufeff"), data...)
	}
	return data
}

func encodeSettings(root *jsonObject) ([]byte, error) {
	buffer := bytes.Buffer{}
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(root); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// eventList 는 hooks.<이벤트> 를 읽는다. 모르는 모양이면 빈 것으로 본다.
func eventList(root *jsonObject, event string) []any {
	hooks, _ := root.get(hooksKey)
	object, ok := hooks.(*jsonObject)
	if !ok {
		return nil
	}
	value, _ := object.get(event)
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	return list
}

func setEventList(root *jsonObject, event string, list []any) {
	hooks, _ := root.get(hooksKey)
	object, ok := hooks.(*jsonObject)
	if !ok {
		object = newObject()
		root.set(hooksKey, object)
	}
	if len(list) > 0 {
		object.set(event, list)
		return
	}
	object.remove(event)
	if object.len() == 0 {
		root.remove(hooksKey)
	}
}

// groupHooks 는 matcher 묶음 하나의 안쪽 훅 목록을 준다.
func groupHooks(group any) ([]any, *jsonObject) {
	entry, ok := group.(*jsonObject)
	if !ok {
		return nil, nil
	}
	value, _ := entry.get(hooksKey)
	inner, ok := value.([]any)
	if !ok {
		return nil, entry
	}
	return inner, entry
}

// isMemHook 은 우리가 붙인 훅인지 본다 : 우리 exe 이름 + 우리 인자 두 개.
// 이름만 mem 인 남의 훅은 남긴다.
func isMemHook(hook any) bool {
	entry, ok := hook.(*jsonObject)
	if !ok {
		return false
	}
	command, ok := entry.values[commandKey].(string)
	if !ok || !memCommand(command) {
		return false
	}
	return ourArgs(entry.values[argsKey])
}

// memCommand 는 mem · mem.exe · 그 둘로 끝나는 경로를 받아들인다.
func memCommand(command string) bool {
	base := strings.ToLower(command)
	if cut := strings.LastIndexAny(base, `/\`); cut >= 0 {
		base = base[cut+1:]
	}
	return base == ExeName || base == ExeName+".exe"
}

func ourArgs(value any) bool {
	list, ok := value.([]any)
	if !ok || len(list) != 2 {
		return false
	}
	verb, okVerb := list[0].(string)
	action, okAction := list[1].(string)
	if !okVerb || !okAction || verb != hookVerb {
		return false
	}
	for _, ours := range ourActions {
		if action == ours {
			return true
		}
	}
	return false
}

// hasMemHook 은 이 이벤트에 그 액션의 우리 훅이 이미 있는지다.
func hasMemHook(list []any, action string) bool {
	for _, group := range list {
		inner, _ := groupHooks(group)
		for _, hook := range inner {
			if isMemHook(hook) && actionOf(hook) == action {
				return true
			}
		}
	}
	return false
}

// actionOf 는 우리 훅 한 줄의 액션 이름이다. 우리 것이 아니면 빈 글이다.
func actionOf(hook any) string {
	entry, ok := hook.(*jsonObject)
	if !ok {
		return ""
	}
	list, ok := entry.values[argsKey].([]any)
	if !ok || len(list) != 2 {
		return ""
	}
	action, _ := list[1].(string)
	return action
}

// withoutMemHooks 는 우리 훅과 그래서 텅 빈 matcher 묶음만 뺀다.
func withoutMemHooks(list []any) []any {
	kept := []any{}
	for _, group := range list {
		inner, entry := groupHooks(group)
		if entry == nil {
			kept = append(kept, group)
			continue
		}
		remaining := keepOthers(inner)
		if len(remaining) == 0 {
			continue
		}
		entry.set(hooksKey, remaining)
		kept = append(kept, entry)
	}
	return kept
}

func keepOthers(inner []any) []any {
	remaining := []any{}
	for _, hook := range inner {
		if !isMemHook(hook) {
			remaining = append(remaining, hook)
		}
	}
	return remaining
}

// ensureSettings 는 표에 든 훅을 이벤트마다 배열 끝에 붙이고, allow 규칙 가운데
// 빠진 것을 채운다. 둘을 한 번에 쓰는 이유는 같은 파일이라서다 — 두 번 쓰면
// 백업도 두 번 나고 두 번째 쓰기가 첫 번째와 경쟁한다.
// 다른 키는 안 건드리고 키 순서도 읽은 그대로 다시 쓴다.
func ensureSettings(path string, timeout int, dryRun, withAllow bool, specs []hookSpec) (Step, error) {
	step := Step{What: shortPath(path)}
	root, before, existed, err := loadSettings(path)
	if manual, ok := handOver(&step, path, err); ok {
		return manual, nil
	}
	if err != nil {
		return step, err
	}
	missing := []string{}
	if withAllow {
		missing = MissingAllow(root)
	}
	needed, hooks := neededSpecs(root, specs)
	if len(needed) == 0 && len(missing) == 0 {
		step.Now, step.Todo = i18n.T(i18n.InitStateMemHook), i18n.T(i18n.InitTodoKeep)
		return step, nil
	}
	step.Now = settingsNow(existed, len(needed) > 0, hooks)
	step.Todo, step.Changed = settingsTodo(len(needed) > 0, hooks, len(missing)), true
	if dryRun {
		return step, nil
	}
	for _, spec := range needed {
		setEventList(root, spec.event, append(eventList(root, spec.event), hookEntry(spec, timeout)))
	}
	if len(missing) > 0 {
		addAllow(root, missing)
	}
	return raced(step, settingsWriter(path, root, before))
}

// neededSpecs 는 아직 안 붙은 훅과, 그 이벤트들에 이미 든 묶음 수를 준다.
func neededSpecs(root *jsonObject, specs []hookSpec) ([]hookSpec, int) {
	needed, hooks := []hookSpec{}, 0
	for _, spec := range specs {
		list := eventList(root, spec.event)
		hooks += len(list)
		if !hasMemHook(list, spec.action) {
			needed = append(needed, spec)
		}
	}
	return needed, hooks
}

func settingsNow(existed, needHook bool, hooks int) string {
	if !existed {
		return i18n.T(i18n.InitStateMissing)
	}
	if !needHook {
		return i18n.T(i18n.InitStateMemHook)
	}
	return i18n.T(i18n.InitStateHooks, hooks)
}

// settingsTodo 는 훅과 allow 규칙 가운데 실제로 할 것만 적는다.
func settingsTodo(needHook bool, hooks, allow int) string {
	switch {
	case needHook && allow > 0:
		return i18n.T(i18n.InitTodoHookAllow, hooks+1, hooks, allow)
	case needHook:
		return i18n.T(i18n.InitTodoHook, hooks+1, hooks)
	}
	return i18n.T(i18n.InitTodoAllow, allow)
}

// removeSettings 는 우리 훅만 도로 빼고 남의 것은 그대로 둔다. 붙이는 표보다
// 넓게 훑는다 — 손으로 걸어 둔 subagent-stop 도 걷어야 한다.
func removeSettings(path string, dryRun bool) (Step, error) {
	step := Step{What: shortPath(path), Now: i18n.T(i18n.InitStateMissing), Todo: i18n.T(i18n.InitTodoKeep)}
	root, before, existed, err := loadSettings(path)
	if manual, ok := handOver(&step, path, err); ok {
		return manual, nil
	}
	if err != nil {
		return step, err
	}
	if !existed {
		return step, nil
	}
	ours, hooks := ourEvents(root)
	if len(ours) == 0 && !hasOurAllow(root) {
		step.Now = i18n.T(i18n.InitStateHooks, hooks)
		return step, nil
	}
	step.Now, step.Todo, step.Changed = i18n.T(i18n.InitStateMemHook), i18n.T(i18n.InitTodoRemove), true
	if dryRun {
		return step, nil
	}
	for _, event := range ours {
		setEventList(root, event, withoutMemHooks(eventList(root, event)))
	}
	// allow 규칙도 우리가 넣은 것이라 같이 뗀다 (설계 6-1 init --undo).
	removeAllow(root)
	return raced(step, settingsWriter(path, root, before))
}

// ourEvents 는 우리 훅이 든 이벤트 키와, 훑은 묶음 수를 준다.
func ourEvents(root *jsonObject) ([]string, int) {
	found, hooks := []string{}, 0
	for _, event := range hookEventKeys {
		list := eventList(root, event)
		hooks += len(list)
		for _, action := range ourActions {
			if hasMemHook(list, action) {
				found = append(found, event)
				break
			}
		}
	}
	return found, hooks
}

// handOver 는 못 읽거나 모르는 모양인 파일을 「손으로」 소단계로 바꾼다.
// 남의 파일을 다시 쓰느니 붙일 블록을 찍어 주고 만다.
func handOver(step *Step, path string, err error) (Step, bool) {
	switch {
	case errors.Is(err, errBadSettings):
		step.Now = i18n.T(i18n.InitStateUnreadable)
	case errors.Is(err, errOddSettings):
		step.Now = i18n.T(i18n.InitStateOddShape)
	default:
		return *step, false
	}
	step.Todo, step.Manual = i18n.T(i18n.InitTodoManual), true
	step.What = shortPath(path)
	return *step, true
}

// raced 는 진 경쟁을 오류가 아니라 「손으로」 소단계로 바꾼다. 남의 settings 를
// 우리가 본 그대로 둔다.
func raced(step Step, err error) (Step, error) {
	if !errors.Is(err, errSettingsChanged) {
		return step, err
	}
	step.Now, step.Todo, step.Changed, step.Manual = i18n.T(i18n.InitStateRaced), i18n.T(i18n.InitTodoManual), false, true
	return step, nil
}
