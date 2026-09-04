// Package install 은 mem install · mem init · mem init --undo 를 맡는다.
// settings.json 병합, .gitignore/.gitattributes 줄, 규칙 블록, 사용자 PATH 다.
package install

import (
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// ExeName 은 훅과 규칙 블록에 적히는 명령 이름이다. 절대경로를 안 쓴다 (설계 7-1).
const ExeName = "mem"

// rootDirName 은 사용자 홈 밑의 기계별 폴더다 (설계 3-1 ②).
const rootDirName = ".aimemory"

// Options 는 install · init · undo 가 같이 쓰는 깃발이다.
// Apply 는 install 전용이다 — 참이어야 exe 복사·PATH 를 실제로 건드린다.
type Options struct {
	Root   string
	DryRun bool
	Apply  bool
	NoPath bool
	NoHook bool
	// NoSubagentHook 은 SessionStart 만 붙이는 것이다. --no-hook 은 둘 다 안 붙인다.
	NoSubagentHook bool
	Gemini         bool
	// NoEmbed 는 의미 검색을 빼고 설치하는 것이다. **기본은 넣는 것이다** —
	// 빼려면 명시로 `--no-embed` 를 줘야 한다 (사용자 결정 2026-08-23).
	NoEmbed bool
	// Bundle 은 오프라인 꾸러미 폴더다. 주면 인터넷 대신 여기서 가져온다.
	Bundle string
	// Model 은 쓸 모델 이름이다. 비면 기본 모델이다.
	Model string
	// Solo 는 혼자 쓰는 사람이다. 내장 auto memory 를 끄자고 **권하기만** 한다 —
	// 안전망 하나를 우리가 말없이 꺼 버리지 않는다 (설계 5-3).
	Solo bool
}

// Step 은 계획 표의 한 줄이다 : 무엇을 · 지금 어떤지 · 무엇을 하는지.
type Step struct {
	What    string
	Now     string
	Todo    string
	Changed bool
	Manual  bool
}

// Report 는 한 번 돌린 결과다.
type Report struct {
	Steps []Step
	Notes []string
}

func (r *Report) add(step Step) { r.Steps = append(r.Steps, step) }

func (r *Report) note(line string) { r.Notes = append(r.Notes, line) }

// Changed 는 실제로 뭔가 건드린 소단계 수다.
func (r *Report) Changed() int {
	count := 0
	for _, step := range r.Steps {
		if step.Changed {
			count++
		}
	}
	return count
}

// changedStep 은 이름으로 찾은 한 소단계가 정말 뭔가 했는지 말한다. PATH 안내를
// 건너뛴 소단계에 붙이면 안 된다.
func (r *Report) changedStep(what string) bool {
	for _, step := range r.Steps {
		if step.What == what {
			return step.Changed
		}
	}
	return false
}

// Lines 는 표를 먼저, 안내를 뒤에 낸다.
func (r *Report) Lines() []string {
	lines := []string{i18n.T(i18n.InitTableHeader)}
	for _, step := range r.Steps {
		lines = append(lines, "| "+step.What+" | "+step.Now+" | "+step.Todo+" |")
	}
	return append(lines, r.Notes...)
}

// Machine 은 mem install 이 다루는 기계별 자리다.
type Machine struct {
	Home    string
	BinDir  string
	ExePath string
	Current string
	InPath  string
}

// Paths 는 exe 가 어디 있고 어디 있어야 하는지, PATH 에서 이름이 잡히는지 알려준다.
func Paths() (Machine, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Machine{}, err
	}
	root := filepath.Join(home, rootDirName)
	machine := Machine{Home: home, BinDir: filepath.Join(root, "bin")}
	machine.ExePath = filepath.Join(machine.BinDir, exeFileName())
	machine.Current, _ = os.Executable()
	machine.InPath, _ = exec.LookPath(ExeName)
	return machine, nil
}

func exeFileName() string {
	if runtime.GOOS == "windows" {
		return ExeName + ".exe"
	}
	return ExeName
}

// Install 은 기계를 한 번 갖춘다 : exe 자기 복사 · 사용자 PATH.
// options.Apply 가 참이어야 실제로 건드린다 — 아니면 계획 표만 보여주고
// exit 은 정상(0)이다. 명시로 준 --dry-run 도 늘 안전 쪽으로 이긴다.
func Install(options Options) (*Report, error) {
	machine, err := Paths()
	if err != nil {
		return nil, err
	}
	dryRun := options.DryRun || !options.Apply
	report := &Report{}
	steps := []func() (Step, error){
		func() (Step, error) { return copyExe(machine, dryRun) },
	}
	skipPath := options.NoPath || SkipPathByEnv()
	if !skipPath {
		steps = append(steps, func() (Step, error) { return ensurePath(machine, dryRun) })
	}
	if err := collect(report, steps); err != nil {
		return report, err
	}
	// 임베딩은 exe·PATH 다음이다. 여기서 실패해도 설치는 성공으로 끝난다.
	embedStep(report, options, dryRun)
	if !dryRun && report.changedStep(i18n.T(i18n.InstallStepPath)) {
		// 되돌릴 자리를 안 알려주면 백업이 있어도 아무도 못 찾는다 (리뷰 C #20).
		report.note(i18n.T(i18n.InstallPathBackedUp, BackupPath(machine.Home)))
		report.note(i18n.T(i18n.InstallNewTerminal))
	}
	if skipPath && SkipPathByEnv() {
		report.note(i18n.T(i18n.InstallPathSkipped))
	}
	if !dryRun {
		report.note(i18n.T(i18n.InstallCheck))
	}
	addInstallFooter(report, dryRun, doneLine(report.Changed()))
	return report, nil
}

// Uninstall 은 `mem install --undo` 다. install 이 고친 **사용자 PATH 만**
// 백업 파일로 되돌린다. exe 와 모델은 안 지운다 — 돌고 있는 exe 는 Windows 에서
// 못 지우고, 모델은 145MB 라 다시 받는 값이 크다 (실데이터 시험 D1).
func Uninstall(options Options) (*Report, error) {
	machine, err := Paths()
	if err != nil {
		return nil, err
	}
	dryRun := options.DryRun || !options.Apply
	report := &Report{}
	if options.NoPath || SkipPathByEnv() {
		report.note(i18n.T(i18n.InstallPathSkipped))
	} else if err := collect(report, []func() (Step, error){
		func() (Step, error) { return restorePath(machine, dryRun) },
	}); err != nil {
		return report, err
	}
	if _, err := os.Stat(machine.ExePath); err == nil {
		report.note(i18n.T(i18n.InstallUndoExeNote, machine.ExePath))
	}
	if !dryRun && report.changedStep(i18n.T(i18n.InstallStepPath)) {
		report.note(i18n.T(i18n.InstallNewTerminal))
	}
	addInstallFooter(report, dryRun, undoneLine(report.Changed()))
	return report, nil
}

// addInstallFooter 는 addFooter 와 달리 install 전용 안내(--apply)를 붙인다.
func addInstallFooter(report *Report, dryRun bool, done string) {
	if dryRun {
		report.note(i18n.T(i18n.InstallDryRunFooter))
		return
	}
	report.note(done)
}

// copyExe 는 돌고 있는 exe 를 기계 bin 폴더에 옮겨 담는다. 이미 그 자리에서
// 돌고 있거나 같은 바이트면 건너뛴다 — Windows 는 돌고 있는 이미지를 못 덮는다.
func copyExe(machine Machine, dryRun bool) (Step, error) {
	step := Step{What: i18n.T(i18n.InstallStepExe), Todo: i18n.T(i18n.InstallTodoCopy), Changed: true}
	if samePath(machine.Current, machine.ExePath) {
		step.Now, step.Todo, step.Changed = i18n.T(i18n.InstallStateSelf), i18n.T(i18n.InitTodoKeep), false
		return step, nil
	}
	step.Now = i18n.T(i18n.InitStateMissing)
	if _, err := os.Stat(machine.ExePath); err == nil {
		step.Now = i18n.T(i18n.InitStatePresent)
	}
	if sameBytes(machine.Current, machine.ExePath) {
		step.Now, step.Todo, step.Changed = i18n.T(i18n.InstallStateSameExe), i18n.T(i18n.InitTodoKeep), false
		return step, nil
	}
	if dryRun || machine.Current == "" {
		return step, nil
	}
	return step, copyFile(machine.Current, machine.ExePath)
}

// sameBytes 는 두 파일이 같은 exe 인지 본다. 같으면 복사할 것이 없다.
func sameBytes(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	first, err := fileHash(left)
	if err != nil {
		return false
	}
	second, err := fileHash(right)
	if err != nil {
		return false
	}
	return first == second
}

func fileHash(path string) ([sha256.Size]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(data), nil
}

// copyFile 은 있던 판을 .bak 으로 밀어내고 새 판을 옆에 쓴 뒤 rename 한다.
// 반쯤 쓰인 exe 가 절대 보이지 않는다.
func copyFile(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if old, err := os.ReadFile(target); err == nil {
		if err := os.WriteFile(target+".bak", old, 0o755); err != nil {
			return err
		}
	}
	temporary := target + ".tmp"
	if err := os.WriteFile(temporary, data, 0o755); err != nil {
		return err
	}
	if err := os.Rename(temporary, target); err != nil {
		os.Remove(temporary)
		return &BusyExeError{Path: target}
	}
	return nil
}

// BusyExeError 는 돌고 있는 mem.exe 를 못 덮었다는 뜻이다. 고장이 아니라
// "mem 을 닫고 다시 해라" 다.
type BusyExeError struct {
	Path string
}

func (e *BusyExeError) Error() string {
	return i18n.T(i18n.InstallExeBusy, shortPath(e.Path))
}

func collect(report *Report, steps []func() (Step, error)) error {
	for _, run := range steps {
		step, err := run()
		if err != nil {
			return err
		}
		report.add(step)
		if step.Manual {
			report.note(i18n.T(i18n.InitManualHook, step.What))
		}
	}
	return nil
}

func addFooter(report *Report, dryRun bool, done string) {
	if dryRun {
		report.note(i18n.T(i18n.InitDryRunFooter))
		return
	}
	report.note(done)
}

func doneLine(changed int) string {
	if changed == 0 {
		return i18n.T(i18n.InitDoneNothing)
	}
	return i18n.T(i18n.InitDoneCount, changed)
}

func undoneLine(changed int) string {
	if changed == 0 {
		return i18n.T(i18n.UndoDoneNothing)
	}
	return i18n.T(i18n.UndoDoneCount, changed)
}

// samePath 는 두 경로를 이 파일 계층이 보는 대로 견준다.
func samePath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	left, right = filepath.Clean(left), filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

// shortPath 는 표에 넣을 짧은 이름이다 : 상위 폴더 하나 + 파일 이름.
func shortPath(path string) string {
	return filepath.ToSlash(filepath.Join(filepath.Base(filepath.Dir(path)), filepath.Base(path)))
}

// readText 는 파일 내용을 주고, 없으면 빈 글과 false 를 준다. BOM 은 벗긴다.
func readText(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return strings.TrimPrefix(string(data), "\ufeff"), true, nil
}

// appendBlock 은 글 끝에 빈 줄 하나를 두고 블록을 붙인다.
func appendBlock(text, block string) string {
	if strings.TrimSpace(text) == "" {
		return block
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if !strings.HasSuffix(text, "\n\n") {
		text += "\n"
	}
	return text + block
}

func lineCount(block string) int {
	return strings.Count(strings.TrimSuffix(block, "\n"), "\n") + 1
}
