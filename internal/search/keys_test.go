package search

import (
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// keys 칸(C1) 시험이다. 「글에 없는 다른 말」 로 찾히는지, 기본(나)에서는 그 말만으로
// 엄격 칸 답이 안 되는지, keys 가 하나도 없는 저장소는 랭킹이 안 늘어나는지를 본다.

const keysSummary = "색인 파일을 통째로 다시 만드는 절차 — 판이 바뀌면 index.db 를 지우고 처음부터 쌓는다"

// newKeysRepo 는 시험 말뭉치에 keys 를 단 기억 하나를 더한 저장소다.
func newKeysRepo(t *testing.T) (*index.DB, string) {
	t.Helper()
	opened := store.Open(t.TempDir(), false)
	if err := opened.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	for _, item := range corpus {
		writeSeed(t, opened, item)
	}
	body := "판이 바뀐 색인은 옮길 것이 없다.\n지우고 다시 쌓는 편이 싸고 안전하다.\n기억 파일(md)은 그대로 남는다."
	request := store.AddRequest{Op: store.OpAdd, Type: model.TypeHowto, Date: seedDate(),
		Summary: keysSummary, Tags: []string{"index"}, Source: model.LegacySourceAI, Scope: "mem-search",
		Body: body, Keys: []string{"reindex", "재빌드절차"}}
	name, err := opened.WriteAdd(request)
	if err != nil {
		t.Fatal(err)
	}
	settings := config.Default("시험")
	if _, err := index.Run(index.Options{Store: opened, GC: settings.GC, Secret: settings.Secret,
		Quiet: true, NoLink: true}); err != nil {
		t.Fatal(err)
	}
	database, err := index.Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database, model.QueueID(name, body, seedDate())
}

func askKeys(t *testing.T, database *index.DB, query string, strict bool) *Result {
	t.Helper()
	result, err := Search(Options{Sources: []Source{{DB: database}}, Query: query, Limit: 5,
		Stopwords: config.DefaultStopwords(), Search: config.SearchConfig{KeysStrict: strict}})
	if err != nil {
		t.Fatalf("검색이 죽었다 : %v", err)
	}
	return result
}

func keysHitOf(result *Result, id string) *Hit {
	for at := range result.Hits {
		if result.Hits[at].ID == id {
			return &result.Hits[at]
		}
	}
	return nil
}

// 글에 없는 말(keys 에만 있는 말)로도 찾힌다. 기본(나)은 느슨한 칸 답이고,
// keys_strict(가)면 엄격 칸 답이다.
func TestKeysFindByOtherWord(t *testing.T) {
	database, id := newKeysRepo(t)
	for _, query := range []string{"reindex", "재빌드절차"} {
		loose := keysHitOf(askKeys(t, database, query, false), id)
		if loose == nil {
			t.Fatalf("%q 로 keys 기억을 못 찾았다", query)
		}
		if Strict(loose.Rung) {
			t.Fatalf("(나)인데 keys 로만 맞은 기억이 엄격 칸(%d)에 앉았다", loose.Rung)
		}
		strict := keysHitOf(askKeys(t, database, query, true), id)
		if strict == nil || !Strict(strict.Rung) {
			t.Fatalf("(가)인데 %q 가 엄격 칸 답이 아니다 : %+v", query, strict)
		}
	}
}

// keys 는 글에도 맞은 기억의 점수만 얹는다 — 들이는 랭킹이 없으면 boost 만으로는
// 후보가 하나도 안 생긴다 (결정 44 와 같은 자리).
func TestKeysBoostNeverAdmits(t *testing.T) {
	database, _ := newKeysRepo(t)
	helper := &rungHelper{db: database}
	only := []ranking{{name: "K:R1", table: "keys_ko", expr: `"reindex"`, weight: 1, boost: true}}
	found, err := runRung(helper, only, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(found.scores) != 0 {
		t.Fatalf("boost 랭킹이 후보를 들였다 : %v", found.scores)
	}
}

// A1 뒷정리 — 덮인 기억은 pinned 가 남아도 고정 가산을 안 받는다 (`--all` 로 볼 때).
func TestSupersededPinnedGetsNoPinBonus(t *testing.T) {
	shared := rules{Cap: 10, Now: time.Now()}
	live := index.SearchRow{ID: "a", Type: model.TypeHistory, Pinned: true}
	dead := live
	dead.SupersededBy = "20260901-aa11bb22"
	liveBonus, liveWhy := bonusOf(live, shared)
	deadBonus, deadWhy := bonusOf(dead, shared)
	if liveBonus <= deadBonus || len(liveWhy) == len(deadWhy) {
		t.Fatalf("덮인 고정 기억이 고정 가산을 받았다 : %v %v / %v %v", liveBonus, liveWhy, deadBonus, deadWhy)
	}
}

// keys 를 단 기억이 없는 저장소는 keys 랭킹을 하나도 안 붙인다 — 그래야 C1 전과
// 결과가 똑같다.
func TestKeysNoopWithoutKeys(t *testing.T) {
	database, _ := newRepo(t)
	helper := &rungHelper{db: database}
	ranks := []ranking{{name: "R1", table: "fts_ko", expr: `"색인"`, weight: 1}}
	if got := withKeys(RungPlain, ranks, helper); len(got) != len(ranks) {
		t.Fatalf("keys 없는 저장소에 keys 랭킹이 붙었다 : %d", len(got))
	}
	keyed, _ := newKeysRepo(t)
	helper = &rungHelper{db: keyed}
	got := withKeys(RungPlain, ranks, helper)
	if len(got) != 2 || got[1].table != "keys_ko" || !got[1].boost || got[1].weight != weightKeys {
		t.Fatalf("keys 쌍둥이가 틀렸다 : %+v", got)
	}
	// 느슨한 칸(OR)에서는 (나)라도 keys 가 후보를 들인다.
	if got := withKeys(RungOr, ranks, helper); got[1].boost {
		t.Fatal("느슨한 칸에서 keys 가 boost 로만 돌았다")
	}
}
