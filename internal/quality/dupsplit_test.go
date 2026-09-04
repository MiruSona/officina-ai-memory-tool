package quality

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 놓친 DUP 을 **1단(후보에 못 올랐다) / 2단(후보엔 올랐는데 걸러졌다)** 으로 가르는 표다
// (v0.4 설계 2절 ② 차례 1). 값을 만드는 시험이라 실패시키지 않고 표만 남긴다 — `-v` 로 본다.
//
// 짝은 골든셋 note 에 적힌 `xxxxxxxx` 를 읽어 만든다. 「위와 같음」·「위와 짝」은 바로 앞
// 줄의 짝을 물려받는다.

var noteIDRe = regexp.MustCompile("`([0-9a-f]{8})`")

// dupPairsOf 는 골든셋 DUP 짝 목록이다.
func dupPairsOf(set *GoldenSet, ids map[string]bool) [][2]string {
	pairs := map[[2]string]bool{}
	prev := []string{}
	prevID := ""
	for _, one := range set.Flag {
		if !hasString(one.Defects, TypeDup) {
			continue
		}
		found := []string{}
		for _, hit := range noteIDRe.FindAllStringSubmatch(one.Note, -1) {
			full := fullIDOf(hit[1], ids)
			if full != "" {
				found = append(found, full)
			}
		}
		if len(found) == 0 && strings.Contains(one.Note, "위와") {
			found = append(found, prev...)
			if prevID != "" {
				found = append(found, prevID)
			}
		}
		for _, other := range found {
			if other == one.ID {
				continue
			}
			key := [2]string{one.ID, other}
			if key[0] > key[1] {
				key[0], key[1] = key[1], key[0]
			}
			pairs[key] = true
		}
		prev, prevID = found, one.ID
	}
	out := make([][2]string, 0, len(pairs))
	for key := range pairs {
		out = append(out, key)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a][0] != out[b][0] {
			return out[a][0] < out[b][0]
		}
		return out[a][1] < out[b][1]
	})
	return out
}

func fullIDOf(short string, ids map[string]bool) string {
	for id := range ids {
		if strings.HasSuffix(id, "-"+short) {
			return id
		}
	}
	return ""
}

// pairVerdict 는 짝 하나가 어디서 떨어졌는지다. Nearest 의 갈래를 그대로 따라 간다.
type pairVerdict struct {
	candidate bool
	narrow    bool
	sketched  bool
	same      bool
	score     float64
	align     alignInfo
	stageTwo  bool
	stage     string // "1단" · "2단" · "잡음"
	why       string
}

func diagnosePair(ss *Session, doc, other *Doc, at int, warn float64) pairVerdict {
	out := pairVerdict{}
	found := ss.candidateIdx(doc)
	round := ss.round
	for _, one := range found {
		if one == at {
			out.candidate = true
		}
	}
	if !out.candidate {
		// 후보가 아니어도 값을 채운다 — 「후보를 넓히면 잡히나」를 여기서 답한다.
		out.score = Score(doc, other)
		out.align = alignOf(doc, other, AlignCut, 0)
		out.stageTwo = passesStageTwo(out.align, true)
		out.stage = "1단"
		out.why = fmt.Sprintf("후보에 안 올랐다 (넓혀도 정렬 %.3f · 2단 %v)", out.align.Best, out.stageTwo)
		return out
	}
	out.narrow = ss.narrowStamp[at] == round
	out.sketched = ss.sketchStamp[at] == round
	out.same = SameBody(doc, other)
	out.score = Score(doc, other)
	out.align = alignOf(doc, other, AlignCut, 0)
	out.stageTwo = passesStageTwo(out.align, true)
	partial := !out.same && out.score < warn && out.sketched &&
		out.align.Best >= AlignCut && out.stageTwo
	switch {
	case out.same:
		out.stage, out.why = "잡음", "본문이 같다"
	case out.score >= warn && out.align.Best >= softAlignFloor:
		out.stage, out.why = "잡음", "통째 점수"
	case out.score >= warn:
		out.stage = "2단"
		out.why = fmt.Sprintf("점수는 넘었는데 띠 하한 미달 (정렬 %.3f < %.2f)", out.align.Best, softAlignFloor)
	case partial:
		out.stage, out.why = "잡음", "부분 중복"
	case !out.sketched:
		out.stage, out.why = "1단", "문장 조각 후보가 아니라 정렬을 아예 안 잰다"
	case out.align.Best < AlignCut:
		out.stage = "2단"
		out.why = fmt.Sprintf("정렬 %.3f < 문턱 %.2f", out.align.Best, AlignCut)
	case out.align.Clash || factsClash(out.align.Left, out.align.Right):
		out.stage, out.why = "2단", "사실 어긋남 (엄한 자)"
	case out.align.Pairs < alignMinPairs:
		out.stage = "2단"
		out.why = fmt.Sprintf("닮은 짝 조각 %d개 < %d", out.align.Pairs, alignMinPairs)
	case alignNeedFact && !sharedFact(out.align.Left, out.align.Right):
		out.stage, out.why = "2단", "같은 사실을 안 나눠 갖는다"
	default:
		out.stage, out.why = "2단", "알 수 없음"
	}
	return out
}

// TestDupStageSplit 은 골든셋 DUP 짝마다 1단/2단 어디서 떨어졌는지 찍는다.
func TestDupStageSplit(t *testing.T) {
	for _, loose := range []bool{false, true} {
		before := stageTwoLoose
		stageTwoLoose = loose
		t.Run(fmt.Sprintf("loose=%v", loose), func(t *testing.T) { dupStageSplit(t) })
		stageTwoLoose = before
	}
}

func dupStageSplit(t *testing.T) {
	set, memories := loadForTune(t)
	settings := config.Default("dup-split")
	opt := RepoOptions{Options: Options{Config: settings, Vocab: testdataVocab(t), Now: testNow()}}
	opt.Options = opt.Options.normalized()

	table := newSimilarFor(opt.Options)
	docs := map[string]*Doc{}
	at := map[string]int{}
	ids := map[string]bool{}
	for _, m := range memories {
		doc := NewDoc(m)
		at[doc.ID] = len(table.Docs())
		table.Add(doc)
		docs[doc.ID] = doc
		ids[doc.ID] = true
	}
	// 잡힌 건과 놓친 건을 가른다 — 채점기와 같은 길이다.
	fired := FiredRules(memories, opt)
	caught := map[string]bool{}
	for _, one := range set.Flag {
		if hasString(one.Defects, TypeDup) {
			caught[one.ID] = caughtBy(one, fired[one.ID])
		}
	}

	session := table.Session()
	warn := settings.Quality.DupWarn
	out := "| 짝 | 잡힘 | 후보 | 좁은후보 | 조각후보 | S | 정렬 | 짝조각 | 어긋남 | 같은사실 | 2단 | 어디서 |\n"
	out += "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n"
	stage := map[string]int{}
	for _, pair := range dupPairsOf(set, ids) {
		left, right := docs[pair[0]], docs[pair[1]]
		if left == nil || right == nil {
			continue
		}
		// 양쪽에서 다 본다 — 후보 뽑기는 대칭이 아니다. 나쁜 쪽(더 늦게 떨어지는 쪽)을 적는다.
		one := diagnosePair(session, left, right, at[right.ID], warn)
		two := diagnosePair(session, right, left, at[left.ID], warn)
		best := one
		if rankStage(two.stage) > rankStage(one.stage) {
			best = two
		}
		hit := caught[pair[0]] || caught[pair[1]]
		stage[best.stage]++
		out += fmt.Sprintf("| %s ↔ %s | %v | %v | %v | %v | %.3f | %.3f | %d | %v | %v | %v | %s %s |\n",
			short(pair[0]), short(pair[1]), hit, best.candidate, best.narrow, best.sketched,
			best.score, best.align.Best, best.align.Pairs, best.align.Clash,
			sharedFact(best.align.Left, best.align.Right), best.stageTwo, best.stage, best.why)
	}
	out += fmt.Sprintf("\n합계 : 잡음 %d · 1단에서 놓침 %d · 2단에서 놓침 %d\n",
		stage["잡음"], stage["1단"], stage["2단"])
	t.Log("\n" + out)
}

// rankStage 는 「더 멀리 간 쪽」이 큰 값이다.
func rankStage(name string) int {
	switch name {
	case "잡음":
		return 2
	case "2단":
		return 1
	}
	return 0
}

func short(id string) string {
	if at := strings.LastIndex(id, "-"); at >= 0 {
		return id[at+1:]
	}
	return id
}

var _ = model.Memory{}
