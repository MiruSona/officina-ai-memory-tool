package model

import (
	"strings"
	"testing"
)

// 조사E #13 — 요약에 `\` 나 따옴표가 들어가도 다시 읽을 수 있어야 한다.
// 손으로 감싸던 시절에는 이런 파일이 색인에서 조용히 빠졌다.
func TestEncodeSurvivesBackslashAndQuotes(t *testing.T) {
	awkward := []string{
		`경로 C:\Users\bob\AppData 를 적었다 그리고 스물다섯 자를 더 채운다`,
		`"따옴표" 와 '홑따옴표' 가 같이 든 요약이다 그리고 스물다섯 자를 더 채운다`,
		`콜론: 그리고 # 우물 정 자와 % 도 든 요약이다 그리고 스물다섯 자를 더 채운다`,
		`끝이 역슬래시로 끝나는 요약이다 그리고 스물다섯 자를 더 채운다\`,
	}
	for _, summary := range awkward {
		memory := Memory{ID: "20260822-3f9a2c1b", Type: TypeHistory, Date: "2026-08-22",
			Summary: summary, Tags: []string{"mem"}, LegacySource: LegacySourceAI, Scope: "mem", Body: "본문"}
		encoded := Encode(&memory)
		back, err := Parse(encoded)
		if err != nil {
			t.Fatalf("%q 를 다시 못 읽었다 : %v\n%s", summary, err, encoded)
		}
		if back.Summary != summary {
			t.Fatalf("요약이 바뀌었다 : %q → %q", summary, back.Summary)
		}
		if back.Body != "본문" {
			t.Fatalf("본문이 바뀌었다 : %q", back.Body)
		}
	}
}

// 설계 4-1 — superseded_by 는 새로 생긴 칸이다. 값은 기억 id 여야 한다.
func TestSupersededByRoundTripAndValidate(t *testing.T) {
	memory := Memory{ID: "20260822-3f9a2c1b", Type: TypeDecision, Date: "2026-08-22",
		Summary: strings.Repeat("가", 40), Tags: []string{"mem"}, LegacySource: LegacySourceUser,
		Scope: "mem", SupersededBy: "20260901-aa11bb22", Body: "본문"}
	if problems := Validate(&memory); len(problems) != 0 {
		t.Fatalf("멀쩡한 기억인데 문제가 나왔다 : %v", problems)
	}
	encoded := string(Encode(&memory))
	if !strings.Contains(encoded, "superseded_by: 20260901-aa11bb22") {
		t.Fatalf("superseded_by 가 안 적혔다 :\n%s", encoded)
	}
	back, err := Parse([]byte(encoded))
	if err != nil || back.SupersededBy != memory.SupersededBy {
		t.Fatalf("superseded_by 를 다시 못 읽었다 : %v %+v", err, back)
	}
	memory.SupersededBy = "어제 그 결정"
	if problems := Validate(&memory); len(problems) == 0 {
		t.Fatal("id 꼴이 아닌 superseded_by 는 문제여야 한다")
	}
}
