package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/safe"
)

// addUsage 는 관문 앞 사용법 검사 결과다. problems 가 비어야 관문으로 간다.
type addUsage struct {
	origin   string
	body     string
	problems []string
}

// addUsageOf 는 관문 앞 「사용법 오류」를 한 번에 다 모은다. 첫 실패에서 돌아가면
// 하나 고칠 때마다 다음 것이 나와 같은 명령을 세 번 다시 친다 (사용 피드백 2026-10-11).
// 본문 없음이 맨 앞이다 — 가장 자주 빠뜨리고, 고치는 데 가장 오래 걸린다.
// --jsonl 은 본문을 표준입력에서 줄마다 따로 읽으니 여기서 본문을 안 읽는다.
func addUsageOf(parsed *options) addUsage {
	usage := addUsage{}
	origin, originProblem := originProblemOf(parsed)
	usage.origin = origin
	if parsed.flags["jsonl"] {
		if originProblem != "" {
			usage.problems = append(usage.problems, originProblem)
		}
		return usage
	}
	body, err := bodyOf(parsed)
	if err != nil {
		usage.problems = append(usage.problems, err.Error())
	}
	usage.body = body
	if originProblem != "" {
		usage.problems = append(usage.problems, originProblem)
	}
	// 옵션 값이 아닌 낱말은 대개 `--sources a b` 처럼 공백으로 이은 것이다.
	// 조용히 먹으면 뒤 토막이 통째로 사라진다 (사용 피드백 2026-09-20).
	if len(parsed.rest) > 0 {
		words := safe.Summary(strings.Join(parsed.rest, " "), strayRoom)
		usage.problems = append(usage.problems, i18n.T(i18n.AddStrayWords, words))
	}
	// `--by` 는 큐에 넣기 **전에** 본다. 넣고 나서 걸리면 「저장됨」과 사용법
	// 오류가 한 화면에 같이 뜬다 (리뷰 2026-09-21).
	if old := parsed.text("by"); old != "" && !model.IsID(old) {
		usage.problems = append(usage.problems, i18n.T(i18n.BadID, old))
	}
	return usage
}

// failUsage 는 모은 사유를 다 찍고 끝줄에 「거절됨」 을 단다. 종료 코드는 사용법 오류(1)다.
func failUsage(problems []string) int {
	for _, problem := range problems {
		fmt.Fprintln(os.Stderr, problem)
	}
	fmt.Fprintln(os.Stderr, i18n.T(i18n.AddRejectedNote, len(problems)))
	return exitUsage
}
