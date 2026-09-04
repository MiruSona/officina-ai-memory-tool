package install

// 7단계 코드리뷰 A — 설치가 진짜 사용자 PATH 를 건드리는 것을 막는 길과
// 되돌리는 길 (실데이터 시험 D1).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// 환경 변수 하나로 PATH 단계가 통째로 빠진다. 시험이 가짜 HOME 만 바꾸면
// bin 폴더는 가짜인데 PATH 는 진짜 HKCU 라 시험 경로가 레지스트리에 박힌다.
func TestEnvSkipsThePathStep(t *testing.T) {
	newHome(t)
	t.Setenv("MEM_INSTALL_NO_PATH", "1")
	fake := &fakeRegistry{value: PathValue{Text: `C:\Tools`}}
	defer UseRegistry(fake)()
	report, err := Install(Options{Apply: true, NoEmbed: true})
	if err != nil {
		t.Fatalf("install 실패 : %v", err)
	}
	if fake.writes != 0 {
		t.Fatalf("PATH 를 %d 번 썼다 — 한 번도 쓰면 안 된다", fake.writes)
	}
	for _, step := range report.Steps {
		if step.What == i18n.T(i18n.InstallStepPath) {
			t.Fatal("PATH 소단계가 표에 남았다")
		}
	}
	if !strings.Contains(strings.Join(report.Notes, "\n"), "MEM_INSTALL_NO_PATH") {
		t.Fatalf("건너뛴 것을 안 알렸다 : %v", report.Notes)
	}
}

// --undo --apply 는 백업 값으로 PATH 를 되돌린다.
func TestUndoRestoresThePath(t *testing.T) {
	home := newHome(t)
	before := `C:\Tools;C:\Other`
	fake := &fakeRegistry{value: PathValue{Text: before, Expand: true}}
	defer UseRegistry(fake)()
	if _, err := Install(Options{Apply: true, NoEmbed: true}); err != nil {
		t.Fatalf("install 실패 : %v", err)
	}
	if !strings.HasPrefix(fake.value.Text, filepath.Join(home, rootDirName, "bin")) {
		t.Fatalf("설치가 PATH 를 안 고쳤다 : %s", fake.value.Text)
	}
	report, err := Uninstall(Options{Apply: true})
	if err != nil {
		t.Fatalf("undo 실패 : %v", err)
	}
	if fake.value.Text != before {
		t.Fatalf("되돌린 값이 다르다 : %q, 원래 %q", fake.value.Text, before)
	}
	if !fake.value.Expand {
		t.Fatal("값 종류(REG_EXPAND_SZ)를 안 지켰다")
	}
	if report.Changed() != 1 {
		t.Fatalf("되돌린 소단계 수 %d", report.Changed())
	}
}

// --undo 만 주면(--apply 없이) 계획만 보여주고 아무것도 안 고친다.
func TestUndoWithoutApplyChangesNothing(t *testing.T) {
	newHome(t)
	fake := &fakeRegistry{value: PathValue{Text: `C:\Tools`}}
	defer UseRegistry(fake)()
	if _, err := Install(Options{Apply: true, NoEmbed: true}); err != nil {
		t.Fatalf("install 실패 : %v", err)
	}
	after := fake.value.Text
	if _, err := Uninstall(Options{}); err != nil {
		t.Fatalf("undo 실패 : %v", err)
	}
	if fake.value.Text != after {
		t.Fatalf("--apply 없이 PATH 를 고쳤다 : %s", fake.value.Text)
	}
}

// 백업이 없으면 우리가 안 고친 PATH 다. 아무것도 안 한다.
func TestUndoWithoutBackupDoesNothing(t *testing.T) {
	home := newHome(t)
	fake := &fakeRegistry{value: PathValue{Text: filepath.Join(home, rootDirName, "bin") + `;C:\Tools`}}
	defer UseRegistry(fake)()
	if _, err := os.Stat(BackupPath(home)); err == nil {
		t.Fatal("백업이 미리 있으면 안 된다")
	}
	report, err := Uninstall(Options{Apply: true})
	if err != nil {
		t.Fatalf("undo 실패 : %v", err)
	}
	if fake.writes != 0 {
		t.Fatalf("백업이 없는데 PATH 를 %d 번 썼다", fake.writes)
	}
	if report.Changed() != 0 {
		t.Fatalf("고친 것이 %d 개다", report.Changed())
	}
}

// 못 놓은 임베딩 파일을 「고쳤다」로 세지 않는다 (실데이터 시험 C7).
func TestFailedEmbedIsNotCountedAsChanged(t *testing.T) {
	newHome(t)
	t.Setenv("MEM_INSTALL_NO_PATH", "1")
	// 빈 폴더를 꾸러미로 준다 — 꾸러미에 파일이 하나도 없으니 다 실패한다.
	report, err := Install(Options{Apply: true, Bundle: t.TempDir()})
	if err != nil {
		t.Fatalf("install 실패 : %v", err)
	}
	for _, step := range report.Steps {
		if strings.HasPrefix(step.What, i18n.T(i18n.EmbedStepRuntime)) ||
			strings.HasPrefix(step.What, i18n.T(i18n.EmbedStepModel)) {
			if step.Changed {
				t.Fatalf("못 놓은 소단계를 고쳤다고 센다 : %s / %s", step.What, step.Todo)
			}
			if step.Todo != i18n.T(i18n.EmbedTodoFailed) {
				t.Fatalf("할 일 칸이 아직 %q 다", step.Todo)
			}
		}
	}
}
