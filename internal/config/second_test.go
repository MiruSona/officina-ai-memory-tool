package config

import (
	"strings"
	"testing"
)

// TestSynonymTableRoundTrip is #22: [synonym] holds lists, not strings.
func TestSynonymTableRoundTrip(t *testing.T) {
	want := Default("시험")
	want.Synonym = map[string][]string{"색인": {"인덱스", "index"}, "훅": {"hook"}}
	text := string(Encode(want))
	if !strings.Contains(text, "[synonym]") {
		t.Fatalf("[synonym] 절이 있어야 한다 :\n%s", text)
	}
	back, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Synonym["색인"]) != 2 || back.Synonym["색인"][0] != "인덱스" {
		t.Fatalf("색인 동의어가 %v 로 돌아왔다", back.Synonym["색인"])
	}
	if len(back.Synonym["훅"]) != 1 || back.Synonym["훅"][0] != "hook" {
		t.Fatalf("훅 동의어가 %v 로 돌아왔다", back.Synonym["훅"])
	}
}

// TestEmptySynonymAndScoreWriteNoSection is #22b: mem init has to write the
// same bytes twice, so a section with nothing in it is never written.
func TestEmptySynonymAndScoreWriteNoSection(t *testing.T) {
	bare := Default("시험")
	bare.Synonym = map[string][]string{}
	text := string(Encode(bare))
	if strings.Contains(text, "[synonym]") || strings.Contains(text, "[score]") {
		t.Fatalf("빈 절은 안 쓴다 :\n%s", text)
	}
	if string(Encode(bare)) != text {
		t.Fatal("두 번 쓴 바이트가 다르다")
	}
	back, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	if back.Score.Importance != DefaultImportanceWeight {
		t.Fatalf("[score] 가 없으면 기본 %v 여야 한다 : %v", DefaultImportanceWeight, back.Score.Importance)
	}
	// [synonym] 절이 없으면 기본 여섯 줄이 살아 있어야 한다 (리뷰B #8).
	// [stopword] 와 규칙이 같아진 자리라 다시 쓰면 그 절이 생긴다.
	if len(back.Synonym) != len(DefaultSynonyms()) {
		t.Fatalf("[synonym] 이 없으면 기본표가 살아야 한다 : %v", back.Synonym)
	}
	if !strings.Contains(string(Encode(back)), "[synonym]") {
		t.Fatal("기본표가 살아 있으면 다시 쓸 때 [synonym] 이 나온다")
	}
}

// TestScoreImportanceZero is #23c: a person may turn the term off, and 0 must
// survive the round trip instead of falling back to the default.
func TestScoreImportanceZero(t *testing.T) {
	want := Default("시험")
	want.Score.Importance = 0
	text := string(Encode(want))
	if !strings.Contains(text, "importance = 0") {
		t.Fatalf("[score] importance = 0 이 있어야 한다 :\n%s", text)
	}
	back, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	if back.Score.Importance != 0 {
		t.Fatalf("0 이 %v 로 돌아왔다", back.Score.Importance)
	}
	if half, err := Parse("[score]\nimportance = 0.5\n"); err != nil || half.Score.Importance != 0.5 {
		t.Fatalf("소수점 값을 읽어야 한다 : %v %v", half.Score.Importance, err)
	}
}
