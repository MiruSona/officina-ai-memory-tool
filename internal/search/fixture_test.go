package search

import (
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// seed 는 시험 저장소에 넣을 기억 하나다. 머리말 규칙(요약 30~120자)을 지킨다.
type seed struct {
	summary  string
	body     string
	tags     []string
	kind     string
	pinned   bool
	severity string
}

// 시험용 말뭉치. 실제 저장소에서 문제가 됐던 다섯 갈래를 다 담았다 (설계 12-3).
var corpus = []seed{
	{summary: "한글 두 글자 검색 회귀 — 공수 리듬 파형 박자 음색 다섯 낱말이 다 잡혀야 한다는 규칙",
		body: "공수 기록은 리듬 과 파형 과 박자 와 음색 처럼 두 글자 낱말로 남긴다.", tags: []string{"token", "korean"}, kind: model.TypeDecision},
	{summary: "영문 낱말 색인 회귀 — Rider 와 dotnet-format 과 MyClassName 이 다 잡히는지 보는 기억",
		body: "IDE(Rider/VS)와 dotnet-format 을 쓴다. MyClassName 규칙도 여기 있다.", tags: []string{"csharp", "tool"}, kind: model.TypeHowto},
	{summary: "기억 저장소를 프로젝트 하나와 기계마다 전역 하나로 나눈 이유와 무엇을 어디에 넣나",
		body: "기억 저장소 는 둘이다. 붙여 쓴 꼴을 찾을 때도 나와야 한다.", tags: []string{"storage"}, kind: model.TypeDecision},
	{summary: "바이그램 검색 방식을 골랐다 — 한글은 글자 바이그램, 영문은 낱말 색인으로 나눠 담는다",
		body: "바이그램 검색 은 두 글자 낱말에 강하다.", tags: []string{"search"}, kind: model.TypeDecision},
	{summary: "실전시험 규모별 측정 표 — 200 과 1,000 과 5,000건 에서 색인 시간을 다 쟀다는 기록",
		body: "5,000건 에서 색인이 느려졌다. 숫자 서식이 달라도 찾혀야 한다.", tags: []string{"measure"}, kind: model.TypeHistory},
	{summary: "훅 주입 예산을 1200 토큰으로 잡은 근거와 그 숫자가 어디서 나왔는지 적어 둔 결정",
		body: "예산 은 토큰 수로 잡는다.", tags: []string{"hook", "budget"}, kind: model.TypeDecision},
	{summary: "관련도가 0 인데도 고정된 결정이 검색 1위를 먹는 문제 — 가산에 고삐를 채워야 한다",
		body: "여기에는 아무 상관 없는 말만 있다. 고정 이라는 이유로 위에 오면 안 된다.",
		tags: []string{"score"}, kind: model.TypeDecision, pinned: true, severity: ""},
	{summary: "훅 성능 측정 — 훅 한 번이 벽시계로 얼마나 걸리는지 20k 저장소에서 재 본 기록 하나",
		body: "훅 성능 은 색인을 몇 번 여느냐로 갈린다.", tags: []string{"hook", "measure"}, kind: model.TypeHistory},
	{summary: "검색 성능이 느려지는 자리는 trigram 색인이었다는 것을 규모별로 재서 알아낸 기록",
		body: "성능 이 나빠지는 자리는 색인 쓰기다.", tags: []string{"search", "measure"}, kind: model.TypeHistory},
	{summary: "색인 성능을 위해 문서마다 조각 계산을 한 번만 하도록 고쳤다는 지난 판의 기록 하나",
		body: "같은 문서에 두 번 부르면 성능 을 버린다.", tags: []string{"index"}, kind: model.TypeHistory},
	{summary: "lint 가 결정 충돌을 오류로 내는 문제를 경고로 낮춘다는 지금 살아 있는 판정 하나",
		body: "결정 충돌 은 경고다. 오류로 내지 않는다.", tags: []string{"lint"}, kind: model.TypeIssue, severity: model.SeverityHigh},
}

// newRepo 는 시험 저장소를 만들고 색인까지 마친 뒤 핸들을 준다.
func newRepo(t *testing.T) (*index.DB, []string) {
	t.Helper()
	opened := store.Open(t.TempDir(), false)
	if err := opened.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, item := range corpus {
		ids = append(ids, writeSeed(t, opened, item))
	}
	settings := config.Default("시험")
	// 이웃 자동 링크는 끈다. 11건짜리 시험 말뭉치는 전부가 같은 scope 라
	// 기계가 거의 모두를 잇고, 그러면 랭킹 시험이 링크 가산만 재게 된다.
	// 1-hop·MMR·묶음은 hop_test.go 가 따로 잰다.
	if _, err := index.Run(index.Options{Store: opened, GC: settings.GC, Secret: settings.Secret,
		Quiet: true, NoLink: true}); err != nil {
		t.Fatal(err)
	}
	database, err := index.Open(opened.Dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database, ids
}

// seedDate 는 씨앗 기억의 날짜다. **오늘에서 30일 전**이다.
//
// 날짜를 글자로 박아 두면(`2026-08-22`) 달력이 넘어가는 날 갑자기 깨진다 —
// 「지난주」 질의를 재는 시험 둘이 2026-08-24 로 넘어가면서 실제로 깨졌다
// (리뷰 B). 그 둘은 **씨앗이 「지난주」 밖에 있어야** 뜻이 서는 시험이라
// (기간이 비면 풀고 다시 찾는다 · 시간 낱말만 준 질의는 저장소 전체가 아니다)
// 어느 요일에 돌려도 밖이 되는 30일 전으로 잡는다. 감쇠 반감기(90일 이상)보다
// 훨씬 짧아 순위 시험에는 값이 안 바뀐다.
func seedDate() string { return time.Now().AddDate(0, 0, -30).Format(model.DayLayout) }

func writeSeed(t *testing.T, opened *store.Store, item seed) string {
	t.Helper()
	request := store.AddRequest{Op: store.OpAdd, Type: item.kind, Date: seedDate(),
		Summary: item.summary, Tags: item.tags, Source: model.LegacySourceAI, Scope: "mem-search",
		Body: item.body, Pinned: item.pinned, Severity: item.severity}
	if item.kind == model.TypeIssue || item.kind == model.TypeCaution {
		request.Severity = model.SeverityHigh
	}
	name, err := opened.WriteAdd(request)
	if err != nil {
		t.Fatal(err)
	}
	return model.QueueID(name, item.body, seedDate())
}

// ask 는 시험이 검색 한 번을 부르는 짧은 길이다.
func ask(t *testing.T, database *index.DB, query string, limit int) *Result {
	t.Helper()
	result, err := Search(Options{
		Sources:   []Source{{DB: database}},
		Query:     query,
		Limit:     limit,
		Stopwords: config.DefaultStopwords(),
		Synonym:   map[string][]string{"기억": {"memory"}, "훅": {"hook"}, "색인": {"index"}},
	})
	if err != nil {
		t.Fatalf("검색이 죽었다 : %v", err)
	}
	return result
}

func hasID(result *Result, id string) bool {
	for _, hit := range result.Hits {
		if hit.ID == id {
			return true
		}
	}
	return false
}

func rankOfID(result *Result, id string) int {
	for place, hit := range result.Hits {
		if hit.ID == id {
			return place + 1
		}
	}
	return 0
}
