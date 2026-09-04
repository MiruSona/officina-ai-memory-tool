package main

import (
	"os"

	"github.com/mirusona/officina-ai-memory-tool/internal/hook"
)

// hook 은 인자를 hook.Run 이 스스로 읽는다. 여기 목록은 `mem hook --help` 를
// 가리는 데만 쓴다 — 훅은 사용법이 틀려도 종료 0 이라야 한다.
var hookBools = []string{"dry-run", "json"}

// 훅은 값을 받는 옵션이 --budget 하나다. `--repo` 는 안 받는다 — 저장소는 훅
// 입력 JSON 의 `cwd` 가 고른다 (보안연동 시험 L-5).
var hookValues = []string{"budget"}

func init() {
	register(command{name: "hook", run: runHook, bools: hookBools, values: hookValues})
}

// runHook 은 훅 진입점이다. 인자를 여기서 안 따지고 hook 에 그대로 넘긴다 —
// 사용법이 틀려도 종료 코드는 늘 0 이어야 한다 (설계 8-1 #7).
func runHook(argv []string) int {
	return hook.Run(argv, os.Stdin, os.Stdout)
}
