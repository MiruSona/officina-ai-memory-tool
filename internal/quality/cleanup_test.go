package quality

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 점검·정리 1단 검사 (설계 `2026-10-10-기억점검정리설계.md` 9절 표).
// 기억은 코드 안에서 만든다 — checks_basis_test.go 와 같은 꼴이다.

const cleanupScope = "aimemorytool"

// cleanupMemory 는 시험용 기억 하나다. 본문은 부르는 쪽이 정한다.
func cleanupMemory(id, kind, date, title, body string) *model.Memory {
	return &model.Memory{ID: id, Type: kind, Date: date, Title: title,
		Summary: title, Tags: []string{"hook", "korean"}, Scope: cleanupScope,
		Spec: model.SpecV2, Body: body, Sources: []string{"file:internal/hook/hook.go"}}
}

// 닮은 짝의 본문 둘이다. 두 줄이 같고 끝 줄만 다르다 — C02(경고) 꼴이고 C01·C03 은 아니다.
const (
	hookBodyOld = "훅이 밀어 넣는 글의 상한은 글자 수가 아니라 바이트로 잰다.\n" +
		"한글은 한 글자가 세 바이트라 글자로 재면 세 배로 틀린다.\n넘치면 훅이 먼저 끝을 잘라 낸다."
	hookBodyNew = "훅이 밀어 넣는 글의 상한은 글자 수가 아니라 바이트로 잰다.\n" +
		"한글은 한 글자가 세 바이트라 글자로 재면 세 배로 틀린다.\n넘치면 잘린 자리에 표시를 남긴다."
)

// hookPair 는 닮은 짝이다. newBody 끝에 extra 를 붙인다 (바뀜 말을 넣는 자리).
func hookPair(kind, oldDate, newDate, extra string) (*model.Memory, *model.Memory) {
	old := cleanupMemory("20260801-aaaa0001", kind, oldDate, "훅 주입 상한을 바이트로 잰다", hookBodyOld)
	newer := cleanupMemory("20260801-aaaa0002", kind, newDate, "훅 주입 상한 재는 법", hookBodyNew+extra)
	return old, newer
}

func cleanupReport(t *testing.T, memories ...*model.Memory) RepoReport {
	t.Helper()
	return CheckRepo(memories, wave2dOptions(t))
}

// ruleHits 는 rule 에 걸린 기억 id → Related 다.
func ruleHits(report RepoReport, rule string) map[string][]string {
	out := map[string][]string{}
	for id, list := range report.ByID {
		for _, one := range list {
			if one.Rule == rule {
				out[id] = one.Related
			}
		}
	}
	return out
}

func wantOnly(t *testing.T, report RepoReport, rule, id, related string) {
	t.Helper()
	hits := ruleHits(report, rule)
	if len(hits) != 1 {
		t.Fatalf("%s 가 한 건이어야 하는데 %d건 : %v", rule, len(hits), hits)
	}
	got, ok := hits[id]
	if !ok {
		t.Fatalf("%s 가 %s 에 걸려야 하는데 %v", rule, id, hits)
	}
	if related != "" && (len(got) == 0 || got[0] != related) {
		t.Fatalf("%s 의 Related 가 %s 여야 하는데 %v", rule, related, got)
	}
}

func wantNone(t *testing.T, report RepoReport, rules ...string) {
	t.Helper()
	for _, rule := range rules {
		if hits := ruleHits(report, rule); len(hits) > 0 {
			t.Fatalf("%s 가 안 걸려야 하는데 %v", rule, hits)
		}
	}
}

// ── C16 ─────────────────────────────────────────────────────────────────

// citePair 는 서로 안 닮은 caution 둘이다. 새 쪽이 mem: 으로 옛 것을 가리킨다.
func citePair(word string) (*model.Memory, *model.Memory) {
	old := cleanupMemory("20260801-cccc0001", model.TypeCaution, "2026-08-01",
		"설치 스크립트는 관리자 권한을 요구한다", "설치 스크립트를 돌리려면 관리자 셸이 있어야 한다.\n일반 셸에서는 등록 단계가 조용히 실패한다.")
	newer := cleanupMemory("20260810-cccc0002", model.TypeCaution, "2026-08-10",
		"설치 등록은 사용자 범위로 한다", "등록을 사용자 범위로 옮겨 관리자 셸 없이 돈다.\n"+word+"\n예전 안내문은 남겨 두지 않는다.")
	newer.Sources = append(newer.Sources, model.SourceMem+old.ID)
	return old, newer
}

func TestC16CitedNotSuperseded(t *testing.T) {
	old, newer := citePair("앞 기억을 이 기억이 덮는다.")
	wantOnly(t, cleanupReport(t, old, newer), RuleCitedNotSuperseded, old.ID, newer.ID)
}

func TestC16QuietWithoutChangeWord(t *testing.T) {
	old, newer := citePair("앞 기억과 함께 읽는다.")
	wantNone(t, cleanupReport(t, old, newer), RuleCitedNotSuperseded)
}

func TestC16QuietAcrossScope(t *testing.T) {
	old, newer := citePair("앞 기억을 이 기억이 덮는다.")
	newer.Scope = "arttool"
	wantNone(t, cleanupReport(t, old, newer), RuleCitedNotSuperseded)
}

func TestC16QuietWhenOldAlreadyRetired(t *testing.T) {
	old, newer := citePair("앞 기억을 이 기억이 덮는다.")
	old.SupersededBy = newer.ID
	wantNone(t, cleanupReport(t, old, newer), RuleCitedNotSuperseded)
}

// ── C17 · C20 (가) 와 둘 가르기 ─────────────────────────────────────────

// 닮은 howto 둘, 날짜 다름 · 새 쪽에 「고친다」 → C17 만 (옛 쪽).
func TestC17NewerSibling(t *testing.T) {
	old, newer := hookPair(model.TypeHowto, "2026-08-01", "2026-08-05", "\n앞 기억의 자르는 자리를 고친다.")
	report := cleanupReport(t, old, newer)
	wantOnly(t, report, RuleNewerSibling, old.ID, newer.ID)
	wantNone(t, report, RuleMergeCandidate)
}

// 같은 짝, 날짜 같음 → C20 만.
func TestC17SameDateGoesMerge(t *testing.T) {
	old, newer := hookPair(model.TypeHowto, "2026-08-01", "2026-08-01", "\n앞 기억의 자르는 자리를 고친다.")
	report := cleanupReport(t, old, newer)
	wantNone(t, report, RuleNewerSibling)
	wantOnly(t, report, RuleMergeCandidate, old.ID, newer.ID)
}

// 같은 짝, 바뀜 말 없음 → C20 만.
func TestC17NoChangeWordGoesMerge(t *testing.T) {
	old, newer := hookPair(model.TypeHowto, "2026-08-01", "2026-08-05", "")
	report := cleanupReport(t, old, newer)
	wantNone(t, report, RuleNewerSibling)
	wantOnly(t, report, RuleMergeCandidate, old.ID, newer.ID)
}

// C17 은 caution·howto·fact 만이다. 둘 다 history 인 짝은 C20 에 history 문턱
// (정렬 0.75 · 닮음 0.10)을 넘을 때만 오른다 (손검사 뒤 보완 2·3바퀴).
func TestC17SkipsHistoryC20HistoryBar(t *testing.T) {
	old, newer := hookPair(model.TypeHistory, "2026-08-01", "2026-08-05", "\n앞 기억의 자르는 자리를 고친다.")
	report := cleanupReport(t, old, newer)
	if len(report.Near) == 0 {
		t.Fatalf("짝이 닮은 짝이어야 시험이 뜻이 있다")
	}
	wantNone(t, report, RuleNewerSibling)
	pair := report.Near[0]
	if pair.Align >= mergeHistoryAlignMin && pair.Score >= mergeHistoryScoreMin {
		wantOnly(t, report, RuleMergeCandidate, old.ID, newer.ID)
	} else {
		wantNone(t, report, RuleMergeCandidate)
	}
}

// 주제가 다른 caution 둘 → 0.
func TestC17QuietOnUnrelated(t *testing.T) {
	one := cleanupMemory("20260801-dddd0001", model.TypeCaution, "2026-08-01",
		"설치 스크립트는 관리자 권한을 요구한다", "설치 스크립트를 돌리려면 관리자 셸이 있어야 한다.\n일반 셸에서는 등록 단계가 조용히 실패한다.")
	two := cleanupMemory("20260805-dddd0002", model.TypeCaution, "2026-08-05",
		"검색 색인은 밤에 다시 만든다", "낮에 색인을 다시 만들면 검색이 몇 초 멈춘다.\n그래서 이제 밤 작업으로 돌린다.")
	two.Sources = []string{"file:internal/index/index.go"}
	report := cleanupReport(t, one, two)
	wantNone(t, report, RuleNewerSibling, RuleMergeCandidate)
	if len(report.Near) != 0 {
		t.Fatalf("닮은 짝이 없어야 하는데 %+v", report.Near)
	}
}

// (가) 경고 자 이상 · 거절 자 미만 caution 둘 → 작은 id 에만 한 건, Related 에 큰 id.
func TestC20SoftPair(t *testing.T) {
	old, newer := hookPair(model.TypeCaution, "2026-08-01", "2026-08-01", "")
	report := cleanupReport(t, old, newer)
	wantOnly(t, report, RuleMergeCandidate, old.ID, newer.ID)
	if !hasRule(report.ByID[old.ID], RuleDuplicateSoft) {
		t.Fatalf("짝이 C02 여야 시험이 뜻이 있다 : %v", report.ByID[old.ID])
	}
}

// 본문이 같은 짝(C03)은 C20 이 아니다 — add 가 막았어야 하고 lint 가 거절로 말한다.
func TestC20SkipsSameBody(t *testing.T) {
	old, newer := hookPair(model.TypeCaution, "2026-08-01", "2026-08-01", "")
	newer.Body = old.Body
	report := cleanupReport(t, old, newer)
	wantNone(t, report, RuleMergeCandidate)
}

// 거절 자 이상(C01)으로 닮은 짝은 C20 이 아니다.
func TestC20SkipsHardDuplicate(t *testing.T) {
	body := "훅 상한은 4096 바이트로 `hook.go` 에서 자른다.\n" +
		"한글은 한 글자가 3 바이트라 4096 바이트는 약 1365 글자다.\n" +
		"넘치면 `hook.go` 가 끝에 잘림 표시를 붙인다.\n"
	old := cleanupMemory("20260801-eeee0001", model.TypeCaution, "2026-08-01", "훅 상한 4096 바이트", body+"끝 줄은 비워 둔다.")
	newer := cleanupMemory("20260801-eeee0002", model.TypeCaution, "2026-08-01", "훅 상한 4096 바이트", body+"끝 줄에 날짜를 적는다.")
	report := cleanupReport(t, old, newer)
	hard := hasRule(report.ByID[old.ID], RuleDuplicateHard) || hasRule(report.ByID[newer.ID], RuleDuplicateHard)
	if !hard {
		t.Skipf("이 짝이 C01 이 안 돼 대조군으로 못 쓴다 : %v", report.ByID[old.ID])
	}
	wantNone(t, report, RuleMergeCandidate)
}

// 모음 기억은 짝에 안 든다.
func TestC20SkipsObservation(t *testing.T) {
	old, newer := hookPair(model.TypeCaution, "2026-08-01", "2026-08-01", "")
	newer.Type = model.TypeObservation
	old.Type = model.TypeObservation
	report := cleanupReport(t, old, newer)
	wantNone(t, report, RuleMergeCandidate)
}

// decision 짝이 C05 에 걸렸으면 C20 을 안 낸다 — conflict 큐가 맡는다.
func TestC20SkipsDecisionConflict(t *testing.T) {
	old, newer := hookPair(model.TypeDecision, "2026-08-01", "2026-08-01", "")
	report := cleanupReport(t, old, newer)
	if len(ruleHits(report, RuleDecisionConflictLive)) == 0 {
		t.Fatalf("짝이 C05 여야 시험이 뜻이 있다")
	}
	wantNone(t, report, RuleMergeCandidate)
}

// ── C20 (나) 쪼개진 짝 ──────────────────────────────────────────────────

// splitTwo 는 같은 file: 근거(줄 번호만 다름)를 단, 본문이 다른 caution 둘이다.
func splitTwo(newDate string) (*model.Memory, *model.Memory) {
	one := cleanupMemory("20260801-ffff0001", model.TypeCaution, "2026-08-01",
		"훅 주입 상한 자르기", "세션 시작 글이 길면 앞에서부터 남기고 뒤를 버린다.\n버린 건수는 마지막 줄에 적는다.")
	one.Sources = []string{"file:internal/hook/inject.go:12"}
	two := cleanupMemory("20260802-ffff0002", model.TypeCaution, newDate,
		"훅 주입 상한 시험", "시험은 가짜 저장소에 기억 삼백 개를 넣고 돌린다.\n윈도 줄바꿈도 같이 넣어 본다.")
	two.Sources = []string{"file:internal/hook/inject.go:40"}
	return one, two
}

func TestC20SplitPair(t *testing.T) {
	one, two := splitTwo("2026-08-04")
	report := cleanupReport(t, one, two)
	if len(report.Near) != 0 {
		t.Fatalf("쪼개진 짝은 닮은 짝이 아니어야 시험이 뜻이 있다 : %+v", report.Near)
	}
	wantOnly(t, report, RuleMergeCandidate, one.ID, two.ID)
	reason := ""
	for _, found := range report.ByID[one.ID] {
		if found.Rule == RuleMergeCandidate {
			reason = found.Reason
		}
	}
	if !strings.Contains(reason, "file:internal/hook/inject.go") || !strings.Contains(reason, "3일") {
		t.Fatalf("까닭에 근거와 날짜 차이가 있어야 한다 : %s", reason)
	}
}

func TestC20SplitQuietOverWeek(t *testing.T) {
	one, two := splitTwo("2026-08-11")
	wantNone(t, cleanupReport(t, one, two), RuleMergeCandidate)
}

func TestC20SplitQuietOnCommonSource(t *testing.T) {
	one, two := splitTwo("2026-08-04")
	memories := []*model.Memory{one, two}
	topics := []string{"색인", "검색", "설치", "그림", "소리", "공수", "판정", "모델", "규칙", "기록",
		"경로", "링크", "태그", "제목", "요약", "본문", "근거", "날짜", "상한", "하한"}
	for at := 0; at < 19; at++ {
		other := cleanupMemory(fmt.Sprintf("20260715-%08x", at), model.TypeHistory, "2026-07-15",
			topics[at]+" 정리 "+fmt.Sprint(at), topics[at]+" 이야기를 "+fmt.Sprint(at)+" 번째로 적는다.\n"+topics[at+1]+" 와는 상관없다.")
		other.Sources = []string{"file:internal/hook/inject.go"}
		memories = append(memories, other)
	}
	// 근거 하나를 21건이 같이 단다 → 흔한 근거라 묶음에서 빠진다.
	report := cleanupReport(t, memories...)
	if hits := ruleHits(report, RuleMergeCandidate); len(hits[one.ID]) > 0 {
		t.Fatalf("흔한 근거로는 쪼개짐을 안 말해야 한다 : %v", hits)
	}
}

// 둘 다 history 인 쪼개진 짝은 C20 (나) 에 안 오른다. (나) 는 정렬 0.35 아래 짝만
// 고르므로 history 문턱(정렬 0.5)을 넘을 수 없다.
func TestC20SplitSkipsHistory(t *testing.T) {
	one, two := splitTwo("2026-08-04")
	one.Type, two.Type = model.TypeHistory, model.TypeHistory
	wantNone(t, cleanupReport(t, one, two), RuleMergeCandidate)
}

// ── 손검사 뒤 보완 : routeNear 문턱 (2026-10-10 밤) ──────────────────────

// routeOnly 는 닮은 짝을 손으로 만들어 routeNear 만 돌린다. 닮음·정렬값을 마음대로
// 정할 수 있어 문턱 바로 위아래를 잴 수 있다.
func routeOnly(t *testing.T, pairs []NearPair, memories ...*model.Memory) RepoReport {
	t.Helper()
	report := RepoReport{ByID: map[string][]Finding{}}
	add := func(m *model.Memory, rule, reason string, related ...string) {
		report.ByID[m.ID] = append(report.ByID[m.ID], Finding{Rule: rule, Reason: reason, Related: related})
	}
	dupWarn := wave2dOptions(t).Config.Quality.DupWarn
	report.routeNear(memories, duplicateResult{near: pairs}, dupWarn, add)
	return report
}

// routePair 는 날짜가 다른 caution 둘이다. 새 쪽 제목에 바뀜 말 「바꾼」 이 있다.
func routePair() (*model.Memory, *model.Memory) {
	old := cleanupMemory("20260801-kkkk0001", model.TypeCaution, "2026-08-01", "랜카드는 1G 다", hookBodyOld)
	newer := cleanupMemory("20260805-kkkk0002", model.TypeCaution, "2026-08-05", "랜선을 바꾼 뒤 속도", hookBodyNew)
	return old, newer
}

func softNear(align, score float64) []NearPair {
	return []NearPair{{Left: "20260801-kkkk0001", Right: "20260805-kkkk0002", Align: align, Score: score, Soft: true}}
}

// Partial 길(닮음 < DupWarn) 짝은 정렬값이 높아도 C20 을 안 낸다.
func TestC20SkipsPartialOnly(t *testing.T) {
	old, newer := routePair()
	newer.Title = "훅 상한 재는 법"
	wantNone(t, routeOnly(t, softNear(0.52, 0.052), old, newer), RuleMergeCandidate)
}

// 닮음은 넘어도 정렬값이 mergeAlignMin 아래면 C20 을 안 낸다.
func TestC20SkipsLowAlign(t *testing.T) {
	old, newer := routePair()
	newer.Title = "훅 상한 재는 법"
	wantNone(t, routeOnly(t, softNear(mergeAlignMin-0.01, 0.086), old, newer), RuleMergeCandidate)
	wantOnly(t, routeOnly(t, softNear(mergeAlignMin, 0.086), old, newer), RuleMergeCandidate, old.ID, newer.ID)
}

// soft 짝의 정렬값이 0.35 아래면 바뀜 말이 있어도 C17 이 아니다. 문턱을 넘으면 C20 으로 간다.
func TestC17SkipsSoftBelowCut(t *testing.T) {
	old, newer := routePair()
	report := routeOnly(t, softNear(0.30, 0.07), old, newer)
	wantNone(t, report, RuleNewerSibling)
	wantOnly(t, report, RuleMergeCandidate, old.ID, newer.ID)

	// 정렬 0.16 (스튜디오 실측 꼴) — C17 도 C20 도 아니다.
	wantNone(t, routeOnly(t, softNear(0.16, 0.07), old, newer), RuleNewerSibling, RuleMergeCandidate)

	// 0.35 이상이면 C17 이다.
	wantOnly(t, routeOnly(t, softNear(0.40, 0.07), old, newer), RuleNewerSibling, old.ID, newer.ID)
}

// 일반 짝의 2바퀴 문턱 — 정렬 mergeAlignMin(0.30) · 닮음 mergeScoreMin(0.07) 바로 위아래.
func TestC20GeneralBoundary(t *testing.T) {
	old, newer := routePair()
	newer.Title = "훅 상한 재는 법"
	// 1바퀴 「틀리다」 꼴 : 정렬 0.29 · 닮음 0.074
	wantNone(t, routeOnly(t, softNear(0.29, 0.074), old, newer), RuleMergeCandidate)
	// 1바퀴 「틀리다」 꼴 : 정렬 0.74 · 닮음 0.065
	wantNone(t, routeOnly(t, softNear(0.74, 0.065), old, newer), RuleMergeCandidate)
	wantNone(t, routeOnly(t, softNear(mergeAlignMin-0.01, 0.086), old, newer), RuleMergeCandidate)
	wantNone(t, routeOnly(t, softNear(0.30, mergeScoreMin-0.001), old, newer), RuleMergeCandidate)
	wantOnly(t, routeOnly(t, softNear(0.30, mergeScoreMin), old, newer), RuleMergeCandidate, old.ID, newer.ID)
	// 1바퀴 「맞다」 꼴 : 정렬 0.34 · 닮음 0.091
	wantOnly(t, routeOnly(t, softNear(0.34, 0.091), old, newer), RuleMergeCandidate, old.ID, newer.ID)
}

// 둘 다 history 인 짝의 문턱 — 정렬 0.75 · 닮음 0.10 을 둘 다 넘어야 C20 이다.
func TestC20HistoryBoundary(t *testing.T) {
	old, newer := routePair()
	newer.Title = "훅 상한 재는 법"
	old.Type, newer.Type = model.TypeHistory, model.TypeHistory
	// 손검사 15번 꼴 (정렬 0.81 · 닮음 0.136) 은 남는다.
	wantOnly(t, routeOnly(t, softNear(0.81, 0.136), old, newer), RuleMergeCandidate, old.ID, newer.ID)
	wantOnly(t, routeOnly(t, softNear(mergeHistoryAlignMin, mergeHistoryScoreMin), old, newer),
		RuleMergeCandidate, old.ID, newer.ID)
	wantNone(t, routeOnly(t, softNear(mergeHistoryAlignMin-0.01, 0.136), old, newer), RuleMergeCandidate)
	wantNone(t, routeOnly(t, softNear(0.81, mergeHistoryScoreMin-0.001), old, newer), RuleMergeCandidate)
	// 일반 문턱은 넘지만 history 문턱 아래인 짝 (정렬 0.34 · 닮음 0.08) — history 면 빠지고,
	// 한쪽만 history 면 일반 문턱이라 남는다.
	wantNone(t, routeOnly(t, softNear(0.34, 0.08), old, newer), RuleMergeCandidate)
	old.Type = model.TypeCaution
	newer.Type = model.TypeCaution
	wantOnly(t, routeOnly(t, softNear(0.34, 0.08), old, newer), RuleMergeCandidate, old.ID, newer.ID)
}

// (나) 길도 같은 history 문턱을 본다 — 정렬·닮음을 손으로 넣어 잰다.
func TestC20SplitHistoryBoundary(t *testing.T) {
	old, newer := routePair()
	old.Type, newer.Type = model.TypeHistory, model.TypeHistory
	route := func(align, score float64) RepoReport {
		report := RepoReport{ByID: map[string][]Finding{}}
		add := func(m *model.Memory, rule, reason string, related ...string) {
			report.ByID[m.ID] = append(report.ByID[m.ID], Finding{Rule: rule, Reason: reason, Related: related})
		}
		split := []splitPair{{left: old.ID, right: newer.ID, source: "file:a.go", days: 4, align: align, score: score}}
		dupWarn := wave2dOptions(t).Config.Quality.DupWarn
		report.routeNear([]*model.Memory{old, newer}, duplicateResult{split: split}, dupWarn, add)
		return report
	}
	wantNone(t, route(0.30, 0.20), RuleMergeCandidate)
	wantNone(t, route(0.80, 0.09), RuleMergeCandidate)
	wantNone(t, route(0.60, 0.12), RuleMergeCandidate)
	wantOnly(t, route(0.80, 0.12), RuleMergeCandidate, old.ID, newer.ID)
}

// 3바퀴 history 문턱 0.75 — 2바퀴 뒤 남은 「이어진 단계」 짝(0.66 · 0.57)은 빠지고
// 「맞다」 짝(0.81 · 0.80)은 남는다.
func TestC20HistoryThirdPass(t *testing.T) {
	old, newer := routePair()
	newer.Title = "훅 상한 재는 법"
	old.Type, newer.Type = model.TypeHistory, model.TypeHistory
	wantNone(t, routeOnly(t, softNear(0.66, 0.101), old, newer), RuleMergeCandidate)
	wantNone(t, routeOnly(t, softNear(0.57, 0.263), old, newer), RuleMergeCandidate)
	wantNone(t, routeOnly(t, softNear(0.74, 0.20), old, newer), RuleMergeCandidate)
	wantOnly(t, routeOnly(t, softNear(0.75, 0.20), old, newer), RuleMergeCandidate, old.ID, newer.ID)
	wantOnly(t, routeOnly(t, softNear(0.80, 0.12), old, newer), RuleMergeCandidate, old.ID, newer.ID)
}

// migratedPair 는 같은 날(id 앞 8자리) 옮겨 온 caution 둘이다. 바뀜 말이 없게 제목을 둔다.
func migratedPair() (*model.Memory, *model.Memory) {
	old, newer := routePair()
	newer.Title = "훅 상한 재는 법"
	old.ID, newer.ID = "20260822-kkkk0001", "20260822-kkkk0002"
	old.Date, newer.Date = "2026-08-22", "2026-08-22"
	old.Migrated, newer.Migrated = true, true
	return old, newer
}

func migratedNear(align, score float64) []NearPair {
	return []NearPair{{Left: "20260822-kkkk0001", Right: "20260822-kkkk0002", Align: align, Score: score, Soft: true}}
}

// 옮겨 온 같은 날 짝은 정렬 0.25 · 닮음 DupWarn 으로 본다 (3바퀴). 2바퀴에 빠진
// 「맞다」 셋(0.36·0.067 / 0.25·0.087 / 0.29·0.067)이 남는다.
func TestC20MigratedBoundary(t *testing.T) {
	old, newer := migratedPair()
	dupWarn := wave2dOptions(t).Config.Quality.DupWarn
	for _, c := range [][2]float64{{0.36, 0.067}, {0.25, 0.087}, {0.29, 0.067}, {mergeMigratedAlignMin, dupWarn}} {
		wantOnly(t, routeOnly(t, migratedNear(c[0], c[1]), old, newer), RuleMergeCandidate, old.ID, newer.ID)
	}
	wantNone(t, routeOnly(t, migratedNear(mergeMigratedAlignMin-0.01, 0.087), old, newer), RuleMergeCandidate)
	wantNone(t, routeOnly(t, migratedNear(0.36, dupWarn-0.001), old, newer), RuleMergeCandidate)
}

// 날짜가 다르거나 한쪽만 옮겨 온 것이면 일반 문턱(0.30 · 0.07)이다.
func TestC20MigratedNeedsBothSameDay(t *testing.T) {
	old, newer := migratedPair()
	newer.ID, newer.Date = "20260823-kkkk0002", "2026-08-23"
	pairs := []NearPair{{Left: old.ID, Right: newer.ID, Align: 0.29, Score: 0.067, Soft: true}}
	wantNone(t, routeOnly(t, pairs, old, newer), RuleMergeCandidate)

	old, newer = migratedPair()
	newer.Migrated = false
	wantNone(t, routeOnly(t, migratedNear(0.29, 0.067), old, newer), RuleMergeCandidate)
	// 일반 문턱을 넘으면 한쪽만 옮겨 온 짝도 남는다.
	wantOnly(t, routeOnly(t, migratedNear(0.30, 0.07), old, newer), RuleMergeCandidate, old.ID, newer.ID)
}

// ── C18 ─────────────────────────────────────────────────────────────────

func TestC18RetiredUnfolded(t *testing.T) {
	gone := cleanupMemory("20260801-gggg0001", model.TypeFact, "2026-08-01", "훅 상한은 바이트다", hookBodyOld)
	gone.SupersededBy = "20260810-gggg0009"
	wantOnly(t, cleanupReport(t, gone), RuleRetiredUnfolded, gone.ID, "20260810-gggg0009")

	gone.State = "cold"
	wantNone(t, cleanupReport(t, gone), RuleRetiredUnfolded)

	invalid := cleanupMemory("20260801-gggg0002", model.TypeFact, "2026-08-01", "훅 상한은 바이트다", hookBodyOld)
	invalid.InvalidAt = "2026-08-10"
	wantOnly(t, cleanupReport(t, invalid), RuleRetiredUnfolded, invalid.ID, "")

	invalid.InvalidAt = "2026-12-31" // 아직 안 지났다
	wantNone(t, cleanupReport(t, invalid), RuleRetiredUnfolded)
}

// pinned 는 `gc --retired` 가 안 접으니 C18 도 안 올린다 — 둘의 건수가 같아야 한다.
func TestC18SkipsPinned(t *testing.T) {
	pinned := cleanupMemory("20260801-gggg0003", model.TypeFact, "2026-08-01", "훅 상한은 바이트다", hookBodyOld)
	pinned.SupersededBy = "20260810-gggg0009"
	pinned.Pinned = true
	wantNone(t, cleanupReport(t, pinned), RuleRetiredUnfolded)
}

// ── D06 ─────────────────────────────────────────────────────────────────

func TestD06NoArtifactSource(t *testing.T) {
	one := cleanupMemory("20260801-hhhh0001", model.TypeDecision, "2026-08-01", "훅 상한은 바이트로 잰다", hookBodyOld)
	one.Sources = []string{"note:회의에서 정함", "mem:20260701-00000000"}
	wantOnly(t, cleanupReport(t, one), RuleNoArtifactSource, one.ID, "")

	one.Sources = append(one.Sources, "file:internal/hook/hook.go")
	wantNone(t, cleanupReport(t, one), RuleNoArtifactSource)

	// sources 가 필수가 아닌 종류는 안 본다.
	two := cleanupMemory("20260801-hhhh0002", model.TypeHistory, "2026-08-01", "훅 상한을 바이트로 바꿨다", hookBodyOld)
	two.Sources = []string{"note:회의"}
	wantNone(t, cleanupReport(t, two), RuleNoArtifactSource)
}

// ── D05 호출 ─────────────────────────────────────────────────────────────

type bodyCall struct {
	path   string
	linked bool
}

func TestD05AsksBodyPaths(t *testing.T) {
	one := cleanupMemory("20260801-iiii0001", model.TypeHowto, "2026-08-01", "훅 경로", "")
	one.Body = "고칠 곳은 AIMemoryTool/internal/hook/hook.go에서 본다.\n" +
		"설계는 [설계](Docs/Design/훅설계.md#2절) 에 있다.\n" +
		"시험은 `internal/hook/bench_test.go`, 폴더 internal/install 은 안 본다.\n" +
		"주소 https://example.com/a/b.go 와 1.2/3.4 는 경로가 아니다.\n" +
		"같은 경로 AIMemoryTool/internal/hook/hook.go 를 또 적는다."
	calls := []bodyCall{}
	opt := wave2dOptions(t)
	opt.BodyMissing = func(m *model.Memory, path string, linked bool) (string, string) {
		calls = append(calls, bodyCall{path, linked})
		if path == "AIMemoryTool/internal/hook/hook.go" {
			return RuleDeadBodyPath, "본문 경로 `" + path + "` 가 없다"
		}
		return "", ""
	}
	report := CheckRepo([]*model.Memory{one}, opt)
	want := []bodyCall{{"Docs/Design/훅설계.md", true}, {"AIMemoryTool/internal/hook/hook.go", false},
		{"internal/hook/bench_test.go", false}}
	if fmt.Sprint(calls) != fmt.Sprint(want) {
		t.Fatalf("물은 경로가 다르다\n얻음 %v\n바람 %v", calls, want)
	}
	wantOnly(t, report, RuleDeadBodyPath, one.ID, "AIMemoryTool/internal/hook/hook.go")

	// 갈고리가 없으면 건너뛴다.
	wantNone(t, cleanupReport(t, one), RuleDeadBodyPath)
}

// ── Near (C19 가 받을 짝) ────────────────────────────────────────────────

func TestNearPairCarriesAlignedText(t *testing.T) {
	old, newer := hookPair(model.TypeCaution, "2026-08-01", "2026-08-05", "")
	report := cleanupReport(t, newer, old) // 차례를 뒤집어도 (작은 id, 큰 id) 다
	if len(report.Near) != 1 {
		t.Fatalf("닮은 짝이 한 쌍이어야 하는데 %+v", report.Near)
	}
	pair := report.Near[0]
	if pair.Left != old.ID || pair.Right != newer.ID || !pair.Soft || pair.Align < staleConflictCut {
		t.Fatalf("짝 꼴이 이상하다 : %+v", pair)
	}
	if pair.LeftText == "" || pair.RightText == "" {
		t.Fatalf("C19 가 쓸 두 문장이 비었다 : %+v", pair)
	}
}

// 다른 scope · 죽은 기억은 짝에 안 든다.
func TestNearPairNeedsSameScopeAndLive(t *testing.T) {
	old, newer := hookPair(model.TypeCaution, "2026-08-01", "2026-08-05", "")
	newer.Scope = "arttool"
	if report := cleanupReport(t, old, newer); len(report.Near) != 0 {
		t.Fatalf("scope 가 다르면 짝이 아니다 : %+v", report.Near)
	}
	old, newer = hookPair(model.TypeCaution, "2026-08-01", "2026-08-05", "")
	old.InvalidAt = "2026-08-02"
	if report := cleanupReport(t, old, newer); len(report.Near) != 0 {
		t.Fatalf("죽은 기억은 짝이 아니다 : %+v", report.Near)
	}
}

// ── 경로 뽑기 낱개 ───────────────────────────────────────────────────────

func TestPathInCutsTail(t *testing.T) {
	cases := map[string]string{
		"internal/hook/hook.go에서":    "internal/hook/hook.go",
		"internal/hook/hook.go.":     "internal/hook/hook.go",
		"internal/hook/hook.go:12":   "internal/hook/hook.go",
		"file:Docs/a.md":             "Docs/a.md",
		"internal/install":           "",
		"github.com/x":               "",
		"hook.go":                    "",
		"mem:20260801-aaaa0001/x.go": "",
	}
	for token, want := range cases {
		if got := pathIn(token); got != want {
			t.Errorf("pathIn(%q) = %q, 바람 %q", token, got, want)
		}
	}
}
