// mem init 의 진입점이다.
//
// main.go 레지스트리에 붙는 한 줄은 아래 init 안에 있다 :
//
//	register(command{name: "init", run: runInit, bools: initBools, values: initValues})

package main

import (
	"os"

	"github.com/mirusona/officina-ai-memory-tool/internal/install"
)

// initBools 는 값을 안 받는 init 깃발이다. --repo 만 값을 먹는다.
var initBools = []string{"dry-run", "no-hook", "no-subagent-hook", "gemini", "undo", "solo"}
var initValues = []string{"repo"}

func init() {
	register(command{name: "init", run: runInit, bools: initBools, values: initValues})
}

func runInit(argv []string) int {
	parsed, err := parseOptions(argv, initBools, initValues)
	if err != nil {
		return fail(err.Error())
	}
	root := parsed.text("repo")
	if root == "" {
		root, err = os.Getwd()
		if err != nil {
			return fail(err.Error())
		}
	}
	options := install.Options{
		Root:           root,
		DryRun:         parsed.flags["dry-run"],
		NoHook:         parsed.flags["no-hook"],
		NoSubagentHook: parsed.flags["no-subagent-hook"],
		Gemini:         parsed.flags["gemini"],
		Solo:           parsed.flags["solo"],
	}
	report, err := runInitOrUndo(options, parsed.flags["undo"])
	if report != nil {
		printReport(report)
	}
	if err != nil {
		return fail(err.Error())
	}
	return exitOK
}

func runInitOrUndo(options install.Options, undo bool) (*install.Report, error) {
	if undo {
		return install.Undo(options)
	}
	return install.Init(options)
}
