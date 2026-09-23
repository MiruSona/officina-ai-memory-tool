package quality

import (
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

func testNow() time.Time { return time.Date(2026, 8, 23, 12, 0, 0, 0, time.Local) }

func testOptions() Options {
	return Options{Config: config.Default("시험"), Vocab: testVocab(), Now: testNow()}
}

// testVocab 은 **표준 목록을 이미 정한** 저장소의 낱말 표다. 씨앗 목록에는
// scope 가 하나도 없어서 그대로 쓰면 F09·F12 가 경고로만 나온다 (Vocab.Learning).
// 거절을 재는 시험이라 여기서 scope 를 못 박는다.
func testVocab() config.Vocab {
	vocab := config.DefaultVocab()
	vocab.Tags["hook"] = []string{}
	vocab.TagAlias["hooks"] = "hook"
	vocab.TagAlias["security-design"] = "security"
	vocab.Scopes["aimemorytool"] = []string{}
	vocab.ScopeAlias["mem"] = "aimemorytool"
	vocab.ScopeAlias["mem-search"] = "aimemorytool"
	return vocab
}

// goodMemory 는 관문을 통과해야 하는 기억이다. 시험마다 여기서 한 칸씩 망가뜨린다.
func goodMemory() *model.Memory {
	return &model.Memory{
		ID: "20260823-3f9a2c1b", Type: model.TypeDecision,
		Title:   "훅 주입 상한을 바이트로 잰다",
		Summary: "훅 주입 블록은 UTF-8 8,000바이트에서 우리가 먼저 자른다. 10,000자 상한에 기대지 않는다",
		Tags:    []string{"hook", "security"}, Scope: "aimemorytool", Date: "2026-08-23",
		Author:  "claude-code/opus-5",
		Sources: []string{"url:https://code.claude.com/docs/en/hooks"},
		Spec:    model.SpecV2,
		Body: strings.Join([]string{
			"10,000자를 넘으면 앞 2,000자만 남고 나머지는 말없이 사라진다.",
			"",
			"한글은 1글자가 UTF-8 3바이트다. 글자 수로 재면 3,300자에서 이미 상한이다.",
			"상한이 글자인지 바이트인지 문서와 이슈가 서로 다르게 적고 있어 안전한 쪽으로 잡는다.",
		}, "\n"),
	}
}

func hasRule(found []Finding, rule string) bool {
	for _, one := range found {
		if one.Rule == rule {
			return true
		}
	}
	return false
}

func TestGatePasses(t *testing.T) {
	verdict := Gate(goodMemory(), testOptions())
	if verdict.Kind != KindPass {
		t.Fatalf("통과해야 하는데 %v : %v", verdict.Kind, verdict.Reasons())
	}
	if verdict.ExitCode() != 0 {
		t.Errorf("종료 코드가 0이 아니다 : %d", verdict.ExitCode())
	}
}

// TestGateFieldReject 는 ① 규격 단계다. 종료 코드 2 로 끝나야 한다.
func TestGateFieldReject(t *testing.T) {
	cases := []struct {
		name   string
		damage func(*model.Memory)
		rule   string
	}{
		{"태그 하나", func(m *model.Memory) { m.Tags = []string{"hook"} }, RuleTagCount},
		{"표준 밖 태그", func(m *model.Memory) { m.Tags = []string{"hook", "wibble"} }, RuleTagStandard},
		{"규격 밖 태그(한글)", func(m *model.Memory) { m.Tags = []string{"hook", "제멋대로"} }, RuleTagShape},
		{"출처 태그", func(m *model.Memory) { m.Tags = []string{"hook", "poc"} }, RuleTagNotSource},
		{"표준 밖 scope", func(m *model.Memory) { m.Scope = "그림툴" }, RuleScopeStandard},
		{"근거 없음", func(m *model.Memory) { m.Sources = nil }, RuleSourcesRequired},
		{"근거 꼴 틀림", func(m *model.Memory) { m.Sources = []string{"어디선가 봤다"} }, RuleSourcesShape},
		{"미래 날짜", func(m *model.Memory) { m.Date = "2026-12-31" }, RuleDateShape},
		{"제목이 요약 베낌", func(m *model.Memory) { m.Title = m.Summary[:40] }, RuleTitleShape},
		{"본문 한 줄", func(m *model.Memory) { m.Body = "한 줄뿐이다" }, RuleBodyThin},
	}
	for _, one := range cases {
		t.Run(one.name, func(t *testing.T) {
			memory := goodMemory()
			one.damage(memory)
			verdict := Gate(memory, testOptions())
			if !hasRule(verdict.Findings, one.rule) {
				t.Fatalf("%s 가 안 걸렸다 : %v", one.rule, verdict.Rules())
			}
			if verdict.Kind != KindQualityReject {
				t.Fatalf("거절이어야 하는데 %v : %v", verdict.Kind, verdict.Reasons())
			}
			if verdict.ExitCode() != 2 {
				t.Fatalf("품질 거절의 종료 코드는 2다 : %d", verdict.ExitCode())
			}
		})
	}
}

// TestGateRejectShowsNextCommand 는 「거절하면 다음 명령까지 쥐여 준다」 다 (설계 3-1).
func TestGateRejectShowsNextCommand(t *testing.T) {
	memory := goodMemory()
	memory.Scope = "그림툴"
	verdict := Gate(memory, testOptions())
	for _, one := range verdict.Findings {
		if one.Level == GradeReject && len(one.Next) == 0 {
			t.Errorf("규칙 %s 가 거절인데 다음 명령을 안 알려준다", one.Rule)
		}
	}
}

func TestGateWarnStillPasses(t *testing.T) {
	memory := goodMemory()
	memory.Sources = []string{"note:옛 설계 2절에서 정했다"}
	verdict := Gate(memory, testOptions())
	if !hasRule(verdict.Findings, RuleSourcesNoteOnly) {
		t.Fatalf("sources-note-only 가 안 걸렸다 : %v", verdict.Rules())
	}
	if verdict.Kind != KindWarn {
		t.Fatalf("경고여야 하는데 %v", verdict.Kind)
	}
	if verdict.Rejected() {
		t.Error("경고는 저장을 막지 않는다")
	}
}

// TestGateSecretRejects 는 ② 보안 단계다. 종료 코드 4 이고, 값은 절대 안 찍는다.
func TestGateSecretRejects(t *testing.T) {
	memory := goodMemory()
	secretValue := "ghp_" + strings.Repeat("a1B2", 6)
	memory.Body += "\n토큰은 " + secretValue + " 였다."
	verdict := Gate(memory, testOptions())
	if verdict.Kind != KindSecurityReject || verdict.ExitCode() != 4 {
		t.Fatalf("보안 거절이어야 한다 : %v (%d)", verdict.Kind, verdict.ExitCode())
	}
	for _, one := range verdict.Findings {
		if strings.Contains(one.Reason, secretValue) {
			t.Fatal("거절 문구가 비밀 값을 그대로 찍었다")
		}
	}
	// 보안에서 멈추므로 중복 검사까지 가지 않는다.
	if hasRule(verdict.Findings, RuleDuplicateHard) {
		t.Error("보안 거절 뒤에도 중복 검사가 돌았다")
	}
}

func TestGateEnvVarNameIsNotPrivatePath(t *testing.T) {
	memory := goodMemory()
	memory.Body += "\n설치 자리는 %USERPROFILE%\\.aimemory 다."
	if hasRule(Gate(memory, testOptions()).Findings, RulePrivatePath) {
		t.Error("환경변수 이름은 사적 경로가 아니다")
	}
}

// TestGateMultiRejectsDecision 은 ③ MULTI 단계다. decision 은 거절, 그 밖은 경고.
func TestGateMultiRejectsDecision(t *testing.T) {
	memory := goodMemory()
	memory.Summary = "훅·색인·검색에서 정한 것 세 가지를 한 번에 적는다. 나중에 쪼갤 것"
	verdict := Gate(memory, testOptions())
	if !hasRule(verdict.Findings, RuleMultiSummary) {
		t.Fatalf("multi-summary 가 안 걸렸다 : %v", verdict.Rules())
	}
	if verdict.Kind != KindQualityReject {
		t.Fatalf("decision 의 MULTI 는 거절이다 : %v", verdict.Kind)
	}
	memory.Type = model.TypeHistory
	memory.Sources = nil
	if kind := Gate(memory, testOptions()).Kind; kind != KindWarn {
		t.Fatalf("history 의 MULTI 는 경고다 : %v", kind)
	}
}

// TestScoreAndSameBody 는 닮음 점수 S 다.
func TestScoreAndSameBody(t *testing.T) {
	left := NewDoc(goodMemory())
	if score := Score(left, left); score < 0.99 {
		t.Fatalf("자기 자신과의 점수는 1에 가까워야 한다 : %.3f", score)
	}
	other := goodMemory()
	other.ID = "20260823-11111111"
	other.Summary = "gc 는 본문을 접기만 하고 지우지 않는다. 되돌리기가 늘 있어야 한다는 뜻이다"
	other.Title = "gc 는 지우지 않는다"
	other.Tags = []string{"gc", "archive"}
	other.Body = "아카이브로 옮기고 머리말만 남긴다.\n\n되돌리기는 mem gc --restore 다.\n지우는 코드 경로는 아예 없다."
	right := NewDoc(other)
	if score := Score(left, right); score > 0.30 {
		t.Fatalf("남남인 기억의 점수가 너무 높다 : %.3f", score)
	}
	if !SameBody(left, NewDoc(goodMemory())) {
		t.Error("본문이 같은데 same-body 가 아니라고 한다")
	}
	if SameBody(left, right) {
		t.Error("본문이 다른데 same-body 라고 한다")
	}
}

func TestDuplicateRejects(t *testing.T) {
	existing := goodMemory()
	existing.ID = "20260822-1fae6641"
	fresh := goodMemory()
	fresh.ID = "20260823-99999999"
	opt := testOptions()
	opt.Repo = MemorySlice{existing}
	verdict := Gate(fresh, opt)
	if !hasRule(verdict.Findings, RuleDuplicateHard) && !hasRule(verdict.Findings, RuleSameBody) {
		t.Fatalf("같은 기억을 다시 넣었는데 안 걸렸다 : %v", verdict.Rules())
	}
	if verdict.Kind != KindQualityReject {
		t.Fatalf("거절이어야 한다 : %v", verdict.Kind)
	}
	for _, one := range verdict.Findings {
		if one.Rule == RuleDuplicateHard || one.Rule == RuleSameBody {
			if len(one.Related) == 0 || one.Related[0] != existing.ID {
				t.Errorf("어느 기억 때문인지 안 알려준다 : %+v", one)
			}
		}
	}
}

// TestDecisionGate 는 ⑤ 결정 관문 C04 다.
func TestDecisionGate(t *testing.T) {
	live := goodMemory()
	live.ID = "20260822-1fae6641"
	live.Summary = "훅은 세션 시작에만 붙이고 그 밖의 자리에는 안 붙인다. 예산은 절마다 다섯 줄이다"
	live.Title = "훅은 세션 시작에만 붙인다"
	live.Body = "다른 훅 자리는 세션을 막는다.\n\n예산은 절마다 5줄.\n넘으면 긴 절부터 깎는다."

	fresh := goodMemory()
	fresh.ID = "20260823-99999999"
	fresh.Summary = "훅 예산을 절마다 다섯 줄에서 세 줄로 줄인다. 20k 저장소에서 주입이 8,000바이트를 넘겨서다"
	fresh.Title = "훅 예산을 세 줄로 줄인다"
	fresh.Body = "20k 저장소에서 주입 블록이 상한을 넘겼다.\n\n절마다 3줄로 줄인다.\n긴 절부터 깎는 규칙은 그대로다."

	opt := testOptions()
	opt.Repo = MemorySlice{live}
	verdict := Gate(fresh, opt)
	if !hasRule(verdict.Findings, RuleDecisionGate) {
		t.Fatalf("decision-gate 가 안 걸렸다 : %v", verdict.Rules())
	}
	if verdict.Kind != KindQualityReject {
		t.Fatalf("거절이어야 한다 : %v", verdict.Kind)
	}

	// --by 로 덮겠다고 말하면 지나간다.
	byOpt := opt
	byOpt.SupersedeOf = live.ID
	if hasRule(Gate(fresh, byOpt).Findings, RuleDecisionGate) {
		t.Error("--by 를 줬는데도 관문이 막는다")
	}
	// --new 로 다른 주제라고 말해도 지나간다.
	newOpt := opt
	newOpt.AllowDuplicate = true
	if hasRule(Gate(fresh, newOpt).Findings, RuleDecisionGate) {
		t.Error("--new 를 줬는데도 관문이 막는다")
	}
	// 덮인 결정은 상대가 아니다.
	live.SupersededBy = "20260823-00000000"
	live.InvalidAt = "2026-08-22"
	if hasRule(Gate(fresh, opt).Findings, RuleDecisionGate) {
		t.Error("이미 덮인 결정을 살아 있는 상대로 본다")
	}
}

func TestDecisionGateSkipsOtherScope(t *testing.T) {
	live := goodMemory()
	live.ID = "20260822-1fae6641"
	live.Scope = "officina"
	fresh := goodMemory()
	fresh.ID = "20260823-99999999"
	fresh.Summary = "훅 예산을 절마다 다섯 줄에서 세 줄로 줄인다. 20k 저장소에서 주입이 8,000바이트를 넘겨서다"
	fresh.Title = "훅 예산을 세 줄로 줄인다"
	fresh.Body = "20k 저장소에서 주입 블록이 상한을 넘겼다.\n\n절마다 3줄로 줄인다.\n긴 절부터 깎는 규칙은 그대로다."
	opt := testOptions()
	opt.Repo = MemorySlice{live}
	if hasRule(Gate(fresh, opt).Findings, RuleDecisionGate) {
		t.Error("scope 가 다른 결정을 상대로 본다")
	}
}

// TestLegacyMixedStore 는 옛 규격(SpecV1) 기억이 섞인 저장소다. 그것들을 중복
// 후보로는 보되, v0.2 규격 검사는 새 기억에만 한다 (설계 2-5).
func TestLegacyMixedStore(t *testing.T) {
	old := &model.Memory{ID: "20260822-1fae6641", Type: model.TypeDecision,
		Summary: "훅 주입 블록은 UTF-8 8,000바이트에서 우리가 먼저 자른다. 10,000자 상한에 기대지 않는다",
		Tags:    []string{"hooks"}, Scope: "mem", Date: "2026-08-22",
		LegacySource: model.LegacySourceAI, Author: "claude-code/unknown", Spec: model.SpecV1,
		Body: goodMemory().Body}

	// 옛 기억 자체는 v0.2 필수 칸으로 안 잰다.
	oldVerdict := Gate(old, testOptions())
	for _, rule := range []string{RuleRequiredField, RuleSourcesRequired, RuleTagCount} {
		if hasRule(oldVerdict.Findings, rule) {
			t.Errorf("옛 규격 기억에 v0.2 규칙 %s 를 들이댔다", rule)
		}
	}
	// 그래도 중복 후보로는 본다.
	fresh := goodMemory()
	fresh.ID = "20260823-99999999"
	opt := testOptions()
	opt.Repo = MemorySlice{old}
	verdict := Gate(fresh, opt)
	if !hasRule(verdict.Findings, RuleSameBody) && !hasRule(verdict.Findings, RuleDuplicateHard) {
		t.Fatalf("옛 규격 기억을 중복 후보로 안 봤다 : %v", verdict.Rules())
	}
}

// TestMigratedGrace 는 mem migrate 가 옮긴 기억의 첫 판 유예다 (설계 2-5).
func TestMigratedGrace(t *testing.T) {
	memory := goodMemory()
	memory.Migrated = true
	memory.Tags = []string{"hook"}
	verdict := Gate(memory, testOptions())
	if !hasRule(verdict.Findings, RuleTagCount) {
		t.Fatal("tag-count 는 여전히 걸려야 한다")
	}
	if verdict.Kind != KindWarn {
		t.Fatalf("이전분은 경고로 본다 : %v", verdict.Kind)
	}
}

func TestTagAliasFixed(t *testing.T) {
	memory := goodMemory()
	memory.Tags = []string{"hooks", "security-design"}
	memory.Scope = "mem-search"
	verdict := Gate(memory, testOptions())
	if len(verdict.Fixes) != 3 {
		t.Fatalf("별칭 치환이 셋이어야 한다 : %+v", verdict.Fixes)
	}
	if got := strings.Join(verdict.Memory.Tags, ","); got != "hook,security" {
		t.Errorf("태그가 표준으로 안 바뀌었다 : %s", got)
	}
	if verdict.Memory.Scope != "aimemorytool" {
		t.Errorf("scope 가 표준으로 안 바뀌었다 : %s", verdict.Memory.Scope)
	}
	if hasRule(verdict.Findings, RuleTagStandard) {
		t.Error("치환된 태그를 표준 밖이라고 한다")
	}
}

// TestDemotedRuleLowersGrade 는 정밀도가 낮은 규칙을 경고로 내리는 자리다.
func TestDemotedRuleLowersGrade(t *testing.T) {
	memory := goodMemory()
	memory.Tags = []string{"hook"}
	opt := testOptions()
	opt.Demoted = map[string]Grade{RuleTagCount: GradeWarn}
	if kind := Gate(memory, opt).Kind; kind != KindWarn {
		t.Fatalf("강등된 규칙은 경고여야 한다 : %v", kind)
	}
}

func TestSecurityRuleCannotBeDemoted(t *testing.T) {
	memory := goodMemory()
	memory.Body += "\n토큰은 ghp_" + strings.Repeat("a1B2", 6) + " 였다."
	opt := testOptions()
	opt.Demoted = map[string]Grade{RuleSecretPattern: GradeWarn}
	if kind := Gate(memory, opt).Kind; kind != KindSecurityReject {
		t.Fatalf("보안 규칙은 못 내린다 : %v", kind)
	}
}

func TestDemotedRoundTrip(t *testing.T) {
	base := "[quality]\ndup_reject = 0.72\n\n[hook]\nmax_bytes = 8000\n"
	table := map[string]Grade{RuleMultiTable: GradeWarn, RuleNotationDrift: GradeOff}
	text := WriteDemoted(base, table, map[string]string{RuleMultiTable: "정밀도 0.620 < 0.80"})
	back := ParseDemoted(text)
	if len(back) != 2 || back[RuleMultiTable] != GradeWarn || back[RuleNotationDrift] != GradeOff {
		t.Fatalf("강등표 왕복이 깨졌다 : %v\n%s", back, text)
	}
	if !strings.Contains(text, "max_bytes = 8000") || !strings.Contains(text, "dup_reject = 0.72") {
		t.Fatalf("다른 절이 사라졌다 :\n%s", text)
	}
	// 두 번 써도 같은 글이어야 한다.
	if again := WriteDemoted(text, table, nil); ParseDemoted(again)[RuleMultiTable] != GradeWarn {
		t.Fatalf("두 번째 쓰기가 깨졌다 :\n%s", again)
	}
}

func TestCatalogNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, rule := range Catalog {
		if seen[rule.Name] {
			t.Errorf("규칙 이름이 겹친다 : %s", rule.Name)
		}
		seen[rule.Name] = true
		if rule.Security() && rule.Demotable {
			t.Errorf("보안 규칙 %s 를 내릴 수 있게 뒀다", rule.Name)
		}
	}
	if len(RuleNames()) != len(Catalog) {
		t.Error("RuleNames 가 카탈로그와 어긋난다")
	}
}
