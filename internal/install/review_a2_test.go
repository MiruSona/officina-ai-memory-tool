package install

import (
	"os"
	"path/filepath"
	"testing"
)

// 리뷰 A(2회차) M-3 — 저장소 자리에 놓인 mem.exe 를 doctor 도 본다. lint 만
// 잡고 doctor 는 「PATH 의 mem」만 찍고 넘어가던 자리다.
func TestDoctorSeesShadowExe(t *testing.T) {
	root := newProject(t)
	found := shadowOf(Doctor(root))
	if found == nil || !found.OK {
		t.Fatalf("그림자 exe 가 없는데 걸렸다 : %+v", found)
	}
	if err := os.WriteFile(filepath.Join(root, "mem.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	found = shadowOf(Doctor(root))
	if found == nil || found.OK {
		t.Fatalf("저장소 자리의 mem.exe 를 못 봤다 : %+v", found)
	}
	if !found.Security {
		t.Fatal("그림자 exe 는 보안 차단이라 종료 4 여야 한다")
	}
}

func shadowOf(checks []Check) *Check {
	for at, check := range checks {
		if check.Security {
			return &checks[at]
		}
	}
	return nil
}

// 죽은 규칙 `Bash(mem log:*)` 는 이제 안 넣는다 — `mem log` 는 없는 명령이고
// `mem status --log` 로 접혔다.
func TestDeadLogAllowRuleIsGone(t *testing.T) {
	for _, rule := range AllowRules {
		if rule == "Bash(mem log:*)" {
			t.Fatal("없는 명령의 규칙을 아직 넣는다")
		}
	}
	// 옛 판이 넣어 둔 것은 --undo 가 같이 뺀다.
	root := newObject()
	permissions := newObject()
	permissions.set(allowKey, []any{"Bash(mem log:*)", "Bash(남의 것)"})
	root.set(permissionsKey, permissions)
	if !removeAllow(root) {
		t.Fatal("옛 규칙을 못 뺐다")
	}
	left := allowList(root)
	if len(left) != 1 || left[0] != "Bash(남의 것)" {
		t.Fatalf("남의 규칙까지 건드렸다 : %v", left)
	}
}
