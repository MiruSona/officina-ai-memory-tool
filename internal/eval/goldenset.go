// Package eval 은 골든셋으로 검색 품질을 잰다 — recall@k · MRR · abstain
// 정답률 · 오배제율 (설계 9-3).
package eval

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// Version 은 이 exe 가 읽는 골든셋 형식이다. v0.2 가 3 으로 올렸다 —
// `variant`·`expect_mode` 칸이 늘었다 (설계 2-5 · 조사 F M7).
const Version = 4

// FileName 은 저장소 안의 골든셋 파일이다.
const FileName = "goldenset.yaml"

// 다섯 가지 질문 종류 (설계 9-3).
const (
	KindExtract      = "extract"
	KindMultisession = "multisession"
	KindTemporal     = "temporal"
	KindUpdate       = "update"
	KindAbstain      = "abstain"
)

// Kinds 는 보고서가 찍는 순서다.
var Kinds = []string{KindExtract, KindMultisession, KindTemporal, KindUpdate, KindAbstain}

// 질의 변이 세 꼴 (설계 4-9). ②③ 이 지금 다 틀리는 유형이다.
const (
	VariantLiteral    = "literal"
	VariantParaphrase = "paraphrase"
	VariantLongform   = "longform"
)

// Variants 는 보고서가 찍는 순서다.
var Variants = []string{VariantLiteral, VariantParaphrase, VariantLongform}

// 정답 판정 방식. multisession 은 반드시 all 이다 — 하나만 맞혀서는 「여러
// 세션을 이었다」고 못 한다 (설계 결정 22).
const (
	ExpectAny = "any"
	ExpectAll = "all"
)

// Case 는 골든셋 질문 하나다.
type Case struct {
	Q      string   `yaml:"q" json:"q"`
	Kind   string   `yaml:"kind" json:"kind"`
	Hard   bool     `yaml:"hard,omitempty" json:"hard,omitempty"`
	Lang   string   `yaml:"lang,omitempty" json:"lang,omitempty"`
	Expect []string `yaml:"expect" json:"expect"`
	// Variant 는 질의를 어떻게 썼나다 — 문서 낱말 그대로(literal) · 사람 말로
	// 바꿔 쓴 것(paraphrase) · 상황까지 설명한 긴 것(longform).
	Variant string `yaml:"variant,omitempty" json:"variant,omitempty"`
	// ExpectMode 는 expect 를 다 맞혀야 하는지(all)다. 안 적으면 any 다.
	ExpectMode string `yaml:"expect_mode,omitempty" json:"expect_mode,omitempty"`
	// NotExpect 는 나오면 안 되는 id 다. 오배제·헛답을 잡는다.
	NotExpect []string `yaml:"not_expect,omitempty" json:"not_expect,omitempty"`
	// Unreachable 은 정답 문서에 질의 낱말이 한 번도 없는 질문이다. 낱말
	// 검색으로는 원리상 못 맞히므로 합격선 계산에서 뺀다 (설계 9-3).
	Unreachable bool `yaml:"unreachable,omitempty" json:"unreachable,omitempty"`
	// 사람이 --type/--tag/--since 를 붙여 묻는 것과 같은 조건이다.
	Type  string   `yaml:"type,omitempty" json:"type,omitempty"`
	Tags  []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	Since string   `yaml:"since,omitempty" json:"since,omitempty"`
}

// LangOf 는 안 적었으면 한국어로 본다.
func (c *Case) LangOf() string {
	if c.Lang == "" {
		return "ko"
	}
	return c.Lang
}

// VariantOf 는 안 적었으면 literal 로 본다.
func (c *Case) VariantOf() string {
	if c.Variant == "" {
		return VariantLiteral
	}
	return c.Variant
}

// ExpectModeOf 는 안 적었으면 any 다.
func (c *Case) ExpectModeOf() string {
	if c.ExpectMode == "" {
		return ExpectAny
	}
	return c.ExpectMode
}

// GoldenSet 은 파일 전체다.
type GoldenSet struct {
	Version int    `yaml:"version" json:"version"`
	Cases   []Case `yaml:"cases" json:"cases"`
	// Warnings 는 구성이 설계 9-3 의 최소 건수를 못 채웠을 때의 경고다.
	// 오류가 아니다 — 못 재는 것이 아니라 얕게 재는 것이라 보고서에 적는다.
	Warnings []string `yaml:"-" json:"-"`
}

// 설계 9-3 이 못 박은 유형별 최소 건수다. 이걸 안 지키면 자가 얕아진다 (리뷰B #13).
var minCases = map[string]int{
	KindExtract: 8, KindMultisession: 5, KindTemporal: 5, KindUpdate: 5, KindAbstain: 7,
}

// minEnglish 는 `lang: en` 질문의 최소 건수다. 골든셋을 42 → 80건으로 늘리면서
// 5 → 12 로 같이 올렸다. 안 올리면 자와 합격선이 어긋난다 (품질규칙표 4-1).
const minEnglish = 12

// checkShape 는 골든셋 구성을 설계 9-3 과 대조한다.
func checkShape(cases []Case) []string {
	byKind, english := map[string]int{}, 0
	for _, item := range cases {
		byKind[item.Kind]++
		if item.LangOf() == "en" {
			english++
		}
	}
	out := []string{}
	for _, kind := range Kinds {
		if byKind[kind] < minCases[kind] {
			out = append(out, i18n.T(i18n.EvalThinKind, kind, byKind[kind], minCases[kind]))
		}
	}
	if english < minEnglish {
		out = append(out, i18n.T(i18n.EvalThinLang, english, minEnglish))
	}
	return append(out, checkVariants(cases)...)
}

// minVariant 는 변이 꼴마다 있어야 하는 최소 건수다. 하나라도 0 이면 그 유형을
// 안 재고 있는 것이다 (설계 4-9).
const minVariant = 8

// checkVariants 는 변이 꼴이 골고루 들었는지 본다.
func checkVariants(cases []Case) []string {
	byVariant := map[string]int{}
	for _, item := range cases {
		byVariant[item.VariantOf()]++
	}
	out := []string{}
	for _, name := range Variants {
		if byVariant[name] < minVariant {
			out = append(out, fmt.Sprintf("변이 `%s` 가 %d건뿐이다. %d건은 있어야 그 꼴을 잰다",
				name, byVariant[name], minVariant))
		}
	}
	return out
}

// Error 는 이미 한국어 문장인 오류다. 평가를 못 한 것은 검사 실패다.
// Missing 은 「골든셋 파일이 아예 없다」 뿐인 경우다 — 검사 실패(2)가 아니라
// 「저장소에 있어야 할 것이 없다」라서 exit 3 이다 (v0.4 결정 17).
// 0 으로 끝내면 CI 가 「다 통과했다」로 읽는다 — 아무것도 안 쟀는데도.
type Error struct {
	Message string
	Missing bool
}

func (e *Error) Error() string { return e.Message }

// ExitCode 는 종료 코드다. 3 은 저장소 없음과 같은 칸을 쓴다 — 코드를 늘리면
// 부르는 쪽이 또 갈라야 한다.
func (e *Error) ExitCode() int {
	if e.Missing {
		return 3
	}
	return 2
}

// Path 는 저장소가 골든셋을 두는 자리다.
func Path(dir string) string {
	return filepath.Join(dir, "golden", FileName)
}

// Load 는 골든셋을 읽고 검사한다. 문제는 전부 한국어 한 문장이다.
func Load(path string) (*GoldenSet, error) {
	text, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, &Error{Message: i18n.T(i18n.EvalNoGolden, path), Missing: true}
	}
	if err != nil {
		return nil, err
	}
	set := GoldenSet{}
	if err := yaml.Unmarshal(text, &set); err != nil {
		return nil, &Error{Message: i18n.T(i18n.EvalBadGolden, path, err.Error())}
	}
	if set.Version > Version {
		return nil, &Error{Message: i18n.T(i18n.EvalBadVersion, Version, set.Version)}
	}
	if len(set.Cases) == 0 {
		return nil, &Error{Message: i18n.T(i18n.EvalNoCases, path)}
	}
	set.Warnings = checkShape(set.Cases)
	return &set, checkCases(path, set.Cases)
}

func checkCases(path string, cases []Case) error {
	for at, item := range cases {
		reason := reasonFor(at, item)
		if reason == "" {
			continue
		}
		return &Error{Message: i18n.T(i18n.EvalBadGolden, path, reason)}
	}
	return nil
}

func reasonFor(at int, item Case) string {
	if item.Q == "" {
		return i18n.T(i18n.EvalNoQuestion, at+1)
	}
	if !knownKind(item.Kind) {
		return i18n.T(i18n.EvalBadKind, item.Kind)
	}
	if item.Kind == KindAbstain && len(item.Expect) > 0 {
		return i18n.T(i18n.EvalAbstainWants, item.Q)
	}
	if item.Kind != KindAbstain && len(item.Expect) == 0 {
		return i18n.T(i18n.EvalNeedExpect, item.Q)
	}
	if mode := item.ExpectModeOf(); mode != ExpectAny && mode != ExpectAll {
		return fmt.Sprintf("`%s` 의 expect_mode 가 `%s` 다. any 나 all 이라야 한다", item.Q, mode)
	}
	if item.Kind == KindMultisession && item.ExpectModeOf() != ExpectAll {
		return fmt.Sprintf("`%s` 는 multisession 인데 expect_mode 가 all 이 아니다", item.Q)
	}
	if variant := item.VariantOf(); !knownVariant(variant) {
		return fmt.Sprintf("`%s` 의 variant 가 `%s` 다. literal · paraphrase · longform 셋 중 하나라야 한다",
			item.Q, variant)
	}
	return ""
}

func knownVariant(name string) bool {
	for _, known := range Variants {
		if known == name {
			return true
		}
	}
	return false
}

func knownKind(kind string) bool {
	for _, known := range Kinds {
		if known == kind {
			return true
		}
	}
	return false
}
