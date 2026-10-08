package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// 근거 지지 판정(R3 · 모음 기억 줄 검증 3-6)의 물음이다. 선택지 순서는 하나만 쓴다 —
// 정·역 평균은 지연이 두 배인데 50문항 중 한 문항만 더 맞혔다 (판정모델 실측 2-2).
const (
	KindSupport = "support"
	// PromptVersion 은 물음 글의 판이다. 물음 글 자체가 해시에 들어가 옛 판정과 섞이지
	// 않는다. 이 판 번호는 기록의 Prompt 라벨을 구분하려고 올린다.
	// v2 (2026-10-08) : v1 은 「근거에 없는 말을 덧붙이면 지지 아님」이라 요약(늘 본문 한 문장보다
	// 넓다)을 통째로 무관으로 밀어 지지 갈래가 2/16 이었다 (Docs/Research/2026-10-08-K실물판정측정.md).
	PromptVersion = "support-v2"

	LetterSupport    = "A"
	LetterContradict = "B"
	LetterUnrelated  = "C"

	judgeSystem = "You are a strict judge. Apply the given criterion to the evidence and choose exactly one option. " +
		"Answer with the single uppercase letter of the chosen option and nothing else."
	supportCriterion = "아래 주장이 근거 문장과 어떤 관계인가? " +
		"주장이 근거의 내용을 요약하거나 일반화한 것이면 지지(A)다. " +
		"근거와 어긋나는 사실·숫자·극성(긍정/부정)이 있으면 반대(B)다. " +
		"근거가 주장에 대해 아무 말도 하지 않으면 무관(C)이다. " +
		"주장이 근거보다 한 마디 넓은 것만으로는 무관이 아니다.\n주장 : "
)

var supportOptions = []option{
	{Letter: LetterSupport, Description: "지지 — 근거가 주장의 핵심을 받친다 (주장이 근거를 요약·일반화한 것도 지지)"},
	{Letter: LetterContradict, Description: "반대 — 근거가 주장과 어긋난다 (사실·숫자가 다름 · 뜻이 뒤집힘)"},
	{Letter: LetterUnrelated, Description: "무관 — 근거가 주장에 대해 아무 말도 하지 않는다"},
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
}

// Supported 는 「지지」 글자를 골랐는지다. 「반대」·「무관」은 거절이다 (자동쌓기설계 2-3 R3).
func (v Verdict) Supported() bool { return v.Letter == LetterSupport }

// Judge 는 판정기다. Dir 이나 프로필(judge_profile)이 비면 판정 기록을 안 남기고 안 읽는다.
// Refuse 가 참을 내는 글(비밀 꼴)은 서버로 보내지 않는다.
type Judge struct {
	Client *Client
	Dir    string
	Fresh  bool
	Refuse func(text string) bool
	// last 는 마지막으로 판정을 못 받은 까닭이다 (retain 관문의 경고 글에 쓴다).
	last string
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
	user, err := json.Marshal(semifInput{Evidence: evidence, Criterion: supportCriterion + claim,
		Options: supportOptions})
	if err != nil {
		return Verdict{}, err
	}
	profile := j.Client.Profile()
	hash := hashOf(KindSupport, PromptVersion, profile, judgeSystem, string(user))
	// 프로필이 비면 서버 쪽에서 모델이 바뀌어도 열쇠가 같다. 다른 모델의 옛 판정을
	// 그대로 쓰지 않게 기록을 읽지도 쓰지도 않는다 (리뷰 2026-10-05).
	remember := j.Client.profile != ""
	if !j.Fresh && remember {
		if cached, ok := j.read(hash); ok {
			cached.Cached = true
			return cached, nil
		}
	}
	choice, err := j.Client.Choose(Question{System: judgeSystem, User: string(user),
		Letters: []string{LetterSupport, LetterContradict, LetterUnrelated}})
	if err != nil {
		return Verdict{}, err
	}
	verdict := Verdict{Hash: hash, Kind: KindSupport, Prompt: PromptVersion, Profile: profile,
		Letter: choice.Letter, Prob: choice.Prob, Probs: choice.Probs, MS: choice.MS,
		At: time.Now().Format(time.RFC3339), Evidence: evidence, Claim: claim}
	if remember {
		j.write(verdict)
	}
	return verdict, nil
}

// Supports 는 retain.Judge 자리다. ok 가 거짓이면 판정을 못 받은 것이다.
func (j *Judge) Supports(quote, summary string) (bool, bool) {
	verdict, err := j.Support(quote, summary)
	if err != nil {
		j.last = err.Error()
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
