package budget

import "testing"

func TestEstimateKoreanAndLatin(t *testing.T) {
	if got := Estimate("가나다"); got != 3 {
		t.Fatalf("3 Korean letters should cost 3 tokens, got %d", got)
	}
	if got := Estimate("abcd"); got != 2 {
		t.Fatalf("4 latin letters should cost 2 tokens, got %d", got)
	}
	if Estimate("") != 0 {
		t.Fatal("an empty string costs nothing")
	}
}

func TestEstimateGrowsWithLength(t *testing.T) {
	short := Estimate("한글 문장 하나")
	long := Estimate("한글 문장 하나 그리고 더 긴 문장 하나 더")
	if long <= short {
		t.Fatalf("longer text must cost more: %d vs %d", short, long)
	}
}

func TestFits(t *testing.T) {
	if !Fits("아주 긴 문장이라도", 0) {
		t.Fatal("a zero budget means no limit")
	}
	if Fits("가나다라마", 2) {
		t.Fatal("over budget must not fit")
	}
}
