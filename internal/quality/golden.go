package quality

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// GoldenFileName 은 품질 골든셋 파일 이름이다 (설계 6-3 `golden/quality.yaml`).
const GoldenFileName = "quality.yaml"

// GoldenCase 는 골든셋 한 줄이다. 규격은 품질규칙표 4-2 절과 파일 머리말에 있다.
type GoldenCase struct {
	ID      string   `yaml:"id" json:"id"`
	Defects []string `yaml:"defects" json:"defects"`
	// Rules 는 이 결함을 잡아야 할 규칙이다. 여럿 중 하나만 켜져도 잡은 것이다.
	Rules  []string `yaml:"rules,omitempty" json:"rules,omitempty"`
	Expect string   `yaml:"expect" json:"expect"`
	Note   string   `yaml:"note,omitempty" json:"note,omitempty"`
	// HumanOnly 는 코드가 못 가리는 유형이다. 재현율 셈에서 뺀다.
	HumanOnly []string `yaml:"human_only,omitempty" json:"human_only,omitempty"`
	// AllowRules 는 대조군인데 이 규칙만은 켜져도 거짓 경보로 안 세는 것이다.
	AllowRules []string `yaml:"allow_rules,omitempty" json:"allow_rules,omitempty"`
}

// GoldenMeta 는 골든셋 전체에 걸리는 조건이다.
type GoldenMeta struct {
	Version    int    `yaml:"version" json:"version"`
	RulesMatch string `yaml:"rules_match" json:"rules_match"`
	// PreMigration 이 참이면 저장소가 아직 v0.1 규격이다. 그래도 v0.2 자로 잰다 —
	// 그러라고 만든 자다. 대신 아래 목록이 대조군에서 눈감을 규칙을 정한다.
	PreMigration        bool     `yaml:"pre_migration" json:"pre_migration"`
	ExcludeRulesOnClean []string `yaml:"exclude_rules_on_clean" json:"exclude_rules_on_clean"`
	Store               string   `yaml:"store" json:"store"`
}

// GoldenSet 은 품질 골든셋 한 벌이다.
type GoldenSet struct {
	Meta  GoldenMeta   `yaml:"meta" json:"meta"`
	Flag  []GoldenCase `yaml:"flag" json:"flag"`
	Clean []GoldenCase `yaml:"clean" json:"clean"`
	// Path 는 읽어 온 자리다. store 상대경로를 푸는 데 쓴다.
	Path string `yaml:"-" json:"-"`
}

// LoadGolden 은 quality.yaml 을 읽는다.
func LoadGolden(path string) (*GoldenSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	set := GoldenSet{}
	if err := yaml.Unmarshal(data, &set); err != nil {
		return nil, fmt.Errorf("%s : %w", path, err)
	}
	set.Path = path
	return &set, nil
}

// StoreDir 은 골든셋이 가리키는 기억 저장소 폴더다.
func (g *GoldenSet) StoreDir() string {
	if g.Meta.Store == "" {
		return ""
	}
	if filepath.IsAbs(g.Meta.Store) {
		return g.Meta.Store
	}
	return filepath.Clean(g.Meta.Store)
}

// LoadMemories 는 폴더와 그 아래 폴더의 `*.md` 를 다 읽는다. 채점용이라 store
// 패키지의 락·경로 감옥을 안 지나간다 — 읽기만 하고 아무것도 안 고친다.
//
// **얕게 읽으면 안 된다** (리뷰 B). 진짜 저장소의 기억은 `store/YYYY/MM/` 아래에
// 있어서 폴더 하나만 보면 「저장소 0건」이 나오고, 채점표가 통째로 0.000 이 된다.
func LoadMemories(dir string) ([]*model.Memory, error) {
	memories := []*model.Memory{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		memory, parseErr := model.Parse(data)
		if parseErr != nil {
			return fmt.Errorf("%s : %w", entry.Name(), parseErr)
		}
		memories = append(memories, memory)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(memories, func(a, b int) bool { return memories[a].ID < memories[b].ID })
	return memories, nil
}

// RuleScore 는 규칙 하나의 채점이다.
//
// 정밀도는 대조군에 대고 잰다. 골든셋이 유형별 전수가 아니라서, 잡을 것 쪽에서
// 표에 안 적힌 규칙이 켜진 것을 오탐으로 세면 안 된다 (골든셋 머리말).
type RuleScore struct {
	Rule string `json:"rule"`
	// Fired 는 이 규칙이 켜진 기억 수다.
	Fired int `json:"fired"`
	// True 는 그중 골든셋이 「이 규칙으로 잡으라」고 적은 것이다.
	True int `json:"true"`
	// False 는 대조군에서 켜진 것이다 (눈감기로 한 것은 뺐다).
	False int `json:"false"`
	// Precision 은 True / (True + False) 다. 셀 수 없으면 -1.
	Precision float64 `json:"precision"`
	// Grade 는 정밀도를 보고 매긴 등급이다.
	Grade Grade `json:"grade"`
	// Note 는 왜 등급이 내려갔는지다.
	Note string `json:"note,omitempty"`
}

// TypeScore 는 결함 유형 하나의 재현율이다.
type TypeScore struct {
	Type   string   `json:"type"`
	Total  int      `json:"total"`
	Caught int      `json:"caught"`
	Recall float64  `json:"recall"`
	Missed []string `json:"missed,omitempty"`
	// Machine 이 참이면 G6 ① 이 재현율 0.90 을 요구하는 기계 규칙군이다.
	Machine bool `json:"machine"`
}

// FalseAlarm 은 대조군에서 켜진 규칙 하나다. 0 이 목표다 (G6 ②).
type FalseAlarm struct {
	ID    string   `json:"id"`
	Rules []string `json:"rules"`
}

// GoldenReport 는 `mem eval --quality` 가 찍는 표다.
type GoldenReport struct {
	Rules       []RuleScore  `json:"rules"`
	Types       []TypeScore  `json:"types"`
	FalseAlarms []FalseAlarm `json:"false_alarms"`
	// Demote 는 정밀도를 보고 내린 등급이다. mem.toml [quality.demoted] 에 적는다.
	Demote map[string]Grade `json:"demote,omitempty"`
	// G6 판정 셋.
	MachineRecallOK bool `json:"machine_recall_ok"`
	NoFalseAlarm    bool `json:"no_false_alarm"`
	PrecisionOK     bool `json:"precision_ok"`
	// Missing 은 골든셋이 이름을 댔지만 카탈로그에 없는 규칙이다.
	Missing []string `json:"missing_rules,omitempty"`
}

// Pass 는 G6 ①~③ 을 다 넘겼는지다. ④(새 기억 결함률)는 저장소 통계라 여기서
// 안 잰다 — `mem status --quality` 의 S06 이다.
func (r GoldenReport) Pass() bool {
	return r.MachineRecallOK && r.NoFalseAlarm && r.PrecisionOK
}

// MachineRecallFloor·PrecisionFloor 는 G6 합격선이다 (설계 1절).
const (
	MachineRecallFloor = 0.90
	PrecisionFloor     = 0.80
)

// ScoreGolden 은 저장소에 규칙을 다 돌리고 규칙별 정밀도·유형별 재현율·대조군
// 거짓 경보를 낸다.
func ScoreGolden(set *GoldenSet, memories []*model.Memory, opt RepoOptions) GoldenReport {
	if set.Meta.PreMigration && opt.Spec == 0 {
		// 이전 전 저장소를 새 자로 잰다. 그러라고 만든 자다.
		opt.Spec = model.SpecV2
	}
	fired := FiredRules(memories, opt)
	report := GoldenReport{Demote: map[string]Grade{}}
	report.Missing = missingRules(set)
	report.Types = typeScores(set, fired)
	report.FalseAlarms = falseAlarms(set, fired)
	report.Rules = ruleScores(set, fired, opt.Config.Quality)

	report.MachineRecallOK = true
	for _, one := range report.Types {
		if one.Machine && one.Recall < MachineRecallFloor {
			report.MachineRecallOK = false
		}
	}
	report.NoFalseAlarm = len(report.FalseAlarms) == 0
	report.PrecisionOK = true
	for _, one := range report.Rules {
		if one.Precision >= 0 && one.Precision < PrecisionFloor {
			report.PrecisionOK = false
		}
		if one.Grade != "" && one.Grade != GradeReject {
			report.Demote[one.Rule] = one.Grade
		}
	}
	return report
}

// FiredRules 는 기억마다 켜진 규칙 이름 집합이다. add 관문 검사와 저장소 전체
// 검사를 다 돌린다.
func FiredRules(memories []*model.Memory, opt RepoOptions) map[string]map[string]bool {
	fired := map[string]map[string]bool{}
	mark := func(id, rule string) {
		if id == "" {
			return
		}
		if fired[id] == nil {
			fired[id] = map[string]bool{}
		}
		fired[id][rule] = true
	}
	// 저장소 전체 검사는 중복까지 여기서 다 본다. 관문 쪽 Repo 는 안 붙인다 —
	// 붙이면 같은 중복을 두 번 세고 값도 두 배다.
	single := opt
	single.Repo = nil
	for _, m := range memories {
		for _, one := range Check(m, single.Options) {
			mark(m.ID, one.Rule)
		}
	}
	report := CheckRepo(memories, opt)
	for id, list := range report.ByID {
		for _, one := range list {
			mark(id, one.Rule)
		}
	}
	return fired
}

func missingRules(set *GoldenSet) []string {
	seen := map[string]bool{}
	for _, one := range append(append([]GoldenCase{}, set.Flag...), set.Clean...) {
		for _, rule := range one.Rules {
			if _, ok := Lookup(rule); !ok {
				seen[rule] = true
			}
		}
	}
	names := []string{}
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func typeScores(set *GoldenSet, fired map[string]map[string]bool) []TypeScore {
	order := []string{}
	byType := map[string]*TypeScore{}
	for _, one := range set.Flag {
		caught := caughtBy(one, fired[one.ID])
		for _, defect := range one.Defects {
			if hasString(one.HumanOnly, defect) {
				continue
			}
			score := byType[defect]
			if score == nil {
				score = &TypeScore{Type: defect, Machine: hasString(MachineTypes, defect)}
				byType[defect] = score
				order = append(order, defect)
			}
			score.Total++
			if caught {
				score.Caught++
				continue
			}
			score.Missed = append(score.Missed, one.ID)
		}
	}
	sort.Strings(order)
	out := make([]TypeScore, 0, len(order))
	for _, defect := range order {
		score := byType[defect]
		if score.Total > 0 {
			score.Recall = float64(score.Caught) / float64(score.Total)
		}
		out = append(out, *score)
	}
	return out
}

// caughtBy 는 이 건이 잡혔는지다 — rules 중 하나라도 켜졌으면 잡은 것이다
// (`rules_match: any`).
func caughtBy(one GoldenCase, rules map[string]bool) bool {
	for _, rule := range one.Rules {
		if rules[rule] {
			return true
		}
	}
	return false
}

// preMigrationRules 는 이전 전 저장소라면 대조군에서 눈감는 규칙이다.
// 골든셋 머리말이 「이전 전 저장소의 규격 미달은 멀쩡한 기억을 잘못 잡은 것이
// 아니다」 라며 일곱을 뺐는데, 표준 태그 목록 대조가 그 목록에서 빠졌다.
// 대조군 20건이 쓰는 태그(`path` `confidence` `settings` …)는 아직 vocab.toml
// 에 없는 것뿐이라 성격이 tag-not-source 와 같다. mem migrate 와 mem tags 가
// 맡을 자리다.
var preMigrationRules = []string{RuleTagStandard}

// blindOnClean 은 대조군에서 이 규칙을 눈감을지다.
func blindOnClean(set *GoldenSet, one GoldenCase, flagged []string, rule string) bool {
	if hasString(set.Meta.ExcludeRulesOnClean, rule) || hasString(one.AllowRules, rule) || hasString(flagged, rule) {
		return true
	}
	return set.Meta.PreMigration && hasString(preMigrationRules, rule)
}

func falseAlarms(set *GoldenSet, fired map[string]map[string]bool) []FalseAlarm {
	// 같은 id 가 잡을 것에도 있으면 거기 적힌 규칙은 거짓 경보가 아니다.
	flagged := map[string][]string{}
	for _, one := range set.Flag {
		flagged[one.ID] = append(flagged[one.ID], one.Rules...)
	}
	alarms := []FalseAlarm{}
	for _, one := range set.Clean {
		hits := []string{}
		for _, rule := range Catalog {
			if !fired[one.ID][rule.Name] {
				continue
			}
			if blindOnClean(set, one, flagged[one.ID], rule.Name) {
				continue
			}
			hits = append(hits, rule.Name)
		}
		if len(hits) > 0 {
			alarms = append(alarms, FalseAlarm{ID: one.ID, Rules: hits})
		}
	}
	return alarms
}

func ruleScores(set *GoldenSet, fired map[string]map[string]bool, thresholds config.QualityConfig) []RuleScore {
	wanted := map[string]map[string]bool{} // 규칙 → 그 규칙으로 잡으라고 적힌 id
	for _, one := range set.Flag {
		for _, rule := range one.Rules {
			if wanted[rule] == nil {
				wanted[rule] = map[string]bool{}
			}
			wanted[rule][one.ID] = true
		}
	}
	clean := map[string]GoldenCase{}
	for _, one := range set.Clean {
		clean[one.ID] = one
	}
	flagged := map[string][]string{}
	for _, one := range set.Flag {
		flagged[one.ID] = append(flagged[one.ID], one.Rules...)
	}

	scores := []RuleScore{}
	for _, rule := range Catalog {
		score := RuleScore{Rule: rule.Name, Precision: -1}
		for id, rules := range fired {
			if !rules[rule.Name] {
				continue
			}
			score.Fired++
			if wanted[rule.Name][id] {
				score.True++
			}
			one, isClean := clean[id]
			if !isClean {
				continue
			}
			if blindOnClean(set, one, flagged[id], rule.Name) {
				continue
			}
			score.False++
		}
		if score.True+score.False > 0 {
			score.Precision = float64(score.True) / float64(score.True+score.False)
		}
		score.Grade, score.Note = gradeFor(rule, score.Precision, thresholds)
		if score.Fired == 0 && score.Precision < 0 {
			continue
		}
		scores = append(scores, score)
	}
	return scores
}

// gradeFor 는 정밀도를 보고 등급을 매긴다 (설계 결정 14).
// 정밀도가 낮은 규칙을 거절로 켜 두면 사람이 경고 전체를 무시하기 시작한다.
func gradeFor(rule Rule, precision float64, thresholds config.QualityConfig) (Grade, string) {
	demote, off := thresholds.PrecisionDemote, thresholds.PrecisionOff
	if demote == 0 {
		demote, off = PrecisionFloor, 0.60
	}
	if precision < 0 || !rule.Demotable || rule.Security() {
		return "", ""
	}
	switch {
	case precision < off:
		return GradeOff, fmt.Sprintf("정밀도 %.3f < %.2f 라 껐다", precision, off)
	case precision < demote:
		return GradeWarn, fmt.Sprintf("정밀도 %.3f < %.2f 라 경고로 내렸다", precision, demote)
	}
	return "", ""
}

// Table 은 사람이 읽는 표다. `mem eval --quality` 가 그대로 찍는다.
func (r GoldenReport) Table() string {
	out := strings.Builder{}
	out.WriteString("규칙별 정밀도\n")
	out.WriteString("| 규칙 | 잡음 | 진짜 | 거짓 | 정밀도 | 등급 |\n| --- | --- | --- | --- | --- | --- |\n")
	for _, one := range r.Rules {
		precision := "—"
		if one.Precision >= 0 {
			precision = fmt.Sprintf("%.3f", one.Precision)
		}
		grade := "그대로"
		if one.Grade != "" {
			grade = string(one.Grade)
		}
		fmt.Fprintf(&out, "| %s | %d | %d | %d | %s | %s |\n",
			one.Rule, one.Fired, one.True, one.False, precision, grade)
	}
	out.WriteString("\n유형별 재현율\n")
	out.WriteString("| 유형 | 전체 | 잡음 | 재현율 | 기계 규칙군 |\n| --- | --- | --- | --- | --- |\n")
	for _, one := range r.Types {
		machine := ""
		if one.Machine {
			machine = "○"
		}
		fmt.Fprintf(&out, "| %s | %d | %d | %.3f | %s |\n", one.Type, one.Total, one.Caught, one.Recall, machine)
	}
	fmt.Fprintf(&out, "\n대조군 거짓 경보 : %d건\n", len(r.FalseAlarms))
	for _, one := range r.FalseAlarms {
		fmt.Fprintf(&out, "  %s : %s\n", one.ID, strings.Join(one.Rules, " "))
	}
	// ① 은 **유형마다** 재는 자다. 묶음 값(0.904)만 찍고 「불합격」이라 쓰면
	// 숫자와 판정이 어긋나 보인다 — 어느 유형이 못 넘었는지 같이 적는다 (리뷰 D3).
	fmt.Fprintf(&out, "\nG6 ① 기계 규칙군 재현율 %s%s · ② 거짓 경보 0 %s · ③ 규칙별 정밀도 %s\n",
		mark(r.MachineRecallOK), r.recallCulprits(), mark(r.NoFalseAlarm), mark(r.PrecisionOK))
	return out.String()
}

// recallCulprits 는 ① 을 못 넘긴 기계 규칙군 유형과 그 값이다. 다 넘겼으면 빈칸이다.
// 「재현율 0.904 인데 ① 불합격」처럼 숫자와 판정이 어긋나 보이던 자리다.
func (r GoldenReport) recallCulprits() string {
	low := []string{}
	for _, one := range r.Types {
		if one.Machine && one.Recall < MachineRecallFloor {
			low = append(low, fmt.Sprintf("%s %.3f", one.Type, one.Recall))
		}
	}
	if len(low) == 0 {
		return ""
	}
	return fmt.Sprintf(" (%s < %.2f)", strings.Join(low, " · "), MachineRecallFloor)
}

func mark(ok bool) string {
	if ok {
		return "합격"
	}
	return "불합격"
}
