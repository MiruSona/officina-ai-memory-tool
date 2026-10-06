package main

// `mem eval --rule-sample` · `--rule-score` 배선이다 (B-05 · B08 설계 2절).
// 표본 뽑기와 셈은 internal/quality 가 하고, 여기서는 파일을 쓰고 읽는다.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/eval"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
	"github.com/mirusona/officina-ai-memory-tool/internal/safe"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// sampleViewLines 는 판정 보조 view 에 넣는 본문 앞 줄 수다.
const sampleViewLines = 3

// sampleViewRunes 는 view 한 칸의 글자 상한이다. 판정에 충분하고 파일이 안 부풀게.
const sampleViewRunes = 400

// sampleFile 은 표본 파일 한 개다. YAML 로 쓰고(.json 이면 JSON), 읽을 때는 둘 다 받는다.
type sampleFile struct {
	Rule  string               `yaml:"rule" json:"rule"`
	Seed  int64                `yaml:"seed" json:"seed"`
	Total int                  `yaml:"total" json:"total"`
	Items []quality.SampleItem `yaml:"items" json:"items"`
}

// runEvalRuleSample 은 규칙 하나를 거절 등급이라 치고 울린 기억을 표본 파일로 쓴다.
func runEvalRuleSample(parsed *options) int {
	out := parsed.text("out")
	if out == "" {
		return fail(i18n.T(i18n.EvalSampleNeedOut))
	}
	n, err := intFlag(parsed, "n", quality.SampleSize)
	if err != nil {
		return fail(err.Error())
	}
	seed, err := intFlag(parsed, "seed", 1)
	if err != nil {
		return fail(err.Error())
	}
	repository, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	opt := quality.Options{Config: repository.Config, Vocab: vocabOf(repository), Now: time.Now(),
		Demoted: demotedOf(repository), Canon: canonOf(repository)}
	memories := allMemories(opened)
	rule := parsed.text("rule-sample")
	result := quality.SampleRule(memories, rule, opt, n, int64(seed))
	if result.TooFew {
		fmt.Println(i18n.T(i18n.EvalSampleTooFew, result.Total, quality.SampleMin))
		return exitCheck
	}
	byID := memoryMap(memories)
	for at := range result.Items {
		result.Items[at].View = sampleView(byID[result.Items[at].ID])
	}
	file := sampleFile{Rule: rule, Seed: int64(seed), Total: result.Total, Items: result.Items}
	if err := writeSampleFile(out, file); err != nil {
		return fail(err.Error())
	}
	fmt.Println(i18n.T(i18n.EvalSampleWrote, len(result.Items), out, result.Total, seed))
	return exitOK
}

// writeSampleFile 은 새 파일로만 쓴다 — 사람이 판정을 적어 둔 파일을 날리지 않게.
func writeSampleFile(path string, file sampleFile) error {
	var body []byte
	var err error
	if strings.EqualFold(filepath.Ext(path), ".json") {
		body, err = jsonIndent(file)
	} else {
		body, err = yaml.Marshal(file)
	}
	if err != nil {
		return err
	}
	handle, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return errors.New(i18n.T(i18n.EvalSampleExists, path))
	}
	if err != nil {
		return err
	}
	if _, err := handle.Write(body); err != nil {
		handle.Close()
		return err
	}
	return handle.Close()
}

// sampleView 는 사람이 판정할 때 볼 요약과 본문 첫 3줄이다. 한 줄씩 다듬어 자른다.
func sampleView(m *model.Memory) string {
	if m == nil {
		return ""
	}
	lines := []string{safe.Clip(safe.OneLine(m.Summary), sampleViewRunes)}
	for _, line := range strings.Split(m.Body, "\n") {
		if len(lines) > sampleViewLines {
			break
		}
		if one := safe.OneLine(line); one != "" {
			lines = append(lines, safe.Clip(one, sampleViewRunes))
		}
	}
	return strings.Join(lines, "\n")
}

// runEvalRuleScore 는 판정을 적은 표본 파일을 센다. 통과선 ③(대조군 거짓 0)은
// 품질 골든셋 보고에서 같이 본다 — 못 읽으면 「못 봄」 으로 두고 판정에서 뺀다.
func runEvalRuleScore(parsed *options) int {
	path := parsed.text("rule-score")
	text, err := os.ReadFile(path)
	if err != nil {
		return fail(i18n.T(i18n.EvalScoreBadFile, err.Error()))
	}
	file := sampleFile{}
	if err := yaml.Unmarshal(text, &file); err != nil {
		return fail(i18n.T(i18n.EvalScoreBadFile, err.Error()))
	}
	if len(file.Items) == 0 {
		return fail(i18n.T(i18n.EvalScoreBadFile, path))
	}
	repository, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	score := quality.ScoreSample(file.Items, memoryMap(allMemories(opened)))
	pass := score.Pass
	fmt.Println(controlLine(parsed, repository, opened, file.Rule, &pass))
	verdict := i18n.T(i18n.EvalScoreFail)
	switch {
	case score.TooFew:
		verdict = i18n.T(i18n.EvalScoreTooFew)
	case pass:
		verdict = i18n.T(i18n.EvalScorePass)
	}
	rate := i18n.T(i18n.EvalScoreNoRate)
	if score.Precision >= 0 {
		rate = fmt.Sprintf("%.3f", score.Precision)
	}
	fmt.Println(i18n.T(i18n.EvalScoreLine, len(file.Items), score.True, score.False, score.Border,
		score.Rehash, score.Unlabeled, rate, score.PinnedFalse, verdict))
	if pass && !score.TooFew {
		return exitOK
	}
	return exitCheck
}

// intFlag 는 정수 옵션 하나다. 안 주면 기본값이다.
func intFlag(parsed *options, name string, fallback int) (int, error) {
	given := parsed.text(name)
	if given == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(given)
	if err != nil || (name == "n" && value <= 0) {
		return 0, errors.New(i18n.T(i18n.EvalSampleBadInt, name, given))
	}
	return value, nil
}

func memoryMap(memories []*model.Memory) map[string]*model.Memory {
	byID := map[string]*model.Memory{}
	for _, m := range memories {
		byID[m.ID] = m
	}
	return byID
}

// controlLine 은 통과선 ③ 대조군 거짓 0 줄이다. 거짓이 있으면 pass 를 끈다.
// 골든셋을 못 읽으면 「못 봄」 이고 pass 는 그대로 둔다 (판정에서 뺀 것을 찍는다).
func controlLine(parsed *options, repository *config.Repository, opened *store.Store, rule string, pass *bool) string {
	options := eval.QualityOptions{Golden: parsed.text("golden"), Store: opened.StoreDir(),
		Config: repository.Config, Vocab: vocabOf(repository), Now: time.Now(), Canon: canonOf(repository)}
	if options.Golden == "" {
		options.Golden = eval.QualityPath(opened.Dir)
	}
	report, err := eval.RunQuality(options)
	if err != nil {
		return i18n.T(i18n.EvalControlUnseen, safe.OneLine(err.Error()))
	}
	for _, one := range report.Score.Rules {
		if one.Rule == rule {
			if one.False > 0 {
				*pass = false
			}
			return i18n.T(i18n.EvalControlLine, rule, one.False)
		}
	}
	return i18n.T(i18n.EvalControlNoScore, rule)
}

// jsonIndent 는 표본 파일의 JSON 꼴이다 (사람이 label 을 적기 쉽게 들여 쓴다).
func jsonIndent(file sampleFile) ([]byte, error) {
	body, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}
