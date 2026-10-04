package quality

import (
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// R1 lint 속도 판. 두 고삐 모두 **답을 한 자리도 안 바꿔야** 한다 — 여기서 못 박는다.

// r1Table 은 씨앗 기억을 늘려 만든 표와 그 문서들이다. 태그가 많이 겹쳐
// 상한(PerDoc)을 넘는 후보가 흔하다.
func r1Table(t *testing.T, count int) (*Similar, []*Doc, []*model.Memory) {
	t.Helper()
	_, seed := loadForTune(t)
	memories := blowUp(seed, count)
	table := NewSimilar(DefaultHamming)
	docs := make([]*Doc, len(memories))
	for at, m := range memories {
		docs[at] = NewDoc(m)
		table.Add(docs[at])
	}
	return table, docs, memories
}

// 싼 상한이 「못 이긴다」고 한 후보는 정말로 꼭대기 뒤여야 한다. 그리고 실제로
// 거르는 일이 있어야 고삐로서 뜻이 있다.
func TestSurelyBehindNeverDropsWinner(t *testing.T) {
	table, docs, memories := r1Table(t, 1500)
	dice := rand.New(rand.NewSource(11))
	dropped := 0
	for round := 0; round < 4000; round++ {
		doc := docs[dice.Intn(len(docs))]
		if dice.Intn(4) == 0 {
			// 표 밖 기억(태그 번호가 없는 쪽)도 섞는다.
			doc = NewDoc(memories[dice.Intn(len(memories))])
		}
		one := table.idsFor(doc)
		left, right := dice.Intn(len(docs)), dice.Intn(len(docs))
		top := rankedOf(docs[left], doc, left, dice.Intn(2) == 0, one)
		sketched := dice.Intn(2) == 0
		if !surelyBehind(top, table.bounds[right], sketched, one, doc.Fingerprint) {
			continue
		}
		dropped++
		actual := rankedOf(docs[right], doc, right, sketched, one)
		if !rankBefore(top, actual) {
			t.Fatalf("%s 를 못 이긴다고 했는데 이긴다 : top %+v · 후보 %+v", doc.ID, top, actual)
		}
	}
	if dropped == 0 {
		t.Fatal("한 번도 안 걸렀다 — 상한이 아무 일도 안 한다")
	}
}

// 상한을 넣은 topRanked 는 「다 재서 정렬한 앞 limit 개」와 같아야 한다.
func TestTopRankedMatchesFullSortOnStore(t *testing.T) {
	table, docs, memories := r1Table(t, 1500)
	session := table.Session()
	dice := rand.New(rand.NewSource(5))
	for round := 0; round < 300; round++ {
		doc := docs[dice.Intn(len(docs))]
		if dice.Intn(4) == 0 {
			doc = NewDoc(memories[dice.Intn(len(memories))])
		}
		session.round++
		found := dice.Perm(len(docs))[:dice.Intn(800)+1]
		for _, at := range found {
			if dice.Intn(10) == 0 {
				session.sketchStamp[at] = session.round
			}
		}
		limit := dice.Intn(60) + 1
		got := topRanked(found, doc, session, limit)

		one := table.idsFor(doc)
		all := make([]ranked, 0, len(found))
		for _, at := range found {
			all = append(all, rankedOf(docs[at], doc, at, session.sketchStamp[at] == session.round, one))
		}
		sort.SliceStable(all, func(a, b int) bool { return rankBefore(all[a], all[b]) })
		if limit < len(all) {
			all = all[:limit]
		}
		want := make([]int, 0, len(all))
		for _, item := range all {
			want = append(want, item.at)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%d 회차 고른 것이 다르다 :\n얻음 %v\n바람 %v", round, got, want)
		}
	}
}

// checkRepoSerial 은 겹쳐 돌리기 전의 CheckRepo 다 — 저장소 규칙을 다 돈 뒤에
// 중복 판정을 돈다. 견줄 기준으로만 쓴다.
func checkRepoSerial(memories []*model.Memory, opt RepoOptions) RepoReport {
	opt.Options = opt.Options.normalized()
	report := RepoReport{ByID: map[string][]Finding{}}
	report.repoRules(memories, opt)
	report.duplicates(memories, opt)
	return report
}

// 저장소 규칙과 중복 판정을 겹쳐 돌려도 결과(건 · 차례)가 한 갈래 판과 같아야 한다.
func TestCheckRepoOverlapMatchesSerial(t *testing.T) {
	_, seed := loadForTune(t)
	memories := blowUp(seed, 2000)
	opt := RepoOptions{Options: Options{Config: config.Default("scale"),
		Vocab: testdataVocab(t), Now: testNow()}, Hits: map[string]time.Time{}}
	want := checkRepoSerial(memories, opt)
	for round := 0; round < 3; round++ {
		got := CheckRepo(memories, opt)
		if !reflect.DeepEqual(got.Wide, want.Wide) {
			t.Fatalf("%d 회차 Wide 가 다르다", round)
		}
		if len(got.ByID) != len(want.ByID) {
			t.Fatalf("%d 회차 걸린 기억 수가 다르다 : %d ≠ %d", round, len(got.ByID), len(want.ByID))
		}
		for id, list := range want.ByID {
			if !reflect.DeepEqual(got.ByID[id], list) {
				t.Fatalf("%d 회차 %s 의 결과가 다르다 :\n얻음 %+v\n바람 %+v", round, id, got.ByID[id], list)
			}
		}
	}
	// 두 무리가 다 무언가를 내야 견준 뜻이 있다.
	rules := map[string]bool{}
	for _, list := range want.ByID {
		for _, one := range list {
			rules[one.Rule] = true
		}
	}
	if len(rules) < 3 {
		t.Fatalf("걸린 규칙이 너무 적어 견준 뜻이 없다 : %v", rules)
	}
}
