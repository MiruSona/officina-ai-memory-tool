package install

import (
	"encoding/json"
	"strings"
	"testing"
)

// U1 — Windows 의 Claude Code 는 PowerShell 도구로 명령을 돌린다. 그래서 규칙
// 하나마다 Bash 꼴과 PowerShell 꼴이 짝으로 있어야 한다 (사용자 실측 3절).
func TestAllowRulesHaveBothShapes(t *testing.T) {
	for _, list := range [][]string{AllowRules, RetiredAllowRules} {
		bash, powerShell := map[string]bool{}, map[string]bool{}
		for _, rule := range list {
			if inner, ok := strings.CutPrefix(rule, "Bash("); ok {
				bash[strings.TrimSuffix(inner, ")")] = true
				continue
			}
			inner, ok := strings.CutPrefix(rule, "PowerShell(")
			if !ok {
				t.Fatalf("모르는 꼴의 규칙이다 : %s", rule)
			}
			powerShell[strings.TrimSuffix(inner, ")")] = true
		}
		if len(bash) != len(powerShell) || len(bash)*2 != len(list) {
			t.Fatalf("짝이 안 맞는다 : Bash %d · PowerShell %d · 전체 %d", len(bash), len(powerShell), len(list))
		}
		for command := range bash {
			want := strings.Replace(command, ":*", " *", 1)
			if !powerShell[want] {
				t.Errorf("`%s` 의 PowerShell 짝 `%s` 가 없다", command, want)
			}
		}
	}
}

// U4 — status 는 읽기 전용이라 넓혔고, index 는 `--full` 때문에 정확 일치로 둔다.
func TestStatusIsWideAndIndexIsExact(t *testing.T) {
	want := []string{"Bash(mem status:*)", "PowerShell(mem status *)", "Bash(mem index)", "PowerShell(mem index)"}
	for _, rule := range want {
		if !contains(AllowRules, rule) {
			t.Errorf("%s 규칙이 없다", rule)
		}
	}
	for _, never := range []string{"Bash(mem status)", "Bash(mem index:*)", "PowerShell(mem index *)"} {
		if contains(AllowRules, never) {
			t.Errorf("%s 는 넣으면 안 된다", never)
		}
	}
	// 옛 정확 일치 규칙은 --undo 가 같이 빼야 한다.
	if !contains(RetiredAllowRules, "Bash(mem status)") {
		t.Error("옛 `Bash(mem status)` 가 폐기 목록에 없어 --undo 때 안 빠진다")
	}
}

// Bash 11개만 든 저장소에 init 을 다시 돌리면 빠진 PowerShell 꼴만 더해진다.
func TestInitFillsOnlyMissingPowerShellRules(t *testing.T) {
	root := newProject(t)
	before := []string{"Bash(git status)"}
	for _, rule := range AllowRules {
		if !isPowerShellShape(rule) {
			before = append(before, rule)
		}
	}
	writeSettings(t, root, allowJSON(t, before))

	mustInit(t, Options{Root: root})

	have := allowOf(t, root)
	if len(have) != len(before)+len(AllowRules)/2 {
		t.Fatalf("더해진 개수가 다르다 : %d → %d", len(before), len(have))
	}
	if !contains(have, "Bash(git status)") {
		t.Fatal("사람이 적은 규칙이 사라졌다")
	}
	for _, rule := range AllowRules {
		if countOf(have, rule) != 1 {
			t.Errorf("%s 가 %d번 들었다", rule, countOf(have, rule))
		}
	}
}

// doctor 는 Bash 꼴만 있으면 O 를 찍지 말고 PowerShell 꼴이 없다고 알려야 한다.
func TestDoctorWarnsWhenPowerShellShapeIsMissing(t *testing.T) {
	root := newProject(t)
	bashOnly := []string{}
	for _, rule := range AllowRules {
		if !isPowerShellShape(rule) {
			bashOnly = append(bashOnly, rule)
		}
	}
	writeSettings(t, root, allowJSON(t, bashOnly))

	check := allowCheck(root)
	if check.OK {
		t.Fatal("Bash 꼴만 있는데 allow 검사가 O 를 찍었다")
	}
	if !strings.Contains(check.Note, "PowerShell") {
		t.Fatalf("PowerShell 꼴이 없다고 안 알린다 : %s", check.Note)
	}

	mustInit(t, Options{Root: root})
	if after := allowCheck(root); !after.OK {
		t.Fatalf("init 뒤인데 아직 안 됐다고 한다 : %s", after.Note)
	}
}

// allowJSON 은 allow 규칙만 든 settings.json 글이다.
func allowJSON(t *testing.T, rules []string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"permissions": map[string]any{"allow": rules}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
