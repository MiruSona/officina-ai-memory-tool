package review

// review --nli 의 C13 · C20 · obs 자리 시험이다 (적용 설계 3절 · 5절 · 시험 #11~#13 · #16).
// NLI 는 httptest 가짜 서버이고 주장 글로 답을 고른다.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
)

const (
	answerB = `{"a":0.03,"b":0.94,"c":0.03,"model":"fake0001","ms":2.0}`
	answerA = `{"a":0.95,"b":0.02,"c":0.03,"model":"fake0001","ms":2.0}`
	answerC = `{"a":0.02,"b":0.03,"c":0.95,"model":"fake0001","ms":2.0}`
)

// fakeNLIBy 는 주장(claim)에 든 낱말로 답을 고르는 가짜 서버다. 맞는 낱말이 없으면 무관이다.
// 물은 주장들을 차례로 모은다.
func fakeNLIBy(t *testing.T, answers map[string]string) (*httptest.Server, *atomic.Int32, *[]string) {
	t.Helper()
	asked := &atomic.Int32{}
	claims := &[]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			io.WriteString(w, `{"ok":true}`)
			return
		}
		var request struct{ Evidence, Claim string }
		_ = json.NewDecoder(r.Body).Decode(&request)
		asked.Add(1)
		*claims = append(*claims, request.Claim)
		for word, body := range answers {
			if strings.Contains(request.Claim, word) {
				io.WriteString(w, body)
				return
			}
		}
		io.WriteString(w, answerC)
	}))
	t.Cleanup(server.Close)
	return server, asked, claims
}

func memoryOf(id, kind, summary string) *model.Memory {
	m := pairMemory(id, kind, "aimemorytool")
	m.Summary = summary
	return m
}

// sieveRun 은 Run 의 NLI 걸음(규칙 층 → 몫 나눠 묻기 → 한 짝 한 큐 → 큐 채우기)만 돈다.
func sieveRun(found *quality.RepoReport, byID map[string]*model.Memory, nli *NLIOptions, kinds ...string) *Report {
	report := &Report{Counts: map[string]int{}, ByScope: map[string]map[string]int{}, Items: []Item{}}
	options := Options{Now: now, NLI: nli, Kinds: kinds, Limit: DefaultLimit}
	report.addContradicts(found, byID, options)
	report.sieveNear(found, byID, options)
	dropShadowed(found.ByID)
	report.fill(*found, byID, options)
	return report
}

func addFinding(found *quality.RepoReport, rule, id, other, reason string) {
	found.ByID[id] = append(found.ByID[id], quality.Finding{Rule: rule, Level: quality.GradeCandidate, ID: id,
		Reason: reason, Related: []string{other}})
}

func placeOf(report *Report, name string) NLIPlace {
	for _, place := range report.NLI.Places {
		if place.Name == name {
			return place
		}
	}
	return NLIPlace{}
}

// #11 merge 짝이 확신 반대면 contradict 큐에 오르고 merge 줄은 지워진다.
func TestSieveMergeContradictMovesToContradict(t *testing.T) {
	server, asked, _ := fakeNLIBy(t, map[string]string{"끈다": answerB})
	found := quality.RepoReport{ByID: map[string][]quality.Finding{}}
	byID := map[string]*model.Memory{
		"20260101-aaaa0001": memoryOf("20260101-aaaa0001", model.TypeDecision, "색인 캐시를 켠다"),
		"20260102-aaaa0002": memoryOf("20260102-aaaa0002", model.TypeDecision, "색인 캐시를 끈다"),
	}
	addFinding(&found, quality.RuleMergeCandidate, "20260101-aaaa0001", "20260102-aaaa0002", "닮았다")
	report := sieveRun(&found, byID, nliOptions(t, server.URL, 0), KindContradict, KindMerge)
	if asked.Load() != 1 || len(report.Items) != 1 || report.Items[0].Kind != KindContradict ||
		!strings.Contains(report.Items[0].Why, "merge 짝에서") {
		t.Fatalf("merge 반대가 contradict 로 안 올랐다 : %d · %+v", asked.Load(), report.Items)
	}
	// C19 가 안 보는 종류(history)는 반대여도 contradict 로 안 올리고 merge 줄을 그대로 둔다.
	found = quality.RepoReport{ByID: map[string][]quality.Finding{}}
	for _, one := range byID {
		one.Type = model.TypeHistory
	}
	addFinding(&found, quality.RuleMergeCandidate, "20260101-aaaa0001", "20260102-aaaa0002", "닮았다")
	report = sieveRun(&found, byID, nliOptions(t, server.URL, 0), KindContradict, KindMerge)
	if len(report.Items) != 1 || report.Items[0].Kind != KindMerge {
		t.Fatalf("history 짝이 contradict 로 올랐다 : %+v", report.Items)
	}
}

// #11 merge 짝이 확신 무관이면 줄은 남고 큐 맨 뒤로 간다.
func TestSieveMergeUnrelatedGoesBack(t *testing.T) {
	server, _, _ := fakeNLIBy(t, map[string]string{"둘째": answerA})
	found := quality.RepoReport{ByID: map[string][]quality.Finding{}}
	byID := map[string]*model.Memory{
		"20260101-aaaa0001": memoryOf("20260101-aaaa0001", model.TypeHistory, "첫 기록"),
		"20260102-aaaa0002": memoryOf("20260102-aaaa0002", model.TypeHistory, "첫 기록 그 뒤"),
		"20260103-bbbb0001": memoryOf("20260103-bbbb0001", model.TypeHistory, "다른 기록"),
		"20260104-bbbb0002": memoryOf("20260104-bbbb0002", model.TypeHistory, "다른 기록 둘째"),
	}
	addFinding(&found, quality.RuleMergeCandidate, "20260101-aaaa0001", "20260102-aaaa0002", "닮았다")
	addFinding(&found, quality.RuleMergeCandidate, "20260103-bbbb0001", "20260104-bbbb0002", "닮았다")
	report := sieveRun(&found, byID, nliOptions(t, server.URL, 0), KindMerge)
	if len(report.Items) != 2 || report.Items[1].ID != "20260101-aaaa0001" ||
		!strings.HasPrefix(report.Items[1].Why, "NLI 무관 0.95") {
		t.Fatalf("무관 merge 줄이 맨 뒤로 안 갔다 : %+v", report.Items)
	}
	if strings.Contains(report.Items[0].Why, "NLI") {
		t.Fatalf("지지 짝의 까닭은 그대로다 : %s", report.Items[0].Why)
	}
}

// #11 C13 짝은 까닭에 NLI 답이 붙는다 — 반대는 큐 앞, 지지는 「숫자 자리」, 무관은 큐 뒤.
func TestSieveStaleConflictReason(t *testing.T) {
	server, _, _ := fakeNLIBy(t, map[string]string{"반대쪽": answerB, "지지쪽": answerA})
	found := quality.RepoReport{ByID: map[string][]quality.Finding{}}
	byID := map[string]*model.Memory{}
	for _, row := range [][3]string{
		{"20260101-aaaa0001", "20260102-aaaa0002", "무관쪽"},
		{"20260103-bbbb0001", "20260104-bbbb0002", "반대쪽"},
		{"20260105-cccc0001", "20260106-cccc0002", "지지쪽"},
	} {
		byID[row[0]] = memoryOf(row[0], model.TypeFact, "옛 값은 3 이다")
		byID[row[1]] = memoryOf(row[1], model.TypeFact, row[2]+" 값은 5 이다")
		addFinding(&found, quality.RuleStaleConflictPair, row[0], row[1], "숫자가 어긋난다")
		addFinding(&found, quality.RuleStaleConflictPair, row[1], row[0], "숫자가 어긋난다")
	}
	report := sieveRun(&found, byID, nliOptions(t, server.URL, 0), KindContradict)
	if len(report.Items) != 6 {
		t.Fatalf("C13 줄 여섯이 다 남아야 한다 : %+v", report.Items)
	}
	first, last := report.Items[0], report.Items[len(report.Items)-1]
	if !strings.HasPrefix(first.Why, "NLI 반대 0.94") || !strings.HasPrefix(last.Why, "NLI 무관 0.95") {
		t.Fatalf("반대는 앞 · 무관은 뒤여야 한다 : %q … %q", first.Why, last.Why)
	}
	if !strings.Contains(found.ByID["20260105-cccc0001"][0].Reason, "숫자 자리가 다른 것일 수 있다") {
		t.Fatalf("지지 까닭이 없다 : %+v", found.ByID["20260105-cccc0001"])
	}
}

// #12 몫 나누기 — C19 가 몫을 다 못 쓰면 남은 몫이 C13 으로 간다. 뒤 자리가 안 쓰는 몫은 앞 자리가 받는다.
func TestAllocateShares(t *testing.T) {
	cases := []struct {
		total        int
		shares, need []int
		want         []int
	}{
		{100, sharesWithoutObs, []int{10, 60, 0}, []int{10, 60, 0}},
		{100, sharesWithoutObs, []int{80, 80, 80}, []int{50, 25, 25}},
		{3, sharesWithoutObs, []int{4, 0, 0}, []int{3, 0, 0}},
		{100, sharesWithObs, []int{90, 5, 5, 90}, []int{75, 5, 5, 15}},
		{10, sharesWithObs, []int{0, 0, 0, 30}, []int{0, 0, 0, 10}},
	}
	for _, one := range cases {
		if got := allocate(one.total, one.shares, one.need); !reflect.DeepEqual(got, one.want) {
			t.Errorf("allocate(%d, %v, %v) = %v, 바란 것 %v", one.total, one.shares, one.need, got, one.want)
		}
	}
}

// #12 몫 넘김과 scope 상한 공유 — 실제로 물은 수가 자리별로 찍힌다. 차례는 (작은 id, 큰 id) 로 고정이다.
func TestSieveQuotaAndScopeCap(t *testing.T) {
	server, asked, claims := fakeNLIBy(t, nil)
	found, byID := pairsFixture(2, plainLeft, plainRight)
	for _, id := range []string{"a", "b", "c", "d", "e", "f"} {
		older, newer := "20260201-0000"+id+"001", "20260202-0000"+id+"002"
		byID[older] = memoryOf(older, model.TypeFact, "옛 값 "+id)
		byID[newer] = memoryOf(newer, model.TypeFact, "새 값 "+id)
		addFinding(&found, quality.RuleStaleConflictPair, older, newer, "숫자가 어긋난다")
	}
	report := sieveRun(&found, byID, nliOptions(t, server.URL, 6), KindContradict, KindMerge)
	c19, c13 := placeOf(report, PlaceC19), placeOf(report, PlaceC13)
	if c19.Quota != 2 || c19.Pairs != 2 || c13.Quota != 4 || c13.Pairs != 4 || c13.Skipped != 2 || asked.Load() != 6 {
		t.Fatalf("몫이 틀렸다 : C19 %+v · C13 %+v · 물음 %d", c19, c13, asked.Load())
	}
	if (*claims)[2] != "새 값 a" || (*claims)[5] != "새 값 d" {
		t.Fatalf("C13 차례가 id 순이 아니다 : %v", *claims)
	}
	if !hasNote(report, "NLI 자리별") || !hasNote(report, "C13 4/2/4") {
		t.Fatalf("자리별 셈 줄이 없다 : %v", report.Notes)
	}
	// scope 당 상한은 세 자리가 함께 쓴다.
	server, asked, _ = fakeNLIBy(t, nil)
	found, byID = pairsFixture(2, plainLeft, plainRight)
	byID["20260201-0000a001"] = memoryOf("20260201-0000a001", model.TypeFact, "옛 값")
	byID["20260202-0000a002"] = memoryOf("20260202-0000a002", model.TypeFact, "새 값")
	addFinding(&found, quality.RuleStaleConflictPair, "20260201-0000a001", "20260202-0000a002", "숫자")
	options := nliOptions(t, server.URL, 0)
	options.PerScope = 2
	sieveRun(&found, byID, options, KindContradict)
	if asked.Load() != 2 {
		t.Fatalf("scope 당 상한 2 를 세 자리가 함께 써야 한다 : %d번", asked.Load())
	}
}

// #13 덮였거나 무효이거나 접힌(fold) 기억의 짝은 묻지 않는다.
func TestSieveSkipsRetiredAndFolded(t *testing.T) {
	server, asked, _ := fakeNLIBy(t, nil)
	found := quality.RepoReport{ByID: map[string][]quality.Finding{}}
	byID := map[string]*model.Memory{}
	for _, id := range []string{"a", "b", "c"} {
		older, newer := "20260201-0000"+id+"001", "20260202-0000"+id+"002"
		byID[older] = memoryOf(older, model.TypeHistory, "옛 기록 "+id)
		byID[newer] = memoryOf(newer, model.TypeHistory, "새 기록 "+id)
		addFinding(&found, quality.RuleMergeCandidate, older, newer, "닮았다")
		addFinding(&found, quality.RuleStaleConflictPair, older, newer, "숫자")
	}
	byID["20260202-0000a002"].SupersededBy = "20260301-00000001"
	byID["20260202-0000b002"].InvalidAt = "2026-01-01"
	byID["20260202-0000c002"].State = index.StateCold
	sieveRun(&found, byID, nliOptions(t, server.URL, 0))
	if asked.Load() != 0 {
		t.Fatalf("죽은·접힌 기억 짝을 %d번 물었다", asked.Load())
	}
}

// #13 `--nli` 가 없으면 걸림도 순서도 그대로다 (sieveNear 는 아무것도 안 한다).
func TestSieveWithoutNLIChangesNothing(t *testing.T) {
	found := quality.RepoReport{ByID: map[string][]quality.Finding{}}
	byID := map[string]*model.Memory{
		"20260101-aaaa0001": memoryOf("20260101-aaaa0001", model.TypeHistory, "첫 기록"),
		"20260102-aaaa0002": memoryOf("20260102-aaaa0002", model.TypeHistory, "둘째 기록"),
	}
	addFinding(&found, quality.RuleMergeCandidate, "20260101-aaaa0001", "20260102-aaaa0002", "닮았다")
	addFinding(&found, quality.RuleStaleConflictPair, "20260101-aaaa0001", "20260102-aaaa0002", "숫자")
	before := map[string][]quality.Finding{}
	for id, list := range found.ByID {
		before[id] = append([]quality.Finding{}, list...)
	}
	report := &Report{Counts: map[string]int{}, ByScope: map[string]map[string]int{}}
	report.sieveNear(&found, byID, Options{Now: now})
	if !reflect.DeepEqual(before, found.ByID) || report.order != nil || report.NLI != nil || len(report.Notes) != 0 {
		t.Fatalf("--nli 없이 무엇이 바뀌었다 : %+v · %v", found.ByID, report.Notes)
	}
}

// obsCard 는 모음 기억 하나와 그 인용 기억 둘이다.
func obsCard(body string) map[string]*model.Memory {
	cited := memoryOf("20260101-aaaa0001", model.TypeDecision, "색인은 SQLite 로 만든다")
	cited.Title = "색인 만들기"
	other := memoryOf("20260102-bbbb0002", model.TypeDecision, "훅은 세션 시작에만 붙인다")
	other.Title = "훅 자리"
	card := memoryOf("20260110-cccc0003", model.TypeObservation, "모음")
	card.Body = body
	return map[string]*model.Memory{cited.ID: cited, other.ID: other, card.ID: card}
}

// #16 베낀 줄은 묻지 않고, 고친 줄만 묻는다. 반대면 basis 큐에 경고(거절 아님), 무관은 큐에 안 올린다.
func TestObsCiteAsksOnlyEditedLines(t *testing.T) {
	server, asked, claims := fakeNLIBy(t, map[string]string{"PostgreSQL": answerB})
	body := "1. [예전] 2026-01-01 색인 만들기 — 색인은 SQLite 로 만든다 [mem:20260101-aaaa0001]\n" +
		"2. [지금] 2026-01-02 훅 자리 — 훅은 세션 시작에만 붙인다 [mem:20260102-bbbb0002]\n"
	byID := obsCard(body)
	found := quality.RepoReport{ByID: map[string][]quality.Finding{}}
	sieveRun(&found, byID, nliOptions(t, server.URL, 0), KindBasis)
	if asked.Load() != 0 {
		t.Fatalf("consolidate 가 베낀 줄을 %d번 물었다", asked.Load())
	}
	body = "1. 2026-01-01 색인은 PostgreSQL 로 만든다 [mem:20260101-aaaa0001]\n" +
		"2. 2026-01-02 훅은 세션 시작과 끝에 붙인다 [mem:20260102-bbbb0002]\n"
	byID = obsCard(body)
	found = quality.RepoReport{ByID: map[string][]quality.Finding{}}
	report := sieveRun(&found, byID, nliOptions(t, server.URL, 0), KindBasis)
	if asked.Load() != 2 || (*claims)[0] != "색인은 PostgreSQL 로 만든다" {
		t.Fatalf("고친 줄 둘을 물어야 한다 : %d · %v", asked.Load(), *claims)
	}
	if len(report.Items) != 1 || report.Items[0].Kind != KindBasis || report.Items[0].Rule != RuleObsCiteNLI ||
		!strings.Contains(report.Items[0].Why, "1번 줄이") || found.ByID["20260110-cccc0003"][0].Level != quality.GradeWarn {
		t.Fatalf("반대 줄만 basis 경고로 올라야 한다 : %+v", report.Items)
	}
	// 다음 수는 --stdin 으로 본문 전체를 넣는 길이다 (--body 는 카드의 다른 줄까지 지운다).
	if next := strings.Join(report.Items[0].Next, " | "); !strings.Contains(next, "--stdin") || strings.Contains(next, "--body") {
		t.Fatalf("obs-cite-nli 다음 수가 다르다 : %s", next)
	}
	// 덮였거나 무효인 인용 기억은 묻지 않는다.
	before := asked.Load()
	byID = obsCard(body)
	byID["20260101-aaaa0001"].SupersededBy = "20260301-00000001"
	byID["20260102-bbbb0002"].InvalidAt = "2026-01-01"
	found = quality.RepoReport{ByID: map[string][]quality.Finding{}}
	sieveRun(&found, byID, nliOptions(t, server.URL, 0), KindBasis)
	if asked.Load() != before {
		t.Fatalf("죽은 인용 기억에 %d번 물었다", asked.Load()-before)
	}
}

// #16 카드 하나에 10줄까지만 묻는다.
func TestObsCiteTenLinesPerCard(t *testing.T) {
	server, asked, _ := fakeNLIBy(t, nil)
	lines := []string{}
	for at := 1; at <= 12; at++ {
		lines = append(lines, strings.Repeat("고친 ", at)+"줄이다 [mem:20260101-aaaa0001]")
	}
	body := ""
	for at, line := range lines {
		body += strings.TrimSpace(string(rune('0'+at%10))) + ". " + line + "\n"
	}
	byID := obsCard(body)
	found := quality.RepoReport{ByID: map[string][]quality.Finding{}}
	report := sieveRun(&found, byID, nliOptions(t, server.URL, 0), KindBasis)
	if asked.Load() != obsLinesPerCard || len(report.Items) != 0 {
		t.Fatalf("카드당 10줄인데 %d번 물었다 · 무관은 큐에 안 오른다 : %+v", asked.Load(), report.Items)
	}
}
