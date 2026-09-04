package quality

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// TestDupScale 은 저장소가 커졌을 때 중복 검사에 드는 시간을 잰다.
// `lint ≤ 10초`(G4 · 결정 35)를 문장 정렬이 까먹지 않는지 보는 자리다.
// 실패시키지 않고 표만 남긴다 — `-run TestDupScale -v`.
func TestDupScale(t *testing.T) {
	_, seed := loadForTune(t)
	out := "| 기억 수 | 넓히기·정렬 끔 | 켬 | 배 |\n| --- | --- | --- | --- |\n"
	for _, count := range []int{500, 2000, 5000} {
		memories := blowUp(seed, count)
		off := dupElapsed(t, memories, false)
		on := dupElapsed(t, memories, true)
		out += fmt.Sprintf("| %d | %.2f초 | %.2f초 | %.1f |\n",
			count, off.Seconds(), on.Seconds(), on.Seconds()/off.Seconds())
	}
	t.Log("\n" + out)
}

// dupElapsed 는 중복 검사 한 판에 걸린 시간이다.
func dupElapsed(t *testing.T, memories []*model.Memory, wide bool) time.Duration {
	t.Helper()
	before := nowKnobs()
	one := before
	one.wide = wide
	if !wide {
		one.cut, one.soft = 2.0, 0
	}
	one.apply()
	defer before.apply()
	opt := RepoOptions{Options: Options{Config: config.Default("scale"),
		Vocab: testdataVocab(t), Now: testNow()}}
	report := RepoReport{ByID: map[string][]Finding{}}
	started := time.Now()
	report.duplicates(memories, opt)
	return time.Since(started)
}

// blowUp 은 씨앗 기억을 조금씩 바꿔 늘린다. 글자를 그대로 복사하면 전부 같은
// 기억이 되어 후보 좁히기가 뜻을 잃는다.
func blowUp(seed []*model.Memory, count int) []*model.Memory {
	out := make([]*model.Memory, 0, count)
	for at := 0; len(out) < count; at++ {
		one := *seed[at%len(seed)]
		round := strconv.Itoa(at / len(seed))
		one.ID = fmt.Sprintf("20260823-%08x", at)
		one.Summary = strings.Replace(one.Summary, " ", " "+round+"번째 ", 1)
		one.Body = one.Body + "\n\n" + round + "번째 판에서 다시 적은 것이다. 숫자는 " + round + " 이다."
		out = append(out, &one)
	}
	return out
}
