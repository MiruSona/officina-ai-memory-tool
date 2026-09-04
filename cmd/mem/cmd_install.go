// mem install 의 진입점이다. --apply 없이는 계획 표만 보여주고 아무것도
// 안 바꾼다 — install 은 exe 를 사용자 폴더에 복사하고 PATH 를 고치는 위험한
// 명령이라 실수로 돌아가면 안 된다.
//
// v0.3 에서 단계 하나가 늘었다 : **의미 검색 모델·런타임 받기**. 기본으로 받고,
// `--no-embed` 를 줘야 뺀다. 못 받아도 설치는 낱말 모드로 성공한다.
//
//	mem install                          계획 표만 (dry-run)
//	mem install --apply                  exe · PATH · 모델까지
//	mem install --bundle <폴더> --apply  인터넷 없이 꾸러미에서
//	mem install --no-embed --apply       낱말 검색만
//	mem install --check                  무엇이 깔렸고 해시가 맞는지만
//	mem install --undo --apply           사용자 PATH 를 백업 값으로 되돌린다
//
// main.go 레지스트리에 붙는 한 줄은 아래 init 안에 있다 :
//
//	register(command{name: "install", run: runInstall, bools: installBools, values: installValues})

package main

import (
	"fmt"

	"github.com/mirusona/officina-ai-memory-tool/internal/install"
)

func init() {
	register(command{name: "install", run: runInstall, bools: installBools, values: installValues})
}

var installBools = []string{"dry-run", "no-path", "apply", "no-embed", "check", "undo"}

// bundle 은 오프라인 꾸러미 폴더, model 은 쓸 모델 이름이다.
var installValues = []string{"bundle", "model"}

func runInstall(argv []string) int {
	parsed, err := parseOptions(argv, installBools, installValues)
	if err != nil {
		return fail(err.Error())
	}
	if parsed.flags["check"] {
		report, ok := install.EmbedCheck(parsed.text("model"))
		printReport(report)
		if !ok {
			return exitCheck
		}
		return exitOK
	}
	options := install.Options{
		DryRun:  parsed.flags["dry-run"],
		Apply:   parsed.flags["apply"],
		NoPath:  parsed.flags["no-path"],
		NoEmbed: parsed.flags["no-embed"],
		Bundle:  parsed.text("bundle"),
		Model:   parsed.text("model"),
	}
	run := install.Install
	if parsed.flags["undo"] {
		run = install.Uninstall
	}
	report, err := run(options)
	if report != nil {
		printReport(report)
	}
	if err != nil {
		return fail(err.Error())
	}
	return exitOK
}

func printReport(report *install.Report) {
	for _, line := range report.Lines() {
		fmt.Println(line)
	}
}
