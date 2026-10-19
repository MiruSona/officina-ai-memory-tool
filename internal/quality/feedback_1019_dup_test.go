package quality

import (
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 2026-10-19 사용자 결정 — 닮음 거절(C01)은 두 자리에서 등급이 다르다.
// 저장소 검사(lint·review)는 경고, add 관문은 그대로 거절이다.

// hardPair 는 같은 날 같은 제목으로 끝 줄만 다른 짝이다. 닮음이 거절 문턱을 넘는다.
func hardPair() (*model.Memory, *model.Memory) {
	body := "훅 상한은 4096 바이트로 `hook.go` 에서 자른다.\n" +
		"한글은 한 글자가 3 바이트라 4096 바이트는 약 1365 글자다.\n" +
		"넘치면 `hook.go` 가 끝에 잘림 표시를 붙인다.\n"
	old := cleanupMemory("20260801-eeee0001", model.TypeCaution, "2026-08-01", "훅 상한 4096 바이트", body+"끝 줄은 비워 둔다.")
	newer := cleanupMemory("20260801-eeee0002", model.TypeCaution, "2026-08-01", "훅 상한 4096 바이트", body+"끝 줄에 날짜를 적는다.")
	return old, newer
}

func TestStoredDuplicateHardIsWarn(t *testing.T) {
	old, newer := hardPair()
	report := cleanupReport(t, old, newer)
	hits := 0
	for _, id := range []string{old.ID, newer.ID} {
		for _, one := range report.ByID[id] {
			if one.Rule != RuleDuplicateHard {
				continue
			}
			hits++
			if one.Level != GradeWarn {
				t.Errorf("저장소 검사의 C01 이 경고가 아니다 : %s %s", id, one.Level)
			}
		}
	}
	if hits == 0 {
		t.Fatalf("대조군 : 이 짝에 C01 이 안 났다 : %v", report.ByID[newer.ID])
	}
}

func TestAddDuplicateHardStillRejects(t *testing.T) {
	old, newer := hardPair()
	opt := wave2dOptions(t).Options
	opt.Repo = MemorySlice{old}
	verdict := Gate(newer, opt)
	for _, one := range verdict.Findings {
		if one.Rule == RuleDuplicateHard {
			if one.Level != GradeReject || verdict.Kind != KindQualityReject {
				t.Fatalf("add 관문의 C01 은 거절이어야 한다 : %s %v", one.Level, verdict.Kind)
			}
			return
		}
	}
	t.Fatalf("add 관문에 C01 이 안 났다 : %v", verdict.Rules())
}
