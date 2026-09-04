package link

import (
	"fmt"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

func doc(id string, tags []string, scope, date string) Doc {
	return Doc{ID: id, Tags: tags, Scope: scope, Date: date, Title: id + " 제목"}
}

// 태그 2개가 겹치면 그것 하나로 후보가 된다 (결정 42 ①).
func TestTwoTagsAreEnough(t *testing.T) {
	target := doc("m1", []string{"search", "index"}, "mem", "2026-01-01")
	others := []Doc{doc("m2", []string{"search", "index"}, "gc", "2025-01-01")}
	found := Suggest(target, others, MaxLinks)
	if len(found) != 1 || found[0].ID != "m2" {
		t.Fatalf("태그 2개 겹침이 후보가 아니다: %+v", found)
	}
}

// 태그 하나만 겹치는 것은 혼자서 못 넘는다. 넓히면 저장소 절반이 이웃이 된다.
func TestOneTagIsNotEnough(t *testing.T) {
	target := doc("m1", []string{"search", "index"}, "mem", "2026-01-01")
	others := []Doc{doc("m2", []string{"search", "hook"}, "gc", "2020-01-01")}
	if found := Suggest(target, others, MaxLinks); len(found) != 0 {
		t.Fatalf("태그 1개로 후보가 됐다: %+v", found)
	}
}

// 같은 근거 파일을 가리키면 후보다 (②).
func TestSameSource(t *testing.T) {
	target := Doc{ID: "m1", Tags: []string{"a"}, Sources: []string{"file:internal/gc/apply.go#118"}}
	others := []Doc{{ID: "m2", Tags: []string{"b"}, Sources: []string{"file:internal/gc/apply.go"}}}
	found := Suggest(target, others, MaxLinks)
	if len(found) != 1 {
		t.Fatalf("같은 파일 근거가 후보가 아니다: %+v", found)
	}
}

// 같은 scope 에 공통 식별자가 있으면 후보다 (④). 한글 보통명사는 안 센다.
func TestScopeAndIdentifier(t *testing.T) {
	target := Doc{ID: "m1", Scope: "mem", Title: "gc apply.go 가 fts_norm 을 안 고친다"}
	others := []Doc{{ID: "m2", Scope: "mem", Title: "fts_norm 재삽입 규칙"},
		{ID: "m3", Scope: "mem", Title: "검색 이야기"}}
	found := Suggest(target, others, MaxLinks)
	if len(found) != 1 || found[0].ID != "m2" {
		t.Fatalf("식별자 겹침만 골라야 한다: %+v", found)
	}
}

// 이미 적힌 링크와 자기 자신은 다시 안 뽑는다.
func TestSkipKnown(t *testing.T) {
	target := doc("m1", []string{"search", "index"}, "mem", "2026-01-01")
	target.Links = []string{"m2"}
	others := []Doc{doc("m2", []string{"search", "index"}, "mem", "2026-01-01"), target}
	if found := Suggest(target, others, MaxLinks); len(found) != 0 {
		t.Fatalf("이미 이은 것을 또 뽑았다: %+v", found)
	}
}

// 상위 몇 개만 남긴다 (결정 42 「상위 3~5개만」).
func TestTopOnly(t *testing.T) {
	target := doc("m0", []string{"search", "index"}, "mem", "2026-01-01")
	others := []Doc{}
	for _, id := range []string{"m1", "m2", "m3", "m4", "m5", "m6", "m7"} {
		others = append(others, doc(id, []string{"search", "index"}, "mem", "2026-01-01"))
	}
	if found := Suggest(target, others, MaxLinks); len(found) != MaxLinks {
		t.Fatalf("상한 %d 을 안 지켰다: %d", MaxLinks, len(found))
	}
}

// 점수 차례가 뒤집히면 안 된다 — 태그 3개가 태그 2개보다 위다.
func TestOrderByScore(t *testing.T) {
	target := doc("m0", []string{"a", "b", "c"}, "mem", "2026-01-01")
	others := []Doc{doc("weak", []string{"a", "b"}, "gc", "2020-01-01"),
		doc("strong", []string{"a", "b", "c"}, "gc", "2020-01-01")}
	found := Suggest(target, others, MaxLinks)
	if len(found) != 2 || found[0].ID != "strong" {
		t.Fatalf("점수 차례가 틀렸다: %+v", found)
	}
}

func TestSimilarityAndTopic(t *testing.T) {
	a := doc("m1", []string{"search", "index"}, "mem", "2026-01-01")
	b := doc("m2", []string{"search", "index"}, "mem", "2026-01-01")
	c := doc("m3", []string{"gc", "hook"}, "build", "2026-01-01")
	if Similarity(a, b) <= Similarity(a, c) {
		t.Fatal("같은 태그 쪽이 더 닮아야 한다")
	}
	if !SameTopic(a, b) || SameTopic(a, c) {
		t.Fatalf("같은 주제 판정이 틀렸다: %v %v", SameTopic(a, b), SameTopic(a, c))
	}
}

// 통을 나눠 재도 짝을 다 도는 것과 답이 같아야 한다. 통 하나가 blockMax 를
// 안 넘는 규모(실기억 206건)에서는 **한 건도 안 달라야** 한다.
func TestSuggestAllMatchesSuggest(t *testing.T) {
	docs := []Doc{}
	tags := [][]string{{"search", "index"}, {"search", "hook"}, {"index", "gc"},
		{"hook", "gc"}, {"search", "index", "gc"}, {"lint"}}
	for at := 0; at < 60; at++ {
		one := doc(string(rune('a'+at%26))+string(rune('a'+at/26))+"-id",
			tags[at%len(tags)], []string{"mem", "officina"}[at%2],
			[]string{"2026-08-20", "2026-08-21", "2026-08-22"}[at%3])
		one.Title = "제목 " + string(rune('가'+at%14))
		one.Summary = "요약 handler" + string(rune('a'+at%7)) + " 자리"
		docs = append(docs, one)
	}
	all := SuggestAll(docs, MaxLinks)
	for at := range docs {
		want := Suggest(docs[at], docs, MaxLinks)
		got := all[docs[at].ID]
		if len(want) != len(got) {
			t.Fatalf("%s : 건수 %d != %d", docs[at].ID, len(want), len(got))
		}
		for k := range want {
			if want[k].ID != got[k].ID || want[k].Score != got[k].Score {
				t.Fatalf("%s : %d번째 %s(%.3f) != %s(%.3f)", docs[at].ID, k+1,
					want[k].ID, want[k].Score, got[k].ID, got[k].Score)
			}
		}
	}
}

// 둘레만 다시 재도 답이 통째로 다시 잰 것과 같아야 한다 (리뷰 B · V1 증분).
func TestSuggestSomeMatchesAll(t *testing.T) {
	docs := []Doc{}
	tags := [][]string{{"search", "index"}, {"search", "hook"}, {"index", "gc"},
		{"hook", "gc"}, {"search", "index", "gc"}, {"lint"}}
	for at := 0; at < 60; at++ {
		one := doc(string(rune('a'+at%26))+string(rune('a'+at/26))+"-id",
			tags[at%len(tags)], []string{"mem", "officina"}[at%2],
			[]string{"2026-08-20", "2026-08-21", "2026-08-22"}[at%3])
		one.Title = "제목 " + string(rune('가'+at%14))
		one.Summary = "요약 handler" + string(rune('a'+at%7)) + " 자리"
		docs = append(docs, one)
	}
	all := SuggestAll(docs, MaxLinks)
	changed := map[string]bool{docs[3].ID: true, docs[17].ID: true}
	some, names, _ := SuggestSome(docs, MaxLinks, changed)
	if len(names) < 2 {
		t.Fatalf("다시 잰 기억이 너무 적다 : %d", len(names))
	}
	seen := map[string]bool{}
	for _, id := range names {
		seen[id] = true
		want, got := all[id], some[id]
		if len(want) != len(got) {
			t.Fatalf("%s : 건수 %d != %d", id, len(want), len(got))
		}
		for at := range want {
			if want[at].ID != got[at].ID || want[at].Score != got[at].Score {
				t.Fatalf("%s : %d번째가 다르다 %s(%.3f) != %s(%.3f)", id, at+1,
					want[at].ID, want[at].Score, got[at].ID, got[at].Score)
			}
		}
	}
	// 다시 안 잰 기억은 바뀐 기억과 통을 안 나눠 가진 것이라, 그 목록에
	// 바뀐 기억이 들어 있으면 안 된다.
	for id, list := range all {
		if seen[id] {
			continue
		}
		for _, one := range list {
			if changed[one.ID] {
				t.Fatalf("%s 가 바뀐 기억 %s 를 가리키는데 다시 안 쟀다", id, one.ID)
			}
		}
	}
}

// v0.4 리뷰 A R5 — `migrate` 가 근거 없는 기억에 일괄로 넣는 자리표시 note 는
// 근거가 아니다. 「둘 다 근거가 없다」를 「같은 근거를 가졌다」로 읽으면
// 20k 이전 저장소에서 15,600건이 한 통에 들어간다.
func TestMigratePlaceholderIsNotEvidence(t *testing.T) {
	target := Doc{ID: "m1", Tags: []string{"a"}, Sources: []string{model.NoteMigrated}}
	others := []Doc{{ID: "m2", Tags: []string{"b"}, Sources: []string{model.NoteMigrated}}}
	if found := Suggest(target, others, MaxLinks); len(found) != 0 {
		t.Fatalf("자리표시 근거만 같은데 후보가 됐다: %+v", found)
	}
}

// 사람이 적은 진짜 note 근거는 그대로 센다 — 자리표시만 뺀 것이지
// `note:` 를 통째로 버린 것이 아니다.
func TestRealNoteSourceStillCounts(t *testing.T) {
	target := Doc{ID: "m1", Tags: []string{"a"}, Sources: []string{"note:아트회의2026-08-01"}}
	others := []Doc{{ID: "m2", Tags: []string{"b"}, Sources: []string{"note:아트회의2026-08-01"}}}
	if found := Suggest(target, others, MaxLinks); len(found) != 1 {
		t.Fatalf("사람이 적은 같은 note 근거가 후보가 아니다: %+v", found)
	}
}

// 자리표시가 통 열쇠도 아니라야 한다 — 통이 상한을 넘으면 통째로 건너뛰어
// 그 안의 진짜 짝까지 같이 죽는다 (20k 이전에서 15,600건짜리 통).
func TestPlaceholderMakesNoBucket(t *testing.T) {
	docs := []Doc{}
	for at := 0; at <= BlockMax(); at++ {
		docs = append(docs, Doc{ID: fmt.Sprintf("m%04d", at), Sources: []string{model.NoteMigrated}})
	}
	_, blocked := SuggestAllStats(docs, MaxLinks)
	if blocked.Skipped != 0 || blocked.Biggest != 0 {
		t.Fatalf("자리표시로 통이 생겼다 : 건너뛴 통 %d · 가장 큰 통 %d", blocked.Skipped, blocked.Biggest)
	}
}
