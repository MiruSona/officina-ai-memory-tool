package search

import (
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
	"github.com/mirusona/officina-ai-memory-tool/internal/token"
)

// 표기 차이 세 건을 못 박는다 (설계 결정 21·23 · 조사 E 가 지목한 자리).
// 저장소는 대표말(`색인`·`기억`·`기억 고치기`)로만 적혀 있고, 물어보는 쪽은
// 다른 말(`인덱싱`·`메모리`·`기억 수정`)로 친다. `[canon]` 표를 꽂으면 셋 다
// **strict 칸에서** 5줄 안에 나와야 한다.
var canonTable = map[string]string{
	"인덱싱":   "색인",
	"메모리":   "기억",
	"기억 수정": "기억 고치기",
}

var canonCorpus = []seed{
	{summary: "색인은 한글을 바이그램으로 쪼개 담는다는 결정과 그렇게 고른 까닭을 적은 기억",
		body: "기억 저장소의 색인은 바이그램으로 담는다. 여기에 인덱스라는 말은 안 쓴다.",
		tags: []string{"index"}, kind: model.TypeDecision},
	{summary: "기억 고치기는 덮어쓰지 않고 새 기억을 쓴 뒤 옛것을 무효로 표시한다는 결정 하나",
		body: "기억 고치기 는 파일을 덮어쓰지 않는다. 무효 표시만 남긴다.",
		tags: []string{"store"}, kind: model.TypeDecision},
}

func newCanonRepo(t *testing.T) (*index.DB, []string) {
	t.Helper()
	opened := store.Open(t.TempDir(), false)
	if err := opened.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, item := range canonCorpus {
		ids = append(ids, writeSeed(t, opened, item))
	}
	settings := config.Default("시험")
	if _, err := index.Run(index.Options{Store: opened, GC: settings.GC,
		Secret: settings.Secret, Quiet: true}); err != nil {
		t.Fatal(err)
	}
	database, err := index.Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database, ids
}

// useCanon 은 색인·질의가 같이 탈 정규화기를 꽂는다. 시험이 끝나면 되돌린다.
func useCanon(t *testing.T, table map[string]string) {
	t.Helper()
	settings := config.Default("시험")
	settings.Canon = table
	canon, err := settings.CanonTable()
	if err != nil {
		t.Fatal(err)
	}
	restore := index.Normalize
	index.SetNormalize(func(text string) string { return token.Normalize(text, canon) })
	t.Cleanup(func() { index.SetNormalize(restore) })
}

func TestCanonNotationFound(t *testing.T) {
	useCanon(t, canonTable)
	database, ids := newCanonRepo(t)
	cases := []struct {
		query string
		want  int
	}{
		{"인덱싱", 0},
		{"메모리 색인", 0},
		{"기억 수정", 1},
	}
	for _, item := range cases {
		result := ask(t, database, item.query, 5)
		place := rankOfID(result, ids[item.want])
		if place == 0 {
			t.Fatalf("%q 로 대표말 기억을 5줄 안에서 못 찾았다 : %d건", item.query, result.Total)
		}
		if !Strict(result.Hits[place-1].Rung) {
			t.Fatalf("%q 가 완화 칸(%d)에서만 나왔다 — strict 자에 안 잡힌다",
				item.query, result.Hits[place-1].Rung)
		}
	}
}

// 표가 비면 정규화는 항등이고 아무것도 안 거절한다 (결정 25). 저장소에 아예
// 없는 말인 `인덱싱` 은 그때 여전히 0건이다 — 위 시험이 무엇 덕에 통과하는지
// 못 박는 짝이다. (`메모리 색인`·`기억 수정` 은 낱말 하나가 저장소에 있어서
// 표 없이도 무언가는 나오므로 이 짝 시험에 안 넣는다.)
func TestCanonEmptyStillMisses(t *testing.T) {
	useCanon(t, map[string]string{})
	database, ids := newCanonRepo(t)
	result := ask(t, database, "인덱싱", 5)
	if rankOfID(result, ids[0]) != 0 {
		t.Fatal("`인덱싱` 이 표 없이도 맞았다 — 이 시험이 재는 것이 [canon] 이 아니다")
	}
}

// 모드 표시는 임베딩이 실제로 돌았을 때만 바뀐다 (결정 16).
func TestModeIsWordWithoutVectors(t *testing.T) {
	database, _ := newRepo(t)
	result := ask(t, database, "색인", 5)
	if result.Mode != ModeWord {
		t.Fatalf("모델이 없는데 모드가 %q 다", result.Mode)
	}
	if want := "[낱말] "; !strings.HasPrefix(Markdown(result, 0), want) {
		t.Fatalf("결과 머리에 %q 가 없다 :\n%s", want, Markdown(result, 0))
	}
}
