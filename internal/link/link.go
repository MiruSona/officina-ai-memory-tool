// Package link 는 기억끼리 이웃을 기계가 찾아 주는 자리다 (설계 결정 42).
//
// v0.2 는 「multisession 은 검색이 아니라 links 로 푼다」고 정해 놓고 링크를
// 채울 장치를 안 만들었다. 실기억 206건에 `links:` 가 2건뿐이라 이을 실이
// 없었다 (결정 41). 여기서 후보를 뽑고, 실제로 파일에 적는 것은 락을 잡은
// `mem index` 승격 자리 하나뿐이다.
//
// 이 패키지는 DB·파일을 모른다. 머리말에서 뽑은 Doc 만 받아 후보를 돌려준다 —
// 그래야 lint 규칙(D04)과 index 승격이 **같은 셈법**을 쓴다.
package link

import (
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// Doc 은 이웃을 재는 데 쓰는 기억 한 건이다. 본문은 안 쓴다 — 20k 에서 파일
// 2만 개를 여는 값이 이웃 하나 값보다 크다.
type Doc struct {
	ID      string
	Type    string
	Title   string
	Summary string
	Tags    []string
	Scope   string
	Sources []string
	Date    string
	Links   []string
}

// Candidate 는 이을 만한 상대 하나다. Why 는 어느 규칙이 걸렸는지다.
type Candidate struct {
	ID    string
	Score float64
	Why   []string
}

// 규칙별 몫 (결정 42 의 네 갈래). ①태그 2개 · ②같은 근거 는 혼자서 MinScore
// 를 넘고, ④식별자는 같은 scope 와 짝이라야 넘는다. 셋 다 「이것 하나로 이을
// 만하다」는 뜻이다.
//
// ③ 시간 인접만 몫이 작다. 머리말 date 는 날짜까지라 「같은 세션」을 못 갈라
// 내고, 같은 날 적은 기억을 다 이으면 하루치가 통째로 한 덩이가 된다.
// 다른 규칙을 밀어 주는 몫으로만 둔다 (결정 42 ③).
const (
	tagPairScore   = 1.0 // 태그 2개 겹침
	tagExtraScore  = 0.3 // 3개째부터 하나당
	tagOneScore    = 0.4 // 태그 1개만 겹침 (혼자서는 모자람)
	sourceScore    = 1.0 // 같은 sources (파일·폴더·커밋)
	nearDateScore  = 0.3 // 시간 인접 — 혼자서는 못 넘는다 (아래 주석)
	identityScore  = 0.8 // 공통 식별자 (같은 scope 일 때만 센다)
	sameScopeScore = 0.2
	closeScore     = 0.5 // 제목·태그 닮음 (같은 값끼리 차례를 가른다)
)

// MinScore 는 후보로 칠 최소 점수다. 1.0 = 「태그 2개가 겹친다」 한 줄로도
// 후보가 된다는 뜻이다.
//
// 실기억 206건 실측 (v2 골든셋 80건 · 링크 수 / 링크 받은 기억 수 / multisession r@5) :
//
//	0.8 → 978 / 201 / 0.375      1.0 → 970 / 201 / 0.375
//	1.4 → 639 / 167 / 0.375      1.8 → 291 / 101 / 0.312
//
// 1.8 에서 multisession 이 도로 내려간다. 0.8 과 1.0 은 사실상 같아 **뜻이
// 분명한 쪽(태그 2개)**인 1.0 을 골랐다.
//
// 이 수치는 이웃을 **머리말에 적던 때** 잰 것이다. 뒷정리에서 auto_links 표로
// 옮긴 뒤 실기억 206건 v2 80건 값은 r@5 0.716 · multisession 0.375 다.
const MinScore = 1.0

// MaxLinks 는 한 기억에 기계가 적는 이웃 수 상한이다 (결정 42 「상위 3~5개만」).
const MaxLinks = 5

// nearDays 는 「시간 인접」으로 치는 날 수다. 같은 날 일한 기억은 대개 같은
// 일의 조각이다.
const nearDays = 1

// Suggest 는 target 에 이을 만한 상대를 점수 내림차순으로 준다. 이미 적힌
// 링크와 자기 자신은 뺀다.
func Suggest(target Doc, others []Doc, max int) []Candidate {
	if max <= 0 {
		max = MaxLinks
	}
	already := map[string]bool{target.ID: true}
	for _, one := range target.Links {
		already[one] = true
	}
	mine := prepared(target)
	found := make([]Candidate, 0, len(others))
	for at := range others {
		if already[others[at].ID] {
			continue
		}
		score, why := pairScore(mine, prepared(others[at]))
		if score < MinScore {
			continue
		}
		found = append(found, Candidate{ID: others[at].ID, Score: score, Why: why})
	}
	return topOf(found, max)
}

// sortCandidates 는 점수 내림차순 · 같은 값이면 id 순이다. 차례가 늘 같아야
// 같은 저장소에서 같은 답이 난다.
func sortCandidates(found []Candidate) {
	sort.SliceStable(found, func(a, b int) bool {
		if found[a].Score != found[b].Score {
			return found[a].Score > found[b].Score
		}
		return found[a].ID < found[b].ID
	})
}

// facts 는 Doc 하나에서 미리 뽑아 둔 것이다. 206건이면 쌍이 2만이라 같은
// 문자열 쪼개기를 되풀이하면 안 된다.
type facts struct {
	doc     Doc
	tags    map[string]bool
	sources map[string]bool
	ids     map[string]bool
	day     time.Time
	dated   bool
}

func prepared(doc Doc) facts {
	out := facts{doc: doc, tags: setOf(doc.Tags), sources: setOf(sourceKeys(doc.Sources)),
		ids: identifiers(doc.Title + " " + doc.Summary)}
	if day, err := time.Parse("2006-01-02", doc.Date); err == nil {
		out.day, out.dated = day, true
	}
	return out
}

func pairScore(a, b facts) (float64, []string) {
	score := 0.0
	why := []string{}
	switch shared := overlap(a.tags, b.tags); {
	case shared >= 2:
		score += tagPairScore + tagExtraScore*float64(shared-2)
		why = append(why, "태그")
	case shared == 1:
		score += tagOneScore
		why = append(why, "태그1")
	}
	if overlap(a.sources, b.sources) > 0 {
		score += sourceScore
		why = append(why, "근거")
	}
	if nearInTime(a, b) {
		score += nearDateScore
		why = append(why, "시각")
	}
	// 같은 값이 여럿일 때 무엇을 고를지 — 제목·태그가 더 닮은 쪽이다. 이것이
	// 없으면 큰 태그 무리에서 모두가 **id 순 앞의 다섯 건**을 가리켜, 이웃이
	// 아니라 허브 다섯 개가 생긴다 (206건 실측에서 그 자국을 봤다).
	score += closeScore * Similarity(a.doc, b.doc)
	if a.doc.Scope != "" && a.doc.Scope == b.doc.Scope {
		score += sameScopeScore
		why = append(why, "scope")
		if overlap(a.ids, b.ids) > 0 {
			score += identityScore
			why = append(why, "식별자")
		}
	}
	return score, why
}

func nearInTime(a, b facts) bool {
	if !a.dated || !b.dated {
		return false
	}
	gap := a.day.Sub(b.day)
	if gap < 0 {
		gap = -gap
	}
	return gap <= nearDays*24*time.Hour
}

func overlap(a, b map[string]bool) int {
	small, big := a, b
	if len(big) < len(small) {
		small, big = big, small
	}
	count := 0
	for key := range small {
		if big[key] {
			count++
		}
	}
	return count
}

func setOf(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, one := range values {
		one = strings.ToLower(strings.TrimSpace(one))
		if one != "" {
			out[one] = true
		}
	}
	return out
}

// placeholderNote 는 `migrate` 자리표시를 견줄 꼴로 미리 접어 둔 것이다.
var placeholderNote = strings.ToLower(model.NoteMigrated)

// sourceKeys 는 근거를 견줄 수 있는 열쇠로 바꾼다. `file:a/b.go#12` 와
// `file:a/b.go` 는 같은 파일이라 같은 자리로 떨어져야 한다.
func sourceKeys(sources []string) []string {
	out := make([]string, 0, len(sources))
	for _, one := range sources {
		one = strings.ToLower(strings.TrimSpace(one))
		if one == "" {
			continue
		}
		// `migrate` 가 일괄로 넣는 자리표시는 「근거가 없다」는 뜻이라 근거로
		// 안 센다. 이것을 세면 이전한 저장소에서 근거 없는 기억끼리 다
		// 이어진다 (v0.4 리뷰 A R5). 사람이 적은 note: 는 그대로 센다.
		if one == placeholderNote {
			continue
		}
		// `file:a/b.go#12` 와 `file:a/b.go` 는 같은 파일이다. note: 는 사람이
		// 적은 한 줄이라 통째로 같아야 같은 근거다.
		if cut := strings.Index(one, "#"); cut >= 0 && !strings.HasPrefix(one, model.SourceNote) {
			one = one[:cut]
		}
		out = append(out, one)
	}
	return out
}

// identifierMin 은 식별자로 칠 최소 글자 수다. 짧은 낱말은 어디에나 있다.
const identifierMin = 4

// identifiers 는 제목·요약에서 「고유명사·식별자·경로」로 볼 만한 낱말이다.
// 경로·확장자·밑줄·점이 들어간 것과 영문 낱말만 센다 — 한글 보통명사를 넣으면
// 「검색」 하나로 저장소 절반이 이웃이 된다.
func identifiers(text string) map[string]bool {
	out := map[string]bool{}
	for _, word := range strings.FieldsFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("()[]{}<>,\"'`·|", r)
	}) {
		word = strings.Trim(strings.ToLower(word), ".:;!?")
		if len([]rune(word)) < identifierMin || !identifierLike(word) {
			continue
		}
		out[word] = true
	}
	return out
}

func identifierLike(word string) bool {
	letters := false
	for _, one := range word {
		if one > unicode.MaxASCII {
			// 한글이 섞이면 식별자가 아니다.
			return false
		}
		if unicode.IsLetter(one) {
			letters = true
		}
	}
	return letters
}

// blockMax 는 후보 통 하나가 가질 수 있는 최대 기억 수다. 이보다 흔한 태그는
// 주제가 아니라 갈래다 — 그 하나만 겹치는 짝을 다 재면 20k 에서 쌍이 억으로
// 간다. 이 통을 건너뛰어도 정말 이을 만한 짝은 **둘째 태그 · 같은 근거 · 같은
// scope 의 식별자** 통에서 다시 만난다. 잃는 것은 「흔한 태그 하나 + 닮음」
// 뿐이고 그건 원래 이을 근거가 약하다.
// 20k 가데이터 실측 : index --full 14.9초(자동 링크 몫 약 2.5초) · 이웃 75,292개.
// 실기억 206건에서는 어떤 통도 300 을 안 넘어 짝을 다 도는 것과 답이 같다
// (TestSuggestAllMatchesSuggest).
const blockMax = 300

// BlockMax 는 그 상한이다. `index` 가 「몇 건짜리 통을 건너뛰었나」를 사람에게
// 알릴 때 같이 찍는다 (V8).
func BlockMax() int { return blockMax }

// SuggestAll 은 모든 기억의 이웃 후보를 한 번에 낸다.
//
// 짝을 다 도는 대신 **같은 통에 든 짝만** 잰다. MinScore 1.0 을 넘으려면
// ①태그 겹침 ②같은 근거 ③같은 scope 의 같은 식별자 중 하나는 반드시
// 걸려야 하기 때문이다 — 나머지(시간 인접 0.3 · scope 0.2 · 닮음 ≤0.5)를
// 다 더해도 1.0 에 못 미친다. 그래서 이 셋을 열쇠로 통을 나누면 답이 안 바뀐다.
func SuggestAll(docs []Doc, max int) map[string][]Candidate {
	found, _ := SuggestAllStats(docs, max)
	return found
}

// Blocked 는 너무 커서 건너뛴 통 이야기다. **셈을 안 찍으면 저장소가 커질 때
// 조용히 링크 품질이 떨어진다** (스트레스 V8) — 20k 가데이터는 태그 통 14개가
// 전부 상한에 걸렸는데 도구가 한 마디도 안 했다.
type Blocked struct {
	// Skipped 는 건너뛴 통 수다.
	Skipped int
	// Biggest 는 그중 가장 큰 통의 기억 수다.
	Biggest int
}

// SuggestAllStats 는 이웃 후보와 함께 「건너뛴 통」을 알려 준다.
func SuggestAllStats(docs []Doc, max int) (map[string][]Candidate, Blocked) {
	if max <= 0 {
		max = MaxLinks
	}
	ready := make([]facts, len(docs))
	for at := range docs {
		ready[at] = prepared(docs[at])
	}
	buckets := map[string][]int{}
	for at := range ready {
		for _, key := range blockKeys(ready[at]) {
			buckets[key] = append(buckets[key], at)
		}
	}
	found := make([][]Candidate, len(docs))
	seen := map[int64]bool{}
	blocked := Blocked{}
	for _, group := range buckets {
		if len(group) > blockMax {
			blocked.Skipped++
			if len(group) > blocked.Biggest {
				blocked.Biggest = len(group)
			}
			continue
		}
		if len(group) < 2 {
			continue
		}
		for a := 0; a < len(group); a++ {
			for b := a + 1; b < len(group); b++ {
				one, two := group[a], group[b]
				key := int64(one)<<32 | int64(two)
				if seen[key] {
					continue
				}
				seen[key] = true
				score, why := pairScore(ready[one], ready[two])
				if score < MinScore {
					continue
				}
				found[one] = appendUnlinked(found[one], ready[one], docs[two].ID, score, why)
				found[two] = appendUnlinked(found[two], ready[two], docs[one].ID, score, why)
			}
		}
	}
	out := make(map[string][]Candidate, len(docs))
	for at := range found {
		if len(found[at]) == 0 {
			continue
		}
		out[docs[at].ID] = topOf(found[at], max)
	}
	return out, blocked
}

// appendUnlinked 는 이미 머리말에 적힌 이웃과 자기 자신은 뺀다.
func appendUnlinked(list []Candidate, mine facts, id string, score float64, why []string) []Candidate {
	if id == mine.doc.ID {
		return list
	}
	for _, one := range mine.doc.Links {
		if one == id {
			return list
		}
	}
	return append(list, Candidate{ID: id, Score: score, Why: why})
}

func topOf(found []Candidate, max int) []Candidate {
	sortCandidates(found)
	if len(found) > max {
		found = found[:max]
	}
	return found
}

// blockKeys 는 이 기억이 들어갈 통들이다. 위 SuggestAll 주석의 셋 그대로다.
func blockKeys(one facts) []string {
	keys := make([]string, 0, len(one.tags)+len(one.sources)+len(one.ids))
	for tag := range one.tags {
		keys = append(keys, "t\x00"+tag)
	}
	for src := range one.sources {
		keys = append(keys, "s\x00"+src)
	}
	// 식별자는 같은 scope 일 때만 점수가 붙는다 (pairScore). 통도 그렇게 나눈다.
	if one.doc.Scope != "" {
		for id := range one.ids {
			keys = append(keys, "i\x00"+one.doc.Scope+"\x00"+id)
		}
	}
	return keys
}

// SuggestSome 은 **바뀐 기억이 든 통만** 다시 잰다 (리뷰 B · V1 증분).
//
// 20k 에서 열 건을 고쳤을 뿐인데 이웃 표를 통째로 다시 만드느라 색인이 2.5초를
// 더 썼다 (자 0.5초). 이웃 점수는 **같은 통에 든 짝**에서만 나므로, 바뀐 기억과
// 통을 나눠 가진 기억만 다시 재면 답이 같다.
//
// 돌려주는 것은 ①다시 잰 기억들의 이웃 목록 ②다시 잰 기억 id 목록 ③건너뛴 통
// 이야기다. 부르는 쪽은 ②에 든 기억의 옛 줄만 지우고 ①을 넣으면 된다.
func SuggestSome(docs []Doc, max int, changed map[string]bool) (map[string][]Candidate, []string, Blocked) {
	if max <= 0 {
		max = MaxLinks
	}
	ready := make([]facts, len(docs))
	for at := range docs {
		ready[at] = prepared(docs[at])
	}
	buckets := map[string][]int{}
	for at := range ready {
		for _, key := range blockKeys(ready[at]) {
			buckets[key] = append(buckets[key], at)
		}
	}
	blocked := Blocked{}
	for _, group := range buckets {
		if len(group) > blockMax {
			blocked.Skipped++
			if len(group) > blocked.Biggest {
				blocked.Biggest = len(group)
			}
		}
	}
	// ① 다시 잴 기억 — 바뀐 것과 그것과 통을 나눠 가진 것.
	again := map[int]bool{}
	for at := range ready {
		if !changed[docs[at].ID] {
			continue
		}
		again[at] = true
		for _, key := range blockKeys(ready[at]) {
			group := buckets[key]
			if len(group) > blockMax {
				continue
			}
			for _, other := range group {
				again[other] = true
			}
		}
	}
	// ② 그 기억마다 **자기 통 전부**를 봐야 목록이 온전하다.
	out := map[string][]Candidate{}
	names := make([]string, 0, len(again))
	for at := range again {
		names = append(names, docs[at].ID)
		seen := map[int]bool{at: true}
		found := []Candidate{}
		for _, key := range blockKeys(ready[at]) {
			group := buckets[key]
			if len(group) > blockMax {
				continue
			}
			for _, other := range group {
				if seen[other] {
					continue
				}
				seen[other] = true
				score, why := pairScore(ready[at], ready[other])
				if score < MinScore {
					continue
				}
				found = appendUnlinked(found, ready[at], docs[other].ID, score, why)
			}
		}
		if len(found) > 0 {
			out[docs[at].ID] = topOf(found, max)
		}
	}
	sort.Strings(names)
	return out, names, blocked
}
