package eval

import "testing"

// TestLoadAbstainProbe 는 abstain 탐침 셋(30건)이 읽히는지 본다. 이 셋은 G2 보조
// 자라 구성 경고(유형별 최소 건수)는 나도 된다 — abstain 만 모은 셋이다.
// 원문 검색에서 답이 나온 1건은 abstain 으로 세지 않도록 extract 로 둔다.
func TestLoadAbstainProbe(t *testing.T) {
	set, err := Load("../../testdata/golden/abstain-probe.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Cases) != 30 {
		t.Fatalf("30건이 아니라 %d건이다", len(set.Cases))
	}
	abstain, answered := 0, 0
	for _, item := range set.Cases {
		switch item.Kind {
		case KindAbstain:
			abstain++
			if len(item.Expect) > 0 {
				t.Fatalf("abstain 인데 정답이 있다 : %q", item.Q)
			}
		case KindExtract:
			answered++
		default:
			t.Fatalf("탐침 셋에 %s 질의가 들었다 : %q", item.Kind, item.Q)
		}
	}
	if abstain != 29 || answered != 1 {
		t.Fatalf("abstain 29 · 정답 있음 1 이 아니라 %d · %d 다", abstain, answered)
	}
}
