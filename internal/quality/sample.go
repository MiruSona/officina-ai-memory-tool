package quality

import (
	"crypto/sha256"
	"encoding/hex"
	"math/rand"
	"sort"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 실기억 표본 정밀도 (B-05). 규칙 등급을 올릴 때만 잰다 — 막는 규칙만 비싸게 잰다.
// 대조군 정밀도(eval --quality)는 골든셋이 표시한 기억만 세서, 표시 없는 기억에서
// 운 것을 못 본다. 표본은 실저장소에서 그 규칙이 거절로 울 기억을 사람이 판정한다.
const (
	// SampleSize 는 표본 상한이다. 넘으면 씨앗 고정 무작위로 줄인다.
	SampleSize = 30
	// SampleMin 은 잴 수 있는 최소 수다. 5건이면 1건 틀림이 20%p 라 못 믿는다.
	SampleMin = 10
	// SamplePassPrecision 은 표본 정밀도 통과선이다.
	SamplePassPrecision = 0.95
)

// 사람이 적는 판정 셋. 영어 꼴도 받는다.
const (
	LabelTrue   = "진짜"
	LabelFalse  = "오탐"
	LabelBorder = "경계"
)

// SampleItem 은 표본 한 줄이다. 판정이 끝나 보관할 때는 View 를 지운다 —
// 기억 내용이 공개 예정인 툴 저장소에 들어가지 않게 id·해시·label 만 남긴다.
type SampleItem struct {
	ID     string `yaml:"id" json:"id"`
	Rule   string `yaml:"rule" json:"rule"`
	Hash   string `yaml:"hash" json:"hash"`
	Pinned bool   `yaml:"pinned,omitempty" json:"pinned,omitempty"`
	Label  string `yaml:"label" json:"label"`
	// Reason 은 울린 까닭(울린 말)이다. View 와 같이 판정 보조 칸이다.
	Reason string `yaml:"reason,omitempty" json:"reason,omitempty"`
	View   string `yaml:"view,omitempty" json:"view,omitempty"`
}

// SampleResult 는 표본 뽑기 결과다. Total 은 줄이기 전 울린 수다.
type SampleResult struct {
	Items []SampleItem
	Total int
	// TooFew 는 Total 이 SampleMin 미만이라 「못 잼」 이라는 뜻이다.
	TooFew bool
}

// SampleRule 은 그 규칙을 **거절 등급이라 치고** memories 를 돌아 울린 기억을 뽑는다.
// 씨앗이 같으면 같은 목록이 나온다 (id 순으로 정렬한 뒤 섞는다).
//
// B08 은 거절 자(결정 종류 · 받친 비율 0.25 미만)를 norm·canon 대조로 그대로 쓴다.
// 단 pinned 는 빼지 않는다 — 「pinned 오탐 0」 을 재려면 표본에 들어와야 한다.
func SampleRule(memories []*model.Memory, rule string, opt Options, n int, seed int64) SampleResult {
	if n <= 0 {
		n = SampleSize
	}
	opt = opt.normalized()
	found := []SampleItem{}
	for _, m := range memories {
		reason, ok := ruleFiresAsReject(m, rule, opt)
		if !ok {
			continue
		}
		found = append(found, SampleItem{ID: m.ID, Rule: rule, Hash: BodyHash(m.Body), Pinned: m.Pinned, Reason: reason})
	}
	sort.Slice(found, func(a, b int) bool { return found[a].ID < found[b].ID })
	result := SampleResult{Total: len(found), TooFew: len(found) < SampleMin}
	if len(found) > n {
		picked := []SampleItem{}
		for _, at := range rand.New(rand.NewSource(seed)).Perm(len(found))[:n] {
			picked = append(picked, found[at])
		}
		sort.Slice(picked, func(a, b int) bool { return picked[a].ID < picked[b].ID })
		found = picked
	}
	result.Items = found
	return result
}

func ruleFiresAsReject(m *model.Memory, rule string, opt Options) (string, bool) {
	if rule == RuleSummaryBodyMatch {
		fixed, _ := applyAliases(m, opt.Vocab)
		reason, share := summaryBodyGap(fixed, true, opt.Canon)
		if reason == "" || fixed.Type != model.TypeDecision || share >= summaryRejectShare {
			return "", false
		}
		return reason, true
	}
	for _, one := range Check(m, opt) {
		if one.Rule == rule {
			return one.Reason, true
		}
	}
	return "", false
}

// BodyHash 는 본문 해시(sha256 앞 16자)다. 판정 뒤 본문이 바뀌면 「다시 판정」 으로 센다.
func BodyHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])[:16]
}

// SampleScore 는 판정된 표본의 셈이다. 정밀도 = 진짜 / (진짜 + 오탐), 경계는 빼고 수만 센다.
type SampleScore struct {
	True, False, Border int
	// Unlabeled 는 label 이 비었거나 셋 중 아무것도 아닌 줄이다.
	Unlabeled int
	// Rehash 는 판정 뒤 본문이 바뀌었거나 저장소에서 사라진 줄이다. 셈에서 뺀다.
	Rehash int
	// PinnedFalse 는 pinned 기억인데 오탐으로 판정된 수다. 0 이어야 통과다.
	PinnedFalse int
	// Precision 은 셀 수 없으면 -1.
	Precision float64
	// TooFew 는 판정된 줄(진짜+오탐+경계)이 SampleMin 미만이라 「못 잼」 이다.
	TooFew bool
	// Pass 는 통과선 ① 정밀도 ≥ 0.95 ② pinned 오탐 0 · 표본 SampleMin 이상이다.
	// ③ 대조군 거짓 0 은 eval --quality 몫이라 부르는 쪽이 같이 본다.
	Pass bool
}

// ScoreSample 은 판정된 표본을 센다. current 는 id → 지금 기억이다 (해시 대조용).
// current 가 nil 이면 해시 대조를 건너뛴다.
func ScoreSample(items []SampleItem, current map[string]*model.Memory) SampleScore {
	score := SampleScore{Precision: -1}
	for _, one := range items {
		if current != nil {
			m := current[one.ID]
			if m == nil || BodyHash(m.Body) != one.Hash {
				score.Rehash++
				continue
			}
		}
		switch one.Label {
		case LabelTrue, "true":
			score.True++
		case LabelFalse, "false":
			score.False++
			if one.Pinned || (current != nil && current[one.ID].Pinned) {
				score.PinnedFalse++
			}
		case LabelBorder, "border":
			score.Border++
		default:
			score.Unlabeled++
		}
	}
	if score.True+score.False > 0 {
		score.Precision = float64(score.True) / float64(score.True+score.False)
	}
	score.TooFew = score.True+score.False+score.Border < SampleMin
	score.Pass = !score.TooFew && score.Precision >= SamplePassPrecision && score.PinnedFalse == 0
	return score
}
