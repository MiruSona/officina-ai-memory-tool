package quality

// 2026-09-21 코드 리뷰 — note: 근거의 공백 오탐 · severity 별칭 안내 ·
// 거절 가짓수 세기.

import (
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// note: 는 사람이 쓰는 글이라 그 안의 `file:…` 은 다음 근거가 아니다.
func TestSourcesNoteKeepsSpaces(t *testing.T) {
	memory := goodMemory()
	memory.Sources = []string{"url:https://example.com/a", "note:file:a.go 를 보고 적었다"}
	for _, one := range checkSources(memory, testOptions()) {
		if strings.Contains(one.Reason, "쉼표로 나눈다") {
			t.Fatalf("note: 글월을 근거 둘로 본다 : %v", one)
		}
	}
	// note: 가 아닌 값은 그대로 잡는다.
	memory.Sources = []string{"file:a/b.go url:https://example.com/a"}
	if !hasReason(checkSources(memory, testOptions()), "쉼표로 나눈다") {
		t.Fatal("공백으로 이은 근거를 안 잡는다")
	}
}

// 파일에 severity: medium 이 적혀 있으면 표준 이름 mid 를 권한다.
func TestSeverityAliasIsSuggested(t *testing.T) {
	memory := goodMemory()
	memory.Type = model.TypeCaution
	memory.Severity = "medium"
	found := checkPerType(memory, testOptions())
	if !hasReason(found, "`mid` 로 적는다") {
		t.Fatalf("표준 이름을 안 권한다 : %v", found)
	}
	// 아예 모르는 값에는 권할 이름이 없다.
	memory.Severity = "zzz"
	if hasReason(checkPerType(memory, testOptions()), "로 적는다") {
		t.Fatal("모르는 값에 엉뚱한 이름을 권한다")
	}
}

// RejectCount 는 경고를 빼고 거절 등급만 센다.
func TestRejectCountSkipsWarnings(t *testing.T) {
	verdict := Verdict{Findings: []Finding{
		{Level: GradeReject}, {Level: GradeWarn}, {Level: GradeReject}, {Level: GradeWarn},
	}}
	if got := verdict.RejectCount(); got != 2 {
		t.Fatalf("거절만 세야 한다 : %d", got)
	}
}

func hasReason(found []Finding, part string) bool {
	for _, one := range found {
		if strings.Contains(one.Reason, part) {
			return true
		}
	}
	return false
}
