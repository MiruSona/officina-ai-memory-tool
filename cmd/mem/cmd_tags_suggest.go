package main

// mem tags --suggest — 동의어·대표말 후보를 **제안만** 한다 (설계 결정 40).
//
// 동의어 표는 잘못 넣으면 오탐이 폭발한다. 그래서 이 명령은 아무것도 안
// 고치고, 사람이 칠 `mem tags --alias …` · `mem.toml [canon]` 줄만 보여준다.
//
// 후보를 뽑는 자리는 둘이다.
//
//	① 괄호 표기   `색인(indexing)` 처럼 한 글에 두 말이 나란히 적힌 것
//	② 흔들린 태그 표준 목록 밖 태그가 표준 태그와 앞머리가 겹치거나 한 글자 차이인 것
//
// 0건 질의(miss.jsonl)로 뽑는 후보는 `mem lint --synonym` 이 이미 한다. 여기서
// 다시 안 뽑는다 — 같은 것을 두 자리에서 재면 값이 어긋난다.
//
// 결정 40 의 「곁은 닮았는데 내용만 안 닮은 쌍」은 **안 넣었다.** 태그·scope
// 만으로 고른 쌍의 드문 낱말을 곱하면 잡음이 잡음을 낳는다(실기억 206건에서
// `poc03 → 30개는` 같은 짝이 나왔다). 뜻 닮음을 재는 자(임베딩)가 붙은 뒤에
// 하는 것이 맞다.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/safe"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// suggestion 은 후보 한 줄이다. 왼쪽을 오른쪽으로 모으자는 제안이다.
type suggestion struct {
	From  string
	To    string
	Why   string
	Count int
}

// parenPair 는 `색인(indexing)` 처럼 두 말이 나란히 적힌 자리다. 괄호 안이
// 한 낱말일 때만 본다 — 문장이 들어가면 표기 변이가 아니다.
var parenPair = regexp.MustCompile(`([가-힣A-Za-z][가-힣A-Za-z0-9_-]{1,29})\s*\(([가-힣A-Za-z][가-힣A-Za-z0-9_-]{1,29})\)`)

const (
	// suggestMin 은 후보로 올릴 최소 횟수다. 한 번 나온 표기는 그 글쓴이의
	// 버릇일 뿐이라 표에 넣을 근거가 못 된다.
	suggestMin = 2
	// suggestMax 는 화면에 찍을 줄 수다.
	suggestMax = 20
	// latinMin 은 로마자 낱말의 최소 글자 수다. 두 글자는 `or`·`db` 처럼
	// 이음말이 많아 표기 변이로 보면 잡음이 된다 (실기억 206건에서 확인).
	latinMin = 3
	// hangulMin 은 한글 낱말의 최소 글자 수다.
	hangulMin = 2
	// tagEditMax 는 오타로 볼 글자 차이다. 둘이면 `gc`↔`ai` 같은 짧은 태그가
	// 다 걸려서 하나로 둔다 (실기억 206건에서 18가지 중 16가지가 잡음이었다).
	tagEditMax = 1
	// tagWordMin 은 견줄 만한 태그의 최소 글자 수다. 짧은 태그는 한 글자만
	// 달라도 아예 다른 말이다.
	tagWordMin = 5
	// suggestRoom 은 후보 한 낱말에 쓰는 룬 수다.
	suggestRoom = 30
)

// runTagsSuggest 는 후보를 뽑아 보여준다. **아무것도 안 고친다.**
func runTagsSuggest(repository *config.Repository, opened *store.Store) int {
	memories := allMemories(opened)
	found := append(parenSuggestions(memories), tagDriftSuggestions(memories, vocabOf(repository))...)
	found = mergeSuggestions(found)
	if len(found) == 0 {
		fmt.Println(i18n.T(i18n.TagsSuggestNone))
		return exitOK
	}
	fmt.Println(i18n.T(i18n.TagsSuggestHead, len(found)))
	if len(found) > suggestMax {
		found = found[:suggestMax]
	}
	for _, one := range found {
		fmt.Println(i18n.T(i18n.TagsSuggestRow, one.From, one.To, one.Why))
	}
	fmt.Println(i18n.T(i18n.TagsSuggestOnly))
	fmt.Println(i18n.T(i18n.TagsSuggestSteps))
	for _, one := range found {
		fmt.Println("  " + suggestStep(one))
	}
	return exitOK
}

// suggestStep 은 사람이 손으로 옮겨 적을 한 줄이다. 태그면 `tags --alias`,
// 본문 낱말이면 `mem.toml [canon]` 이다.
func suggestStep(one suggestion) string {
	if model.IsTag(one.From) && model.IsTag(one.To) {
		return "mem tags --alias " + one.From + "=" + one.To
	}
	return `mem.toml [canon] : "` + one.To + `" = ["` + one.From + `"]`
}

// parenSuggestions 는 괄호 표기다. `색인(indexing)` 이면 indexing → 색인 을
// 제안한다 — 괄호 밖이 그 글이 고른 대표말이다.
func parenSuggestions(memories []*model.Memory) []suggestion {
	counts := map[[2]string]int{}
	for _, one := range memories {
		text := one.Title + "\n" + one.Summary + "\n" + one.Body
		for _, match := range parenPair.FindAllStringSubmatch(text, -1) {
			outside, inside := match[1], match[2]
			if !pairWorthIt(outside, inside) {
				continue
			}
			counts[[2]string{strings.ToLower(inside), outside}]++
		}
	}
	return rowsOf(counts, "괄호 표기")
}

// pairWorthIt 은 두 말이 표기 변이 후보인지다. **한쪽은 한글, 한쪽은 로마자**
// 여야 「같은 것을 다른 말로 쓴 것」이다.
func pairWorthIt(outside, inside string) bool {
	if strings.EqualFold(outside, inside) || hasHangul(outside) == hasHangul(inside) {
		return false
	}
	return longEnough(outside) && longEnough(inside)
}

func longEnough(word string) bool {
	least := latinMin
	if hasHangul(word) {
		least = hangulMin
	}
	return len([]rune(word)) >= least
}

func hasHangul(text string) bool {
	for _, letter := range text {
		if letter >= 0xac00 && letter <= 0xd7a3 {
			return true
		}
	}
	return false
}

// tagDriftSuggestions 는 흔들린 태그다. 표준 목록 밖 태그가 표준 태그와 앞머리가
// 겹치거나 한 글자만 다르면 같은 것을 다르게 적은 것일 때가 많다 (`aimemory` ↔ `aimemorytool`).
func tagDriftSuggestions(memories []*model.Memory, vocab config.Vocab) []suggestion {
	offList := map[string]int{}
	for _, one := range memories {
		for _, tag := range one.Tags {
			if vocab.TagDenied(tag) {
				continue
			}
			if fixed, known, alias := vocab.NormalizeTag(tag); !known && !alias {
				offList[fixed]++
			}
		}
	}
	standard := vocab.StandardTags()
	counts := map[[2]string]int{}
	for tag, count := range offList {
		if near := nearestTag(tag, standard); near != "" {
			counts[[2]string{tag, near}] += count
		}
	}
	// 흔들린 태그는 한 번만 나와도 값이 있다. 표준 목록과 견준 것이라 근거가
	// 이미 있어서다.
	found := []suggestion{}
	for pair, count := range counts {
		found = append(found, suggestion{From: pair[0], To: pair[1], Why: "흔들린 태그", Count: count})
	}
	return found
}

// nearestTag 는 표준 태그 가운데 「같은 말을 다르게 적은 것」으로 볼 만한
// 하나다. 자는 둘이다 — **앞이 겹치거나**(`aimemory`↔`aimemorytool`)
// **한 글자 오타**거나. 여럿이면 짧은 쪽을 고른다.
func nearestTag(tag string, standard []string) string {
	if len([]rune(tag)) < tagWordMin {
		return ""
	}
	best := ""
	for _, known := range standard {
		if known == tag || len([]rune(known)) < tagWordMin {
			continue
		}
		if !sharesHead(tag, known) && editDistance(tag, known) > tagEditMax {
			continue
		}
		if best == "" || len(known) < len(best) {
			best = known
		}
	}
	return best
}

// sharesHead 는 한쪽이 다른 쪽의 앞머리인지다. `aimemory` 와 `aimemorytool` 은
// 사람이 같은 것을 가리키며 길이만 다르게 쓴 흔한 꼴이다.
func sharesHead(left, right string) bool {
	return strings.HasPrefix(left, right) || strings.HasPrefix(right, left)
}

// editDistance 는 두 낱말의 글자 차이다 (Levenshtein). 태그는 짧아서 표를
// 통째로 잡아도 싸다.
func editDistance(left, right string) int {
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
			row[j] = min3(row[j]+1, row[j-1]+1, last+cost)
			last = keep
		}
	}
	return row[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// rowsOf 는 셈한 짝을 후보 줄로 만든다. 하한을 못 넘으면 버린다.
func rowsOf(counts map[[2]string]int, why string) []suggestion {
	found := []suggestion{}
	for pair, count := range counts {
		if count < suggestMin {
			continue
		}
		found = append(found, suggestion{From: pair[0], To: pair[1], Why: why, Count: count})
	}
	return found
}

// mergeSuggestions 는 같은 짝을 한 줄로 모으고 잦은 것부터 놓는다. 출력은
// 기억 글에서 온 낱말이라 safe 를 지난다 (결정 59).
func mergeSuggestions(found []suggestion) []suggestion {
	merged := map[[2]string]suggestion{}
	for _, one := range found {
		key := [2]string{one.From, one.To}
		kept, seen := merged[key]
		if !seen {
			merged[key] = one
			continue
		}
		kept.Count += one.Count
		kept.Why += " · " + one.Why
		merged[key] = kept
	}
	out := make([]suggestion, 0, len(merged))
	for _, one := range merged {
		one.From = safe.Summary(one.From, suggestRoom)
		one.To = safe.Summary(one.To, suggestRoom)
		out = append(out, one)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Count != out[b].Count {
			return out[a].Count > out[b].Count
		}
		return out[a].From < out[b].From
	})
	return out
}
