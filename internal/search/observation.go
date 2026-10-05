package search

import (
	"os"

	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 모음 기억(observation)의 검색 자리 (자동쌓기설계 3-5).
//
//   - 순위 가산은 0 이다. 낡았으면 무효와 같은 자(신뢰 ×0.5)로 깎고 `[낡음]` 을 단다.
//   - 뜨면 같은 줄 아래 근거 id·제목을 들여 쓴 줄로 보인다 (많아야 3줄). 근거가 따로
//     순위에 끼지 않는다 — 결정 44 그대로다.
//   - `[search] obs_strict_full` 을 켜면 모음 기억은 칸 0·1 에서 온 것만 strict 다.
//     모음은 여러 기억의 낱말을 한 문서에 모아 칸 2(낱말 하나 빼기)에 걸리기 쉽다.

// basisShown 은 결과 줄 아래 펼치는 근거 수다. 나머지는 「… 외 N건」 이다.
const basisShown = 3

// obsFullSwitch 는 **시험용 뒷문**이다 (MEM_MIX 와 같은 성격). 같은 exe 로 두 판을
// 재려고 쓴다. 주면 mem.toml 값보다 세다.
var obsFullSwitch = os.Getenv("MEM_OBS_FULL")

// obsStrictFull 은 모음 기억을 칸 0·1 에서만 strict 로 칠지다.
func (o Options) obsStrictFull() bool {
	switch obsFullSwitch {
	case "1", "true", "on":
		return true
	case "0", "false", "off":
		return false
	}
	return o.Search.ObsStrictFull
}

// IsStrict 는 이 답을 「정확히 찾았다」 로 세는지다. 칸 자(Strict)에 모음 기억
// 손잡이 하나를 더 얹은 것이다. eval 과 결과의 「넓혀서 찾음」 표시가 이것을 쓴다.
func (h Hit) IsStrict() bool {
	return Strict(h.Rung) && !h.ObsLoose
}

// markObservations 는 모음 기억 답에 낡음·느슨함 표시를 단다. 모음 기억이 없는
// 결과는 한 줄도 안 바뀐다.
func markObservations(hits []Hit, options Options) {
	full := options.obsStrictFull()
	for at := range hits {
		if hits[at].Type != model.TypeObservation {
			continue
		}
		hits[at].ObsLoose = full && hits[at].Rung > RungAnd
	}
}

// fillBasis 는 화면에 남은 모음 기억 답마다 근거 id·제목을 채운다. 색인이 여럿이면
// 근거가 나온 첫 색인을 쓴다. 보류 근거는 숨긴다 (index.BasisOf).
func fillBasis(hits []Hit, sources []Source) {
	for at := range hits {
		if hits[at].Type != model.TypeObservation {
			continue
		}
		for _, from := range sources {
			if from.DB == nil {
				continue
			}
			found, err := from.DB.BasisOf(hits[at].ID)
			if err == nil && len(found) > 0 {
				hits[at].Basis = found
				break
			}
		}
	}
}

// BasisIDs 는 모음 기억 답의 근거 id 다. 모음이 아니면 빈 목록이다.
func (h Hit) BasisIDs() []string {
	out := make([]string, 0, len(h.Basis))
	for _, one := range h.Basis {
		out = append(out, one.ID)
	}
	return out
}

// staleOf 는 색인이 적은 낡음 까닭을 그대로 넘긴다. 빈 글이면 안 낡았다.
func staleOf(row index.SearchRow) string { return row.ObsStale }
