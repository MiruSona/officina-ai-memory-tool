package search

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 모음 기억(B1) 검색 자리 — 근거 줄 · [낡음] · strict 손잡이 (자동쌓기설계 3-5).

// 모음 기억이 없는 답 목록은 표시 단계에서 한 칸도 안 바뀐다.
func TestObservationMarksLeavePlainHitsAlone(t *testing.T) {
	hits := []Hit{{ID: "20260822-aaaa0001", Type: model.TypeDecision, Rung: RungDropWord},
		{ID: "20260822-aaaa0002", Type: model.TypeFact, Rung: RungPlain}}
	before := append([]Hit(nil), hits...)
	obsFullSwitch = "1"
	defer func() { obsFullSwitch = "" }()
	markObservations(hits, Options{})
	fillBasis(hits, nil)
	if !reflect.DeepEqual(before, hits) {
		t.Fatalf("모음 기억이 없는데 답이 바뀌었다 : %+v", hits)
	}
	if rows := tableRows(hits); answerRows(rows) != len(rows) {
		t.Fatalf("근거 줄이 없는데 줄 수가 어긋난다 : %v", rows)
	}
}

func TestObservationStrictKnob(t *testing.T) {
	hits := []Hit{{ID: "20261005-cccc0001", Type: model.TypeObservation, Rung: RungDropWord},
		{ID: "20261005-cccc0002", Type: model.TypeObservation, Rung: RungAnd}}
	markObservations(hits, Options{})
	if !hits[0].IsStrict() {
		t.Fatal("손잡이가 꺼졌는데 칸 2 모음 기억을 strict 에서 뺐다")
	}
	markObservations(hits, Options{Search: searchWith(true)})
	if hits[0].IsStrict() || !hits[1].IsStrict() {
		t.Fatalf("obs_strict_full 은 칸 0·1 만 strict 다 : %+v", hits)
	}
}

func TestBasisRowsAndStaleMark(t *testing.T) {
	hit := Hit{ID: "20261005-cccc0001", Type: model.TypeObservation, Summary: "모음 카드 요약이다",
		Stale: index.ObsStaleChanged}
	for at := 0; at < 5; at++ {
		hit.Basis = append(hit.Basis, index.BasisRef{ID: "20260822-aaaa000" + string(rune('1'+at)), Title: "근거"})
	}
	rows := tableRows([]Hit{hit})
	if len(rows) != 1+basisShown+1 || answerRows(rows) != 1 {
		t.Fatalf("근거 줄은 셋 + 「외 N건」 이다 : %v", rows)
	}
	if !strings.Contains(rows[0], "[낡음]") || !strings.Contains(rows[len(rows)-1], "외 2건") {
		t.Fatalf("[낡음] 이나 「외 N건」 이 없다 : %v", rows)
	}
}

func searchWith(full bool) config.SearchConfig {
	return config.SearchConfig{ObsStrictFull: full}
}
