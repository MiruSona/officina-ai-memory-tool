package install

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// mem init 이 프로젝트 안에서 건드리는 파일 이름이다.
const (
	gitignoreName = ".gitignore"
	agentsName    = "AGENTS.md"
	claudeName    = "CLAUDE.md"
	localDirName  = "local"
)

// Init 은 프로젝트에 mem 을 붙인다. 여섯 가지 전부 멱등이라 두 번 돌려도
// 아무것도 안 바뀌고 표는 전부 「그대로」 가 된다.
func Init(options Options) (*Report, error) {
	root, err := filepath.Abs(options.Root)
	if err != nil {
		return nil, err
	}
	report := &Report{}
	steps := []func() (Step, error){
		func() (Step, error) { return ensureStore(root, options.DryRun) },
		func() (Step, error) { return ensureConfig(root, options.DryRun) },
		func() (Step, error) { return ensureVocab(root, options.DryRun) },
		func() (Step, error) { return ensureLog(root, options.DryRun) },
		func() (Step, error) { return ensureUsageDoc(root, options.DryRun) },
		func() (Step, error) { return ensureGitFiles(root, options.DryRun) },
	}
	if !options.NoHook {
		steps = append(steps, func() (Step, error) {
			return ensureSettings(ClaudeSettingsPath(root), claudeTimeout, options.DryRun, true,
				specsFor(false, options.NoSubagentHook))
		})
	}
	if options.Gemini {
		steps = append(steps, func() (Step, error) {
			// Gemini CLI 에는 auto 모드 allow 규칙도 SubagentStart 도 없다.
			return ensureSettings(GeminiSettingsPath(root), geminiTimeout, options.DryRun, false,
				specsFor(true, options.NoSubagentHook))
		})
	}
	steps = append(steps, func() (Step, error) { return ensureRules(rulesPath(root), options.DryRun) })
	if err := collect(report, steps); err != nil {
		return report, err
	}
	addPathNote(report)
	if options.Solo {
		report.note(i18n.T(i18n.InitSoloNote))
	}
	addFooter(report, options.DryRun, doneLine(report.Changed()))
	return report, nil
}

// Undo 는 훅 항목과 규칙 블록만 뗀다. Memory/ 와 .gitignore 는 그대로 둔다 —
// 기억은 툴보다 오래 산다 (설계 11-1).
func Undo(options Options) (*Report, error) {
	root, err := filepath.Abs(options.Root)
	if err != nil {
		return nil, err
	}
	report := &Report{}
	steps := []func() (Step, error){
		func() (Step, error) { return removeSettings(ClaudeSettingsPath(root), options.DryRun) },
	}
	if _, err := os.Stat(GeminiSettingsPath(root)); err == nil {
		steps = append(steps, func() (Step, error) {
			return removeSettings(GeminiSettingsPath(root), options.DryRun)
		})
	}
	steps = append(steps, func() (Step, error) { return removeRules(rulesPath(root), options.DryRun) })
	if err := collect(report, steps); err != nil {
		return report, err
	}
	report.note(i18n.T(i18n.UndoKeepsMemory))
	addFooter(report, options.DryRun, undoneLine(report.Changed()))
	return report, nil
}

func addPathNote(report *Report) {
	machine, err := Paths()
	if err != nil || machine.InPath == "" {
		report.note(i18n.T(i18n.InitPathMissing))
		return
	}
	report.note(i18n.T(i18n.InitPathFound, machine.InPath))
}

// ensureStore 는 Memory/ 뼈대를 만든다.
func ensureStore(root string, dryRun bool) (Step, error) {
	dir := filepath.Join(root, config.DirName)
	step := Step{What: i18n.T(i18n.InitStepStore), Now: i18n.T(i18n.InitStatePresent), Todo: i18n.T(i18n.InitTodoKeep)}
	missing := missingParts(dir)
	if missing == 0 {
		return step, nil
	}
	step.Now = i18n.T(i18n.InitStateMissingParts, missing)
	step.Todo, step.Changed = i18n.T(i18n.InitTodoCreate), true
	if dryRun {
		return step, nil
	}
	return step, createSkeleton(dir)
}

// skeletonDirs 는 저장소 하나가 가져야 할 폴더다 (설계 5-1).
func skeletonDirs(dir string) []string {
	tree := store.Open(dir, false)
	return []string{tree.StoreDir(), tree.InboxTmpDir(), tree.InboxNewDir(), tree.InboxBadDir(),
		tree.ArchiveDir(), tree.GoldenDir(), filepath.Join(dir, localDirName)}
}

// missingParts 는 아직 없는 폴더 수를 센다.
func missingParts(dir string) int {
	count := 0
	for _, path := range skeletonDirs(dir) {
		if _, err := os.Stat(path); err != nil {
			count++
		}
	}
	return count
}

// createSkeleton 은 저장소 하나를 깐다.
func createSkeleton(dir string) error {
	tree := store.Open(dir, false)
	if err := tree.EnsureDirs(); err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(dir, localDirName), 0o755)
}

// ensureConfig 는 Memory/mem.toml 을 쓴다. 이미 있으면 사람이 고친 값은 그대로
// 두고 이 exe 가 아는데 파일에 없는 키만 채운다.
func ensureConfig(root string, dryRun bool) (Step, error) {
	dir := filepath.Join(root, config.DirName)
	path := filepath.Join(dir, config.FileName)
	step := Step{What: config.DirName + "/" + config.FileName}
	text, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			step.Now, step.Todo = i18n.T(i18n.InitStateBadConfig), i18n.T(i18n.InitTodoManual)
			return step, nil
		}
		step.Now, step.Todo, step.Changed = i18n.T(i18n.InitStateMissing), i18n.T(i18n.InitTodoCreate), true
		if dryRun {
			return step, nil
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return step, err
		}
		return step, os.WriteFile(path, config.Encode(config.Default(filepath.Base(root))), 0o644)
	}
	missing := config.MissingKeys(string(text))
	if len(missing) == 0 {
		step.Now, step.Todo = i18n.T(i18n.InitStatePresent), i18n.T(i18n.InitTodoKeep)
		return step, nil
	}
	step.Now = i18n.T(i18n.InitStateKeysMissing, len(missing))
	step.Todo, step.Changed = i18n.T(i18n.InitTodoFillKeys), true
	if dryRun {
		return step, nil
	}
	loaded, err := config.Parse(string(text))
	if err != nil {
		step.Now, step.Todo, step.Changed = i18n.T(i18n.InitStateBadConfig), i18n.T(i18n.InitTodoManual), false
		return step, nil
	}
	fillSynonymSeeds(&loaded)
	return step, os.WriteFile(path, config.Encode(loaded), 0o644)
}

// fillSynonymSeeds 는 이미 있는 mem.toml 에 빠진 씨앗만 더한다. 사람이 적은
// 것은 건드리지 않는다 (설계 6-7).
func fillSynonymSeeds(loaded *config.Config) {
	if loaded.Synonym == nil {
		loaded.Synonym = map[string][]string{}
	}
	for word, others := range config.DefaultSynonyms() {
		if _, found := loaded.Synonym[word]; found {
			continue
		}
		loaded.Synonym[word] = others
	}
}

// ensureVocab 은 Memory/vocab.toml 을 쓴다. mem.toml 과 같은 자를 쓴다 —
// 있는 파일은 안 덮고 이 exe 가 아는데 파일에 없는 절만 채운다 (설계 6-1).
func ensureVocab(root string, dryRun bool) (Step, error) {
	dir := filepath.Join(root, config.DirName)
	path := filepath.Join(dir, config.VocabFileName)
	step := Step{What: config.DirName + "/" + config.VocabFileName}
	text, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			step.Now, step.Todo = i18n.T(i18n.InitStateBadVocab), i18n.T(i18n.InitTodoManual)
			return step, nil
		}
		step.Now, step.Todo, step.Changed = i18n.T(i18n.InitStateMissing), i18n.T(i18n.InitTodoCreate), true
		if dryRun {
			return step, nil
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return step, err
		}
		return step, os.WriteFile(path, config.EncodeVocab(config.DefaultVocab()), 0o644)
	}
	missing := config.MissingVocabKeys(string(text))
	if len(missing) == 0 {
		step.Now, step.Todo = i18n.T(i18n.InitStatePresent), i18n.T(i18n.InitTodoKeep)
		return step, nil
	}
	step.Now = i18n.T(i18n.InitStateKeysMissing, len(missing))
	step.Todo, step.Changed = i18n.T(i18n.InitTodoFillKeys), true
	if dryRun {
		return step, nil
	}
	loaded, err := config.ParseVocab(string(text))
	if err != nil {
		step.Now, step.Todo, step.Changed = i18n.T(i18n.InitStateBadVocab), i18n.T(i18n.InitTodoManual), false
		return step, nil
	}
	return step, os.WriteFile(path, config.EncodeVocab(fillVocab(loaded)), 0o644)
}

// fillVocab 은 빠진 절만 채운다. **낱말은 하나도 안 더한다** — vocab.toml 은
// 설정이 아니라 사람이 정한 목록이라, 씨앗을 도로 밀어 넣으면 지운 태그가
// 살아 돌아온다. 못 쓰는 태그 목록만은 비어 있으면 씨앗을 준다.
func fillVocab(loaded config.Vocab) config.Vocab {
	if loaded.Tags == nil {
		loaded.Tags = map[string][]string{}
	}
	if loaded.Scopes == nil {
		loaded.Scopes = map[string][]string{}
	}
	if loaded.TagAlias == nil {
		loaded.TagAlias = map[string]string{}
	}
	if loaded.ScopeAlias == nil {
		loaded.ScopeAlias = map[string]string{}
	}
	if len(loaded.TagDeny) == 0 {
		loaded.TagDeny = config.DefaultVocab().TagDeny
	}
	return loaded
}

// ensureLog 는 Memory/log.md 자리를 마련한다. 도구가 한 줄씩 덧붙여 쓰는
// 시간순 기록이라, 있으면 절대 안 덮는다 (설계 결정 27 · 3-5).
func ensureLog(root string, dryRun bool) (Step, error) {
	dir := filepath.Join(root, config.DirName)
	path := filepath.Join(dir, i18n.InstallLogName)
	step := Step{What: config.DirName + "/" + i18n.InstallLogName}
	if _, err := os.Stat(path); err == nil {
		step.Now, step.Todo = i18n.T(i18n.InitStatePresent), i18n.T(i18n.InitTodoKeep)
		return step, nil
	}
	step.Now, step.Todo, step.Changed = i18n.T(i18n.InitStateMissing), i18n.T(i18n.InitTodoCreate), true
	if dryRun {
		return step, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return step, err
	}
	return step, os.WriteFile(path, []byte(i18n.InstallLogDoc), 0o644)
}

// ensureUsageDoc 은 AI 가 읽는 요약 문서를 쓴다. 있으면 절대 안 덮는다 —
// 프로젝트가 제 말투로 고쳤을 수 있다.
func ensureUsageDoc(root string, dryRun bool) (Step, error) {
	dir := filepath.Join(root, config.DirName)
	path := filepath.Join(dir, i18n.InstallUsageName)
	step := Step{What: i18n.InstallUsagePath}
	if _, err := os.Stat(path); err == nil {
		step.Now, step.Todo = i18n.T(i18n.InitStatePresent), i18n.T(i18n.InitTodoKeep)
		return step, nil
	}
	step.Now, step.Todo, step.Changed = i18n.T(i18n.InitStateMissing), i18n.T(i18n.InitTodoCreate), true
	if dryRun {
		return step, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return step, err
	}
	return step, os.WriteFile(path, []byte(i18n.InstallUsageDoc), 0o644)
}

// ensureGitFiles 는 .gitignore 블록과 .gitattributes 두 줄을 한 소단계로 다룬다.
// 둘 다 「아직 없는 줄만 붙인다」 라 두 번째 실행은 아무것도 안 한다.
func ensureGitFiles(root string, dryRun bool) (Step, error) {
	step := Step{What: gitignoreName + " · " + i18n.InstallAttributeName}
	ignorePath := filepath.Join(root, gitignoreName)
	attrPath := filepath.Join(root, i18n.InstallAttributeName)
	ignoreText, ignoreFound, err := readText(ignorePath)
	if err != nil {
		return step, err
	}
	attrText, attrFound, err := readText(attrPath)
	if err != nil {
		return step, err
	}
	needIgnore := missingLines(ignoreText, i18n.InstallIgnoreLines)
	needAttr := missingLines(attrText, i18n.InstallAttributeLines)
	if len(needIgnore) == 0 && len(needAttr) == 0 {
		step.Now, step.Todo = i18n.T(i18n.InitStateHasBlock), i18n.T(i18n.InitTodoKeep)
		return step, nil
	}
	step.Now = i18n.T(i18n.InitStateNoBlock)
	if !ignoreFound && !attrFound {
		step.Now = i18n.T(i18n.InitStateMissing)
	}
	step.Todo = i18n.T(i18n.InitTodoLines, len(needIgnore)+len(needAttr))
	step.Changed = true
	if dryRun {
		return step, nil
	}
	if len(needIgnore) > 0 {
		block := i18n.InstallIgnoreOpen + "\n" + strings.Join(needIgnore, "\n") + "\n" + i18n.InstallIgnoreClose + "\n"
		if err := os.WriteFile(ignorePath, []byte(appendBlock(ignoreText, block)), 0o644); err != nil {
			return step, err
		}
	}
	if len(needAttr) == 0 {
		return step, nil
	}
	return step, os.WriteFile(attrPath, []byte(appendBlock(attrText, strings.Join(needAttr, "\n")+"\n")), 0o644)
}

// missingLines 는 글에 아직 없는 줄만 골라 준다. 사이 빈칸 수가 달라도 같은
// 줄로 본다 — 사람이 손으로 넣은 .gitattributes 에 두 번째 블록을 붙이면 안 된다.
func missingLines(text string, want []string) []string {
	have := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		have[squeeze(line)] = true
	}
	missing := []string{}
	for _, line := range want {
		if !have[squeeze(line)] {
			missing = append(missing, line)
		}
	}
	return missing
}

func squeeze(line string) string {
	return strings.Join(strings.Fields(line), " ")
}

// rulesPath 는 AGENTS.md 가 있으면 그것, 없고 CLAUDE.md 가 있으면 그것,
// 둘 다 없으면 AGENTS.md 를 새로 만든다.
func rulesPath(root string) string {
	agents := filepath.Join(root, agentsName)
	if _, err := os.Stat(agents); err == nil {
		return agents
	}
	claude := filepath.Join(root, claudeName)
	if _, err := os.Stat(claude); err == nil {
		return claude
	}
	return agents
}

func ensureRules(path string, dryRun bool) (Step, error) {
	text, found, err := readText(path)
	if err != nil {
		return Step{}, err
	}
	step := Step{What: filepath.Base(path)}
	if strings.Contains(text, i18n.InstallRulesBlock) {
		step.Now, step.Todo = i18n.T(i18n.InitStateHasBlock), i18n.T(i18n.InitTodoKeep)
		return step, nil
	}
	// 옛 판이 붙인 블록은 우리 것이라 갈아 끼운다. 문안이 바뀌었는데 그대로
	// 두면 AI 가 옛 명령 이름을 계속 쓴다 (설계 5-5).
	old := hasRulesBlock(text)
	step.Now = i18n.T(i18n.InitStateNoBlock)
	step.Todo = i18n.T(i18n.InitTodoLines, lineCount(i18n.InstallRulesBlock))
	switch {
	case old:
		step.Now, step.Todo = i18n.T(i18n.InitStateOldBlock), i18n.T(i18n.InitTodoReplace)
	case !found:
		step.Now = i18n.T(i18n.InitStateMissing)
	}
	step.Changed = true
	if dryRun {
		return step, nil
	}
	if old {
		text = cutRulesBlock(text)
	}
	return step, os.WriteFile(path, []byte(appendBlock(text, i18n.InstallRulesBlock)), 0o644)
}

// hasRulesBlock 은 표식이든 제목이든 하나만 있으면 이미 붙은 것으로 본다.
// v0.0 이 표식 없이 제목만 넣던 저장소도 두 번 붙지 않는다.
func hasRulesBlock(text string) bool {
	return strings.Contains(text, i18n.InstallBlockOpen) || strings.Contains(text, i18n.InstallRulesHeading)
}

func removeRules(path string, dryRun bool) (Step, error) {
	text, found, err := readText(path)
	if err != nil {
		return Step{}, err
	}
	step := Step{What: filepath.Base(path), Now: i18n.T(i18n.InitStateNoBlock), Todo: i18n.T(i18n.InitTodoKeep)}
	if !found {
		step.Now = i18n.T(i18n.InitStateMissing)
		return step, nil
	}
	if !hasRulesBlock(text) {
		return step, nil
	}
	step.Now, step.Todo, step.Changed = i18n.T(i18n.InitStateHasBlock), i18n.T(i18n.InitTodoRemove), true
	if dryRun {
		return step, nil
	}
	return step, os.WriteFile(path, []byte(cutRulesBlock(text)), 0o644)
}

// cutRulesBlock 은 표식 사이를 통째로 지운다. 표식이 없으면 제목부터 다음
// "## " 제목 앞까지 지운다 (표식 없이 붙은 옛 블록).
func cutRulesBlock(text string) string {
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	inMarked, inPlain := false, false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == i18n.InstallBlockOpen {
			inMarked = true
			continue
		}
		if inMarked {
			if trimmed == i18n.InstallBlockClose {
				inMarked = false
			}
			continue
		}
		if trimmed == i18n.InstallRulesHeading {
			inPlain = true
			continue
		}
		if inPlain && !strings.HasPrefix(line, "## ") {
			continue
		}
		inPlain = false
		kept = append(kept, line)
	}
	return strings.TrimRight(strings.Join(kept, "\n"), "\n") + "\n"
}
