// Package quality 는 기억이 저장소에 들어오기 전에 막는 관문이다.
//
// v0.1 은 품질 장치를 lint(사후)에 몰아 뒀고, 실제 기억 206건은 「들어오고 나면
// 아무도 안 고친다」를 보여 줬다. 그래서 v0.2 는 무게중심을 add(사전)로 옮긴다
// (설계 3절).
//
// 여기 있는 검사 함수는 add 관문과 lint 가 같이 쓴다 — 같은 규칙이 두 곳에
// 따로 적히면 반드시 어긋난다. lint·eval 은 이 패키지의 규칙 이름 상수를 쓴다.
package quality

import "github.com/mirusona/officina-ai-memory-tool/internal/model"

// 규칙 이름. --json · 골든셋 · lint 보고가 다 이 이름으로 말한다.
// 딸림 문서 `2026-08-23-v0.2-품질규칙표.md` 1절의 코드(F01…X03)와 짝이다.
const (
	// F 계열 — 머리말·규격. 전부 기계가 100% 판정한다.
	RuleFrontMatter     = "front-matter"      // F01
	RuleRequiredField   = "required-field"    // F02
	RuleIDShape         = "id-shape"          // F03
	RuleTypeValue       = "type-value"        // F04
	RuleSummaryLen      = "summary-len"       // F05
	RuleTitleShape      = "title-shape"       // F06
	RuleTitleNotGeneric = "title-not-generic" // F07
	RuleTagCount        = "tag-count"         // F08
	RuleTagStandard     = "tag-standard"      // F09
	RuleTagShape        = "tag-shape"         // F09-2 · 규격을 벗어난 태그 글자
	RuleTagNotSource    = "tag-not-source"    // F10
	RuleTagTooBroad     = "tag-too-broad"     // F11
	RuleScopeStandard   = "scope-standard"    // F12
	RuleScopeSkew       = "scope-skew"        // F13
	RuleTodoStatus      = "todo-status"       // F14
	RuleSeverity        = "severity"          // F15
	RuleSourcesRequired = "sources-required"  // F16
	RuleSourcesShape    = "sources-shape"     // F17
	RuleSourcesNoteOnly = "sources-note-only" // F18
	RuleAuthorShape     = "author-shape"      // F19
	RuleDateShape       = "date-shape"        // F20

	// B 계열 — 본문.
	RuleBodyThin            = "body-thin"             // B01
	RuleBodyLong            = "body-long"             // B02
	RuleFirstLineConclusion = "first-line-conclusion" // B03
	RuleHeadingsMax         = "headings-max"          // B04
	RuleEmptySection        = "empty-section"         // B05
	RuleIssueSections       = "issue-sections"        // B06
	RuleHowtoNumbered       = "howto-numbered"        // B07
	RuleSummaryBodyMatch    = "summary-body-match"    // B08
	RuleRelativeDate        = "relative-date"         // B09
	RuleUnfixedMarker       = "unfixed-marker"        // B10
	RuleBodyMax             = "body-max"              // B11
	RuleNoValue             = "no-value"              // B12

	// M 계열 — 여러 사실 섞임.
	RuleMultiTable   = "multi-table"   // M01
	RuleMultiSummary = "multi-summary" // M02
	RuleMultiHeading = "multi-heading" // M03

	// C 계열 — 중복·모순·낡음.
	RuleDuplicateHard        = "duplicate-hard"         // C01
	RuleDuplicateSoft        = "duplicate-soft"         // C02
	RuleSameBody             = "same-body"              // C03
	RuleDecisionGate         = "decision-gate"          // C04
	RuleDecisionConflictLive = "decision-conflict-live" // C05
	RuleStaleSourceChanged   = "stale-source-changed"   // C06
	RuleStaleAfterPassed     = "stale-after-passed"     // C07
	RuleSupersedeMissing     = "supersede-missing"      // C08
	RuleCold                 = "cold"                   // C09
	RuleOrphan               = "orphan"                 // C10
	RuleNotationDrift        = "notation-drift"         // C11
	RuleStaleAge             = "stale-age"              // C12
	RuleStaleConflictPair    = "stale-conflict-pair"    // C13
	RuleStaleBasis           = "stale-basis"            // C14

	// D 계열 — 링크·경로.
	RuleDeadMemLink = "dead-mem-link" // D01
	RuleDeadPath    = "dead-path"     // D02
	RuleDeadCommit  = "dead-commit"   // D03
	RuleLinkMissing = "link-missing"  // D04

	// E 계열 — 보안. 전부 거절이고 경고로 못 내린다.
	RuleSecretPattern       = "secret-pattern"       // E01
	RuleSecretEntropy       = "secret-entropy"       // E02
	RulePrivatePath         = "private-path"         // E03
	RuleShadowExe           = "shadow-exe"           // E04
	RuleInjectionNeutralize = "injection-neutralize" // E05
	RuleInboxBad            = "inbox-bad"            // E06

	// X 계열 — 인코딩. 셋 다 자동 고침이라 거절이 아니다.
	RuleEncoding = "encoding" // X01
	RuleBOM      = "bom"      // X02
	RuleCRLF     = "crlf"     // X03
)

// Grade 는 규칙 하나의 등급이다. 정밀도가 낮은 규칙은 이 등급이 스스로 내려간다
// (설계 결정 14).
type Grade string

const (
	// GradeReject 는 저장을 막는다. add 는 종료 코드 2(보안이면 4)로 끝난다.
	GradeReject Grade = "reject"
	// GradeWarn 은 넣되 알린다.
	GradeWarn Grade = "warn"
	// GradeCandidate 는 사람이 판정할 자리만 좁힌다 (mem review 큐).
	GradeCandidate Grade = "candidate"
	// GradeOff 는 꺼진 규칙이다. 정밀도가 PrecisionOff 아래면 여기로 간다.
	GradeOff Grade = "off"
)

// Stage 는 규칙이 언제 도는지다.
type Stage string

const (
	StageAdd  Stage = "add"  // 넣을 때만 본다
	StageLint Stage = "lint" // 뒤에 훑을 때만 본다 (시간이 지나야 아는 것)
	StageBoth Stage = "both"
)

// 골든셋 유형 코드 (조사 D 딸림 표 1절). 규칙 하나가 여러 유형을 맡을 수 있다.
const (
	TypeDup      = "DUP"
	TypeConflict = "CONFLICT"
	TypeStale    = "STALE"
	TypeMulti    = "MULTI"
	TypeThin     = "THIN"
	TypeSummary  = "SUMMARY"
	TypeTag      = "TAG"
	TypeSrc      = "SRC"
	TypeScope    = "SCOPE"
	TypeKind     = "TYPE"
	TypeNoValue  = "NOVALUE"
	TypeFormat   = "FORMAT"
	TypeNotation = "NOTATION"
	TypeField    = "FIELD"
)

// MachineTypes 는 G6 ① 이 재현율 0.90 을 요구하는 「기계 규칙군」이다.
// 나머지(CONFLICT·STALE·NOVALUE·NOTATION·TYPE)는 사람 판정이 섞인다.
var MachineTypes = []string{TypeField, TypeTag, TypeThin, TypeFormat, TypeMulti, TypeDup, TypeSrc}

// Rule 은 규칙 하나의 메타다. 등급·언제 도나·자동 강등이 되나.
type Rule struct {
	Name string
	Code string // F01 … X03
	// Family 는 관문 단계다 — F·B·M·C·D·E·X.
	Family string
	Stage  Stage
	Grade  Grade
	// DecisionGrade 가 비지 않으면 「한 건에 하나」 종류(기본 decision·fact)에 이 등급을 쓴다.
	// 결정 묶음은 하나가 낡으면 나머지도 못 믿어서 반드시 쪼갠다 (설계 결정 8).
	DecisionGrade Grade
	// Demotable 이 거짓이면 정밀도가 낮아도 등급을 못 내린다. 보안(E)이 그렇다 —
	// 되돌릴 수 없는 유출이라 오탐 몇 건과 바꿀 수 없다.
	Demotable bool
	// Types 는 이 규칙이 잡으라고 골든셋이 적어 둔 유형이다.
	Types []string
}

// Catalog 는 규칙 전부다. 차례가 곧 보고 표의 차례다.
var Catalog = []Rule{
	{RuleFrontMatter, "F01", "F", StageBoth, GradeReject, "", false, []string{TypeFormat}},
	{RuleRequiredField, "F02", "F", StageBoth, GradeReject, "", false, []string{TypeField}},
	{RuleIDShape, "F03", "F", StageBoth, GradeReject, "", false, []string{TypeFormat}},
	{RuleTypeValue, "F04", "F", StageBoth, GradeReject, "", false, []string{TypeFormat}},
	{RuleSummaryLen, "F05", "F", StageBoth, GradeReject, "", false, []string{TypeFormat}},
	{RuleTitleShape, "F06", "F", StageBoth, GradeReject, "", true, []string{TypeField}},
	{RuleTitleNotGeneric, "F07", "F", StageBoth, GradeWarn, "", true, []string{TypeField}},
	{RuleTagCount, "F08", "F", StageBoth, GradeReject, "", true, []string{TypeTag}},
	{RuleTagStandard, "F09", "F", StageBoth, GradeReject, "", true, []string{TypeTag}},
	// F09-2 는 「목록 밖」이 아니라 「규격 밖」이다. model.Validate 가 승격 때
	// 무조건 막는 자라, 관문이 경고로 받으면 기억이 조용히 inbox/bad 로 간다 (W3).
	{RuleTagShape, "F09-2", "F", StageBoth, GradeReject, "", false, []string{TypeTag}},
	{RuleTagNotSource, "F10", "F", StageBoth, GradeReject, "", true, []string{TypeTag}},
	{RuleTagTooBroad, "F11", "F", StageLint, GradeWarn, "", true, []string{TypeTag}},
	{RuleScopeStandard, "F12", "F", StageBoth, GradeReject, "", true, []string{TypeScope}},
	{RuleScopeSkew, "F13", "F", StageLint, GradeWarn, "", true, []string{TypeScope}},
	{RuleTodoStatus, "F14", "F", StageBoth, GradeReject, "", false, []string{TypeFormat}},
	{RuleSeverity, "F15", "F", StageBoth, GradeReject, "", false, []string{TypeFormat}},
	{RuleSourcesRequired, "F16", "F", StageBoth, GradeReject, "", true, []string{TypeSrc}},
	{RuleSourcesShape, "F17", "F", StageBoth, GradeReject, "", true, []string{TypeSrc}},
	{RuleSourcesNoteOnly, "F18", "F", StageBoth, GradeWarn, "", true, []string{TypeSrc}},
	{RuleAuthorShape, "F19", "F", StageBoth, GradeReject, "", false, []string{TypeField}},
	{RuleDateShape, "F20", "F", StageBoth, GradeReject, "", false, []string{TypeFormat}},

	{RuleBodyThin, "B01", "B", StageBoth, GradeReject, "", true, []string{TypeThin}},
	{RuleBodyLong, "B02", "B", StageBoth, GradeWarn, "", true, nil},
	{RuleFirstLineConclusion, "B03", "B", StageBoth, GradeWarn, "", true, []string{TypeSummary}},
	{RuleHeadingsMax, "B04", "B", StageBoth, GradeWarn, "", true, []string{TypeMulti}},
	{RuleEmptySection, "B05", "B", StageBoth, GradeReject, "", true, []string{TypeFormat}},
	{RuleIssueSections, "B06", "B", StageBoth, GradeReject, "", true, []string{TypeFormat}},
	{RuleHowtoNumbered, "B07", "B", StageBoth, GradeReject, "", true, []string{TypeFormat}},
	// (v0.4 리뷰 B) B08 은 **어느 종류에서도 경고**다. 한때 결정에서만 거절로
	// 올렸다가 되돌렸다 — 실기억 206건에서 결정 거절 5건 중 둘이 오탐이었고
	// 둘 다 사람이 확정해 pinned 로 박은 결정이었다. 요약과 본문이 같은 말을
	// 다른 낱말로 적은 것(「색인·SQLite」 ↔ `index.db`)을 이름씨 대조가 못 본다.
	// 다음 판 후보 : norm/canon 을 태운 이름씨 대조로 오탐을 없앤 뒤 다시 올린다.
	{RuleSummaryBodyMatch, "B08", "B", StageBoth, GradeWarn, "", true, []string{TypeSummary}},
	// (v0.4) B09 는 종류를 안 가리고 거절이다. 「어제」·「지난주」는 반년 뒤에 그냥
	// 거짓말이 되고 고치는 값도 싸다(날짜 한 줄). 정밀도 1.000 · 좋은 10 에 0건 ·
	// 대조군 거짓 경보 0 을 재고 올렸다 (설계 결정 12).
	{RuleRelativeDate, "B09", "B", StageBoth, GradeReject, "", true, []string{TypeStale}},
	{RuleUnfixedMarker, "B10", "B", StageLint, GradeCandidate, "", true, []string{TypeStale}},
	// B11 은 본문 상한이다. B02 가 「길다」를 경고로 알리고, 이 규칙이 상한을
	// 넘은 것을 거절한다. 등급이 다르니 규칙도 갈라야 한다 — 하나에 두 등급을
	// 담으면 강등표(설계 결정 14)가 둘을 같이 내려 버린다.
	{RuleBodyMax, "B11", "B", StageBoth, GradeReject, "", true, []string{TypeFormat}},
	// B12 는 「여섯 달 뒤에 이 기억이 쓸모가 있나」다 (설계 결정 37). 짧다가
	// 아니라 **확인 가능한 조각이 있나**를 본다. 사람이 판정할 자리라 경고다.
	{RuleNoValue, "B12", "B", StageBoth, GradeWarn, "", true, []string{TypeNoValue}},

	{RuleMultiTable, "M01", "M", StageBoth, GradeWarn, GradeReject, true, []string{TypeMulti}},
	{RuleMultiSummary, "M02", "M", StageBoth, GradeWarn, GradeReject, true, []string{TypeMulti}},
	{RuleMultiHeading, "M03", "M", StageBoth, GradeWarn, "", true, []string{TypeMulti}},

	{RuleDuplicateHard, "C01", "C", StageBoth, GradeReject, "", true, []string{TypeDup}},
	{RuleDuplicateSoft, "C02", "C", StageBoth, GradeWarn, "", true, []string{TypeDup}},
	{RuleSameBody, "C03", "C", StageBoth, GradeReject, "", true, []string{TypeDup}},
	{RuleDecisionGate, "C04", "C", StageAdd, GradeReject, "", true, []string{TypeConflict}},
	{RuleDecisionConflictLive, "C05", "C", StageLint, GradeCandidate, "", true, []string{TypeConflict}},
	{RuleStaleSourceChanged, "C06", "C", StageLint, GradeCandidate, "", true, []string{TypeStale}},
	{RuleStaleAfterPassed, "C07", "C", StageLint, GradeReject, "", true, []string{TypeStale}},
	{RuleSupersedeMissing, "C08", "C", StageLint, GradeCandidate, "", true, []string{TypeStale}},
	{RuleCold, "C09", "C", StageLint, GradeWarn, "", true, []string{TypeNoValue}},
	{RuleOrphan, "C10", "C", StageLint, GradeWarn, "", true, []string{TypeNoValue}},
	// C11 은 경고다 (설계 결정 26). 어느 쪽이 정본인지는 코드가 못 가르지만
	// 「저장소가 같은 수를 갈라 적고 있다」는 기계가 확실히 아는 사실이다.
	{RuleNotationDrift, "C11", "C", StageLint, GradeWarn, "", true, []string{TypeNotation}},
	{RuleStaleAge, "C12", "C", StageLint, GradeCandidate, "", true, []string{TypeStale}},
	{RuleStaleConflictPair, "C13", "C", StageLint, GradeCandidate, "", true, []string{TypeStale, TypeConflict}},
	// C14 는 근거로 삼은 기억이 죽은 것이다. 「그래서 이 기억이 틀렸나」는
	// 코드가 못 가리므로 후보 등급이고, 사람이 review 큐에서 판정한다.
	{RuleStaleBasis, "C14", "C", StageLint, GradeCandidate, "", true, []string{TypeStale}},

	{RuleDeadMemLink, "D01", "D", StageBoth, GradeReject, "", true, nil},
	{RuleDeadPath, "D02", "D", StageBoth, GradeWarn, "", true, nil},
	{RuleDeadCommit, "D03", "D", StageLint, GradeWarn, "", true, nil},
	{RuleLinkMissing, "D04", "D", StageLint, GradeCandidate, "", true, nil},

	{RuleSecretPattern, "E01", "E", StageBoth, GradeReject, "", false, nil},
	{RuleSecretEntropy, "E02", "E", StageBoth, GradeWarn, "", false, nil},
	{RulePrivatePath, "E03", "E", StageBoth, GradeWarn, "", false, nil},
	{RuleShadowExe, "E04", "E", StageBoth, GradeReject, "", false, nil},
	{RuleInjectionNeutralize, "E05", "E", StageBoth, GradeWarn, "", false, nil},
	{RuleInboxBad, "E06", "E", StageLint, GradeReject, "", false, nil},

	{RuleEncoding, "X01", "X", StageBoth, GradeWarn, "", false, nil},
	{RuleBOM, "X02", "X", StageBoth, GradeWarn, "", false, nil},
	{RuleCRLF, "X03", "X", StageBoth, GradeWarn, "", false, nil},
}

var catalogByName = func() map[string]Rule {
	table := make(map[string]Rule, len(Catalog))
	for _, rule := range Catalog {
		table[rule.Name] = rule
	}
	return table
}()

// Lookup 은 이름으로 규칙 메타를 찾는다.
func Lookup(name string) (Rule, bool) {
	rule, ok := catalogByName[name]
	return rule, ok
}

// RuleNames 는 규칙 이름 전부다. 차례는 Catalog 그대로다.
func RuleNames() []string {
	names := make([]string, 0, len(Catalog))
	for _, rule := range Catalog {
		names = append(names, rule.Name)
	}
	return names
}

// GradeFor 는 이 기억 종류에 대한 규칙의 기본 등급이다. 「한 건에 하나」를
// 요구하는 종류가 decision 자를 쓴다.
func (r Rule) GradeFor(kind model.TypeSpec) Grade {
	if r.DecisionGrade != "" && kind.OneThing {
		return r.DecisionGrade
	}
	return r.Grade
}

// Security 는 보안 규칙(E 계열)인지다. 거절이면 종료 코드가 2가 아니라 4다.
func (r Rule) Security() bool { return r.Family == "E" }
