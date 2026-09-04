package search

import (
	"math"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 반감기 (설계 6-5). 함정은 나이를 먹어도 덜 참이 되지 않는다.
const (
	halfLifeHistory  = 90.0
	halfLifeDecision = 720.0
	halfLifeNone     = 0.0
)

// 가산 항 (설계 6-5).
const (
	pinnedBonus    = 0.5
	kindBonus      = 0.3
	severityBonus  = 0.2
	hitWeight      = 0.2
	scopeBonus     = 0.2
	linkBonus      = 0.1
	phraseBonus    = 0.3
	importanceStep = 0.1
)

// bonusCap 은 가산의 고삐다. 상한이 있어야 「잘 맞은 평범한 기억」 이 「안 맞은
// 고정 결정」 을 이긴다 (설계 6-5 고삐 ②).
// 설계 6-5 가 1.5·1.8·2.2 를 재고 고르라고 했다. 실측(206건 골든셋)에서
// 1.5 가 MRR 0.523, 1.8 이 0.523, 2.2 가 0.523 로 같거나 낮아 1.5 를 골랐다.

// decayFloor 는 감쇠 바닥이다. 오래돼도 최대 70%까지만 깎인다 (설계 6-5).
const decayFloor = 0.3

// invalidPenalty 는 무효가 된 기억에 매기는 감점이다.
const invalidPenalty = 0.5

// foldedPenalty 는 gc 가 본문을 접은(cold) 기억에 매기는 감점이다. 지우는
// 것이 아니라 뒤로 미는 것이다 — 접힌 기억도 요약은 그대로 맞는 말이다
// (설계 4-1 감쇠).
const foldedPenalty = 0.7

// baseImportance 는 아무 말도 없는 기억의 중요도다.
const baseImportance = 3

// rules 는 질의 하나가 한 번만 정하고 모든 결과에 똑같이 쓰는 것이다.
type rules struct {
	// Raw 는 가산·감쇠·신뢰계수를 다 끄고 관련도만 본다. eval 이 오배제율을
	// 잴 때만 쓴다 (설계 9-3).
	Raw bool
	// Cap 은 가산 상한이다 (mem.toml [search] bonus_cap).
	Cap        float64
	BoostScope string
	Linked     map[string]bool
	Phrase     map[int64]bool
	Now        time.Time
	// Types 는 이 저장소의 기억 종류 표다. 비면 기본 7종이다.
	Types model.TypeTable
}

// defaultTypes 는 표를 안 받은 자리가 쓸 기본표다. 줄마다 표를 새로 지으면
// 한 번 검색에 수백 번 짓게 된다.
var defaultTypes = model.DefaultTypes()

// typeSpec 은 이 종류의 취급 한 줄이다.
func (r rules) typeSpec(name string) model.TypeSpec {
	if len(r.Types) == 0 {
		return defaultTypes.Spec(name)
	}
	return r.Types.Spec(name)
}

// Parts 는 점수를 쪼갠 것이다. --explain 이 이걸 그대로 찍는다 (설계 6-10).
type Parts struct {
	RRF   float64 `json:"rrf"`
	Bonus float64 `json:"bonus"`
	Decay float64 `json:"decay"`
	Trust float64 `json:"trust"`
	// Diverse 는 MMR 이 깎은 몫이다. 1 이면 안 깎았다 (결정 44).
	Diverse float64 `json:"diverse,omitempty"`
	// Why 는 어느 가산이 붙었는지 사람 말로 적은 것이다.
	Why []string `json:"why,omitempty"`
	// From 은 어느 랭킹의 몇 위에서 왔는지다.
	From []string `json:"from,omitempty"`
}

// scoreOf 는 rrf × 가산 × 감쇠바닥 × 신뢰계수다. RRF 후보가 아니면 애초에
// 여기 안 온다 (설계 6-5 고삐 ①).
func scoreOf(row index.SearchRow, rrf float64, rung int, shared rules) (float64, Parts) {
	if shared.Raw {
		return rrf, Parts{RRF: rrf, Bonus: 1, Decay: 1, Trust: 1}
	}
	bonus, why := bonusOf(row, shared)
	decay := decayFloor + (1-decayFloor)*decayOf(row, shared)
	trust := TrustOf(rung)
	// 뒤집힌 결정은 --all 로만 보인다. 보이더라도 지금 맞는 것보다 위에 오면
	// 안 되니 절반으로 깎는다 (실데이터 시험 5절).
	if invalidated(row, shared.Now) {
		trust *= invalidPenalty
	}
	if row.State == index.StateCold {
		trust *= foldedPenalty
	}
	broken := Parts{RRF: rrf, Bonus: bonus, Decay: decay, Trust: trust, Why: why}
	return rrf * bonus * decay * trust, broken
}

func bonusOf(row index.SearchRow, shared rules) (float64, []string) {
	total := 1.0
	why := []string{}
	total, why = addBonus(total, why, row.Pinned, pinnedBonus, "고정")
	total, why = addBonus(total, why, shared.typeSpec(row.Type).SearchBonus, kindBonus, row.Type)
	total, why = addBonus(total, why, row.Severity == model.SeverityHigh, severityBonus, "high")
	total, why = addBonus(total, why, shared.BoostScope != "" && row.Scope == shared.BoostScope, scopeBonus, "scope")
	total, why = addBonus(total, why, shared.Linked[row.ID], linkBonus, "지목됨")
	total, why = addBonus(total, why, shared.Phrase[row.Docid], phraseBonus, "구절")
	if row.HitCount > 0 {
		total += hitWeight * math.Log(1+float64(row.HitCount))
		why = append(why, "조회수"+placeText(row.HitCount))
	}
	if row.Importance != 0 && row.Importance != baseImportance {
		total += importanceStep * float64(row.Importance-baseImportance)
		why = append(why, "중요도"+placeText(row.Importance))
	}
	if total > shared.Cap {
		total = shared.Cap
		why = append(why, "상한")
	}
	return total, why
}

func addBonus(total float64, why []string, on bool, value float64, name string) (float64, []string) {
	if !on {
		return total, why
	}
	return total + value, append(why, name)
}

func decayOf(row index.SearchRow, shared rules) float64 {
	now := shared.Now
	halfLife := halfLifeFor(row, shared.typeSpec(row.Type))
	if halfLife == halfLifeNone {
		return 1
	}
	age := now.Sub(time.Unix(row.CreatedAt, 0)).Hours() / 24
	if age < 0 {
		age = 0
	}
	return math.Pow(0.5, age/halfLife)
}

// halfLifeFor 는 설계 6-5 의 표다. 반감기 수는 코드에 남는다 — 표는 이름으로
// 고를 뿐이다. 끝난 todo 는 history 처럼 늙는다 (표 한 칸으로 못 적는 예외).
func halfLifeFor(row index.SearchRow, kind model.TypeSpec) float64 {
	if kind.TodoStatus && row.Status == model.StatusDone {
		return halfLifeHistory
	}
	switch kind.HalfLife {
	case model.HalfLifeHistory:
		return halfLifeHistory
	case model.HalfLifeDecision:
		return halfLifeDecision
	}
	return halfLifeNone
}
