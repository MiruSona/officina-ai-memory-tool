package install

import (
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// blockKind 는 규칙 문서에 이미 든 mem 블록이 누구 것인지다.
type blockKind int

const (
	blockNone    blockKind = iota // 블록이 없다 — 덧붙인다
	blockCurrent                  // 지금 문안과 같다 — 그대로 둔다
	blockOld                      // 알려진 옛 문안이다 — 갈아 끼운다
	blockEdited                   // 사람이 고쳤다 — 손대지 않는다
)

// classifyRulesBlock 은 블록 본문을 지금 문안·옛 문안과 맞춰 본다. 줄 끝 공백과
// CRLF 는 비교에서 뺀다 — 체크아웃이나 편집기가 바꾼 것은 사람이 고친 게 아니다.
// 어느 문안과도 다르면 사람이 고친 것이다. 덮으면 그 사람의 줄이 말없이 사라진다
// (2026-10-19 사용자 결정).
func classifyRulesBlock(text string) blockKind {
	if !hasRulesBlock(text) {
		return blockNone
	}
	body := rulesBlockBody(text)
	if body == rulesBlockBody(i18n.InstallRulesBlock) {
		return blockCurrent
	}
	for _, old := range i18n.InstallOldRulesBlocks {
		if body == rulesBlockBody(old) {
			return blockOld
		}
	}
	return blockEdited
}

// keptRulesStep 은 손대지 않는 블록의 표 한 줄이다. 사람이 고친 블록이면
// 안 덮었다는 것과 새 문안을 받는 길을 안내 줄로 붙인다.
func keptRulesStep(step Step, kind blockKind) Step {
	step.Now, step.Todo = i18n.T(i18n.InitStateHasBlock), i18n.T(i18n.InitTodoKeep)
	if kind == blockEdited {
		step.Now, step.Todo = i18n.T(i18n.InitStateEditedBlock), i18n.T(i18n.InitTodoKeepEdited)
		step.Note = i18n.T(i18n.InitRulesEditedNote, step.What)
	}
	return step
}

// rulesBlockBody 는 블록의 제목부터 끝까지를 고른 글로 돌려준다. 고르는 범위는
// cutRulesBlock 이 지우는 범위와 같다 : 표식이 있으면 표식 사이, 없으면 제목부터
// 다음 "## " 제목 앞까지. 표식 자체는 빼서 표식 없는 옛 판과도 맞춰 볼 수 있다.
func rulesBlockBody(text string) string {
	lines := strings.Split(toLF(text), "\n")
	if start := indexOfTrimmed(lines, i18n.InstallBlockOpen); start >= 0 {
		return normalizeBlock(markedLines(lines[start+1:]))
	}
	if start := indexOfTrimmed(lines, i18n.InstallRulesHeading); start >= 0 {
		return normalizeBlock(plainLines(lines[start:]))
	}
	return ""
}

func indexOfTrimmed(lines []string, want string) int {
	for index, line := range lines {
		if strings.TrimSpace(line) == want {
			return index
		}
	}
	return -1
}

// markedLines 는 닫는 표식 앞까지다. 닫는 표식이 없으면 끝까지 — cutRulesBlock 과 같다.
func markedLines(lines []string) []string {
	if end := indexOfTrimmed(lines, i18n.InstallBlockClose); end >= 0 {
		return lines[:end]
	}
	return lines
}

// plainLines 는 제목 줄과 그 뒤 다음 "## " 제목 앞까지다.
func plainLines(lines []string) []string {
	for index := 1; index < len(lines); index++ {
		if strings.HasPrefix(lines[index], "## ") {
			return lines[:index]
		}
	}
	return lines
}

// normalizeBlock 은 줄 끝 공백을 걷고 앞뒤 빈 줄을 뺀다.
func normalizeBlock(lines []string) string {
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		kept = append(kept, strings.TrimRight(line, " \t\r"))
	}
	return strings.Trim(strings.Join(kept, "\n"), "\n")
}
