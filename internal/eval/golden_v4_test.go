package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// TestLoadGoldenV4 는 v3 에서 의심 정답 3건만 고친 81건 판이 읽히는지 본다
// (뜻후보섞기측정 1절). multisession 둘이 extract 로 옮겨 16 → 14 다.
func TestLoadGoldenV4(t *testing.T) {
	set, err := Load("../../testdata/golden/goldenset-v4.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Cases) != 81 {
		t.Fatalf("81건이 아니라 %d건이다", len(set.Cases))
	}
	if len(set.Warnings) > 0 {
		t.Fatalf("구성 경고가 남았다 : %v", set.Warnings)
	}
	multi, since := 0, 0
	for _, item := range set.Cases {
		if item.Kind == KindMultisession {
			multi++
		}
		if item.Since != "" {
			since++
		}
	}
	if multi != 14 {
		t.Fatalf("multisession 이 14건이 아니라 %d건이다", multi)
	}
	// 측정일에 기대던 temporal 한 건에 since 를 고정했다.
	if since != 1 {
		t.Fatalf("since 를 단 질문이 1건이 아니라 %d건이다", since)
	}
}

// TestLoadFreshSet 은 검색을 안 본 판이 쓴 새 질문 33건이 읽히는지 본다.
// 답 있음 26 · 답 없음 7 이고, 셋이 작아 구성 경고는 나도 된다.
func TestLoadFreshSet(t *testing.T) {
	set, err := Load("../../testdata/golden/fresh-2026-10-05.yaml")
	if err != nil {
		t.Fatal(err)
	}
	answered, abstain, english := 0, 0, 0
	for _, item := range set.Cases {
		switch item.Kind {
		case KindExtract:
			answered++
		case KindAbstain:
			abstain++
		default:
			t.Fatalf("새 셋에 %s 질의가 들었다 : %q", item.Kind, item.Q)
		}
		if item.LangOf() == "en" {
			english++
		}
	}
	if answered != 26 || abstain != 7 {
		t.Fatalf("답 있음 26 · 답 없음 7 이 아니라 %d · %d 다", answered, abstain)
	}
	if english < 4 {
		t.Fatalf("영어 질의가 %d건뿐이다", english)
	}
}

// freshBSum 은 C2 2차 판의 최종 판정 셋 fresh-B 의 얼린 지문이다. 판정 전에
// 문구나 정답을 검색 결과에 맞춰 고치면 이 시험이 깨진다 (뜻후보섞기측정 7절).
const freshBSum = "321458f608279868efaaf2e6cb0345ba0baab59b01584dfc7814823ad7714b4f"

// TestLoadFreshBSet 은 두 번째 눈가림 셋 43건이 읽히고 얼린 그대로인지 본다.
// 답 있음 34 · 답 없음 9 이다.
func TestLoadFreshBSet(t *testing.T) {
	path := "../../testdata/golden/fresh-b-2026-10-05.yaml"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256(raw); hex.EncodeToString(sum[:]) != freshBSum {
		t.Fatalf("fresh-B 가 얼린 판과 다르다 : %x", sum)
	}
	set, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	answered, abstain, english := 0, 0, 0
	for _, item := range set.Cases {
		switch item.Kind {
		case KindExtract:
			answered++
			if len(item.Expect) == 0 {
				t.Fatalf("답 있는 질문에 정답이 없다 : %q", item.Q)
			}
		case KindAbstain:
			abstain++
		default:
			t.Fatalf("새 셋에 %s 질의가 들었다 : %q", item.Kind, item.Q)
		}
		if item.LangOf() == "en" {
			english++
		}
	}
	if answered != 34 || abstain != 9 {
		t.Fatalf("답 있음 34 · 답 없음 9 가 아니라 %d · %d 다", answered, abstain)
	}
	if english < 6 {
		t.Fatalf("영어 질의가 %d건뿐이다", english)
	}
}
