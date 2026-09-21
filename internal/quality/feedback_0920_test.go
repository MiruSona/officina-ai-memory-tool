package quality

// 2026-09-20 사용 피드백 — 표준 밖 태그에 가까운 후보 · 빈 본문에 B08 건너뛰기 ·
// 공백으로 이은 sources.

import (
	"strings"
	"testing"
)

// nextLines 는 판정이 준 「다음에 할 것」 줄을 다 모은다.
func nextLines(found []Finding) []string {
	lines := []string{}
	for _, one := range found {
		lines = append(lines, one.Next...)
	}
	return lines
}

// 표준 밖 태그를 거절할 때 가까운 표준 태그를 먼저 찍는다.
func TestTagStandardShowsNearTags(t *testing.T) {
	opt := testOptions()
	opt.Vocab.Tags["tilemap"] = []string{}
	memory := goodMemory()
	memory.Tags = []string{"hook", "tilemapp"}
	lines := nextLines(checkField(memory, opt))
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "가까운 표준 태그 : tilemap") {
		t.Fatalf("가까운 후보가 첫 줄이 아니다 : %v", lines)
	}
	if !hasLinePrefix(lines, "mem tags --add tilemapp") {
		t.Fatalf("표준으로 삼는 명령이 빠졌다 : %v", lines)
	}
	if !hasLinePrefix(lines, "mem tags --list") {
		t.Fatalf("목록 보기가 빠졌다 : %v", lines)
	}
}

// 가까운 후보가 없으면 그 줄은 아예 안 찍는다.
func TestTagStandardSkipsNearWhenNone(t *testing.T) {
	memory := goodMemory()
	memory.Tags = []string{"hook", "zzzqqq"}
	for _, line := range nextLines(checkField(memory, testOptions())) {
		if strings.HasPrefix(line, "가까운 표준 태그") {
			t.Fatalf("후보가 없는데 줄을 찍는다 : %s", line)
		}
	}
}

func hasLinePrefix(lines []string, prefix string) bool {
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// 본문이 아예 비면 summary-body-match 는 아무 말도 안 한다.
func TestSummaryBodyGapSkipsEmptyBody(t *testing.T) {
	memory := goodMemory()
	memory.Body = "   \n\n"
	if reason := summaryBodyGap(memory); reason != "" {
		t.Fatalf("빈 본문에 낱말을 센다 : %s", reason)
	}
}

// 근거 하나에 접두 둘이 공백으로 이어져 있으면 쉼표를 알려준다.
func TestSourcesSpacedIsFlagged(t *testing.T) {
	memory := goodMemory()
	memory.Sources = []string{"file:a/b.go note:재보고 적는다"}
	found := checkSources(memory, testOptions())
	hit := false
	for _, one := range found {
		if one.Rule == RuleSourcesShape && strings.Contains(one.Reason, "쉼표로 나눈다") {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("공백으로 이은 근거를 안 잡는다 : %v", found)
	}
	// 공백이 들어가도 접두가 하나뿐이면 멀쩡한 값이다.
	memory.Sources = []string{"file:어떤 폴더/이름.go"}
	for _, one := range checkSources(memory, testOptions()) {
		if strings.Contains(one.Reason, "쉼표로 나눈다") {
			t.Fatalf("멀쩡한 경로를 잡는다 : %v", one)
		}
	}
}
