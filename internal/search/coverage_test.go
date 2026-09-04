package search

import (
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
)

// 낱말 넷 중 하나만 걸린 문서는 0·1번 칸에 못 앉는다. "없다" 가 "몇 건 있다" 로
// 바뀌면 이 툴을 못 믿는다 (실데이터 시험 2-1).
func TestOneWordDoesNotQualify(t *testing.T) {
	database, _ := newRepo(t)
	result := ask(t, database, "셰이더 그래프 물리 리듬", 5)
	for _, hit := range result.Hits {
		if Strict(hit.Rung) {
			t.Fatalf("낱말 하나만 걸렸는데 %d번 칸에 앉았다 : %s", hit.Rung, hit.ID)
		}
	}
	if len(result.Hits) == 0 {
		return
	}
	text := Markdown(result, 0)
	if !strings.Contains(text, "넓혀서 찾았다") || !strings.Contains(text, LabelOf(result.Hits[0].Rung)) {
		t.Fatalf("넓혀서 찾은 것은 꼬리표와 알림이 붙어야 한다 : %s", text)
	}
}

// 낱말이 다 맞으면 0번 칸에 앉는다 — 관문이 정상 질의를 막으면 안 된다.
func TestAllWordsStillQualify(t *testing.T) {
	database, ids := newRepo(t)
	result := ask(t, database, "공수 리듬", 5)
	if len(result.Hits) == 0 || result.Hits[0].ID != ids[0] {
		t.Fatalf("다 맞은 질의가 막혔다 : %+v", result.Hits)
	}
	if !Strict(result.Hits[0].Rung) {
		t.Fatalf("다 맞았는데 %d번 칸이다", result.Hits[0].Rung)
	}
}

// 한 글자 한글 접두는 혼자서 자격을 못 준다. `방` 이 `방식`·`방지` 를 물어 온다
// (실데이터 시험 #19).
func TestOneLetterAloneDoesNotQualify(t *testing.T) {
	database, _ := newRepo(t)
	result := ask(t, database, "훅 셰이더", 5)
	for _, hit := range result.Hits {
		if Strict(hit.Rung) {
			t.Fatalf("한 글자만 맞았는데 %d번 칸이다 : %s", hit.Rung, hit.ID)
		}
	}
	// 질의가 통째로 한 글자면 봐준다 (설계 6-3 의 1글자 접두 규칙).
	single := ask(t, database, "훅", 5)
	if len(single.Hits) == 0 {
		t.Fatal("한 글자 질의는 접두로 찾아야 한다")
	}
}

// 과반 규칙 자체.
func TestNeedIsMajority(t *testing.T) {
	// 결정 30 을 재 보고 되돌린 자다 (coverage.go needOf 주석의 표).
	cases := map[int]int{1: 1, 2: 1, 3: 2, 4: 2, 5: 3}
	for count, want := range cases {
		if got := needOf(count); got != want {
			t.Fatalf("낱말 %d개면 %d개를 맞춰야 하는데 %d", count, want, got)
		}
	}
}

// 0건이면 네 가지가 **늘** 나온다. ③④가 빠지면 사람이 다음에 뭘 할지 모른다
// (설계 6-9 · 실데이터 시험 3-2).
func TestEmptyAlwaysHasFourParts(t *testing.T) {
	database, _ := newRepo(t)
	for _, query := range []string{"셰이더그래프zzz", "물리엔진 중력 계산"} {
		result := ask(t, database, query, 5)
		if len(result.Hits) != 0 {
			continue
		}
		text := Markdown(result, 0)
		near := strings.Contains(text, "비슷한 낱말") || strings.Contains(text, "닮은 낱말도 없다")
		next := strings.Contains(text, "다시 해 보기") || strings.Contains(text, "다시 해 볼 것")
		if !near || !next {
			t.Fatalf("%q : ③④가 빠졌다 : %s", query, text)
		}
	}
}

// 뒤집힌 기억은 --all 에서만 보이고, 보일 때는 표시와 감점이 붙는다
// (실데이터 시험 5절).
func TestInvalidIsMarkedAndPenalised(t *testing.T) {
	now := time.Now()
	shared := rules{Cap: config.DefaultBonusCap, Now: now,
		Phrase: map[int64]bool{}, Linked: map[string]bool{}}
	live := index.SearchRow{Type: "decision", CreatedAt: now.Unix()}
	dead := live
	dead.InvalidAt = now.Unix()
	liveScore, _ := scoreOf(live, 0.1, RungPlain, shared)
	deadScore, _ := scoreOf(dead, 0.1, RungPlain, shared)
	if deadScore >= liveScore {
		t.Fatalf("무효가 산 것보다 위다 : %.4f vs %.4f", deadScore, liveScore)
	}
	if deadScore != liveScore*invalidPenalty {
		t.Fatalf("감점이 %.2f 배가 아니다", invalidPenalty)
	}
	result := Result{Query: "x", Total: 1, Rungs: map[int]int{},
		Hits: []Hit{{ID: "20260822-00000000", Invalid: true, Summary: "뒤집힌 결정"}}}
	if !strings.Contains(Markdown(&result, 0), "[무효]") {
		t.Fatal("무효 표시가 없다")
	}
}

// 리뷰B #2 — 글자는 쳤는데 낱말이 하나도 안 나오면 목록이 아니라 0건 설명이다.
func TestNoWordQueryIsEmptyNotList(t *testing.T) {
	database, _ := newRepo(t)
	for _, query := range []string{"%", "*", "🔥🚀", "a", "_"} {
		result := ask(t, database, query, 5)
		if len(result.Hits) != 0 {
			t.Fatalf("%q 가 %d건을 냈다 — 전체 목록으로 샜다", query, len(result.Hits))
		}
		if result.Why == nil {
			t.Fatalf("%q 에 0건 설명이 없다 (불변조건 9)", query)
		}
	}
}

// 리뷰B #5 (되고침) — 낱말 하나가 저장소에 없으면 0·1 칸 자격은 없지만
// 사다리는 계속 밟는다. 답은 보여주되 꼬리표와 「뺀 낱말」 줄이 붙는다.
func TestMissingWordDropsToLowerRung(t *testing.T) {
	database, _ := newRepo(t)
	result := ask(t, database, "검색 셰이더", 5)
	if len(result.Hits) == 0 {
		t.Fatal("남은 낱말로도 못 찾았다 — 사다리가 끊겼다")
	}
	if Strict(result.Hits[0].Rung) {
		t.Fatalf("없는 낱말이 섞였는데 %d번 칸에 앉았다", result.Hits[0].Rung)
	}
	if len(result.MissingWords) != 1 || result.MissingWords[0] != "셰이더" {
		t.Fatalf("뺀 낱말을 안 알렸다 : %v", result.MissingWords)
	}
}

// 낱말이 다 저장소에 없으면 0건이고, 0건 설명은 **네 가지가 다** 나와야 한다
// (불변조건 9 · 설계 6-9). ④ 가 빠지면 사람은 다음에 뭘 칠지 모른다.
func TestAllWordsMissingIsEmpty(t *testing.T) {
	database, _ := newRepo(t)
	result := ask(t, database, "셰이더 리깅", 5)
	if len(result.Hits) != 0 {
		t.Fatalf("없는 낱말뿐인데 %d건이 나왔다", len(result.Hits))
	}
	if result.Why == nil || len(result.Why.Missing) != 2 {
		t.Fatalf("0건 설명이 모자라다 : %+v", result.Why)
	}
	text := Markdown(result, 0)
	// ① 없는 낱말 둘 (조사도 낱말에 맞아야 한다) ② 있는 낱말 없음 ③ 닮은 낱말 ④ 다시 해 볼 것
	for _, want := range []string{`"셰이더" 가 들어간`, `"리깅" 이 들어간`,
		"닮은 낱말도 없다", "다시 해 볼 것", "mem search 리깅", "--type"} {
		if !strings.Contains(text, want) {
			t.Fatalf("0건 설명에 %q 가 없다 : %s", want, text)
		}
	}
}

// 리뷰B #4 — 같은 낱말을 여러 번 쳐도 낱말은 하나다.
func TestRepeatedWordsCollapse(t *testing.T) {
	query := ParseQuery("훅 훅 훅 훅", nil)
	if len(query.Terms) != 1 {
		t.Fatalf("같은 낱말이 %d개로 남았다", len(query.Terms))
	}
}

// 리뷰B #3 — 낱말 수 상한을 넘으면 앞 MaxTerms 개만 쓰고 그렇다고 알린다.
func TestTermCap(t *testing.T) {
	query := ParseQuery("가나 다라 마바 사아 자차 카타 파하 아야 어여 오요", nil)
	if len(query.Terms) != MaxTerms || len(query.Dropped) != 2 {
		t.Fatalf("낱말 %d개 · 버린 것 %d개", len(query.Terms), len(query.Dropped))
	}
}

// 리뷰B #9 — 기호가 붙어도 낱말은 같은 것으로 읽는다.
func TestSymbolsDoNotChangeTerm(t *testing.T) {
	if ParseQuery("훅,", nil).Terms[0].Text != ParseQuery("훅", nil).Terms[0].Text {
		t.Fatal("`훅,` 과 `훅` 이 다른 낱말이 됐다")
	}
}

// 리뷰B #20 — 다시 해 보기가 방금 친 질의와 같으면 안 낸다.
func TestNextCommandIsNotTheSameQuery(t *testing.T) {
	if got := nextCommand(map[string]int{"훅": 3}, 1); got != "" {
		t.Fatalf("같은 질의를 다시 권했다 : %s", got)
	}
	if got := nextCommand(map[string]int{"훅": 3, "예산": 1}, 3); got != "mem search 훅 예산" {
		t.Fatalf("가장 많이 나온 둘만 남겨야 한다 : %s", got)
	}
}

// 리뷰B #1 — 시간 낱말만 있는 질의는 그 기간 목록이지 저장소 전체가 아니다.
func TestTimeOnlyQueryStaysNarrow(t *testing.T) {
	database, _ := newRepo(t)
	result := ask(t, database, "지난주", 5)
	if !result.FilterOnly {
		t.Fatal("조건만 준 목록이라고 알리지 않았다")
	}
	if result.Total >= len(corpus) {
		t.Fatalf("기간을 안 좁혔다 : 총 %d건", result.Total)
	}
}
