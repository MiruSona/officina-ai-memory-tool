package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
)

// 근거 지지 판정(R3 · 모음 기억 줄 검증 3-6)의 답 글자와 「모른다」 선이다.
const (
	KindSupport = "support"

	LetterSupport    = "A"
	LetterContradict = "B"
	LetterUnrelated  = "C"

	// UnsureBelow 는 「모른다」 선이다. 최고 확률이 이 아래면 글자를 믿지 않는다.
	UnsureBelow = 0.40
	// tieGap 은 1·2등 확률 차이가 이 안이면 동점으로 보는 폭이다.
	tieGap = 0.01
)

// Verdict 는 판정 기록 한 건이다. Memory/local/judge/<해시>.json 으로 남는다.
// 주소·키는 안 적는다.
type Verdict struct {
	Hash     string             `json:"hash"`
	Kind     string             `json:"kind"`
	Prompt   string             `json:"prompt"`
	Profile  string             `json:"profile"`
	Letter   string             `json:"letter"`
	Prob     float64            `json:"prob"`
	Probs    map[string]float64 `json:"probs"`
	MS       int64              `json:"ms"`
	At       string             `json:"at"`
	Evidence string             `json:"evidence"`
	Claim    string             `json:"claim"`
	// Stage 는 답을 낸 단(rules · nli)이고 Reason 은 규칙의 까닭이다 (neg:한다→안 한다).
	Stage  string `json:"stage,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Unsure 는 「모른다」다 — 최고 확률이 UnsureBelow 아래거나 1·2등이 동점. 글자는 그대로
	// 두되(판정 갈래에 안 넣는다) 부르는 쪽이 경고로 흘린다 (사용자 결정 2026-10-08).
	Unsure bool `json:"unsure,omitempty"`
	// LabelSet 은 NLI 서버가 답한 응답 꼴(nli3 · nli4)이다. 다른 단은 비어 있다.
	LabelSet string `json:"label_set,omitempty"`
}

// Supported 는 「지지」 글자를 골랐는지다. 「반대」·「무관」은 거절이다 (자동쌓기설계 2-3 R3).
func (v Verdict) Supported() bool { return v.Letter == LetterSupport }

// unsureOf 는 받은 확률로 「모른다」를 가른다. 동점은 가장 단순하게 「모른다」로 둔다.
func unsureOf(choice Choice) bool {
	if choice.Prob < UnsureBelow {
		return true
	}
	second := 0.0
	for letter, prob := range choice.Probs {
		if letter != choice.Letter && prob > second {
			second = prob
		}
	}
	return choice.Prob-second < tieGap
}

// 판정 사다리의 단 이름이다 (자체판정프로그램설계 1절 · ② 는 길1 NLI 설계로 바뀜). 이 차례로 묻는다.
const (
	StageRules = "rules"
	StageNLI   = "nli"
)

// Stages 는 사다리 차례다.
var Stages = []string{StageRules, StageNLI}

// Judge 는 판정기다. 규칙 → NLI 차례로 묻고 규칙이 최종 글자를 내면 멈춘다.
// NLI 가 nil 이면 그 단을 건너뛴다. Dir 이 비면 NLI 판정 기록을 안 남긴다.
// Refuse 가 참을 내는 글(비밀 꼴)은 프로세스 밖(NLI)으로 보내지 않는다.
type Judge struct {
	NLI    *NLIClient
	Dir    string
	Refuse func(text string) bool
	// RulesFinal 은 규칙 글자 중 최종으로 칠 것이다 (mem judge {B,C} · retain {B}).
	// 비면 규칙에 걸려도 늘 다음 단에 묻는다.
	RulesFinal []string
	// Only 는 돌 단을 고른다 (mem judge --stage). 비면 사다리 전체다.
	Only []string
	// last 는 마지막으로 판정을 못 받은 까닭이다 (retain 관문의 경고 글에 쓴다).
	last string
	// skipped 는 마지막 Supports 에서 물을 단이 하나도 없었는지다 (관문은 조용히 넘긴다).
	skipped bool
	// probs 는 마지막 Supports 가 받은 세 칸 확률이다. 판정을 못 받았으면 nil 이다.
	probs map[string]float64
}

// ErrRefused 는 비밀 꼴이 있어 보내지 않은 것이다.
var ErrRefused = errors.New("secret-shape")

// Support 는 (근거, 주장) 한 쌍을 사다리로 판정한다. NLI 답은 확신이 없어도(Unsure) 그대로
// 돌려준다. NLI 가 오류면 그 오류다. 물을 단이 없으면 규칙 답을 Unsure 로, 그마저 없으면 ErrDisabled 다.
func (j *Judge) Support(evidence, claim string) (Verdict, error) {
	if j == nil {
		return Verdict{}, ErrDisabled
	}
	var pending *Verdict
	if j.uses(StageRules) {
		verdict := ruleVerdict(evidence, claim)
		if verdict.Letter != "" && j.ruleFinal(verdict.Letter) {
			return verdict, nil
		}
		pending = &verdict
	}
	if !j.uses(StageNLI) || j.NLI == nil {
		return settle(pending, ErrDisabled)
	}
	if j.Refuse != nil && (j.Refuse(evidence) || j.Refuse(claim)) {
		return settle(pending, ErrRefused)
	}
	verdict, err := j.askNLI(evidence, claim)
	if err != nil {
		j.last = err.Error()
		return Verdict{}, err
	}
	return verdict, nil
}

// settle 은 NLI 답 없이 사다리가 끝났을 때다. 비밀 꼴로 못 보냈으면 그 오류다 — 부르는 쪽이
// 「판정 못 받음」 경고로 흘린다. 물을 단이 없었으면 규칙 답을 Unsure 로 돌려준다
// (규칙에 안 걸린 답은 글자가 비어 있다).
func settle(pending *Verdict, err error) (Verdict, error) {
	if pending == nil || !errors.Is(err, ErrDisabled) {
		return Verdict{}, err
	}
	pending.Unsure = true
	return *pending, nil
}

// uses 는 그 단을 돌지다.
func (j *Judge) uses(stage string) bool {
	if len(j.Only) == 0 {
		return true
	}
	for _, one := range j.Only {
		if one == stage {
			return true
		}
	}
	return false
}

func (j *Judge) ruleFinal(letter string) bool {
	for _, one := range j.RulesFinal {
		if one == letter {
			return true
		}
	}
	return false
}

// ruleVerdict 는 규칙 단의 답이다. 안 걸리면 글자가 빈 답이다. 같은 입력이면 늘 같은 답이라
// 기록을 안 남긴다.
func ruleVerdict(evidence, claim string) Verdict {
	started := time.Now()
	verdict := Verdict{Kind: KindSupport, Prompt: RulesVersion, Profile: RulesProfile, Stage: StageRules,
		Evidence: evidence, Claim: claim, At: started.Format(time.RFC3339)}
	verdict.Hash = hashOf(KindSupport, RulesVersion, RulesProfile, evidence, claim)
	label, reason, ok := RuleJudge(evidence, claim)
	if ok {
		verdict.Letter, verdict.Prob, verdict.Reason = label, 1, reason
		verdict.Probs = map[string]float64{label: 1}
	}
	verdict.MS = time.Since(started).Milliseconds()
	return verdict
}

// askNLI 는 NLI 단이다. 동점이거나, 답이 지지(A)인데 nli_sure_support 아래거나, 반대·무관(B·C)인데
// nli_sure 아래면 Unsure 다 — 지지에만 높은 선을 걸어 거짓 지지를 막고 B·C 확정은 덜 잃는다
// (길1 NLI 설계 7절). 판정 기록은 남기기만 한다 — 모델 이름(Profile)을 응답에서야 알아 묻기 전에
// 기록을 찾을 수 없고, 1초 안쪽(p95 ≤ 1.0초)이라 다시 물어도 싸다.
//
// 보내기 전에 근거·주장에서 기억 본문 꼬리표(`왜 :` 따위, quality.StripTags)를 뗀다 — 서버는 안 떼고
// 학습·측정은 뗀 글로 했다. 문장을 이어 붙여 보내는 쪽은 잇기 전에 문장마다 뗀다. 기록에는 보낸(뗀)
// 글을 남긴다.
func (j *Judge) askNLI(evidence, claim string) (Verdict, error) {
	evidence, claim = quality.StripTags(evidence), quality.StripTags(claim)
	choice, model, err := j.NLI.Judge(evidence, claim)
	if err != nil {
		return Verdict{}, err
	}
	hash := hashOf(KindSupport, NLIVersion, model, evidence, claim)
	verdict := Verdict{Hash: hash, Kind: KindSupport, Prompt: NLIVersion, Profile: model, Stage: StageNLI,
		Letter: choice.Letter, Prob: choice.Prob, Probs: choice.Probs, MS: choice.MS,
		At: time.Now().Format(time.RFC3339), Evidence: evidence, Claim: claim, LabelSet: choice.LabelSet,
		Unsure: unsureOf(choice) || choice.Prob < j.NLI.sureFor(choice.Letter)}
	if model != "-" {
		j.write(verdict)
	}
	return verdict, nil
}

// ErrUnsure 는 「모른다」다 — 글자를 받았지만 확률이 낮거나 동점이라 믿지 않는다.
var ErrUnsure = errors.New("unsure")

// Supports 는 retain.Judge 자리다. ok 가 거짓이면 판정을 못 받은 것이다. 「모른다」도
// ok 거짓이다 — 거절 대신 경고로 흘린다 (사용자 결정 2026-10-08). 물을 단이 하나도 없었으면
// Skipped 가 참이다 (규칙에 안 걸렸고 다음 단이 없음).
func (j *Judge) Supports(quote, summary string) (bool, bool) {
	j.skipped, j.probs = false, nil
	verdict, err := j.Support(quote, summary)
	j.probs = verdict.Probs
	if err != nil {
		j.last = err.Error()
		j.skipped = errors.Is(err, ErrDisabled)
		return false, false
	}
	if verdict.Letter == "" {
		j.last = ""
		j.skipped = true
		return false, false
	}
	if verdict.Unsure {
		j.last = fmt.Sprintf("%s %s=%.2f", ErrUnsure.Error(), verdict.Letter, verdict.Prob)
		if verdict.Reason != "" {
			j.last += " " + verdict.Reason
		}
		return false, false
	}
	return verdict.Supported(), true
}

// Skipped 는 마지막 Supports 에 물을 단이 없었는지다. 관문은 이때 경고 없이 넘긴다 —
// 서버 없는 기계에서 규칙에 안 걸린 쌍마다 경고가 찍히면 안 된다.
func (j *Judge) Skipped() bool { return j.skipped }

// Problem 은 마지막으로 판정을 못 받은 까닭이다 (nli:timeout · nli:http-500 · unsure …).
func (j *Judge) Problem() string { return j.last }

// Probs 는 마지막 Supports 가 받은 세 칸 확률이다 (「모른다」여도 있다). 관문 까닭 글이 쓴다.
func (j *Judge) Probs() map[string]float64 { return j.probs }

// hashOf 는 판정 기록의 열쇠다. 주소는 안 넣는다 — 같은 프로필이면 같은 판정으로 친다.
func hashOf(parts ...string) string {
	sum := sha256.New()
	for _, part := range parts {
		sum.Write([]byte(part))
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func (j *Judge) path(hash string) string {
	return filepath.Join(j.Dir, hash+".json")
}

// write 는 옆에 쓰고 rename 한다. 못 써도 판정은 이미 받았으니 조용히 넘어간다 —
// 다음에 한 번 더 물을 뿐이다.
func (j *Judge) write(verdict Verdict) {
	if j.Dir == "" {
		return
	}
	data, err := json.MarshalIndent(verdict, "", "  ")
	if err != nil || os.MkdirAll(j.Dir, 0o755) != nil {
		return
	}
	target := j.path(verdict.Hash)
	temporary := target + ".tmp"
	if os.WriteFile(temporary, data, 0o644) != nil {
		return
	}
	if os.Rename(temporary, target) != nil {
		os.Remove(temporary)
	}
}
