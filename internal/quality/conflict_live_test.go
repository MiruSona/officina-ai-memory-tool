package quality

import (
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 덮이거나 무효인 결정은 「같은 자리에 산 결정이 더 있다」 고 혼나지 않는다.
// 자기가 이미 물러난 기억이다 (mem issue 20260906-913bd993).
func TestConflictSkipsDeadMemory(t *testing.T) {
	cases := []struct {
		name  string
		dress func(m *model.Memory)
		want  int
	}{
		{"덮인 기억", func(m *model.Memory) {
			m.SupersededBy = "20260822-bbbb0002"
			m.InvalidAt = "2026-08-22"
		}, 0},
		{"무효 기억", func(m *model.Memory) { m.InvalidAt = "2026-08-01" }, 0},
		{"산 기억", func(m *model.Memory) {}, 1},
	}
	for _, test := range cases {
		mine := &model.Memory{ID: "20260822-aaaa0001", Type: model.TypeDecision, Date: "2026-08-22",
			Scope: "aimemorytool", Tags: []string{"hook"}, Summary: "훅은 세션 시작에만 붙인다"}
		test.dress(mine)
		rival := &model.Memory{ID: "20260822-bbbb0002", Type: model.TypeDecision, Date: "2026-08-22",
			Scope: "aimemorytool", Tags: []string{"hook"}, Summary: "훅 예산을 세 줄로 줄인다"}
		report := CheckRepo([]*model.Memory{mine, rival}, wave2dOptions(t))
		count := 0
		for _, one := range report.ByID[mine.ID] {
			if one.Rule == RuleDecisionConflictLive {
				count++
			}
		}
		if count != test.want {
			t.Fatalf("%s : conflict %d건, 바란 것 %d건", test.name, count, test.want)
		}
	}
}
