package token

import (
	"strings"
	"testing"
)

// 설계 12-3 「토큰 짝」 — 색인용이 만든 조각 안에 질의용 조각이 반드시 들어 있어야 한다.
func TestIndexAndQueryPair(t *testing.T) {
	words := []string{"공수", "리듬", "파형", "박자", "음색", "바이트로", "unity빌드",
		"MyClassName", "dotnet-format", "5,000건", "IDE(Rider/VS)와", "Rider"}
	for _, word := range words {
		indexed := " " + ForIndex(word) + " "
		for _, part := range Runs(word) {
			for _, piece := range strings.Fields(strings.Trim(MatchFor(part), `"*`)) {
				if !holds(indexed, piece) {
					t.Fatalf("%q : 질의 조각 %q 가 색인 조각 %q 에 없다", word, piece, indexed)
				}
			}
		}
	}
}

// 두 글자 한글은 바이그램 하나가 되고 그것이 색인에도 있어야 한다 (설계 12-3).
func TestTwoLetterKorean(t *testing.T) {
	for _, word := range []string{"공수", "리듬", "파형", "박자", "음색"} {
		parts := Runs(word)
		if len(parts) != 1 || parts[0].Kind != RunKO {
			t.Fatalf("%q 가 한글 조각 하나가 아니다 : %+v", word, parts)
		}
		if MatchFor(parts[0]) != `"`+word+`"` {
			t.Fatalf("%q 질의 꼴이 이상하다 : %s", word, MatchFor(parts[0]))
		}
	}
}

// 영문 회귀 — trigram 을 뺀 대신 색인 쪼개기가 버티는지 (설계 12-3).
func TestLatinSplits(t *testing.T) {
	indexed := ForIndex("MyClassName dotnet-format IDE(Rider/VS)와")
	for _, want := range []string{"myclassname", "my", "class", "name", "dotnet", "format", "Rider", "VS"} {
		if !strings.Contains(" "+indexed+" ", " "+want+" ") {
			t.Fatalf("색인에 %q 가 없다 : %s", want, indexed)
		}
	}
}

// 글자종 경계에서 갈라야 한다 (설계 6-3).
func TestRunsSplitByScript(t *testing.T) {
	parts := Runs("unity빌드")
	if len(parts) != 2 || parts[0].Kind != RunEN || parts[1].Kind != RunKO {
		t.Fatalf("글자종으로 안 갈렸다 : %+v", parts)
	}
	number := Runs("5,000건")
	if len(number) != 2 || number[0].Text != "5000" || number[1].Kind != RunKO1 {
		t.Fatalf("숫자 정규화가 안 됐다 : %+v", number)
	}
	if len(Runs("a")) != 0 {
		t.Fatal("영문 한 글자는 버려야 한다")
	}
}

// 조사 떼기 — 뗀 결과가 한 글자면 안 뗀다 (설계 6-1c).
func TestStripParticle(t *testing.T) {
	cases := map[string]string{"결정을": "결정", "오류로": "오류", "저장소에서": "저장소", "도구": "", "가로": "", "정보": ""}
	for word, want := range cases {
		got, ok := StripParticle(word)
		if want == "" && ok {
			t.Fatalf("%q 는 안 떼야 한다 : %q", word, got)
		}
		if want != "" && got != want {
			t.Fatalf("%q → %q 여야 하는데 %q(%v)", word, want, got, ok)
		}
	}
}

// 붙은 낱말 쪼개기는 후보를 다 낸다. 가운데를 무조건 자르면 안 된다 (조사E #5).
func TestSplitPairs(t *testing.T) {
	known := func(word string) bool {
		return word == "바이그램" || word == "검색" || word == "기억" || word == "저장소"
	}
	pairs := SplitPairs("바이그램검색", known)
	if len(pairs) != 1 || pairs[0][0] != "바이그램" || pairs[0][1] != "검색" {
		t.Fatalf("바이그램+검색 으로 안 쪼갰다 : %v", pairs)
	}
	if len(SplitPairs("기억저장소", known)) != 1 {
		t.Fatalf("기억+저장소 를 못 찾았다")
	}
	if SplitPairs("바이그램검색", nil) != nil {
		t.Fatal("어휘를 모르면 안 쪼갠다")
	}
}

// holds 는 조각이 색인 줄에 들어 있는지 본다. 기호는 unicode61 이 어차피
// 낱말 경계로 보므로 글자·숫자만 남겨 견준다.
func holds(indexed, piece string) bool {
	if strings.Contains(indexed, " "+piece+" ") {
		return true
	}
	return strings.Contains(plainLetters(indexed), plainLetters(piece))
}

func plainLetters(text string) string {
	kept := strings.Builder{}
	for _, letter := range text {
		if letter == ' ' || (letter >= '0' && letter <= '9') ||
			(letter >= 'a' && letter <= 'z') || (letter >= 'A' && letter <= 'Z') || letter > 127 {
			kept.WriteRune(letter)
			continue
		}
		kept.WriteRune(' ')
	}
	return kept.String()
}
