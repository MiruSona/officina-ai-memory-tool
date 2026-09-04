package install

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// backupName 은 PATH 를 고치기 전 원래 값을 담는 파일이다 (설계 3-1 ②).
const backupName = "path-backup.txt"

// PathValue 는 사용자 PATH 의 날 값과 그 값 종류다. Expand 가 참이면
// REG_EXPAND_SZ 라 %USERPROFILE% 같은 참조가 살아 있다.
type PathValue struct {
	Text   string
	Expand bool
}

// PathRegistry 는 사용자 PATH 를 읽고 쓰는 자리다. 시험에서는 가짜로 갈아 끼워
// 진짜 레지스트리를 건드리지 않는다.
type PathRegistry interface {
	Read() (PathValue, error)
	Write(PathValue) error
}

// ErrNoPathSupport 는 이 OS 에 우리가 고칠 사용자 PATH 가 없다는 뜻이다.
var ErrNoPathSupport = errors.New("no user path on this os")

// registry 는 갈아 끼울 수 있는 실제 구현이다.
var registry PathRegistry = windowsRegistry{}

// UseRegistry 는 시험이 가짜 구현을 끼우고 되돌리게 해 준다.
func UseRegistry(fake PathRegistry) func() {
	before := registry
	registry = fake
	return func() { registry = before }
}

// ensurePath 는 bin 폴더를 사용자 PATH 맨 앞에 넣는다. 넣기 전에 원래 값을
// 파일로 남기고, 값 종류는 있던 그대로 다시 쓴다 (설계 10절).
func ensurePath(machine Machine, dryRun bool) (Step, error) {
	step := Step{What: i18n.T(i18n.InstallStepPath)}
	current, err := registry.Read()
	if err != nil {
		step.Now, step.Todo, step.Manual = i18n.T(i18n.InstallStateNoPathAPI), i18n.T(i18n.InitTodoManual), true
		return step, nil
	}
	if listedIn(current.Text, machine.BinDir) {
		step.Now, step.Todo = i18n.T(i18n.InstallStateInPath), i18n.T(i18n.InitTodoKeep)
		return step, nil
	}
	step.Now = i18n.T(i18n.InstallStateNotInPath)
	step.Todo, step.Changed = i18n.T(i18n.InstallTodoAddPath), true
	if dryRun {
		return step, nil
	}
	if err := backupUserPath(machine, current.Text); err != nil {
		return step, err
	}
	if err := registry.Write(PathValue{Text: withFront(current.Text, machine.BinDir), Expand: current.Expand}); err != nil {
		return step, err
	}
	os.Setenv("PATH", machine.BinDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return step, nil
}

// withFront 는 우리 폴더를 맨 앞에 붙인다. 다른 폴더에 떨어진 mem.exe 가
// 먼저 대답하면 안 된다 (설계 10절 그림자 exe).
// 우리가 붙이는 것 말고 남의 값은 그대로 둔다. 앞뒤 `;` 를 떼면 우리가 안
// 건드려야 할 것을 건드리는 것이다 (리뷰 A #18).
func withFront(current, dir string) string {
	if strings.TrimSpace(current) == "" {
		return dir
	}
	if strings.HasPrefix(current, ";") {
		return dir + current
	}
	return dir + ";" + current
}

// BackupPath 는 PATH 백업 파일이 놓이는 자리다.
func BackupPath(home string) string {
	return filepath.Join(home, rootDirName, backupName)
}

func backupUserPath(machine Machine, current string) error {
	if err := os.MkdirAll(filepath.Join(machine.Home, rootDirName), 0o755); err != nil {
		return err
	}
	return os.WriteFile(BackupPath(machine.Home), []byte(current+"\n"), 0o644)
}

// listedIn 은 %USERPROFILE% 같은 참조를 푼 뒤에 견준다. 날 값은 참조를 그대로
// 갖고 있고 우리 폴더는 풀린 경로라서 그냥 비교하면 못 찾는다.
func listedIn(pathValue, dir string) bool {
	for _, entry := range strings.Split(pathValue, ";") {
		if samePath(expandRefs(strings.TrimSpace(entry)), dir) {
			return true
		}
	}
	return false
}

// expandRefs 는 %이름% 을 환경 변수 값으로 바꾼다. 모르는 이름은 그대로 둔다.
func expandRefs(text string) string {
	out := strings.Builder{}
	rest := text
	for {
		open := strings.Index(rest, "%")
		if open < 0 {
			break
		}
		shut := strings.Index(rest[open+1:], "%")
		if shut < 0 {
			break
		}
		name := rest[open+1 : open+1+shut]
		value, found := os.LookupEnv(name)
		if !found {
			out.WriteString(rest[:open+shut+2])
			rest = rest[open+shut+2:]
			continue
		}
		out.WriteString(rest[:open] + value)
		rest = rest[open+shut+2:]
	}
	return out.String() + rest
}

// 사용자 PATH 가 사는 레지스트리 자리다. [Environment]::SetEnvironmentVariable
// 을 안 쓰는 이유는, 그게 %USERPROFILE% 같은 참조를 고정 글로 펴고 값 종류를
// REG_SZ 로 바꿔 버려서 다른 프로그램이 참조를 잃기 때문이다.
const (
	environmentKey = "Environment"
	pathValueName  = "Path"
)

// windowsRegistry 는 powershell 로 레지스트리를 직접 연다. setx 는 1024자에서
// 잘라 먹어서 쓰지 않는다.
type windowsRegistry struct{}

// readScript 는 날 값과 값 종류를 탭으로 갈라 한 줄로 낸다.
func readScript() string {
	return "$k = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('" + environmentKey + "'); " +
		"if ($k -eq $null) { \"`tString\" } else { " +
		"$v = $k.GetValue('" + pathValueName + "', '', 'DoNotExpandEnvironmentNames'); " +
		"$t = 'String'; " +
		"if ($k.GetValueNames() -icontains '" + pathValueName + "') { $t = $k.GetValueKind('" + pathValueName + "') }; " +
		"$k.Close(); if ($v -eq $null) { $v = '' }; \"$v`t$t\" }"
}

// writeScript 는 받은 값을 받은 종류 그대로 다시 쓴다.
func writeScript() string {
	return "$k = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('" + environmentKey + "', $true); " +
		"if ($k -eq $null) { $k = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey('" + environmentKey + "') }; " +
		"$kind = [Microsoft.Win32.RegistryValueKind]::String; " +
		"if ($env:MEM_PATH_EXPAND -eq '1') { $kind = [Microsoft.Win32.RegistryValueKind]::ExpandString }; " +
		"$k.SetValue('" + pathValueName + "', $env:MEM_PATH_VALUE, $kind); $k.Close()"
}

func (windowsRegistry) Read() (PathValue, error) {
	if runtime.GOOS != "windows" {
		return PathValue{}, ErrNoPathSupport
	}
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", readScript()).Output()
	if err != nil {
		return PathValue{}, err
	}
	text, kind, _ := strings.Cut(strings.TrimRight(string(out), "\r\n"), "\t")
	return PathValue{Text: text, Expand: strings.EqualFold(strings.TrimSpace(kind), "ExpandString")}, nil
}

func (windowsRegistry) Write(value PathValue) error {
	if runtime.GOOS != "windows" {
		return ErrNoPathSupport
	}
	expand := "0"
	if value.Expand {
		expand = "1"
	}
	command := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", writeScript())
	command.Env = append(os.Environ(), "MEM_PATH_VALUE="+value.Text, "MEM_PATH_EXPAND="+expand)
	return command.Run()
}

// noPathEnv 는 PATH 단계를 통째로 건너뛰게 하는 환경 변수다. 시험이 가짜
// HOME(`USERPROFILE`)만 바꾸면 bin 폴더는 가짜로 가지만 **사용자 PATH 는 진짜
// HKCU** 라 시험 경로가 레지스트리에 박힌다 (실데이터 시험 D1). 옵션(`--no-path`)
// 은 사람이 손으로 칠 때 쓰고, 이 변수는 스크립트·시험이 통째로 막을 때 쓴다.
const noPathEnv = "MEM_INSTALL_NO_PATH"

// SkipPathByEnv 는 환경 변수로 PATH 단계를 막았는지 말한다.
func SkipPathByEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(noPathEnv))) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

// restorePath 는 install 이 남긴 path-backup.txt 로 사용자 PATH 를 되돌린다.
// 백업이 없으면 아무것도 안 한다 — 우리가 안 고친 PATH 를 우리가 지우면 안 된다.
func restorePath(machine Machine, dryRun bool) (Step, error) {
	step := Step{What: i18n.T(i18n.InstallStepPath)}
	saved, found, err := readText(BackupPath(machine.Home))
	if err != nil {
		return step, err
	}
	if !found {
		step.Now, step.Todo = i18n.T(i18n.InstallStateNoBackup), i18n.T(i18n.InitTodoKeep)
		return step, nil
	}
	current, err := registry.Read()
	if err != nil {
		step.Now, step.Todo, step.Manual = i18n.T(i18n.InstallStateNoPathAPI), i18n.T(i18n.InitTodoManual), true
		return step, nil
	}
	if !listedIn(current.Text, machine.BinDir) {
		step.Now, step.Todo = i18n.T(i18n.InstallStateNotInPath), i18n.T(i18n.InitTodoKeep)
		return step, nil
	}
	step.Now = i18n.T(i18n.InstallStateInPath)
	step.Todo, step.Changed = i18n.T(i18n.InstallTodoRestorePath), true
	if dryRun {
		return step, nil
	}
	return step, registry.Write(PathValue{Text: strings.TrimRight(saved, "\r\n"), Expand: current.Expand})
}
