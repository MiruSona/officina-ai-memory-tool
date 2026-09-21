package main

// mem version — 이 실행 파일이 어느 소스로 언제 빌드됐는지 찍는다.
//
// bin\ 은 git 에 안 올라가서, 소스를 새로 받아도 옛 exe 가 그대로 남는다.
// 「고쳤는데 그대로다」의 태반이 그것이라 눈으로 보는 길을 둔다.

import (
	"fmt"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// buildCommit·buildTime 은 build.ps1 이 -ldflags 로 박는다. 그냥 `go build` 로
// 만들면 비어 있고, 그때는 dev 로 찍는다.
var (
	buildCommit = ""
	buildTime   = ""
)

func init() {
	register(command{name: "version", run: runVersion})
}

func runVersion(argv []string) int {
	commit, stamp := buildCommit, buildTime
	if commit == "" {
		commit = devMark
	}
	if stamp == "" {
		stamp = devMark
	}
	fmt.Printf("mem %s (%s · %s)\n", i18n.Version, commit, stamp)
	return exitOK
}

// devMark 는 빌드 정보가 안 박힌 실행 파일 표시다.
const devMark = "dev"
