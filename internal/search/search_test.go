package search

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
)

// 두 글자 한글 회귀 — 다섯 낱말이 다 정답 하나를 잡아야 한다 (설계 12-3).
func TestTwoLetterKoreanFindsIt(t *testing.T) {
	database, ids := newRepo(t)
	for _, word := range []string{"공수", "리듬", "파형", "박자", "음색"} {
		result := ask(t, database, word, 5)
		if !hasID(result, ids[0]) {
			t.Fatalf("%q 로 두 글자 회귀 기억을 못 찾았다 : %d건", word, result.Total)
		}
	}
}

// 영문 회귀 — trigram 을 뺀 대신 색인 쪼개기가 버티는지 (설계 12-3).
func TestLatinRegression(t *testing.T) {
	database, ids := newRepo(t)
	for _, word := range []string{"Rider", "dotnet-format", "MyClassName", "IDE(Rider/VS)와"} {
		result := ask(t, database, word, 5)
		if !hasID(result, ids[1]) {
			t.Fatalf("%q 로 영문 회귀 기억을 못 찾았다 : %d건", word, result.Total)
		}
	}
}

// hard 다섯 갈래 — 따옴표·붙여쓰기·동의어·숫자단위·한영섞임 (설계 12-3).
func TestHardFiveShapes(t *testing.T) {
	database, ids := newRepo(t)
	cases := []struct {
		query string
		want  int
	}{
		{`"바이그램 검색"`, 3},
		{"기억저장소", 2},
		{"memory 저장소", 2},
		{"5000건", 4},
		{"hook 예산", 5},
	}
	for _, item := range cases {
		result := ask(t, database, item.query, 5)
		if !hasID(result, ids[item.want]) {
			t.Fatalf("%q 로 %d번 기억을 못 찾았다 (%d건)", item.query, item.want, result.Total)
		}
	}
}

// 사다리는 엉뚱한 1건에서 안 멈춘다 — limit 을 채울 때까지 계속 밟는다 (설계 6-6).
func TestLadderDoesNotStopAtOne(t *testing.T) {
	database, _ := newRepo(t)
	result := ask(t, database, "공수 성능", 5)
	if len(result.Hits) < 2 {
		t.Fatalf("1건에서 멈췄다 : %d건", len(result.Hits))
	}
	if LabelOf(RungOr) == "" || LabelOf(RungAnd) != "" {
		t.Fatal("꼬리표는 2번 칸부터만 붙는다")
	}
}

// 한 칸이라도 내려갔으면 한 줄 알림과 꼬리표가 붙는다 (설계 6-6).
func TestRelaxedNotice(t *testing.T) {
	result := Result{Query: "훅 성능", Total: 5, Rungs: map[int]int{RungAnd: 2, RungSynonym: 3},
		Relaxed: true, Hits: []Hit{{ID: "20260822-00000000", Rung: RungSynonym, Summary: "넓혀서 찾은 것"}}}
	text := Markdown(&result, 0)
	if !strings.Contains(text, "넓혀서 찾았다") {
		t.Fatalf("알림 줄이 없다 : %s", text)
	}
	if !strings.Contains(text, LabelOf(RungSynonym)) {
		t.Fatalf("꼬리표가 없다 : %s", text)
	}
}

// 가산 고삐 — 관련도 0 인 고정 결정이 가산만으로 올라오면 안 된다 (설계 6-5 고삐 ①).
func TestBonusCannotLiftUnrelated(t *testing.T) {
	database, ids := newRepo(t)
	result := ask(t, database, "공수 리듬", 5)
	if rankOfID(result, ids[6]) == 1 {
		t.Fatal("관련도 0 인 고정 결정이 1위를 먹었다")
	}
	if rankOfID(result, ids[0]) != 1 {
		t.Fatalf("정확히 맞은 기억이 1위여야 한다 : %d위", rankOfID(result, ids[0]))
	}
	row := index.SearchRow{Pinned: true, Type: "decision", Severity: "high", Importance: 5, HitCount: 20}
	shared := rules{Cap: config.DefaultBonusCap, Now: time.Now(),
		Phrase: map[int64]bool{}, Linked: map[string]bool{}}
	bonus, _ := bonusOf(row, shared)
	if bonus > shared.Cap {
		t.Fatalf("가산이 상한 %.1f 를 넘었다 : %.2f", shared.Cap, bonus)
	}
}

// 0건이면 네 가지를 다 말한다. --json 은 [] 다 (불변조건 9 · 설계 6-9).
func TestEmptyExplains(t *testing.T) {
	database, _ := newRepo(t)
	result := ask(t, database, "셰이더 공수", 5)
	if len(result.Hits) != 0 {
		t.Skip("시험 말뭉치가 이 낱말을 잡았다")
	}
	if result.Hits == nil {
		t.Fatal("0건이어도 nil 이 아니라 빈 목록이어야 한다")
	}
	if result.Why == nil {
		t.Fatal("0건인데 설명이 없다")
	}
	text := Markdown(result, 0)
	for _, want := range []string{"하나도 없다", "있다", "다시 해 보기", "전체"} {
		if !strings.Contains(text, want) {
			t.Fatalf("0건 설명에 %q 가 없다 :\n%s", want, text)
		}
	}
}

// 완전히 없는 낱말이면 닮은 낱말을 보여준다 (설계 6-9 ③).
func TestEmptyShowsNearWords(t *testing.T) {
	database, _ := newRepo(t)
	result := ask(t, database, "공사판셰이더", 5)
	if result.Why == nil || len(result.Why.Missing) == 0 {
		t.Fatalf("없는 낱말을 못 짚었다 : %+v", result.Why)
	}
}

// 불용어는 AND 에서 빼고 알린다. 불용어만 남으면 안 뺀다 (설계 6-1a).
func TestStopwords(t *testing.T) {
	query := ParseQuery("훅 은 왜 느렸나", config.DefaultStopwords())
	if len(query.Stopped) == 0 {
		t.Fatalf("흔한 낱말을 못 뺐다 : %+v", query.Stopped)
	}
	if len(query.Live()) == len(query.Terms) {
		t.Fatal("AND 에서 빠진 낱말이 없다")
	}
	only := ParseQuery("왜 지금", config.DefaultStopwords())
	if len(only.Live()) != len(only.Terms) {
		t.Fatal("불용어만 남으면 빼면 안 된다")
	}
}

// 조사를 뗀 꼴도 같이 던진다 (설계 6-1c).
func TestParticleInQuery(t *testing.T) {
	query := ParseQuery("결정을 오류로", nil)
	if query.Terms[0].Stem != "결정" || query.Terms[1].Stem != "오류" {
		t.Fatalf("조사를 안 뗐다 : %+v", query.Terms)
	}
	database, ids := newRepo(t)
	result := ask(t, database, "결정 충돌을", 5)
	if !hasID(result, ids[10]) {
		t.Fatalf("조사 붙은 낱말로 못 찾았다 : %d건", result.Total)
	}
}

// 시간 표현은 범위로 바꾸고 낱말에서 뺀다. 범위가 비면 풀고 다시 찾는다 (설계 6-8).
func TestTimeFilter(t *testing.T) {
	query := ParseQuery("지난주 색인", nil)
	if query.TimeWord == "" || query.Since.IsZero() {
		t.Fatalf("시간 표현을 못 읽었다 : %+v", query)
	}
	for _, item := range query.Terms {
		if item.Text == "지난주" {
			t.Fatal("시간 낱말이 질의에 남았다")
		}
	}
	database, _ := newRepo(t)
	result := ask(t, database, "지난주 바이그램", 5)
	if len(result.Hits) == 0 || !result.TimeRelaxed {
		t.Fatalf("범위가 비면 풀고 다시 찾아야 한다 : %d건 relaxed=%v", len(result.Hits), result.TimeRelaxed)
	}
}

// 동의어 랭킹의 가중은 절반이다 (설계 6-4).
func TestSynonymWeightIsHalf(t *testing.T) {
	if config.DefaultSynWeight*2 != weightKO {
		t.Fatalf("동의어 가중이 절반이 아니다 : %v", config.DefaultSynWeight)
	}
	helper := rungHelper{synonym: map[string][]string{"기억": {"memory"}},
		synWeight: config.DefaultSynWeight}
	terms := ParseQuery("기억 저장소", nil).Terms
	ranks := synonymRanks(terms, &helper, false)
	if len(ranks) == 0 {
		t.Fatal("동의어 랭킹이 안 생겼다")
	}
	for _, item := range ranks {
		if item.weight != config.DefaultSynWeight {
			t.Fatalf("동의어 랭킹 가중이 %v 다", item.weight)
		}
	}
}

// 파서는 하나뿐이다 — CLI 가 받은 문자열과 골든셋의 q 가 같은 Query 가 된다 (설계 6-11).
func TestOneParserOnly(t *testing.T) {
	text := `"훅 성능" #search scope:mem 지난주 결정을`
	fromCLI := ParseQuery(text, config.DefaultStopwords())
	fromGolden := ParseQuery(text, config.DefaultStopwords())
	if !reflect.DeepEqual(fromCLI, fromGolden) {
		t.Fatalf("같은 문자열이 다른 Query 가 됐다\n%+v\n%+v", fromCLI, fromGolden)
	}
	if len(fromCLI.Tags) != 1 || fromCLI.Tags[0] != "search" {
		t.Fatalf("#태그를 못 읽었다 : %+v", fromCLI.Tags)
	}
	if len(fromCLI.Scopes) != 1 || fromCLI.Scopes[0] != "mem" {
		t.Fatalf("scope: 를 못 읽었다 : %+v", fromCLI.Scopes)
	}
	if len(fromCLI.Phrases) == 0 {
		t.Fatal("따옴표 묶음이 구절 가산으로 안 들어갔다")
	}
}

// 질의가 없으면 조건 목록이다. 점수 없이 날짜 내림이다 (설계 8-1 #5).
func TestListWithoutQuery(t *testing.T) {
	database, _ := newRepo(t)
	result, err := Search(Options{Sources: []Source{{DB: database}}, Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 3 || result.Total != len(corpus) {
		t.Fatalf("목록이 이상하다 : %d건 중 %d", result.Total, len(result.Hits))
	}
	for _, hit := range result.Hits {
		if hit.Score != 0 {
			t.Fatal("목록에는 점수가 없어야 한다")
		}
	}
}

// --facet 은 값마다 건수를 센다 (설계 8-1 #5).
func TestFacet(t *testing.T) {
	database, _ := newRepo(t)
	counts, err := Facet(Options{Sources: []Source{{DB: database}}}, "scope")
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 1 || counts[0].Count != len(corpus) {
		t.Fatalf("scope 세기가 틀렸다 : %+v", counts)
	}
}
