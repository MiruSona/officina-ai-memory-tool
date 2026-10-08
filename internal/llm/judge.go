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
)

// 근거 지지 판정(R3 · 모음 기억 줄 검증 3-6)의 물음이다. 선택지 순서는 하나만 쓴다 —
// 정·역 평균은 지연이 두 배인데 50문항 중 한 문항만 더 맞혔다 (판정모델 실측 2-2).
const (
	KindSupport = "support"
	// PromptVersion 은 기본 물음 글의 판이다 (DefaultPrompt.Version 과 같다). 물음 글 자체가
	// 해시에 들어가 옛 판정과 섞이지 않는다. 이 판 번호는 기록의 Prompt 라벨을 구분하려고 올린다.
	// v2 (2026-10-08) : v1 은 「근거에 없는 말을 덧붙이면 지지 아님」이라 요약(늘 본문 한 문장보다
	// 넓다)을 통째로 무관으로 밀어 지지 갈래가 2/16 이었다 (Docs/Research/2026-10-08-K실물판정측정.md).
	// v3 (2026-10-08) : v2 는 근거가 주장의 **일부**만 받치는 꼴을 무관으로 밀어 지지 7/16 이었다.
	// 사용자 결정(10-08 낮) — 일부만 받쳐도 지지로 친다 · 최고 확률 0.40 아래는 「모른다」 경고.
	PromptVersion = "support-v3"

	LetterSupport    = "A"
	LetterContradict = "B"
	LetterUnrelated  = "C"

	// UnsureBelow 는 「모른다」 선이다. 최고 확률이 이 아래면 글자를 믿지 않는다 (v2 측정에서
	// 동점 근처 쌍 k033·k048 이 0.35~0.40 이었다).
	UnsureBelow = 0.40
	// tieGap 은 1·2등 확률 차이가 이 안이면 동점으로 보는 폭이다.
	tieGap = 0.01

	systemStrict = "You are a strict judge. Apply the given criterion to the evidence and choose exactly one option. " +
		"Answer with the single uppercase letter of the chosen option and nothing else."
	systemPlain = "You are a judge. Apply the given criterion to the evidence and choose exactly one option. " +
		"Answer with the single uppercase letter of the chosen option and nothing else."
	criterionV2 = "아래 주장이 근거 문장과 어떤 관계인가? " +
		"주장이 근거의 내용을 요약하거나 일반화한 것이면 지지(A)다. " +
		"근거와 어긋나는 사실·숫자·극성(긍정/부정)이 있으면 반대(B)다. " +
		"근거가 주장에 대해 아무 말도 하지 않으면 무관(C)이다. " +
		"주장이 근거보다 한 마디 넓은 것만으로는 무관이 아니다.\n주장 : "
	criterionV3 = "아래 주장이 근거 문장과 어떤 관계인가? " +
		"주장 가운데 근거가 다루는 부분이 근거와 맞으면 지지(A)다. 근거가 주장의 일부만 받쳐도 지지다. " +
		"근거가 다루지 않는 부분은 판단에 넣지 않는다. " +
		"근거가 다루는 부분에 어긋나는 사실·숫자·극성(긍정/부정)이 있으면 반대(B)다. " +
		"근거가 주장의 어느 부분도 다루지 않으면 무관(C)이다.\n주장 : "
)

var optionsV2 = []option{
	{Letter: LetterSupport, Description: "지지 — 근거가 주장의 핵심을 받친다 (주장이 근거를 요약·일반화한 것도 지지)"},
	{Letter: LetterContradict, Description: "반대 — 근거가 주장과 어긋난다 (사실·숫자가 다름 · 뜻이 뒤집힘)"},
	{Letter: LetterUnrelated, Description: "무관 — 근거가 주장에 대해 아무 말도 하지 않는다"},
}

var optionsV3 = []option{
	{Letter: LetterSupport, Description: "지지 — 근거가 주장의 전부 또는 일부를 받친다 (요약·일반화·일부만 받침도 지지)"},
	{Letter: LetterContradict, Description: "반대 — 근거가 다루는 부분에서 주장과 어긋난다 (사실·숫자가 다름 · 뜻이 뒤집힘)"},
	{Letter: LetterUnrelated, Description: "무관 — 근거가 주장의 어느 부분도 다루지 않는다"},
}

// Prompt 는 물음 글 한 판이다. 판마다 글이 달라 해시가 갈리므로 판정 기록이 섞이지 않는다.
type Prompt struct {
	Version   string
	System    string
	Criterion string
	Options   []option
}

// 물음 글 판들. 측정에서 나란히 재려고 셋을 다 둔다 (`mem judge support --prompt`).
var (
	PromptV2       = Prompt{Version: "support-v2", System: systemStrict, Criterion: criterionV2, Options: optionsV2}
	PromptV3       = Prompt{Version: "support-v3", System: systemPlain, Criterion: criterionV3, Options: optionsV3}
	PromptV3Strict = Prompt{Version: "support-v3-strict", System: systemStrict, Criterion: criterionV3, Options: optionsV3}
	// DefaultPrompt 는 R3 와 mem judge 가 기본으로 쓰는 판이다.
	DefaultPrompt = PromptV3
)

// PromptNamed 는 이름(v2 · v3 · v3-strict 또는 판 이름 그대로)으로 물음 글을 찾는다.
func PromptNamed(name string) (Prompt, bool) {
	for _, prompt := range []Prompt{PromptV2, PromptV3, PromptV3Strict} {
		if name == prompt.Version || "support-"+name == prompt.Version {
			return prompt, true
		}
	}
	return Prompt{}, false
}

type option struct {
	Letter      string `json:"letter"`
	Description string `json:"description"`
}

// semifInput 은 사용자 턴 JSON 이다 (SemIf direct 꼴 — evidence · criterion · options).
type semifInput struct {
	Evidence  string   `json:"evidence"`
	Criterion string   `json:"criterion"`
	Options   []option `json:"options"`
}

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
	Cached   bool               `json:"cached,omitempty"`
	// Unsure 는 「모른다」다 — 최고 확률이 UnsureBelow 아래거나 1·2등이 동점. 글자는 그대로
	// 두되(판정 갈래에 안 넣는다) 부르는 쪽이 경고로 흘린다 (사용자 결정 2026-10-08).
	Unsure bool `json:"unsure,omitempty"`
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

// Judge 는 판정기다. Dir 이나 프로필(judge_profile)이 비면 판정 기록을 안 남기고 안 읽는다.
// Refuse 가 참을 내는 글(비밀 꼴)은 서버로 보내지 않는다. Prompt 가 비면 DefaultPrompt 다.
type Judge struct {
	Client *Client
	Dir    string
	Fresh  bool
	Refuse func(text string) bool
	Prompt *Prompt
	// last 는 마지막으로 판정을 못 받은 까닭이다 (retain 관문의 경고 글에 쓴다).
	last string
}

// prompt 는 쓰는 물음 글이다.
func (j *Judge) prompt() Prompt {
	if j.Prompt == nil {
		return DefaultPrompt
	}
	return *j.Prompt
}

// ErrRefused 는 비밀 꼴이 있어 보내지 않은 것이다.
var ErrRefused = errors.New("secret-shape")

// Support 는 (근거, 주장) 한 쌍을 판정한다. 같은 입력의 기록이 있으면 다시 묻지
// 않는다 — 같은 입력에 확률이 최대 0.23 흔들렸다 (판정모델 실측 2-6).
func (j *Judge) Support(evidence, claim string) (Verdict, error) {
	if j == nil || j.Client == nil {
		return Verdict{}, ErrDisabled
	}
	if j.Refuse != nil && (j.Refuse(evidence) || j.Refuse(claim)) {
		return Verdict{}, ErrRefused
	}
	prompt := j.prompt()
	user, err := json.Marshal(semifInput{Evidence: evidence, Criterion: prompt.Criterion + claim,
		Options: prompt.Options})
	if err != nil {
		return Verdict{}, err
	}
	profile := j.Client.Profile()
	hash := hashOf(KindSupport, prompt.Version, profile, prompt.System, string(user))
	// 프로필이 비면 서버 쪽에서 모델이 바뀌어도 열쇠가 같다. 다른 모델의 옛 판정을
	// 그대로 쓰지 않게 기록을 읽지도 쓰지도 않는다 (리뷰 2026-10-05).
	remember := j.Client.profile != ""
	if !j.Fresh && remember {
		if cached, ok := j.read(hash); ok {
			cached.Cached = true
			return cached, nil
		}
	}
	choice, err := j.Client.Choose(Question{System: prompt.System, User: string(user),
		Letters: []string{LetterSupport, LetterContradict, LetterUnrelated}})
	if err != nil {
		return Verdict{}, err
	}
	verdict := Verdict{Hash: hash, Kind: KindSupport, Prompt: prompt.Version, Profile: profile,
		Letter: choice.Letter, Prob: choice.Prob, Probs: choice.Probs, MS: choice.MS,
		At: time.Now().Format(time.RFC3339), Evidence: evidence, Claim: claim, Unsure: unsureOf(choice)}
	if remember {
		j.write(verdict)
	}
	return verdict, nil
}

// ErrUnsure 는 「모른다」다 — 글자를 받았지만 확률이 낮거나 동점이라 믿지 않는다.
var ErrUnsure = errors.New("unsure")

// Supports 는 retain.Judge 자리다. ok 가 거짓이면 판정을 못 받은 것이다. 「모른다」도
// ok 거짓이다 — 거절 대신 경고로 흘린다 (사용자 결정 2026-10-08).
func (j *Judge) Supports(quote, summary string) (bool, bool) {
	verdict, err := j.Support(quote, summary)
	if err != nil {
		j.last = err.Error()
		return false, false
	}
	if verdict.Unsure {
		j.last = fmt.Sprintf("%s %s=%.2f", ErrUnsure.Error(), verdict.Letter, verdict.Prob)
		return false, false
	}
	return verdict.Supported(), true
}

// Problem 은 마지막으로 판정을 못 받은 까닭이다 (timeout · http-500 · no-logprobs …).
func (j *Judge) Problem() string { return j.last }

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

func (j *Judge) read(hash string) (Verdict, bool) {
	if j.Dir == "" {
		return Verdict{}, false
	}
	data, err := os.ReadFile(j.path(hash))
	if err != nil {
		return Verdict{}, false
	}
	var verdict Verdict
	if json.Unmarshal(data, &verdict) != nil || verdict.Hash != hash || verdict.Letter == "" {
		return Verdict{}, false
	}
	return verdict, true
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
