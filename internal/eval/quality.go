package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
)

// QualityPath 는 저장소가 품질 골든셋을 두는 자리다.
func QualityPath(dir string) string {
	return filepath.Join(dir, "golden", quality.GoldenFileName)
}

// QualityOptions 는 `mem eval --quality` 한 번이다.
type QualityOptions struct {
	// Golden 은 품질 골든셋 파일이다. 대상 저장소는 그 파일의 meta.store 가 적는다.
	Golden string
	// Store 가 비지 않으면 meta.store 대신 이 폴더의 기억을 잰다.
	Store  string
	Config config.Config
	Vocab  config.Vocab
	Now    time.Time
	// Near 는 중복 후보의 셋째 신호(임베딩)다. nil 이면 안 쓴다 (결정 15).
	Near quality.Vectors
}

// QualityReport 는 규칙 42개가 실제로 듣는지 잰 결과다 (설계 3-6).
type QualityReport struct {
	Golden string `json:"golden"`
	Store  string `json:"store"`
	// Flag 는 잡아야 할 기억, Clean 은 잡으면 안 되는 대조군 건수다.
	Flag     int                  `json:"flag"`
	Clean    int                  `json:"clean"`
	Memories int                  `json:"memories"`
	Score    quality.GoldenReport `json:"score"`
	Pass     bool                 `json:"pass"`
	Elapsed  time.Duration        `json:"-"`
}

// RunQuality 는 품질 골든셋으로 규칙마다 정밀도·재현율을 잰다.
//
// 이전 전 저장소를 새 자로 재는 것이라 규격 판을 v0.2 로 못 박는다 —
// 그래야 「v0.2 필수 칸이 없다」가 규칙 판정에 제대로 잡힌다. 대조군 거짓
// 경보 셈에서는 골든셋의 `exclude_rules_on_clean` 이 그것을 다시 빼 준다.
func RunQuality(options QualityOptions) (*QualityReport, error) {
	started := time.Now()
	set, bad := loadQualityGolden(options.Golden)
	if bad != nil {
		return nil, bad
	}
	dir := options.Store
	if dir == "" {
		dir = set.StoreDir()
	}
	memories, err := quality.LoadMemories(dir)
	if err != nil {
		return nil, &Error{Message: err.Error()}
	}
	if len(memories) == 0 {
		return nil, &Error{Message: emptyStoreText(dir)}
	}
	report := QualityReport{Golden: options.Golden, Store: dir,
		Flag: len(set.Flag), Clean: len(set.Clean), Memories: len(memories)}
	report.Score = quality.ScoreGolden(set, memories, repoOptionsFor(options, set))
	report.Pass = report.Score.Pass()
	report.Elapsed = time.Since(started)
	return &report, nil
}

// repoOptionsFor 는 채점에 쓸 조건이다. 강등표는 안 넘긴다 — 강등을 반영하고
// 정밀도를 재면 자기가 자기를 재는 꼴이 된다.
func repoOptionsFor(options QualityOptions, set *quality.GoldenSet) quality.RepoOptions {
	vocab := options.Vocab
	if len(vocab.Tags) == 0 {
		vocab = config.DefaultVocab()
	}
	spec := 0
	if set.Meta.PreMigration {
		spec = model.SpecV2
	}
	return quality.RepoOptions{Options: quality.Options{Near: options.Near,
		Config: options.Config, Vocab: vocab, Now: options.Now, Spec: spec}}
}

// loadQualityGolden 은 품질 골든셋을 읽는다. 없으면 **한국어로** 왜 없는지와
// 무엇이 안 재졌는지를 말한다. 종료 코드는 0 이다 — 「미달」이 아니라 「잴 것이
// 없다」이기 때문이다 (v0.1 리뷰 C #13).
func loadQualityGolden(path string) (*quality.GoldenSet, *Error) {
	set, err := quality.LoadGolden(path)
	if err == nil {
		return set, nil
	}
	if missingFile(err) {
		return nil, &Error{Missing: true, Message: fmt.Sprintf(
			"품질 골든셋이 없다 : %s — 그 저장소의 기억을 사람이 읽고 매긴 자라 `mem init` 이 만들어 주지 못한다. 아무것도 안 쟀다", path)}
	}
	return nil, &Error{Message: err.Error()}
}

// emptyStoreText 는 잴 기억이 하나도 없을 때다. 「0건을 다 놓쳤다」는 표를
// 내면 CI 가 재현율 0.000 을 진짜 값으로 읽는다 — 그래서 오류로 끝낸다.
func emptyStoreText(dir string) string {
	return fmt.Sprintf("잴 기억이 없다 : %s 아래에 `.md` 가 0건이다. 저장소 자리를 확인한다 (골든셋 meta.store 나 --repo)", dir)
}

func missingFile(err error) bool {
	return strings.Contains(err.Error(), "no such file") || strings.Contains(err.Error(), "cannot find")
}

// QualityMarkdown 은 사람이 읽는 표다 (설계 3-6 · 품질규칙표 4-2).
func QualityMarkdown(report *QualityReport) string {
	out := strings.Builder{}
	out.WriteString(fmt.Sprintf("품질 골든셋 — 잡을 것 %d건 · 대조군 %d건 · 저장소 %d건 (%.2f초)\n",
		report.Flag, report.Clean, report.Memories, report.Elapsed.Seconds()))
	out.WriteString(report.Score.Table())
	out.WriteString("\n" + gateTable(QualityGates(report)))
	if len(report.Score.Demote) > 0 {
		out.WriteString("\n정밀도가 낮아 등급을 내릴 규칙 :\n")
		for _, rule := range quality.RuleNames() {
			down, found := report.Score.Demote[rule]
			if !found {
				continue
			}
			out.WriteString(fmt.Sprintf("- %s → %s\n", rule, down))
		}
		out.WriteString("`mem.toml` 의 `[quality.demoted]` 에 적으면 다음 lint·add 부터 그 등급으로 돈다.\n")
	}
	return out.String()
}

// TuneReport 는 중복 문턱 곡선이다 (`--tune-dup`).
type TuneReport struct {
	Points []quality.TunePoint `json:"points"`
	// Align 은 문장 최대 정렬 문턱 곡선이다 (v0.3 · 설계 결정 30).
	Align  []quality.AlignPoint `json:"align,omitempty"`
	Reject float64              `json:"dup_reject"`
	Warn   float64              `json:"dup_warn"`
	// NowReject·NowWarn 은 지금 mem.toml 에 적힌 값이다.
	NowReject float64 `json:"now_reject"`
	NowWarn   float64 `json:"now_warn"`
	// Here·Wanted 는 골든셋에 적힌 기억 중 이 저장소에 있는 수와 전체 수다.
	// Here 가 0 이면 곡선이 전부 0 이다 (스트레스 V6).
	Here    int           `json:"golden_here"`
	Wanted  int           `json:"golden_total"`
	Elapsed time.Duration `json:"-"`
}

// RunTune 은 문턱을 0.05~0.90 으로 훑어 정밀도·재현율 곡선을 찍고 권장값을
// 고른다 (설계 3-2). **고르기만 한다 — mem.toml 은 사람이 고친다.**
func RunTune(options QualityOptions) (*TuneReport, error) {
	started := time.Now()
	set, bad := loadQualityGolden(options.Golden)
	if bad != nil {
		return nil, bad
	}
	dir := options.Store
	if dir == "" {
		dir = set.StoreDir()
	}
	memories, err := quality.LoadMemories(dir)
	if err != nil {
		return nil, &Error{Message: err.Error()}
	}
	if len(memories) == 0 {
		return nil, &Error{Message: emptyStoreText(dir)}
	}
	report := TuneReport{NowReject: options.Config.Quality.DupReject, NowWarn: options.Config.Quality.DupWarn}
	report.Here, report.Wanted = quality.GoldenInStore(set, memories)
	report.Points = quality.TuneDuplicate(set, memories, repoOptionsFor(options, set))
	report.Align = quality.TuneAlign(set, memories, repoOptionsFor(options, set))
	report.Reject, report.Warn = quality.Recommend(report.Points)
	report.Elapsed = time.Since(started)
	return &report, nil
}

// TuneMarkdown 은 곡선과 권장값이다.
func TuneMarkdown(report *TuneReport) string {
	out := strings.Builder{}
	out.WriteString(fmt.Sprintf("중복 문턱 곡선 (%.2f초)\n\n", report.Elapsed.Seconds()))
	// 곡선이 전부 0 이면 **왜 0 인지** 먼저 말한다. 골든셋 기억이 이 저장소에
	// 없으면 아무리 오래 훑어도 셀 것이 없다 (스트레스 V6 — 146초를 쓰고 얻은
	// 것이 0 이었다).
	switch {
	case report.Here == 0 && report.Wanted > 0:
		out.WriteString(fmt.Sprintf(
			"골든셋에 적힌 기억 %d건이 이 저장소에 하나도 없다. 곡선이 전부 0 인 까닭이다 —"+
				" 골든셋과 같은 저장소에서 돌린다.\n\n", report.Wanted))
	case report.Here < report.Wanted:
		out.WriteString(fmt.Sprintf("골든셋 %d건 중 이 저장소에 있는 것은 %d건이다.\n\n",
			report.Wanted, report.Here))
	}
	out.WriteString(quality.TuneTable(report.Points))
	if len(report.Align) > 0 {
		out.WriteString("\n문장 최대 정렬 곡선 (부분 중복 1단)\n\n")
		out.WriteString(quality.AlignTable(report.Align))
	}
	out.WriteString(fmt.Sprintf("\n권장 : dup_reject %.2f (DUP 재현율 %.3f) · dup_warn %.2f (%.3f)",
		report.Reject, quality.RecallAt(report.Points, report.Reject),
		report.Warn, quality.RecallAt(report.Points, report.Warn)))
	if report.NowReject > 0 {
		out.WriteString(fmt.Sprintf("   (지금 %.2f · %.2f)", report.NowReject, report.NowWarn))
	}
	out.WriteString("\n`Memory/mem.toml` 의 `[quality]` 를 사람이 고친다. 도구는 안 고친다.\n")
	return out.String()
}

// WriteDemotion 은 정밀도가 낮아 내린 등급을 `mem.toml` 의 `[quality.demoted]`
// 에 적는다 (설계 3-6 ⑤). 내릴 것이 없으면 아무것도 안 쓰고 거짓을 준다.
//
// **파일을 고치는 것은 사람이 시켰을 때만이다** — cmd 가 `--write` 같은 뜻을
// 받았을 때만 이것을 부른다. 재기만 하는 명령이 설정을 몰래 고치면 안 된다.
func WriteDemotion(path string, report *QualityReport) (bool, error) {
	if len(report.Score.Demote) == 0 {
		return false, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	next := quality.WriteDemoted(string(raw), report.Score.Demote, report.Score.DemoteNotes())
	if next == string(raw) {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(next), 0o644)
}
