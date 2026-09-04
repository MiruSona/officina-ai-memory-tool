package install

// mem status --doctor 가 쓰는 환경 점검이다 (설계 6-1). 여기서는 아무것도
// 고치지 않는다 — 무엇이 어긋났고 무엇을 치면 되는지만 말한다.
// 화면에 찍는 것은 cmd 쪽이 한다.

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
)

// staleLockAfter 는 락이 이만큼 앉아 있으면 잔해로 보는 시간이다. 색인 쪽
// 값(90초)과 같은 자다 — 여기서 더 짧게 잡으면 도는 색인을 잔해라고 말한다.
const staleLockAfter = 90 * time.Second

// Check 는 점검 한 줄이다. OK 가 거짓이면 Note 에 할 일이 적힌다.
type Check struct {
	// What 은 무엇을 봤는지다.
	What string `json:"what"`
	// OK 는 지금 상태가 괜찮은지다.
	OK bool `json:"ok"`
	// Note 는 사람이 읽을 한 줄이다. 괜찮을 때도 알림이 붙을 수 있다.
	Note string `json:"note"`
	// Security 는 이 점검이 보안 차단인지다. 걸리면 종료 4 다 (설계 6-1).
	Security bool `json:"security,omitempty"`
}

// Doctor 는 프로젝트 하나를 훑는다. root 는 Memory/ 와 .claude/ 를 담은 폴더다.
func Doctor(root string) []Check {
	return DoctorAt(root, filepath.Join(root, config.DirName))
}

// DoctorAt 은 기억 폴더 자리를 **직접 받아** 훑는다.
//
// `--repo` 로 기억 폴더를 곧장 준 사람은 그 폴더 이름이 `Memory` 가 아닐 수
// 있다. root 에서 이름을 다시 지어 찾으면 있는 색인을 「없다」고 한다 (리뷰 D6).
func DoctorAt(root, memory string) []Check {
	checks := hookChecks(root)
	return append(checks,
		allowCheck(root),
		trustCheck(),
		pathCheck(),
		databaseCheck(memory),
		lockCheck(memory),
		shadowCheck(root),
		embedCheck(),
	)
}

// shadowCheck 는 저장소 자리에 놓인 mem 을 찾는다. 윈도우는 지금 폴더의 exe 를
// PATH 보다 먼저 잡아서, 남이 놓아둔 것이 조용히 돈다. lint 는 잡는데 doctor 가
// 못 보던 자리다 (보안시험 M-3 · 설계 6-1 종료 4).
func shadowCheck(root string) Check {
	check := Check{What: i18n.T(i18n.DoctorShadow), OK: true, Security: true,
		Note: i18n.T(i18n.DoctorShadowNone)}
	for _, dir := range []string{root, filepath.Join(root, config.DirName)} {
		for _, name := range []string{"mem.exe", "mem"} {
			info, err := os.Lstat(filepath.Join(dir, name))
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			return Check{What: check.What, Security: true,
				Note: i18n.T(i18n.DoctorShadowFound, name)}
		}
	}
	return check
}

// hookChecks 는 .claude/settings.json 에 우리 훅이 붙어 있는지를 **훅마다** 본다.
// init 이 붙이는 표(hookSpecs)를 그대로 돌아서 훅이 늘면 점검도 같이 는다 —
// 예전에는 SessionStart 만 봐서 SubagentStart 가 빠져도 「괜찮다」고 했다.
func hookChecks(root string) []Check {
	settings, _, existed, err := loadSettings(ClaudeSettingsPath(root))
	checks := []Check{}
	for _, spec := range hookSpecs {
		check := Check{What: i18n.T(i18n.DoctorHookOne, spec.event)}
		switch {
		case err != nil || !existed:
			check.Note = i18n.T(i18n.DoctorHookMissing)
		case !hasMemHook(eventList(settings, spec.event), spec.action):
			check.Note = i18n.T(i18n.DoctorHookMissing)
		default:
			check.OK = true
		}
		checks = append(checks, check)
	}
	return checks
}

// allowCheck 는 auto 모드 allow 규칙이 다 들어 있는지다. 없으면 mem 을 부를
// 때마다 분류기 왕복이 붙는다 (설계 5-2).
func allowCheck(root string) Check {
	check := Check{What: i18n.T(i18n.DoctorAllow)}
	settings, _, existed, err := loadSettings(ClaudeSettingsPath(root))
	if err != nil || !existed {
		check.Note = i18n.T(i18n.DoctorAllowMissing, len(AllowRules))
		return check
	}
	missing := MissingAllow(settings)
	if len(missing) > 0 {
		check.Note = allowNote(missing)
		return check
	}
	check.OK = true
	return check
}

// allowNote 는 빠진 규칙을 어떻게 알릴지다. 빠진 것이 전부 PowerShell 꼴이면
// Bash 꼴만 넣던 옛 판이 남긴 자리라 Windows 에서 안 맞는다고 못 박는다.
func allowNote(missing []string) string {
	for _, rule := range missing {
		if !isPowerShellShape(rule) {
			return i18n.T(i18n.DoctorAllowMissing, len(missing))
		}
	}
	return i18n.T(i18n.DoctorAllowShell, len(missing))
}

// trustCheck 는 사람만 할 수 있는 것이라 늘 안내다. 프로젝트 settings 의
// allow 규칙은 폴더를 신뢰해야 살아난다 (설계 5-4 · 조사 C 2-4).
func trustCheck() Check {
	return Check{What: i18n.T(i18n.DoctorTrust), OK: true, Note: i18n.T(i18n.DoctorTrustNote)}
}

// pathCheck 는 PATH 에서 mem 이 잡히는지다. 안 잡히면 훅이 조용히 논다.
func pathCheck() Check {
	check := Check{What: i18n.T(i18n.DoctorPath)}
	machine, err := Paths()
	if err != nil || machine.InPath == "" {
		check.Note = i18n.T(i18n.InitPathMissing)
		return check
	}
	check.OK, check.Note = true, machine.InPath
	return check
}

// databaseCheck 는 색인이 읽을 만한지다. 없거나 빈 파일이면 훅이 침묵한다.
func databaseCheck(memory string) Check {
	check := Check{What: i18n.T(i18n.DoctorDB)}
	if !index.Usable(memory) {
		check.Note = i18n.T(i18n.DoctorDBMissing)
		return check
	}
	database, err := index.Open(memory)
	if err != nil {
		check.Note = i18n.T(i18n.DoctorDBBroken)
		return check
	}
	defer database.Close()
	count, err := database.Count()
	if err != nil {
		check.Note = i18n.T(i18n.DoctorDBBroken)
		return check
	}
	check.OK, check.Note = true, i18n.T(i18n.DoctorDBOK, count)
	return check
}

// lockCheck 는 죽은 락과 그 잔해가 남았는지다. 잔해는 다음 mem index 가 치운다.
func lockCheck(memory string) Check {
	check := Check{What: i18n.T(i18n.DoctorLock), OK: true}
	stale := 0
	entries, err := os.ReadDir(memory)
	if err != nil {
		return check
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), index.LockName+".stale.") {
			stale++
		}
	}
	if info, err := os.Stat(filepath.Join(memory, index.LockName)); err == nil {
		if time.Since(info.ModTime()) > staleLockAfter {
			check.OK = false
			check.Note = i18n.T(i18n.DoctorLockStuck)
			return check
		}
	}
	if stale > 0 {
		check.Note = i18n.T(i18n.DoctorLockLeftover, stale)
	}
	return check
}
