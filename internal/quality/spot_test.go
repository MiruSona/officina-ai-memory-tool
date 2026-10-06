package quality

import (
	"fmt"
	"math/rand"
	"slices"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// TestBySpotKeepsRivals 는 자리 묶음만 넘겨도 결정 경쟁 목록(차례 포함)이 전부를
// 넘길 때와 같은지 본다 (R1).
func TestBySpotKeepsRivals(t *testing.T) {
	rng := rand.New(rand.NewSource(6))
	kinds := []string{"decision", "caution", "todo"}
	scopes := []string{"", "a", "b"}
	tags := []string{"x", "X", "y", "z", "w"}
	now := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	memories := make([]*model.Memory, 600)
	for at := range memories {
		m := &model.Memory{ID: fmt.Sprintf("m%03d", at%550), Type: kinds[rng.Intn(3)],
			Scope: scopes[rng.Intn(3)], Tags: []string{tags[rng.Intn(5)], tags[rng.Intn(5)]}}
		switch rng.Intn(6) {
		case 0:
			m.SupersededBy = "m000"
		case 1:
			m.InvalidAt = "2026-01-01"
		case 2:
			m.InvalidAt = "2027-01-01"
		}
		memories[at] = m
	}
	spec := model.TypeSpec{Gate: true}
	grouped := bySpot(memories)
	for _, m := range memories {
		want := LiveDecisionRivals(m, memories, now, spec)
		got := LiveDecisionRivals(m, grouped[spotOf(m)], now, spec)
		if !slices.Equal(got, want) {
			t.Fatalf("%s : got %d want %d", m.ID, len(got), len(want))
		}
	}
}
