package quality

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 2026-10-10·10-11 실사용 피드백 (Docs/Todo 두 문서) — 접힌·죽은 기억 빼기 · C16 오탐 ·
// C14 다음 명령 · links 로 이은 중복 짝 · B01·E02 까닭 글.

// foldedMemory 는 `gc --retired` 가 접은 꼴이다 — 덮였고, 본문이 한 줄 안내뿐이다.
func foldedMemory() *model.Memory {
	one := cleanupMemory("20260801-ffff0001", model.TypeDecision, "2026-08-01",
		"훅 상한을 바이트로 잰다", "본문을 접었다. 원문은 아카이브에 있다.")
	one.SupersededBy = "20260810-ffff0009"
	one.InvalidAt = "2026-08-10"
	one.State = "cold"
	one.Archived = "archive/2026-10.jsonl"
	one.StaleAfter = "2026-08-15"
	return one
}

func TestFoldedSkipsBodyChecks(t *testing.T) {
	opt := wave2dOptions(t)
	folded := foldedMemory()
	for _, rule := range []string{RuleBodyThin, RuleNoValue} {
		if hasRule(Check(folded, opt.Options), rule) {
			t.Errorf("접힌 기억에 %s 가 걸렸다", rule)
		}
	}
	// 대조군 : 접힘 표시만 지우면 B01 이 난다 — 위 시험이 헛돌지 않는다는 표시다.
	open := foldedMemory()
	open.State, open.Archived = "", ""
	if !hasRule(Check(open, opt.Options), RuleBodyThin) {
		t.Fatal("대조군 : 안 접힌 한 줄 본문에 B01 이 안 났다")
	}
}

func TestFoldedSkipsValueQueue(t *testing.T) {
	opt := wave2dOptions(t).Options
	open := foldedMemory()
	open.SupersededBy, open.InvalidAt, open.State, open.Archived = "", "", "", ""
	found := ValueFindings([]*model.Memory{foldedMemory(), open}, opt)
	if hasRule(found[0], RuleNoValue) {
		t.Error("접힌 기억이 value 큐(B12)에 올랐다")
	}
	if !hasRule(found[1], RuleNoValue) {
		t.Fatal("대조군 : 안 접힌 같은 본문에 B12 가 안 났다")
	}
}

func TestDeadSkipsRepoRules(t *testing.T) {
	opt := wave2dOptions(t)
	opt.SourceMissing = func(m *model.Memory, source string) (string, string) {
		return RuleDeadPath, "없다"
	}
	report := CheckRepo([]*model.Memory{foldedMemory()}, opt)
	for _, rule := range []string{RuleStaleAfterPassed, RuleDeadPath, RuleStaleAge} {
		if hits := ruleHits(report, rule); len(hits) > 0 {
			t.Errorf("죽은 기억에 %s 가 걸렸다 : %v", rule, hits)
		}
	}
	// C18 은 죽은 기억이 대상이라 그대로 돈다 — state cold 라 이번엔 안 난다.
	alive := foldedMemory()
	alive.SupersededBy, alive.InvalidAt, alive.State, alive.Archived = "", "", "", ""
	report = CheckRepo([]*model.Memory{alive}, opt)
	if len(ruleHits(report, RuleStaleAfterPassed)) == 0 || len(ruleHits(report, RuleDeadPath)) == 0 {
		t.Fatal("대조군 : 산 기억에 C07·D02 가 안 났다")
	}
}

func TestDeadLeavesDuplicatePairs(t *testing.T) {
	live := cleanupMemory("20260801-dddd0001", model.TypeCaution, "2026-08-01", "훅 주입 상한을 바이트로 잰다", hookBodyOld)
	dead := cleanupMemory("20260802-dddd0002", model.TypeCaution, "2026-08-02", "훅 주입 상한을 바이트로 잰다", hookBodyOld)
	both := cleanupReport(t, live, dead)
	if len(ruleHits(both, RuleSameBody))+len(ruleHits(both, RuleDuplicateHard)) == 0 {
		t.Fatal("대조군 : 둘 다 살아 있을 때 중복이 안 났다")
	}
	dead.SupersededBy, dead.InvalidAt = live.ID, "2026-08-02"
	wantNone(t, cleanupReport(t, live, dead), RuleSameBody, RuleDuplicateHard, RuleDuplicateSoft)
}

// C13 짝은 골든 저장소의 실제 짝(b850d0b8 ↔ a557d32c, lint 임계표)을 빌린다.
func TestDeadLeavesClashPairs(t *testing.T) {
	memories, err := LoadMemories(filepath.FromSlash(storePath))
	if err != nil {
		t.Fatal(err)
	}
	byID := memoriesByID(memories)
	live, other := byID["20260822-b850d0b8"], byID["20260822-a557d32c"]
	if len(ruleHits(cleanupReport(t, live, other), RuleStaleConflictPair)) == 0 {
		t.Fatal("대조군 : 둘 다 살아 있을 때 C13 이 안 났다")
	}
	dead := *other
	dead.SupersededBy, dead.InvalidAt = live.ID, "2026-08-22"
	wantNone(t, cleanupReport(t, live, &dead), RuleStaleConflictPair)
}

// links 로 서로 이은 짝은 사람이 「다른 기억이다」라고 이미 정한 것이다 (저장소 검사만).
func TestLinkedPairLeavesDuplicate(t *testing.T) {
	old, newer := hookPair(model.TypeCaution, "2026-08-01", "2026-08-05", "")
	newer.Body = old.Body + "\n덧붙임 한 줄."
	if len(ruleHits(cleanupReport(t, old, newer), RuleDuplicateSoft)) == 0 {
		t.Fatal("대조군 : links 없이 C02 가 안 났다")
	}
	newer.Links = []string{old.ID}
	wantNone(t, cleanupReport(t, old, newer), RuleDuplicateHard, RuleDuplicateSoft)
}

// ── C16 오탐 넷 · 놓침 하나 ─────────────────────────────────────────────

func TestC16SkipsNegatedChange(t *testing.T) {
	old, newer := citePair("앞 기억을 덮지 않고 잇는다.")
	wantNone(t, cleanupReport(t, old, newer), RuleCitedNotSuperseded)
}

// 「덮어쓰지 않」 꼴도 부정이다 — 「덮지 않」 에 안 걸려 바뀜 말 「덮」 이 남았다 (리뷰 13 #10).
func TestC16SkipsOverwriteNegation(t *testing.T) {
	old, newer := citePair("앞 기억을 덮어쓰지 않고 잇는다.")
	wantNone(t, cleanupReport(t, old, newer), RuleCitedNotSuperseded)
}

// 시키는 꼴 부정(바꾸지 말·고치지 말)도 바뀜이 아니다. C16 낱말에 없는 「이제」 만 든 문장도 안 붙는다.
func TestC16SkipsImperativeNegationAndOtherWords(t *testing.T) {
	for _, text := range []string{"앞 기억은 바꾸지 말고 잇는다.", "앞 기억 숫자를 고치지 말라.", "앞 기억대로 이제 돈다."} {
		old, newer := citePair(text)
		wantNone(t, cleanupReport(t, old, newer), RuleCitedNotSuperseded)
	}
}

func TestC16SkipsHistoryPair(t *testing.T) {
	old, newer := citePair("앞 판을 이 판이 덮는다.")
	old.Type, newer.Type = model.TypeHistory, model.TypeHistory
	wantNone(t, cleanupReport(t, old, newer), RuleCitedNotSuperseded)
}

func TestC16SkipsOtherIDInSentence(t *testing.T) {
	old, newer := citePair("옛 규칙 20260909-abcd1234 는 이것으로 덮는다.")
	wantNone(t, cleanupReport(t, old, newer), RuleCitedNotSuperseded)
}

func TestC16CrossTypeWhenSentenceNamesOld(t *testing.T) {
	old, newer := citePair("옛 규칙 20260801-cccc0001 은 이걸로 덮는다.")
	newer.Type = model.TypeDecision
	wantOnly(t, cleanupReport(t, old, newer), RuleCitedNotSuperseded, old.ID, newer.ID)
	// 이름을 안 댄 문장이면 종류가 달라 그대로 안 붙는다.
	old, newer = citePair("앞 기억을 이 기억이 덮는다.")
	newer.Type = model.TypeDecision
	wantNone(t, cleanupReport(t, old, newer), RuleCitedNotSuperseded)
}

// ── C14 · B01 · E02 까닭 ──────────────────────────────────────────────

func TestBasisLinksNextRelinks(t *testing.T) {
	x := deadMemory("20260822-aaaa0101", "20260830-bbbb0101")
	keep := basisMemory("20260822-aaaa0102", model.TypeFact)
	y := basisMemory("20260822-aaaa0103", model.TypeFact)
	y.Links = []string{keep.ID, x.ID}
	report := CheckRepo([]*model.Memory{x, keep, y}, wave2dOptions(t))
	want := "mem set " + y.ID + " --links " + keep.ID + ",20260830-bbbb0101"
	for _, one := range report.ByID[y.ID] {
		if one.Rule == RuleStaleBasis && strings.Contains(strings.Join(one.Next, "\n"), want) {
			return
		}
	}
	t.Fatalf("C14 다음 명령에 %q 가 없다 : %+v", want, report.ByID[y.ID])
}

func reasonOf(found []Finding, rule string) string {
	for _, one := range found {
		if one.Rule == rule {
			return one.Reason
		}
	}
	return ""
}

func TestBodyThinHints(t *testing.T) {
	opt := wave2dOptions(t).Options
	one := cleanupMemory("20260801-bbbb0201", model.TypeCaution, "2026-08-01", "팝업이 남는다",
		"팝업이 남는다. 닫기를 눌러도 남는다. 씬을 바꾸면 사라진다. 원인은 모른다.")
	if got := reasonOf(Check(one, opt), RuleBodyThin); !strings.Contains(got, "문장 4개") {
		t.Errorf("한 줄 4문장 힌트가 없다 : %q", got)
	}
	one.Body = `팝업이 남는다.\n닫기를 눌러도 남는다.`
	if got := reasonOf(Check(one, opt), RuleBodyThin); !strings.Contains(got, "--stdin") {
		t.Errorf("글자 \\n 힌트가 없다 : %q", got)
	}
}

func TestEntropyReasonHasColumn(t *testing.T) {
	opt := wave2dOptions(t).Options
	one := cleanupMemory("20260801-eeee0301", model.TypeCaution, "2026-08-01", "열쇠 꼴 토막",
		"첫 줄은 멀쩡하다.\n값 aZ9kQ2xP7mL4vB8nR3tY6wE1 이 들었다.\n끝 줄도 멀쩡하다.")
	got := reasonOf(Check(one, opt), RuleSecretEntropy)
	if !strings.Contains(got, "3번째 칸") {
		t.Errorf("열 위치가 없다 : %q", got)
	}
	if strings.Contains(got, "aZ9kQ2xP") {
		t.Errorf("값이 찍혔다 (I3) : %q", got)
	}
}
