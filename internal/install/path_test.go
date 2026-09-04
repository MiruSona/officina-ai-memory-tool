package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// fakeRegistry 는 진짜 레지스트리 대신 값 하나를 들고 있는 가짜다.
type fakeRegistry struct {
	value   PathValue
	writes  int
	failing bool
}

func (f *fakeRegistry) Read() (PathValue, error) {
	if f.failing {
		return PathValue{}, ErrNoPathSupport
	}
	return f.value, nil
}

func (f *fakeRegistry) Write(value PathValue) error {
	if f.failing {
		return ErrNoPathSupport
	}
	f.value = value
	f.writes++
	return nil
}

// newHome 은 가짜 홈을 깐다. 진짜 %USERPROFILE% 은 안 건드린다.
func newHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("PATH", os.Getenv("PATH"))
	return home
}

// PATH 맨 앞에 넣고, 값 종류(REG_EXPAND_SZ)를 그대로 지키고, 백업을 남긴다.
func TestPathGoesToFrontKeepingKind(t *testing.T) {
	home := newHome(t)
	fake := &fakeRegistry{value: PathValue{Text: `%USERPROFILE%\bin;C:\Tools`, Expand: true}}
	defer UseRegistry(fake)()
	report, err := Install(Options{Apply: true})
	if err != nil {
		t.Fatalf("install 실패 : %v", err)
	}
	if fake.writes != 1 {
		t.Fatalf("PATH 를 %d 번 썼다", fake.writes)
	}
	want := filepath.Join(home, rootDirName, "bin")
	if !strings.HasPrefix(fake.value.Text, want+";") {
		t.Errorf("맨 앞에 안 들어갔다 : %s", fake.value.Text)
	}
	if !strings.Contains(fake.value.Text, `%USERPROFILE%\bin`) {
		t.Errorf("원래 참조가 펴졌다 : %s", fake.value.Text)
	}
	if !fake.value.Expand {
		t.Error("값 종류(REG_EXPAND_SZ)를 못 지켰다")
	}
	backup := readFile(t, BackupPath(home))
	if !strings.Contains(backup, `C:\Tools`) {
		t.Errorf("백업이 원래 값이 아니다 : %s", backup)
	}
	if !strings.Contains(strings.Join(report.Lines(), "\n"), i18n.T(i18n.InstallNewTerminal)) {
		t.Error("새 터미널 안내가 없다")
	}
}

// 이미 들어 있으면 다시 안 쓴다. 두 번째 install 은 아무것도 안 바꾼다.
func TestPathAlreadyThere(t *testing.T) {
	home := newHome(t)
	bin := filepath.Join(home, rootDirName, "bin")
	fake := &fakeRegistry{value: PathValue{Text: bin + `;C:\Tools`, Expand: false}}
	defer UseRegistry(fake)()
	if _, err := Install(Options{Apply: true}); err != nil {
		t.Fatal(err)
	}
	if fake.writes != 0 {
		t.Errorf("이미 들어 있는데 %d 번 썼다", fake.writes)
	}
	if _, err := Install(Options{Apply: true}); err != nil {
		t.Fatal(err)
	}
}

// --dry-run 은 레지스트리에 안 쓰고 .aimemory 도 안 만든다.
func TestInstallDryRun(t *testing.T) {
	home := newHome(t)
	fake := &fakeRegistry{value: PathValue{Text: `C:\Tools`, Expand: true}}
	defer UseRegistry(fake)()
	report, err := Install(Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if fake.writes != 0 {
		t.Error("dry-run 이 레지스트리에 썼다")
	}
	if _, err := os.Stat(filepath.Join(home, rootDirName)); err == nil {
		t.Error("dry-run 이 .aimemory 를 만들었다")
	}
	table := strings.Join(report.Lines(), "\n")
	for _, want := range []string{i18n.T(i18n.InstallStepExe),
		i18n.T(i18n.InstallStepPath), i18n.T(i18n.InstallDryRunFooter)} {
		if !strings.Contains(table, want) {
			t.Errorf("dry-run 표에 %s 가 없다\n%s", want, table)
		}
	}
}

// 인자 없이 부르면 --dry-run 을 준 것과 같다 — Apply 가 없으면 항상 안전 쪽이다.
func TestInstallNoArgsIsSafe(t *testing.T) {
	home := newHome(t)
	fake := &fakeRegistry{value: PathValue{Text: `C:\Tools`, Expand: true}}
	defer UseRegistry(fake)()
	report, err := Install(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if fake.writes != 0 {
		t.Error("인자 없는 install 이 레지스트리에 썼다")
	}
	if _, err := os.Stat(filepath.Join(home, rootDirName)); err == nil {
		t.Error("인자 없는 install 이 .aimemory 를 만들었다")
	}
	if !strings.Contains(strings.Join(report.Lines(), "\n"), i18n.T(i18n.InstallDryRunFooter)) {
		t.Error("--apply 안내가 없다")
	}
}

// --dry-run 과 --apply 를 같이 주면 안전 쪽(dry-run)이 이긴다.
func TestInstallDryRunBeatsApply(t *testing.T) {
	newHome(t)
	fake := &fakeRegistry{}
	defer UseRegistry(fake)()
	if _, err := Install(Options{DryRun: true, Apply: true}); err != nil {
		t.Fatal(err)
	}
	if fake.writes != 0 {
		t.Error("--dry-run --apply 인데 레지스트리에 썼다")
	}
}

// --no-path 는 --apply 와 같이 써야 PATH 소단계만 건너뛴다.
func TestInstallNoPath(t *testing.T) {
	newHome(t)
	fake := &fakeRegistry{}
	defer UseRegistry(fake)()
	report, err := Install(Options{Apply: true, NoPath: true})
	if err != nil {
		t.Fatal(err)
	}
	if fake.writes != 0 {
		t.Error("--no-path 인데 PATH 를 썼다")
	}
	if strings.Contains(strings.Join(report.Lines(), "\n"), i18n.T(i18n.InstallStepPath)) {
		t.Error("--no-path 인데 PATH 줄이 표에 있다")
	}
}

// PATH 를 못 읽으면 오류가 아니라 「손으로」 로 넘긴다.
func TestPathUnreadableIsManual(t *testing.T) {
	newHome(t)
	fake := &fakeRegistry{failing: true}
	defer UseRegistry(fake)()
	report, err := Install(Options{})
	if err != nil {
		t.Fatalf("PATH 를 못 읽었다고 install 이 실패했다 : %v", err)
	}
	if !strings.Contains(strings.Join(report.Lines(), "\n"), i18n.T(i18n.InstallStateNoPathAPI)) {
		t.Error("PATH 를 못 읽었다고 알리지 않았다")
	}
}

// listedIn 은 %USERPROFILE% 을 풀어 견준다.
func TestListedInExpandsReferences(t *testing.T) {
	home := newHome(t)
	bin := filepath.Join(home, rootDirName, "bin")
	if !listedIn(`C:\Tools;%USERPROFILE%\`+rootDirName+`\bin`, bin) {
		t.Error("참조가 든 항목을 못 알아봤다")
	}
	if listedIn(`C:\Tools;C:\Other`, bin) {
		t.Error("없는 것을 있다고 했다")
	}
}
