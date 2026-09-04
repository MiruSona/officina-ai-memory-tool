package install

import "strings"

// auto 모드는 2026-08-14 부터 기본이다. allow 규칙이 없으면 mem 호출마다
// 분류기 왕복이 붙고, 넓은 규칙(`Bash(mem *)`)은 auto 모드가 버릴 수 있다.
// 그래서 명령별 좁은 규칙만 넣는다 (설계 5-2 · 조사 C 2-3).

const (
	permissionsKey = "permissions"
	allowKey       = "allow"
)

// allowCommands 는 규칙 하나의 안쪽 명령이다. 인자를 받는 것은 Bash 글로브
// 꼴(`:*`)로 적고, 정확 일치는 그대로 둔다.
// gc · install · uninstall 은 **일부러 뺐다** — 지우거나 기계를 건드리는
// 명령은 사람이 봐야 한다 (설계 5-2).
var allowCommands = []string{
	"mem search:*",
	"mem show:*",
	"mem add:*",
	"mem set:*",
	"mem lint:*",
	// `review` 만 두 줄이다. 넓은 `mem review:*` 는 `mem review --promote`
	// 까지 통과시켜 「승격은 사람이 한다」(결정 6)를 첫날 무너뜨린다 (결정 60).
	"mem review",
	"mem review --kind:*",
	"mem tags:*",
	// `index` 는 정확 일치 그대로다. `--full` 은 색인을 다시 만드니 승인 창이 떠야 한다.
	"mem index",
	// `status` 는 읽기 전용이라 넓혔다 (U4). 안 그러면 `--doctor`·`--log`·`--embed`
	// 마다 승인 창이 뜬다.
	"mem status:*",
	"mem eval:*",
}

// retiredCommands 는 예전 판이 넣었지만 이제 안 넣는 것이다. `mem log` 는
// `mem status --log` 로 접혀서 **없는 명령**이라 규칙이 죽어 있었다 (보안시험 M-3).
// 새로 넣지는 않고, --undo 때는 우리가 넣은 것이니 같이 뺀다.
var retiredCommands = []string{
	"mem log:*",
	// v0.2 가 넣던 넓은 규칙. --promote 까지 통과시켜서 뺀다 (결정 60).
	"mem review:*",
	// U4 전의 정확 일치 규칙.
	"mem status",
}

// AllowRules 는 mem init 이 .claude/settings.json 에 넣는 규칙이다. 명령마다
// Bash 꼴과 PowerShell 꼴을 **둘 다** 넣는다 — Windows 의 Claude Code 는 명령을
// Bash 도구가 아니라 PowerShell 도구로 돌려서 Bash 꼴만 있으면 하나도 안 맞는다
// (2026-08-24 사용자 실측 3절).
var AllowRules = bothShapes(allowCommands)

// RetiredAllowRules 는 --undo 가 같이 빼 줄 옛 규칙이다.
var RetiredAllowRules = bothShapes(retiredCommands)

// bothShapes 는 명령 목록을 Bash·PowerShell 두 꼴로 펼친다. 짝이 붙어 있어야
// 사람이 settings.json 을 열었을 때 빠진 것을 바로 본다.
func bothShapes(commands []string) []string {
	rules := make([]string, 0, len(commands)*2)
	for _, command := range commands {
		rules = append(rules, bashShape(command), powerShellShape(command))
	}
	return rules
}

func bashShape(command string) string {
	return "Bash(" + command + ")"
}

// powerShellShape 는 Bash 의 인자 글로브 `:*` 를 PowerShell 꼴 ` *` 로 바꾼다.
func powerShellShape(command string) string {
	return "PowerShell(" + strings.Replace(command, ":*", " *", 1) + ")"
}

// isPowerShellShape 는 PowerShell 꼴 규칙인지다. doctor 가 「Bash 만 있다」를
// 가려내는 데 쓴다.
func isPowerShellShape(rule string) bool {
	return strings.HasPrefix(rule, "PowerShell(")
}

// allowShape 는 permissions 가 객체이고 permissions.allow 가 배열인지 본다.
// 아니면 남의 파일이라 손대지 않는다 (knownShape 와 같은 자).
func allowShape(root *jsonObject) bool {
	value, found := root.get(permissionsKey)
	if !found {
		return true
	}
	object, ok := value.(*jsonObject)
	if !ok {
		return false
	}
	list, found := object.get(allowKey)
	if !found {
		return true
	}
	_, ok = list.([]any)
	return ok
}

// allowList 는 지금 든 allow 규칙이다. 모르는 모양이면 빈 것으로 본다.
func allowList(root *jsonObject) []any {
	value, _ := root.get(permissionsKey)
	object, ok := value.(*jsonObject)
	if !ok {
		return nil
	}
	list, _ := object.get(allowKey)
	array, ok := list.([]any)
	if !ok {
		return nil
	}
	return array
}

// MissingAllow 는 우리 규칙 가운데 아직 없는 것이다. 있는 것은 그대로 두고
// 다른 키의 순서도 안 바꾼다 (설계 5-2).
func MissingAllow(root *jsonObject) []string {
	have := map[string]bool{}
	for _, item := range allowList(root) {
		if text, ok := item.(string); ok {
			have[text] = true
		}
	}
	missing := []string{}
	for _, rule := range AllowRules {
		if !have[rule] {
			missing = append(missing, rule)
		}
	}
	return missing
}

// addAllow 는 빠진 규칙만 뒤에 붙인다.
func addAllow(root *jsonObject, missing []string) {
	object := permissionsObject(root)
	list := allowList(root)
	for _, rule := range missing {
		list = append(list, rule)
	}
	object.set(allowKey, list)
}

// removeAllow 는 우리가 넣은 규칙만 뺀다. 사람이 손으로 적은 다른 규칙은
// 그대로 둔다. 우리 것을 다 빼서 배열이 비면 키째 없앤다.
func removeAllow(root *jsonObject) bool {
	value, found := root.get(permissionsKey)
	object, ok := value.(*jsonObject)
	if !found || !ok {
		return false
	}
	ours := map[string]bool{}
	for _, rule := range AllowRules {
		ours[rule] = true
	}
	for _, rule := range RetiredAllowRules {
		ours[rule] = true
	}
	kept, cut := []any{}, false
	for _, item := range allowList(root) {
		text, isText := item.(string)
		if isText && ours[text] {
			cut = true
			continue
		}
		kept = append(kept, item)
	}
	if !cut {
		return false
	}
	if len(kept) > 0 {
		object.set(allowKey, kept)
		return true
	}
	object.remove(allowKey)
	if object.len() == 0 {
		root.remove(permissionsKey)
	}
	return true
}

// hasOurAllow 는 우리 규칙이 하나라도 들어 있는지다.
func hasOurAllow(root *jsonObject) bool {
	return len(MissingAllow(root)) < len(AllowRules)
}

func permissionsObject(root *jsonObject) *jsonObject {
	value, _ := root.get(permissionsKey)
	object, ok := value.(*jsonObject)
	if ok {
		return object
	}
	object = newObject()
	root.set(permissionsKey, object)
	return object
}
