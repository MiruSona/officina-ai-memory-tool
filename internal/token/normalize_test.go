package token

import "testing"

func mustCanon(t *testing.T, table map[string]string) *Canon {
	t.Helper()
	canon, err := NewCanon(table)
	if err != nil {
		t.Fatalf("표를 못 읽었다: %v", err)
	}
	return canon
}

// 표가 비면 정규화는 대표말 치환을 안 한다 (결정 25 · P12 「배우는 중」).
func TestNormalizeWithoutCanonIsPlain(t *testing.T) {
	for _, canon := range []*Canon{nil, mustCanon(t, nil), mustCanon(t, map[string]string{})} {
		if got := Normalize("색인 얘기", canon); got != "색인 얘기" {
			t.Fatalf("빈 표인데 글이 바뀌었다: %q", got)
		}
	}
}

func TestNormalizeFoldsWidthCaseSpaceAndNumber(t *testing.T) {
	cases := map[string]string{
		"Ｕｎｉｔｙ　빌드":       "unity 빌드",
		"  Unity   빌드  ": "unity 빌드",
		"19,880건":        "19880건",
		"MyClass":        "myclass",
	}
	for in, want := range cases {
		if got := Normalize(in, nil); got != want {
			t.Fatalf("%q → %q, 바란 것은 %q", in, got, want)
		}
	}
}

// 조사 떼기는 v0.2 것을 그대로 쓴다 (결정 27). 뗀 결과가 한 글자면 안 뗀다.
func TestNormalizeReusesV02ParticleStrip(t *testing.T) {
	if got := Normalize("색인을 저장소에서", nil); got != "색인 저장소" {
		t.Fatalf("조사를 못 뗐다: %q", got)
	}
	// 뗀 결과가 한 글자면 안 뗀다 — `훅에서` 도 `도구` 도 그대로다.
	if got := Normalize("도구 훅에서", nil); got != "도구 훅에서" {
		t.Fatalf("한 글자로 줄어드는 것은 안 뗀다: %q", got)
	}
}

// 대표말 치환은 어절이 아니라 최장 일치 문자열 치환이다 (결정 25).
func TestCanonLongestMatchAndPhrase(t *testing.T) {
	canon := mustCanon(t, map[string]string{
		"인덱싱":   "색인",
		"인덱스":   "색인",
		"index": "색인",
		"기억 수정": "기억고치기",
	})
	cases := map[string]string{
		"인덱싱 얘기":   "색인 얘기",
		"Index 얘기": "색인 얘기",
		"기억 수정 절차": "기억고치기 절차",
		// 어절 경계를 안 본다 — 붙어 있어도 잡는다.
		"전체인덱스재생성": "전체색인재생성",
	}
	for in, want := range cases {
		if got := Normalize(in, canon); got != want {
			t.Fatalf("%q → %q, 바란 것은 %q", in, got, want)
		}
	}
}

// 라틴 키는 어절 단위로만 바꾼다 (결정 25). 한글 키는 그대로 문자열 최장 일치다.
func TestCanonLatinKeyNeedsWordEdges(t *testing.T) {
	canon := mustCanon(t, map[string]string{"index": "색인", "인덱스": "색인"})
	cases := map[string]string{
		"indexer 얘기":  "indexer 얘기", // 낱말 속은 안 바꾼다
		"reindex 얘기":  "reindex 얘기", //
		"index 얘기":    "색인 얘기",      // 단독은 바꾼다
		"index.db 얘기": "색인.db 얘기",   // 점도 경계다
		"(index)":     "(색인)",       // 괄호도 경계다
		"전체인덱스재생성":    "전체색인재생성",    // 한글 키는 붙어 있어도 바꾼다
	}
	for in, want := range cases {
		if got := Normalize(in, canon); got != want {
			t.Fatalf("%q → %q, 바란 것은 %q", in, got, want)
		}
	}
}

// 색인 쪽과 질의 쪽이 같은 함수를 타면 같은 글자가 된다 (결정 21·23).
func TestIndexAndQuerySideMeet(t *testing.T) {
	canon := mustCanon(t, map[string]string{"인덱싱": "색인"})
	doc := Normalize("인덱싱을 다시 돌린다", canon)
	query := Normalize("색인", canon)
	if !contains(NormBigrams(doc, nil), "색인") {
		t.Fatalf("색인 쪽 조각에 대표말이 없다: %q", NormBigrams(doc, nil))
	}
	if NormQueryExpr("인덱싱", canon) != NormQueryExpr("색인", canon) {
		t.Fatalf("질의 두 꼴이 다르다: %q vs %q",
			NormQueryExpr("인덱싱", canon), NormQueryExpr("색인", canon))
	}
	if query == "" {
		t.Fatal("질의가 비었다")
	}
}

func contains(haystack, needle string) bool {
	for at := 0; at+len(needle) <= len(haystack); at++ {
		if haystack[at:at+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestNewCanonRejectsEmptyAndCycle(t *testing.T) {
	bad := []map[string]string{
		{"": "색인"},
		{"인덱싱": ""},
		{"인덱싱": "  "},
		{"색인": "색인"},
		{"인덱싱": "색인", "색인": "index"},
	}
	for _, table := range bad {
		if _, err := NewCanon(table); err == nil {
			t.Fatalf("%v 는 거절해야 한다", table)
		}
	}
}

// 자모는 보조 신호다 (결정 29). 주 신호로 안 쓴다.
func TestJamo(t *testing.T) {
	if got := Jamo("한글a"); got != "ㅎㅏㄴㄱㅡㄹa" {
		t.Fatalf("자모 분해가 %q 다", got)
	}
	if Jamo("갔") != "ㄱㅏㅆ" {
		t.Fatalf("겹받침 자리가 틀렸다: %q", Jamo("갔"))
	}
}
