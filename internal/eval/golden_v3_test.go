package eval

import "testing"

// TestLoadGoldenV3 는 2026-10-05 저장소에 맞춘 81건 골든셋이 구성 경고 없이
// 읽히는지 본다. v2 는 옛 측정과 견주는 자라 그대로 남고(TestLoadGoldenV2),
// v3 가 공식 판정 자다. 수치는 검색이 재고, 여기서는 자만 본다.
func TestLoadGoldenV3(t *testing.T) {
	set, err := Load("../../testdata/golden/goldenset-v3.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Cases) != 81 {
		t.Fatalf("81건이 아니라 %d건이다", len(set.Cases))
	}
	if len(set.Warnings) > 0 {
		t.Fatalf("구성 경고가 남았다 : %v", set.Warnings)
	}
	abstain, unreachable := 0, 0
	for _, item := range set.Cases {
		if item.Kind == KindAbstain {
			abstain++
			if len(item.Expect) > 0 {
				t.Fatalf("abstain 인데 정답이 있다 : %q", item.Q)
			}
		}
		if item.Kind == KindMultisession && item.ExpectModeOf() != ExpectAll {
			t.Fatalf("multisession 이 expect_mode: all 이 아니다 : %q", item.Q)
		}
		if item.Unreachable {
			unreachable++
		}
	}
	// 셰이더를 extract 로 옮기고 새 abstain 하나로 채웠다 — 수가 그대로여야 한다.
	if abstain != 10 {
		t.Fatalf("abstain 이 10건이 아니라 %d건이다", abstain)
	}
	// 훅 잘림이 새 기억으로 닿게 되어 3 → 2 다.
	if unreachable != 2 {
		t.Fatalf("unreachable 이 2건이 아니라 %d건이다", unreachable)
	}
}
