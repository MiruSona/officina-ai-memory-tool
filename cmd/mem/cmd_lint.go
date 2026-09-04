package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/lint"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// 아래 셋은 한글이 한 글자도 없는 짜임새용 서식이라 i18n 표에 안 둔다.
const (
	lintTableRow   = "| %s | %s | %d |"
	lintProblemRow = "  [%s] %s : %s"
	lintFixRow     = "  %s — %s"
)

var lintBools = []string{"fix", "json", "synonym", "no-git"}
var lintValues = []string{"repo", "rule"}

func init() {
	register(command{name: "lint", run: runLint, bools: lintBools, values: lintValues})
}

// runLint 는 규칙 22개를 검사한다. 오류가 하나라도 있으면 종료 코드 2 다
// (설계 8-1 · 9-2).
func runLint(argv []string) int {
	parsed, err := parseOptions(argv, lintBools, lintValues)
	if err != nil {
		return fail(err.Error())
	}
	repository, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	rule := parsed.text("rule")
	if rule != "" && !lint.KnownRule(rule) {
		// 오타 하나로 「걸린 것 없음」이 나오면 사람이 통과한 줄 안다.
		return fail(fmt.Sprintf("모르는 규칙 이름 : %s", rule))
	}
	report, err := lint.Run(lint.Options{Store: opened, Config: repository.Config,
		Vocab: vocabOf(repository), Demoted: demotedOf(repository),
		Fix: parsed.flags["fix"], Rule: rule,
		Synonym: parsed.flags["synonym"], NoGit: parsed.flags["no-git"],
		Near: nearOf(vectorsFor(repository.Dir))})
	if err != nil {
		return fail(err.Error())
	}
	// 셈을 못 남겨도 검사 결과는 그대로다.
	store.AppendHit(opened.Dir, "lint", "")
	if parsed.flags["json"] {
		return printLintJSON(report)
	}
	printLint(report, parsed.flags["fix"])
	if report.Errors() > 0 {
		return exitCheck
	}
	return exitOK
}

func printLintJSON(report *lint.Report) int {
	data, err := json.Marshal(report)
	if err != nil {
		return fail(err.Error())
	}
	fmt.Println(string(data))
	if report.Errors() > 0 {
		return exitCheck
	}
	return exitOK
}

func printLint(report *lint.Report, fixing bool) {
	fmt.Println(i18n.T(i18n.LintHeader, report.Checked, report.Errors(), report.Warnings(),
		report.Elapsed.Seconds()))
	if len(report.Problems) == 0 {
		fmt.Println(i18n.T(i18n.LintClean))
	} else {
		printLintTable(report)
		printLintProblems(report)
	}
	printLintSynonyms(report)
	for _, note := range report.Notes {
		fmt.Println(note)
	}
	if fixing {
		printLintFixes(report)
	}
}

// printLintTable 은 규칙별 건수 표다. 0건인 규칙은 안 적는다.
func printLintTable(report *lint.Report) {
	counts := report.CountByRule()
	fmt.Println(i18n.T(i18n.LintTableHead))
	for _, rule := range lint.Rules {
		if counts[rule] == 0 {
			continue
		}
		fmt.Println(fmt.Sprintf(lintTableRow, rule, lint.LevelText(report.LevelOfRule(rule)), counts[rule]))
	}
}

// printLintProblems 는 파일:줄 목록이다.
func printLintProblems(report *lint.Report) {
	fmt.Println()
	for _, problem := range report.Problems {
		fmt.Println(fmt.Sprintf(lintProblemRow, lint.LevelText(problem.Level), whereOf(problem), problem.Text))
	}
	fmt.Println(i18n.T(i18n.LintTotals, report.Errors(), report.Warnings()))
}

// whereOf 는 `파일:줄` 이다. 저장소 전체 규칙은 규칙 이름만 적는다.
func whereOf(problem lint.Problem) string {
	if problem.Path == "" {
		return problem.Rule
	}
	if problem.Line == 0 {
		return problem.Path
	}
	return fmt.Sprintf("%s:%d", problem.Path, problem.Line)
}

// printLintSynonyms 는 0건이 자꾸 나는 낱말이다 (`--synonym`). 없으면 아무 말도
// 안 한다 — 못 뽑은 이유는 Notes 가 말한다.
func printLintSynonyms(report *lint.Report) {
	if len(report.Synonyms) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("0건이 잦은 낱말 (동의어 후보)")
	for _, one := range report.Synonyms {
		fmt.Println(fmt.Sprintf("  %s — %d회", one.Word, one.Count))
	}
}

func printLintFixes(report *lint.Report) {
	if report.FixLocked {
		fmt.Fprintln(os.Stderr, i18n.T(i18n.LintFixLocked))
		return
	}
	if report.FixReadOnly {
		fmt.Fprintln(os.Stderr, i18n.T(i18n.LintFixReadOnly))
		return
	}
	if len(report.Fixes) == 0 && len(report.Held) == 0 {
		fmt.Println(i18n.T(i18n.LintFixNone))
		return
	}
	fmt.Println(i18n.T(i18n.LintFixDone, len(report.Fixes)))
	for _, fixed := range report.Fixes {
		fmt.Println(fmt.Sprintf(lintFixRow, fixed.Path, fixed.What))
	}
	for _, held := range report.Held {
		if held.Note {
			fmt.Println(i18n.T(i18n.LintFixHeldNote, held.Path, held.What))
			continue
		}
		fmt.Println(i18n.T(i18n.LintFixHeld, held.Path, held.What))
	}
}
