package quality

import (
	"sort"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 쪼개진 짝 — C20 (나) (점검·정리 설계 ⑥ 절).
//
// 한 내용을 두 기억으로 나눠 쓴 짝이다. 글은 서로 다른 부분을 말해서 닮음 표로는
// 안 잡힌다. 그래서 「같은 실물 근거를 단다」로 먼저 묶고, 묶음 안에서만 짝을 본다
// — O(n²) 을 안 만든다.

// splitPair 는 쪼개진 짝 하나다. left 가 작은 id 다.
type splitPair struct {
	left, right string
	// source 는 둘이 같이 단 근거다 (file: 은 줄 번호를 뗀 꼴).
	source string
	// days 는 두 날짜의 차이(일)다.
	days int
	// align·score 는 두 기억의 정렬값·통째 닮음이다. 둘 다 history 인 짝의 문턱
	// (historyKept)이 본다.
	align, score float64
}

const (
	// splitSourceMax 를 넘는 기억이 단 근거는 묶음에서 뺀다 — README 처럼 흔한 근거는
	// 쪼개짐 신호가 아니다.
	splitSourceMax = 20
	// splitDaysMax 는 「가까운 날짜」의 자다.
	splitDaysMax = 7
	// splitTitleMin 은 겹쳐야 할 제목 낱말 수다.
	splitTitleMin = 2
)

// splitPairs 는 쪼개진 짝을 모두 고른다. 넷을 다 만족해야 한다.
//
//	① file:·commit: 근거를 하나 이상 같이 단다 (file: 은 줄 번호를 떼고 견준다)
//	② 날짜가 splitDaysMax 일 안이다
//	③ 제목 낱말(두 글자 이상)이 splitTitleMin 개 이상 겹친다
//	④ 본문이 같지 않고, 문장 정렬값이 0.35(C13·C17 의 자) 아래다
//
// 공통 조건(같은 scope·종류 · 둘 다 살아 있음 · 모음 기억·todo 아님)도 본다.
// 짝이 C17·C13 에 걸렸는지는 부르는 쪽(routeNear)이 가른다.
func splitPairs(memories []*model.Memory, docs []*Doc, opt RepoOptions) []splitPair {
	groups := map[string][]int{}
	for at, m := range memories {
		if m.Type == model.TypeObservation || m.Type == model.TypeTodo || !Live(m, opt.Now) {
			continue
		}
		for _, key := range artifactKeys(m.Sources) {
			groups[key] = append(groups[key], at)
		}
	}
	keys := make([]string, 0, len(groups))
	for key, spots := range groups {
		if len(spots) >= 2 && len(spots) <= splitSourceMax {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	words := map[int]map[string]bool{}
	titleOf := func(at int) map[string]bool {
		if words[at] == nil {
			words[at] = titleWords(memories[at])
		}
		return words[at]
	}
	seen := map[nearPairKey]bool{}
	out := []splitPair{}
	for _, key := range keys {
		spots := groups[key]
		for one := 0; one < len(spots); one++ {
			for two := one + 1; two < len(spots); two++ {
				a, b := spots[one], spots[two]
				left, right := memories[a], memories[b]
				pair := pairKeyOf(left.ID, right.ID)
				if seen[pair] || left.ID == right.ID || left.Type != right.Type || left.Scope != right.Scope {
					continue
				}
				days, ok := dayGap(left.Date, right.Date)
				if !ok || days > splitDaysMax {
					continue
				}
				if sharedTagCount(titleOf(a), titleOf(b)) < splitTitleMin {
					continue
				}
				if SameBody(docs[a], docs[b]) {
					continue
				}
				cut := staleConflictFloor()
				align := alignOf(docs[a], docs[b], cut, cut).Best
				if align >= cut {
					continue
				}
				seen[pair] = true
				out = append(out, splitPair{left: pair[0], right: pair[1], source: key, days: days,
					align: align, score: Score(docs[a], docs[b])})
			}
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].left != out[b].left {
			return out[a].left < out[b].left
		}
		return out[a].right < out[b].right
	})
	return out
}

// artifactKeys 는 기억 하나의 file:·commit: 근거를 견줄 꼴로 낸다. 한 기억 안에서
// 같은 열쇠는 한 번만 낸다 — 같은 파일의 다른 줄을 둘 단 기억이 자기와 짝이 되면 안 된다.
func artifactKeys(sources []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, source := range sources {
		source = strings.TrimSpace(source)
		switch model.SourceKind(source) {
		case "file":
			source = trimLineSuffix(source)
		case "commit":
		default:
			continue
		}
		if seen[source] {
			continue
		}
		seen[source] = true
		out = append(out, source)
	}
	return out
}

// trimLineSuffix 는 `file:경로:줄` · `file:경로:줄-줄` 꼴에서 줄 번호를 뗀다.
func trimLineSuffix(source string) string {
	colon := strings.LastIndex(source, ":")
	if colon <= len(model.SourceFile) {
		return source
	}
	tail := source[colon+1:]
	if tail == "" {
		return source
	}
	for _, letter := range tail {
		if (letter < '0' || letter > '9') && letter != '-' {
			return source
		}
	}
	return source[:colon]
}

// titleWords 는 제목 낱말 집합이다 (text.go 의 낱말 자르기). 두 글자 이상만 두고,
// 한글은 토씨 하나를 뗀다 — 「설계를」과 「설계」가 남남이 되지 않게.
func titleWords(m *model.Memory) map[string]bool {
	set := map[string]bool{}
	for _, word := range wordSplit(m.DisplayTitle()) {
		letters := []rune(word)
		if len(letters) < 2 {
			continue
		}
		if isHangul(letters[0]) {
			word = stripTail(word)
		}
		set[word] = true
	}
	return set
}

// dayGap 은 두 날짜의 차이(일)다. 하나라도 못 읽으면 ok 가 거짓이다.
func dayGap(one, other string) (int, bool) {
	first, err := time.ParseInLocation("2006-01-02", one, time.UTC)
	if err != nil {
		return 0, false
	}
	second, err := time.ParseInLocation("2006-01-02", other, time.UTC)
	if err != nil {
		return 0, false
	}
	gap := int(second.Sub(first).Hours() / 24)
	if gap < 0 {
		gap = -gap
	}
	return gap, true
}
