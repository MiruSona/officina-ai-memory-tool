package token

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestForIndexIsPiecesInOrder(t *testing.T) {
	got := ForIndex("바이트로 저장")
	if got != "바이 이트 트로 저장" {
		t.Fatalf("index stems must be the pieces in reading order, got %q", got)
	}
}

func TestForIndexSingleLetterAndEnglish(t *testing.T) {
	got := ForIndex("Rider 를 dotnet-format 로 돌린다")
	if !strings.Contains(got, "Rider") || !strings.Contains(got, "dotnet-format") {
		t.Fatalf("English must survive as is: %s", got)
	}
	if !strings.Contains(got, " 를 ") {
		t.Fatalf("a single Korean letter must stay as itself: %s", got)
	}
}

func TestForQueryIsBigramPhraseOnly(t *testing.T) {
	got := ForQuery("바이트")
	if got != `"바이 이트"` {
		t.Fatalf("query stems must be one phrase, got %s", got)
	}
	if strings.Contains(got, "바이트\"") {
		t.Fatal("the whole word must not be required")
	}
	if single := ForQuery("공"); single != `"공"*` {
		t.Fatalf("one letter must be a prefix query, got %s", single)
	}
	if two := ForQuery("공수"); two != `"공수"` {
		t.Fatalf("two letters make one bigram, got %s", two)
	}
}

func TestForQueryMixedTerm(t *testing.T) {
	got := ForQuery("Rider포매터")
	if got != `"Rider" AND "포매 매터"` {
		t.Fatalf("unexpected mixed expression: %s", got)
	}
	if expr := QueryExpr("공수 기록"); expr != `"공수" AND "기록"` {
		t.Fatalf("unexpected query expression: %s", expr)
	}
}

// The pair test runs through a real fts5 table: what ForIndex wrote must be
// found by what ForQuery asks for. Comparing the two strings by hand used to
// pass while search silently returned nothing (design 4-1a).
func TestIndexAndQuerySidesAgree(t *testing.T) {
	cases := []struct{ doc, query string }{
		{"저장소를 바이트로 저장한다", "바이트"},
		{"공수기록을 다시 본다", "공수"},
		{"리스크가 크다", "리스크"},
		{"훅에서 색인이 돈다", "색인"},
		{"unity빌드 설정을 바꿨다", "unity빌드"},
		{"unity빌드 설정을 바꿨다", "빌드"},
		{"빌드unity 로 적어도 찾는다", "빌드unity"},
		{"Rider포매터를 돌린다", "Rider포매터"},
		{"파일을 dotnet-format 로 돌린다", "dotnet-format"},
		{"두 낱말 모두 있어야 한다", "낱말 한다"},
	}
	for _, item := range cases {
		if count := matchCount(t, item.doc, QueryExpr(item.query)); count != 1 {
			t.Errorf("%q must find %q, got %d hits", item.query, item.doc, count)
		}
	}
}

// A phrase query must find the same document too, which is why the pieces of a
// mixed word are written in reading order.
func TestPhraseSidesAgree(t *testing.T) {
	for _, item := range []struct{ doc, phrase string }{
		{"unity빌드 설정을 바꿨다", "unity빌드"},
		{"저장소를 바이트로 저장한다", "바이트"},
	} {
		if count := matchCount(t, item.doc, PhraseFor(item.phrase)); count != 1 {
			t.Errorf("phrase %q must find %q, got %d hits", item.phrase, item.doc, count)
		}
	}
}

// An unrelated document must not be dragged in by the bigrams.
func TestUnrelatedDocumentStaysOut(t *testing.T) {
	if count := matchCount(t, "리스트를 스크립트로 옮긴다", QueryExpr("리스크")); count != 0 {
		t.Errorf("bigrams must stay a phrase, got %d hits", count)
	}
}

// matchCount indexes one document the way index does and asks fts5 the way
// search does.
func matchCount(t *testing.T, doc, expr string) int {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+t.TempDir()+"/pair.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec("CREATE VIRTUAL TABLE fts_ko USING fts5(stems, tokenize='unicode61')"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO fts_ko(stems) VALUES(?)", ForIndex(doc)); err != nil {
		t.Fatal(err)
	}
	count := 0
	if err := database.QueryRow("SELECT COUNT(*) FROM fts_ko WHERE fts_ko MATCH ?", expr).Scan(&count); err != nil {
		t.Fatalf("MATCH %s: %v", expr, err)
	}
	return count
}

// #2 A phrase query must find the document whose words sit next to each other,
// and only that one. This is what the position preserving ForIndex buys: the
// original chunk used to be pasted in front, which pushed the pieces apart.
func TestPhraseNeedsAdjacentTokens(t *testing.T) {
	doc := "팀은 AND 교집합 규칙을 쓴다"
	if count := matchCount(t, doc, PhraseFor("AND 교집합")); count != 1 {
		t.Errorf("phrase must find the adjacent words, got %d hits", count)
	}
	apart := "AND 로 묶는다. 한참 뒤에 교집합 이야기가 나온다"
	if count := matchCount(t, apart, PhraseFor("AND 교집합")); count != 0 {
		t.Errorf("words far apart must not answer a phrase, got %d hits", count)
	}
}

// #3 One Korean letter is asked as a prefix, because the index holds bigrams.
func TestOneLetterAsksPrefix(t *testing.T) {
	if got := ForQuery("훅"); got != `"훅"*` {
		t.Fatalf("one letter must be a prefix query, got %s", got)
	}
	if got := PhraseFor("훅"); got != `"훅"*` {
		t.Fatalf("a quoted one letter must be a prefix query too, got %s", got)
	}
	if count := matchCount(t, "훅에서 색인이 돈다", ForQuery("훅")); count != 1 {
		t.Errorf("a one letter query must find the word it starts, got %d hits", count)
	}
	if count := matchCount(t, "색인만 있는 문서다", ForQuery("훅")); count != 0 {
		t.Errorf("the prefix must not drag in unrelated text, got %d hits", count)
	}
	if got := ForQuery("훅에"); got != `"훅에"` {
		t.Fatalf("two letters stay an exact bigram, got %s", got)
	}
}

// 조사E #5 — 가운데 자르기는 없앴다. 어휘를 모르면 아예 안 쪼갠다.
func TestSplitRunDoesNotCutInTheMiddle(t *testing.T) {
	if _, _, ok := SplitRun("바이그램검색"); ok {
		t.Fatal("어휘 없이 쪼개면 안 된다")
	}
	if _, _, ok := SplitRun("기억품질"); ok {
		t.Fatal("어휘 없이 쪼개면 안 된다")
	}
}

// 어휘를 주면 아는 낱말 중 가장 긴 것을 앞에 놓고 쪼갠다.
func TestSplitByVocabUsesLongestKnownWord(t *testing.T) {
	vocab := map[string]bool{"바이그램": true, "검색": true, "바이": true, "그램검색": true}
	left, right, ok := SplitByVocab("바이그램검색", func(word string) bool { return vocab[word] })
	if !ok || left != "바이그램" || right != "검색" {
		t.Fatalf("긴 낱말 먼저 쪼개야 한다 : %q %q %v", left, right, ok)
	}
	if _, _, ok := SplitByVocab("dotnet-format", func(string) bool { return true }); ok {
		t.Fatal("한글 낱말만 쪼갠다")
	}
	if _, _, ok := SplitByVocab("기억", func(string) bool { return true }); ok {
		t.Fatal("짧은 낱말은 안 쪼갠다")
	}
}

// 설계 6-1 — 숫자 쉼표를 없애고 단위를 떼되 원형도 남긴다.
func TestForIndexNormalisesNumbers(t *testing.T) {
	got := ForIndex("5,000건")
	for _, want := range []string{"5000", "건", "5000건", "5,000건"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q 가 없다 : %s", want, got)
		}
	}
	if count := matchCount(t, "가데이터 5,000건을 만들었다", QueryExpr("5000건")); count != 1 {
		t.Errorf("쉼표 없이 물어도 찾아야 한다, %d 건", count)
	}
	if count := matchCount(t, "가데이터 5000건을 만들었다", QueryExpr("5,000건")); count != 1 {
		t.Errorf("쉼표를 넣고 물어도 찾아야 한다, %d 건", count)
	}
}

// 설계 6-1 — camelCase 와 기호에서 쪼개되 원형은 남긴다.
func TestForIndexSplitsCamelCaseAndSymbols(t *testing.T) {
	got := ForIndex("MyClassName 을 dotnet-format 로 고친다")
	for _, want := range []string{"MyClassName", "myclassname", "my", "class", "name", "dotnet-format", "dotnet", "format"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q 가 없다 : %s", want, got)
		}
	}
	if count := matchCount(t, "MyClassName 을 고쳤다", QueryExpr("class")); count != 1 {
		t.Errorf("camelCase 가운데 낱말로 찾아야 한다, %d 건", count)
	}
	if count := matchCount(t, "MyClassName 을 고쳤다", QueryExpr("MyClassName")); count != 1 {
		t.Errorf("원형으로도 찾아야 한다, %d 건", count)
	}
}

// 글자종 경계에서 쪼갠 것이 원형과 같이 살아 있어야 한다.
func TestForIndexKeepsScriptBoundaryAndWhole(t *testing.T) {
	for _, pair := range []struct{ doc, query string }{
		{"unity빌드 설정을 바꿨다", "빌드"},
		{"unity빌드 설정을 바꿨다", "unity"},
		{"unity빌드 설정을 바꿨다", "unity빌드"},
	} {
		if count := matchCount(t, pair.doc, QueryExpr(pair.query)); count != 1 {
			t.Errorf("%q 로 %q 를 찾아야 한다, %d 건", pair.query, pair.doc, count)
		}
	}
}

// 2글자 한글 회귀 — 깨지면 token 경로가 망가진 것이다 (설계 12-3).
func TestTwoLetterKoreanRegression(t *testing.T) {
	doc := "공수 기록과 리듬 파형, 박자와 음색을 함께 적었다"
	for _, word := range []string{"공수", "리듬", "파형", "박자", "음색"} {
		if count := matchCount(t, doc, QueryExpr(word)); count != 1 {
			t.Errorf("%q 를 못 찾았다, %d 건", word, count)
		}
	}
	for _, word := range []string{"설계", "문서", "도구", "로그"} {
		if count := matchCount(t, doc, QueryExpr(word)); count != 0 {
			t.Errorf("%q 는 없는 낱말인데 %d 건 나왔다", word, count)
		}
	}
}
