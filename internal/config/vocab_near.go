package config

// 표준 태그 가운데 「같은 말을 다르게 적은 것」을 찾는 자다. `mem tags --suggest`
// 와 표준 밖 태그 거절 안내가 같은 자를 쓴다.

import (
	"sort"
	"strings"
)

const (
	// nearWordMin 은 견줄 태그의 최소 글자 수다. 두 자짜리는 한 글자만 달라도
	// 아예 다른 말이라 후보로 올리면 잡음이다.
	nearWordMin = 3
	// nearShortRunes 는 편집 거리를 하나로 좁히는 길이다. 짧은 태그일수록 한
	// 글자 차이가 다른 뜻이 된다.
	nearShortRunes = 5
	// nearEditMax 는 긴 태그에서 오타로 볼 글자 차이다.
	nearEditMax = 2
)

// nearTag 는 후보 하나와 그 순위 재료다. 점수가 같으면 길이가 비슷한 쪽이 먼저다.
type nearTag struct {
	Tag   string
	Score int
	Gap   int
}

// NearTags 는 표준 태그 가운데 tag 와 가까운 것을 가까운 차례로 최대 limit개 준다.
// 점수는 0 앞머리 · 1 포함 · 1+거리 오타 차례다.
func (v Vocab) NearTags(tag string, limit int) []string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "" || limit <= 0 {
		return nil
	}
	found := []nearTag{}
	for _, known := range v.StandardTags() {
		if known == tag {
			continue
		}
		score, near := nearScore(tag, known)
		if !near {
			continue
		}
		found = append(found, nearTag{Tag: known, Score: score, Gap: runeGap(tag, known)})
	}
	sort.Slice(found, func(a, b int) bool { return nearBefore(found[a], found[b]) })
	if len(found) > limit {
		found = found[:limit]
	}
	out := make([]string, 0, len(found))
	for _, one := range found {
		out = append(out, one.Tag)
	}
	return out
}

func nearBefore(left, right nearTag) bool {
	if left.Score != right.Score {
		return left.Score < right.Score
	}
	if left.Gap != right.Gap {
		return left.Gap < right.Gap
	}
	return left.Tag < right.Tag
}

// nearScore 는 두 태그가 얼마나 가까운지다. 두 번째 값이 거짓이면 후보가 아니다.
func nearScore(tag, known string) (int, bool) {
	short := len([]rune(tag))
	if other := len([]rune(known)); other < short {
		short = other
	}
	if short < nearWordMin {
		return 0, false
	}
	if SharesHead(tag, known) {
		return 0, true
	}
	if strings.Contains(tag, known) || strings.Contains(known, tag) {
		return 1, true
	}
	most := nearEditMax
	if short < nearShortRunes {
		most = 1
	}
	distance := EditDistance(tag, known)
	if distance > most {
		return 0, false
	}
	return 1 + distance, true
}

func runeGap(left, right string) int {
	gap := len([]rune(left)) - len([]rune(right))
	if gap < 0 {
		return -gap
	}
	return gap
}

// SharesHead 는 한쪽이 다른 쪽의 앞머리인지다. `aimemory` 와 `aimemorytool` 은
// 사람이 같은 것을 가리키며 길이만 다르게 쓴 흔한 꼴이다.
func SharesHead(left, right string) bool {
	return strings.HasPrefix(left, right) || strings.HasPrefix(right, left)
}

// EditDistance 는 두 낱말의 글자 차이다 (Levenshtein). 태그는 짧아서 표를
// 통째로 잡아도 싸다.
func EditDistance(left, right string) int {
	a, b := []rune(left), []rune(right)
	row := make([]int, len(b)+1)
	for at := range row {
		row[at] = at
	}
	for i := 1; i <= len(a); i++ {
		last := row[0]
		row[0] = i
		for j := 1; j <= len(b); j++ {
			keep := row[j]
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			row[j] = minOfThree(row[j]+1, row[j-1]+1, last+cost)
			last = keep
		}
	}
	return row[len(b)]
}

func minOfThree(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
