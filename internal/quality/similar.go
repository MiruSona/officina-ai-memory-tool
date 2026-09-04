package quality

import (
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/simhash"
)

// 닮음 점수 S 의 몫 (품질규칙표 2절).
//
//	내용 = 0.45·J3(요약) + 0.35·J3(본문)                    (0 ~ 0.80)
//	곁   = 0.15·J(태그) + 0.05·[scope 같음]                 (0 ~ 0.20)
//	S    = 내용 + 곁 × (내용 / 0.80)
//
// 본문만 보면 「표만 다른 결정 쌍」을 못 잡아서 요약에 더 큰 몫을 준다.
//
// **(리뷰 B) 태그·scope 를 그냥 더하지 않는다.** 더하면 태그와 scope 가 글자까지
// 같은 두 기억은 **내용을 하나도 안 보고** 0.20 을 얻어 경고 문턱(0.17)을 넘는다.
// 실제로 대조군 거짓 경보 3건이 전부 「같은 조사 회차에 같은 태그로 쓴 서로 다른
// 발견」이었다. 그래서 곁 몫은 **내용이 닮은 만큼만** 실린다 — 같은 태그는
// 보너스가 아니라 「다르면 깎는 것」이 되고, 자 범위(0~1)와 문턱은 그대로다.
const (
	weightSummary = 0.45
	weightBody    = 0.35
	weightTags    = 0.15
	weightScope   = 0.05
	// contentMax 는 내용 몫이 다 찼을 때의 값이다.
	contentMax = weightSummary + weightBody
)

// Doc 은 닮음 비교용으로 미리 갈아 둔 기억 하나다. 조각 집합과 지문을 짝마다
// 다시 만들면 2만 건에서 그 할당이 그대로 시간이다.
type Doc struct {
	ID    string
	Type  string
	Scope string
	Tags  []string

	summary []uint64
	body    []uint64
	tags    []string
	// tagBits 는 태그 이름을 64칸에 흩뿌린 표식이다. 두 기억의 표식이 한 칸도
	// 안 겹치면 나눠 가진 태그가 하나도 없다 — 글자를 대 볼 것도 없다.
	// 후보 차례 매기기가 20k 에서 태그 글자 비교만 18초를 쓰던 자리다 (리뷰 B · V3).
	tagBits uint64
	// tagIDs 는 tags 를 표가 매긴 번호로 바꾼 것이다. 번호 차례로 정렬돼 있다.
	// 후보 차례를 매길 때 태그를 **글자로** 견주면 20k lint 에서 그 문자열
	// 비교만 8초다 (memeqbody·cmpbody). 번호로 견주면 답은 그대로고 훨씬 싸다.
	tagIDs []uint32
	// scopeID 는 Scope 를 표가 매긴 번호로 바꾼 것이다. 표에 없으면 -1.
	// scope 도 후보마다 글자로 견주던 자리다.
	scopeID     int32
	bodyNorm    string
	Fingerprint uint64
	// SummaryFP 는 요약만 뜬 지문이다. 요약+본문을 합쳐 하나만 뜨면 본문이 긴
	// 쪽이 지문을 끌고 가서, 같은 말을 짧게·길게 쓴 짝이 밴드 하나도 안 겹친다
	// (0실측 T0 2절의 ② 2건이 그 꼴이다).
	SummaryFP uint64
	// units 는 문장·표 줄 단위 조각이다 (설계 결정 30 의 1단).
	units []unit
	// summaryW·bodyW 는 조각 차례에 맞춘 IDF 무게다. 표에 넣기 전에는 비어
	// 있고, 그때는 무게 없는 자카드를 쓴다 (결정 34).
	summaryW     []float32
	bodyW        []float32
	summaryTotal float32
	bodyTotal    float32
}

// NewDoc 은 기억 하나를 비교할 수 있는 꼴로 간다.
func NewDoc(m *model.Memory) *Doc {
	summary := normalizeText(m.Summary + " " + m.Title)
	body := normalizeText(m.Body)
	doc := Doc{ID: m.ID, Type: m.Type, Scope: m.Scope, Tags: m.Tags,
		summary: grams(summary), body: grams(body), tags: lowerTagSet(m.Tags), bodyNorm: body,
		units: unitsOf(m.Title + "\n" + m.Summary + "\n" + m.Body)}
	doc.tagBits = tagBitsOf(doc.tags)
	// 지문은 요약과 본문을 같이 뜬다 — 후보 좁히기라 넓게 잡는 편이 안전하다.
	doc.Fingerprint = simhash.Of(summary + " " + body)
	doc.SummaryFP = simhash.Of(summary)
	return &doc
}

// Score 는 닮음 점수 S 다. 0~1.
func Score(left, right *Doc) float64 {
	content := weightSummary*gramSim(left.summary, left.summaryW, left.summaryTotal,
		right.summary, right.summaryW, right.summaryTotal) +
		weightBody*gramSim(left.body, left.bodyW, left.bodyTotal, right.body, right.bodyW, right.bodyTotal)
	return withContext(content, left, right)
}

// gramSim 은 조각 겹침이다. 무게가 있으면 IDF 를 실어 재고, 없으면(표에 안 든
// 홀 기억) 무게 없이 잰다.
func gramSim(left []uint64, leftW []float32, leftTotal float32,
	right []uint64, rightW []float32, rightTotal float32) float64 {
	if leftW == nil || rightW == nil {
		return jaccard(left, right)
	}
	return weightedJaccard(left, leftW, leftTotal, right, rightW, rightTotal)
}

// gramSimAtLeast 는 「need 아래면 값을 안 채워도 되는」 자리에서 쓴다. 무게가
// 실린 쪽은 중간에 못 그만두므로 그대로 다 잰다.
func gramSimAtLeast(left []uint64, leftW []float32, leftTotal float32,
	right []uint64, rightW []float32, rightTotal float32, need float64) float64 {
	if leftW == nil || rightW == nil {
		return jaccardAtLeast(left, right, need)
	}
	return weightedJaccard(left, leftW, leftTotal, right, rightW, rightTotal)
}

// withContext 는 내용 점수에 태그·scope 몫을 실어 준다. 내용이 안 닮았으면
// 곁 몫도 거의 안 실린다.
func withContext(content float64, left, right *Doc) float64 {
	side := weightTags * tagJaccard(left.tags, right.tags)
	if left.Scope != "" && left.Scope == right.Scope {
		side += weightScope
	}
	return content + side*content/contentMax
}

// scoreUpTo 는 S 를 재되 **싼 몫부터** 재고, 남은 본문 몫을 다 얹어도 floor
// 에 못 닿으면 거기서 멈춘다. 본문 자카드가 가장 비싼데 후보 대부분은 그
// 앞에서 갈린다. 자카드는 작은 쪽/큰 쪽 크기 비를 절대 못 넘는다.
func scoreUpTo(left, right *Doc, floor float64) float64 {
	content := weightSummary * gramSim(left.summary, left.summaryW, left.summaryTotal,
		right.summary, right.summaryW, right.summaryTotal)
	if withContext(content+weightBody*bodyBound(left, right), left, right) < floor {
		return withContext(content, left, right)
	}
	return withContext(content+weightBody*gramSim(left.body, left.bodyW, left.bodyTotal,
		right.body, right.bodyW, right.bodyTotal), left, right)
}

// bodyBound 는 본문 겹침이 절대 못 넘는 값이다. 무게가 있으면 무게 합의 비다 —
// 겹침 무게는 작은 쪽 합을, 합집합 무게는 큰 쪽 합을 못 넘는다.
func bodyBound(left, right *Doc) float64 {
	if left.bodyW == nil || right.bodyW == nil {
		return sizeBound(len(left.body), len(right.body))
	}
	small, big := left.bodyTotal, right.bodyTotal
	if small > big {
		small, big = big, small
	}
	if big <= 0 {
		return 0
	}
	return float64(small / big)
}

// sizeBound 는 두 집합의 자카드가 절대 못 넘는 값이다.
func sizeBound(left, right int) float64 {
	if left == 0 || right == 0 {
		return 0
	}
	if left > right {
		left, right = right, left
	}
	return float64(left) / float64(right)
}

// SameBody 는 정규형 본문이 완전히 같은지다 (규칙 C03). 빈 본문끼리는 안 센다.
func SameBody(left, right *Doc) bool {
	return left.bodyNorm != "" && left.bodyNorm == right.bodyNorm
}

// lowerTagSet 은 소문자로 낮춘 태그를 정렬해 중복을 뺀 것이다. 태그를 map
// 으로 들면 짝마다 map 을 다시 훑게 되고 20k 에서 그것만 5초다.
func lowerTagSet(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		out = append(out, strings.ToLower(tag))
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// tagBitsOf 는 태그 이름을 64칸에 흩뿌린 표식이다. 겹치는 칸이 없으면 겹치는
// 태그도 없다 (거짓 없음은 없고 거짓 있음만 있는 자다).
func tagBitsOf(tags []string) uint64 {
	bits := uint64(0)
	for _, tag := range tags {
		sum := uint64(1469598103934665603)
		for at := 0; at < len(tag); at++ {
			sum ^= uint64(tag[at])
			sum *= 1099511628211
		}
		bits |= 1 << (sum % 64)
	}
	return bits
}

// sharedTags 는 두 정렬된 태그 목록이 나눠 가진 개수다.
func sharedTags(left, right []string) int {
	shared, at, other := 0, 0, 0
	for at < len(left) && other < len(right) {
		switch {
		case left[at] == right[other]:
			shared++
			at++
			other++
		case left[at] < right[other]:
			at++
		default:
			other++
		}
	}
	return shared
}

func tagJaccard(left, right []string) float64 {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	shared := sharedTags(left, right)
	return float64(shared) / float64(len(left)+len(right)-shared)
}

// Similar 은 저장소의 닮음 후보를 좁히는 표다. 20k 에서 모든 짝을 보면 O(n²)
// 이라, simhash 밴딩으로 후보를 걸러 낸 뒤 그 후보에만 S 를 정확히 잰다.
// simhash 는 후보 좁히기 도구이지 판정자가 아니다 (설계 결정 11).
type Similar struct {
	docs []*Doc
	// bands 는 16비트 열쇠 → 그 열쇠를 가진 문서 자리다.
	bands [simhash.BandCount]map[uint16][]int
	// sumBands 는 요약만 뜬 지문의 밴드다 (② 대책).
	sumBands [simhash.BandCount]map[uint16][]int
	// wide 는 후보 넓히기(요약 지문 밴드·문장 조각)를 켜는지다. 스윕 시험이 끈다.
	// sketch 는 문장 조각의 대표 지문 → 그 조각을 가진 문서 자리다. 긴 글 안에
	// 같은 한 줄이 박힌 짝을 후보로 데려오는 자리다 (③ 대책 · 결정 30).
	sketch map[uint64][]int
	// once 는 IDF 무게를 딱 한 번만 재게 한다. lint 는 표를 만든 뒤 일꾼마다
	// Session() 을 부르므로 여기서 갈라지면 안 된다.
	once sync.Once
	// tagged·scoped 는 태그·scope 가 겹치는 것을 지문과 상관없이 후보에 넣는다.
	tagged map[string][]int
	scoped map[string][]int
	// byID 는 id → 자리 번호다. 벡터 신호가 id 로 후보를 주기 때문에 필요하다.
	byID map[string]int
	// tagID·scopeID 는 글자 → 번호다. 표에 든 것만 번호를 받는다.
	tagID   map[string]uint32
	scopeID map[string]int32
	// vectors 는 임베딩 셋째 신호다. nil 이면 안 쓴다 (vectors.go).
	vectors Vectors
	// near 는 기억마다 가장 가까운 k 개다 (dupRankK). ready() 가 채운다.
	near map[string]map[string]bool
	// nearWide 는 기억마다 벡터가 데려오는 후보다 (vectorLimit). 옛 판은 이것을
	// 문서마다 그때그때 물었는데, 벡터 하나를 묻는 값이 LSH 통을 훑고 정렬하는
	// 값이라 20k 에서 lint 가 16초를 더 썼다. ready() 가 한 번에 채운다 (리뷰 B · V3).
	nearWide map[string][]string
	// own 은 한 갈래로 쓸 때의 자리다. 문서마다 map 을 새로 만들면 20k 에서
	// 그 할당이 그대로 시간이라 회차 표식을 찍어 재활용한다.
	own *Session
	// Hamming 은 후보로 칠 지문 거리다.
	Hamming int
	// Reject 는 거절선(dup_reject)이다. **넓혀서 데려온 후보**를 통째 점수로
	// 잡을 때만 쓴다. 0 이면 넓힌 후보도 경고선부터 잡는다.
	Reject float64
	// PerDoc 은 문서 하나가 볼 후보 상한이다. v0.1 이 20k 에서 5.4초를 낸 고삐다.
	PerDoc int
}

// 벡터 가산 (뒷정리-2a 실험 2 · 결정 33).
//
//	S' = S + dupVecBeta × max(0, cos − dupVecFloor)
//
// **판정자는 아니다.** e5 계열은 아무 짝이나 0.7 위로 뭉쳐서 절대값 문턱이
// 뜻을 잃는다(결정 33). 그래서 「아주 가까울 때만, 조금」 얹는 꼴로 쓴다 —
// 문턱 위 몫만 세고, 그 몫에 β 를 곱한다. cos 를 모르는 짝은 가산 0 이다.
//
// dupVecBeta·dupVecFloor 는 골든셋 130건 스윕으로 골랐다 (보고 실험 2 표).
var (
	dupVecBeta  = envFloat("MEM_DUP_BETA", DefaultDupVecBeta)
	dupVecFloor = envFloat("MEM_DUP_C0", DefaultDupVecFloor)
)

// 스윕이 고른 값. **시험용 뒷문(환경변수)은 재기용이고 평소엔 아무도 안 준다.**
const (
	DefaultDupVecBeta  = 1.0
	DefaultDupVecFloor = 0.50
)

func envFloat(name string, fallback float64) float64 {
	value, err := strconv.ParseFloat(os.Getenv(name), 64)
	if err != nil {
		return fallback
	}
	return value
}

// dupRankK 가 0보다 크면 **차례로** 가른다 — 「서로가 서로의 가장 가까운 k
// 안에 있을 때만」 가산한다. e5 는 아무 짝이나 0.7 위로 뭉쳐서 절대 문턱이
// 뜻을 잃기 때문이다(결정 33). 0 이면 절대 문턱만 쓴다.
var (
	dupRankK  = envInt("MEM_DUP_RANK", DefaultDupRankK)
	dupMutual = os.Getenv("MEM_DUP_ONEWAY") == ""
)

// DefaultDupRankK 는 스윕이 고른 값이다. **1 — 서로가 서로의 가장 가까운
// 하나일 때만** 얹는다. 2 로 넓히면 DUP 0.889 까지 올라가지만 대조군 거짓
// 경보가 7건 난다 (보고 실험 2 표).
const DefaultDupRankK = 1

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil {
		return fallback
	}
	return value
}

// nearSet 은 이 기억의 가장 가까운 k 개다. ready() 가 한 번에 다 채운다 —
// 일꾼 여럿이 같은 표를 보므로 늦게 채우면 map 을 같이 쓰다 죽는다.
func (s *Similar) nearSet(id string) map[string]bool { return s.near[id] }

// vectorLift 는 벡터가 얹어 주는 몫이다. 표에 벡터가 없으면 0 이다.
func (s *Similar) vectorLift(left, right *Doc) float64 {
	if s.vectors == nil || dupVecBeta <= 0 {
		return 0
	}
	if dupRankK > 0 {
		if !s.nearSet(left.ID)[right.ID] {
			return 0
		}
		if dupMutual && !s.nearSet(right.ID)[left.ID] {
			return 0
		}
	}
	gap := s.vectors.Cos(left.ID, right.ID) - dupVecFloor
	if gap <= 0 {
		return 0
	}
	return dupVecBeta * gap
}

// maxVectorLift 는 가산이 얹을 수 있는 최대다. scoreUpTo 의 「여기서 멈춰도
// floor 에 못 닿는다」 판단을 그만큼 늦춰 줘야 한다.
func (s *Similar) maxVectorLift() float64 {
	if s.vectors == nil || dupVecBeta <= 0 {
		return 0
	}
	return dupVecBeta * (1 - dupVecFloor)
}

// wideCandidates 는 후보 넓히기(요약 지문 밴드 · 문장 조각 겹침)를 켜는지다.
// 무엇이 값을 냈는지 가르려고 스윕 시험이 끈다. 늘 켜져 있다.
var wideCandidates = true

// 후보 좁히기 기본값. Hamming 은 mem.toml [quality] simhash_hamming 이 덮는다.
const (
	DefaultCandidatePerDoc = 50
	DefaultHamming         = 8
)

// NewSimilar 는 빈 표를 만든다.
func NewSimilar(hamming int) *Similar {
	if hamming <= 0 {
		hamming = DefaultHamming
	}
	table := Similar{Hamming: hamming, PerDoc: DefaultCandidatePerDoc,
		tagged: map[string][]int{}, scoped: map[string][]int{}, sketch: map[uint64][]int{},
		byID: map[string]int{}, tagID: map[string]uint32{}, scopeID: map[string]int32{}}
	for band := range table.bands {
		table.bands[band] = map[uint16][]int{}
		table.sumBands[band] = map[uint16][]int{}
	}
	return &table
}

// Add 는 견줄 기억 하나를 표에 넣는다.
func (s *Similar) Add(doc *Doc) {
	at := len(s.docs)
	s.docs = append(s.docs, doc)
	s.byID[doc.ID] = at
	for band, key := range simhash.Bands(doc.Fingerprint) {
		s.bands[band][key] = append(s.bands[band][key], at)
	}
	for band, key := range simhash.Bands(doc.SummaryFP) {
		s.sumBands[band][key] = append(s.sumBands[band][key], at)
	}
	for one := range doc.units {
		for _, key := range sketchOf(&doc.units[one]) {
			list := s.sketch[key]
			if len(list) > 0 && list[len(list)-1] == at {
				continue
			}
			s.sketch[key] = append(list, at)
		}
	}
	doc.tagIDs = make([]uint32, 0, len(doc.tags))
	for _, tag := range doc.tags {
		s.tagged[tag] = append(s.tagged[tag], at)
		number, known := s.tagID[tag]
		if !known {
			number = uint32(len(s.tagID))
			s.tagID[tag] = number
		}
		doc.tagIDs = append(doc.tagIDs, number)
	}
	slices.Sort(doc.tagIDs)
	doc.scopeID = -1
	if doc.Scope != "" {
		s.scoped[doc.Scope] = append(s.scoped[doc.Scope], at)
		number, known := s.scopeID[doc.Scope]
		if !known {
			number = int32(len(s.scopeID))
			s.scopeID[doc.Scope] = number
		}
		doc.scopeID = number
	}
}

// probe 는 견줄 기억 하나를 번호로 옮겨 둔 것이다. 후보마다 다시 만들지 않는다.
type probe struct {
	tags  []uint32
	scope int32
	// bits 는 태그 표식이다. 한 칸도 안 겹치면 나눠 가진 태그가 없다 —
	// 번호를 대 볼 것도 없다 (리뷰 B · V3 가 20k 에서 18초를 뗀 고삐다).
	bits uint64
}

// idsFor 는 표 밖 기억의 태그·scope 를 표의 번호로 바꾼다. 표가 모르는 것은
// 뺀다 — 표 안 어느 기억과도 못 겹치니 겹침 수가 안 바뀐다.
func (s *Similar) idsFor(doc *Doc) probe {
	one := probe{tags: doc.tagIDs, scope: doc.scopeID, bits: doc.tagBits}
	if doc.tagIDs == nil {
		one.tags = make([]uint32, 0, len(doc.tags))
		for _, tag := range doc.tags {
			if number, known := s.tagID[tag]; known {
				one.tags = append(one.tags, number)
			}
		}
		slices.Sort(one.tags)
		one.scope = -1
		if doc.Scope != "" {
			if number, known := s.scopeID[doc.Scope]; known {
				one.scope = number
			}
		}
	}
	return one
}

// Docs 는 표에 든 기억 전부다.
func (s *Similar) Docs() []*Doc { return s.docs }

// ready 는 IDF 무게를 한 번 잰다. 표를 다 채운 뒤 처음 견줄 때 불린다.
func (s *Similar) ready() {
	s.once.Do(func() {
		df := make(map[uint64]int32, len(s.docs)*64)
		for _, doc := range s.docs {
			for _, one := range doc.summary {
				df[one]++
			}
			for _, one := range doc.body {
				df[one]++
			}
		}
		if s.vectors != nil {
			// 벡터 후보는 **한 번에** 다 뽑는다. 가까운 차례라 앞 k 개가 곧
			// 「가장 가까운 k」다.
			found := make([][]string, len(s.docs))
			InParallel(len(s.docs), func(from, to int) {
				for at := from; at < to; at++ {
					found[at] = s.vectors.Near(s.docs[at].ID, vectorLimit)
				}
			})
			s.nearWide = make(map[string][]string, len(s.docs))
			if dupRankK > 0 {
				s.near = make(map[string]map[string]bool, len(s.docs))
			}
			for at, doc := range s.docs {
				s.nearWide[doc.ID] = found[at]
				if dupRankK <= 0 {
					continue
				}
				set := make(map[string]bool, dupRankK)
				for slot, one := range found[at] {
					if slot >= dupRankK {
						break
					}
					set[one] = true
				}
				s.near[doc.ID] = set
			}
		}
		// 2단 검사가 볼 「숫자·이름」을 여기서 한 번에 뽑는다. 일꾼이 짝마다
		// 다시 뽑으면 그것만 20k 에서 10초다 (리뷰 B · V3).
		InParallel(len(s.docs), func(from, to int) {
			for at := from; at < to; at++ {
				doc := s.docs[at]
				for one := range doc.units {
					known := factsOf(doc.units[one].text)
					doc.units[one].known = &known
				}
			}
		})
		mean := meanIDF(df, len(s.docs))
		if useAlignIDF {
			for _, doc := range s.docs {
				for at := range doc.units {
					one := &doc.units[at]
					one.weight, one.total = weightsFor(one.grams, df, len(s.docs), mean)
				}
			}
		}
		if !useIDF {
			return
		}
		for _, doc := range s.docs {
			doc.summaryW, doc.summaryTotal = weightsFor(doc.summary, df, len(s.docs), mean)
			doc.bodyW, doc.bodyTotal = weightsFor(doc.body, df, len(s.docs), mean)
		}
	})
}

// BucketMax 는 후보 통 하나에서 가져오는 최대 개수다. 태그 하나가 이만큼
// 많은 기억에 붙어 있으면 **그 태그로는 아무것도 못 좁힌다** — 그것을 알려
// 주는 규칙이 따로 있다(F11 tag-too-broad). 통째로 후보에 넣으면 20k 에서
// 문서마다 2만 개를 훑고 정렬해 O(n²) 이 된다.
//
// (파도 G) 20k 시험에서 2,000건이 19초였고 그중 절반이 이 자리였다.
const BucketMax = 2000

// Session 은 표 하나를 여러 일꾼이 같이 볼 때 일꾼마다 드는 자리다. 표
// 자체는 다 만든 뒤로는 안 바뀌고, 바뀌는 것은 「이번 회차에 이미 담은 후보」
// 표식뿐이라 그것만 갈라 준다.
type Session struct {
	table *Similar
	stamp []int32
	// sketchStamp 은 「이번 회차에 문장 조각 겹침으로 들어온 후보」 표식이다.
	// 문장 정렬은 이 후보에만 잰다 — 조각 곱셈을 모든 후보에 돌리면 20k 에서
	// lint 10초를 못 지킨다 (결정 35).
	sketchStamp []int32
	// narrowStamp 은 「v0.2 부터 있던 후보원으로 들어온 후보」 표식이다.
	narrowStamp []int32
	round       int32
	// lastDoc·lastFound 는 **바로 앞에 낸 후보**다. 같은 기억을 두 번 묻는
	// 쪽이 있어서(중복 판정 C01~C03 과 모순 짝 C13) 후보 뽑기가 20k lint
	// 시간의 3분의 1을 두 번 냈다 (리뷰 B · V3). 표식(stamp)도 그대로라
	// 되쓰는 것이 다시 뽑는 것과 답이 같다.
	lastDoc   *Doc
	lastFound []int
}

// Session 은 이 표를 볼 자리 하나를 낸다. 표를 다 채운 뒤에 부른다.
func (s *Similar) Session() *Session {
	s.ready()
	return &Session{table: s, stamp: make([]int32, len(s.docs)),
		sketchStamp: make([]int32, len(s.docs)), narrowStamp: make([]int32, len(s.docs))}
}

// Candidates 는 이 기억과 견줄 만한 것만 돌려준다. 자기 자신은 뺀다.
func (s *Similar) Candidates(doc *Doc) []*Doc {
	if s.own == nil {
		s.own = s.Session()
	}
	return s.own.Candidates(doc)
}

// Candidates 는 이 기억과 견줄 만한 것만 돌려준다. 자기 자신은 뺀다.
func (ss *Session) Candidates(doc *Doc) []*Doc {
	found := ss.candidateIdx(doc)
	out := make([]*Doc, 0, len(found))
	for _, at := range found {
		out = append(out, ss.table.docs[at])
	}
	return out
}

// candidateIdx 는 후보를 자리 번호로 돌려준다. 문장 정렬을 잴 후보를 가리려면
// 자리 번호가 있어야 한다.
func (ss *Session) candidateIdx(doc *Doc) []int {
	if ss.lastDoc == doc {
		return ss.lastFound
	}
	found := ss.freshCandidateIdx(doc)
	ss.lastDoc, ss.lastFound = doc, found
	return found
}

func (ss *Session) freshCandidateIdx(doc *Doc) []int {
	s := ss.table
	s.ready()
	if len(ss.stamp) < len(s.docs) {
		ss.stamp = make([]int32, len(s.docs))
	}
	if len(ss.sketchStamp) < len(s.docs) {
		ss.sketchStamp = make([]int32, len(s.docs))
	}
	ss.round++
	round := ss.round
	if len(ss.narrowStamp) < len(s.docs) {
		ss.narrowStamp = make([]int32, len(s.docs))
	}
	found := make([]int, 0, s.PerDoc*2)
	narrow := true
	// (v0.4 C2) 자기 자신은 **자리 번호로** 거른다. id 글자를 후보마다 견주던
	// 자리가 20k lint 에서 문자열 비교만 5초였다. id 는 표 안에서 하나뿐이라
	// 자리 번호로 견주는 것과 답이 같다.
	self := -1
	if at, here := s.byID[doc.ID]; here {
		self = at
	}
	pick := func(list []int) {
		if len(list) > BucketMax {
			return
		}
		for _, at := range list {
			if narrow {
				ss.narrowStamp[at] = round
			}
			if ss.stamp[at] == round || at == self {
				continue
			}
			ss.stamp[at] = round
			found = append(found, at)
		}
	}
	// ① v0.2 부터 있던 후보원 — 지문 밴드 · 태그 · scope.
	for band, key := range simhash.Bands(doc.Fingerprint) {
		pick(s.bands[band][key])
	}
	for _, tag := range doc.tags {
		pick(s.tagged[tag])
	}
	pick(s.scoped[doc.Scope])
	// 밴드가 하나도 안 겹쳐도 지문이 가까울 수 있다. 표가 작을 때는 지문 거리로
	// 한 번 더 훑는다 — 놓치는 것보다 조금 더 보는 편이 낫다.
	if len(s.docs) <= smallStore {
		for at, other := range s.docs {
			if ss.stamp[at] == round || at == self {
				continue
			}
			if simhash.Distance(other.Fingerprint, doc.Fingerprint) <= s.Hamming {
				ss.stamp[at] = round
				ss.narrowStamp[at] = round
				found = append(found, at)
			}
		}
	}
	// ② v0.3 이 넓힌 후보원 — 요약만 뜬 지문 밴드(② 부류) · 문장 조각 겹침(③ 부류).
	// 여기서만 들어온 후보는 **경고선으로는 안 잡는다**(Nearest 참고) — 넓힌 값을
	// 거짓 경보로 치르지 않으려는 고삐다.
	narrow = false
	if wideCandidates {
		for band, key := range simhash.Bands(doc.SummaryFP) {
			pick(s.sumBands[band][key])
		}
		for one := range doc.units {
			for _, key := range sketchOf(&doc.units[one]) {
				list := s.sketch[key]
				if len(list) > BucketMax {
					continue
				}
				for _, at := range list {
					ss.sketchStamp[at] = round
				}
				pick(list)
			}
		}
	}
	// ③ 임베딩 셋째 신호 — 3A 가 꽂으면 그때부터 도는 자리다 (결정 15·30).
	if s.vectors != nil {
		for _, id := range s.nearWide[doc.ID] {
			if other, found := s.byID[id]; found {
				pick([]int{other})
			}
		}
	}
	// 상한을 넘으면 「태그를 많이 나눠 가진 것 → 지문이 가까운 것」 차례로 남긴다.
	// 아무 차례로나 자르면 정작 닮은 짝이 잘려 나간다 — 실측에서 정답 짝 31개 중
	// 4개만 남았다.
	//
	// (파도 G) 자를 **미리 재 두고** 정렬한다. 견줄 때마다 다시 재면 태그 집합을
	// 후보 수 × log(후보 수) 번 훑게 되고, 그것이 2,000건 19초의 절반이었다.
	// 후보가 상한 안이면 자를 일이 없다. 자를 재고 정렬하는 값이 통째로 빈다.
	if s.PerDoc <= 0 || len(found) <= s.PerDoc {
		return found
	}
	// (리뷰 B · V3) **상한만큼만 고른다.** 후보가 수천인데 50개만 남길 것을
	// 통째로 정렬하면 그 정렬이 20k lint 시간의 절반이었다 (reflect 로 자리를
	// 바꾸는 sort.Slice 라 더 비쌌다). 답은 「정렬해서 앞 50개」와 똑같다 —
	// 자가 완전 순서(마지막이 id)라 고르는 방법이 답을 안 바꾼다.
	return topRanked(found, doc, ss, s.PerDoc)
}

// topRanked 는 후보에서 가장 앞설 limit 개를 골라 자리 번호로 준다.
func topRanked(found []int, doc *Doc, ss *Session, limit int) []int {
	s := ss.table
	round := ss.round
	picker := topPicker{limit: limit}
	// 태그·scope 번호는 후보마다가 아니라 **한 번만** 구한다.
	one := s.idsFor(doc)
	for _, at := range found {
		picker.push(rankedOf(s.docs[at], doc, at, ss.sketchStamp[at] == round, one))
	}
	best := picker.sorted()
	out := make([]int, 0, len(best))
	for _, one := range best {
		out = append(out, one.at)
	}
	return out
}

// topPicker 는 앞서는 limit 개만 들고 훑는 자리다. 큰 것이 위인 힙이라
// 꼭대기는 늘 「지금 들고 있는 것 중 가장 뒤」다 — 훑기 O(n), 정렬은 limit 개만.
// 답은 「다 정렬해서 앞 limit 개」와 똑같다. 자(rankBefore)가 마지막에 id 로
// 갈라 완전 순서라 고르는 방법이 답을 안 바꾼다.
type topPicker struct {
	limit int
	heap  []ranked
}

func (p *topPicker) push(one ranked) {
	if p.limit <= 0 {
		return
	}
	if len(p.heap) < p.limit {
		p.heap = append(p.heap, one)
		if len(p.heap) == p.limit {
			p.build()
		}
		return
	}
	if rankBefore(p.heap[0], one) {
		return
	}
	p.heap[0] = one
	p.siftDown(0)
}

// sorted 는 고른 것을 앞선 차례로 준다.
func (p *topPicker) sorted() []ranked {
	sort.Slice(p.heap, func(a, b int) bool { return rankBefore(p.heap[a], p.heap[b]) })
	return p.heap
}

func (p *topPicker) build() {
	for at := len(p.heap)/2 - 1; at >= 0; at-- {
		p.siftDown(at)
	}
}

func (p *topPicker) siftDown(at int) {
	for {
		worst := at
		for _, child := range [2]int{2*at + 1, 2*at + 2} {
			if child < len(p.heap) && rankBefore(p.heap[worst], p.heap[child]) {
				worst = child
			}
		}
		if worst == at {
			return
		}
		p.heap[at], p.heap[worst] = p.heap[worst], p.heap[at]
		at = worst
	}
}

// ranked 는 후보 하나와 미리 잰 자다.
type ranked struct {
	doc      *Doc
	at       int
	shared   int
	gap      int
	sketched bool
}

func rankedOf(other, doc *Doc, at int, sketched bool, one probe) ranked {
	return ranked{doc: other, at: at, shared: shared(other, one), sketched: sketched,
		gap: simhash.Distance(other.Fingerprint, doc.Fingerprint)}
}

// shared 는 두 기억이 나눠 가진 태그 수다. 같은 scope 면 하나 더 친다.
// one 은 오른쪽 기억을 번호로 옮긴 것이다 (부르는 쪽이 한 번만 구해 넘긴다).
func shared(left *Doc, one probe) int {
	count := 0
	if left.tagBits&one.bits != 0 {
		count = sharedNumbers(left.tagIDs, one.tags)
	}
	if left.scopeID >= 0 && left.scopeID == one.scope {
		count++
	}
	return count
}

// sharedNumbers 는 번호 차례로 정렬된 두 목록이 나눠 가진 개수다.
func sharedNumbers(left, right []uint32) int {
	shared, at, other := 0, 0, 0
	for at < len(left) && other < len(right) {
		switch {
		case left[at] == right[other]:
			shared++
			at++
			other++
		case left[at] < right[other]:
			at++
		default:
			other++
		}
	}
	return shared
}

// rankBefore 는 왼쪽이 앞서는지다 (앞설수록 남는다).
//
//	① 문장 조각이 겹쳐 들어온 후보 — 상한에서 잘리면 부분 중복이 통째로 사라진다
//	② 나눠 가진 태그가 많은 것 ③ 지문이 가까운 것 ④ id 차례 (동점을 못 박는다)
func rankBefore(left, right ranked) bool {
	if left.sketched != right.sketched {
		return left.sketched
	}
	if left.shared != right.shared {
		return left.shared > right.shared
	}
	if left.gap != right.gap {
		return left.gap < right.gap
	}
	return left.doc.ID < right.doc.ID
}

// smallStore 는 전수 지문 대조를 해도 값이 싼 크기다.
const smallStore = 2000

// Match 는 닮은 것 하나다.
type Match struct {
	Doc   *Doc
	Score float64
	Same  bool
	// Align 은 문장 최대 정렬 값이다 (1단). Partial 은 그 값이 문턱을 넘고
	// **2단(사실 어긋남 검사)까지 통과**했다는 뜻이다 — 부분 중복 확정.
	Align   float64
	Partial bool
	// Hard 는 **저장을 막아도 되는 확신**이 있는지다 (C01). 통째 점수가 거절선을
	// 넘는 것만으로는 안 된다 — 실저장소에서 그 자리는 「같은 바닥 낱말을 나눠 쓴
	// 남남」으로 꽉 찬다(실데이터 40건에 22건 · 새 프로젝트 3건에 2건). 본문이
	// 글자까지 같거나(Same), 같은 대목을 2단까지 통과하며 나눠 가질 때만 참이다.
	Hard bool
	// order 는 차례를 매길 때 쓰는 값이다. 부분 중복은 통째 점수가 낮아도
	// 경고선에 선 것으로 친다.
	order float64
}

// Nearest 는 닮은 것을 점수 높은 차례로 돌려준다. floor 아래는 안 준다.
func (s *Similar) Nearest(doc *Doc, floor float64, limit int) []Match {
	if s.own == nil {
		s.own = s.Session()
	}
	return s.own.Nearest(doc, floor, limit)
}

// Nearest 는 닮은 것을 점수 높은 차례로 돌려준다. floor 아래는 안 준다.
func (ss *Session) Nearest(doc *Doc, floor float64, limit int) []Match {
	matches := []Match{}
	found := ss.candidateIdx(doc)
	round := ss.round
	for _, at := range found {
		other := ss.table.docs[at]
		same := SameBody(doc, other)
		lift := ss.table.vectorLift(doc, other)
		score := scoreUpTo(doc, other, floor-ss.table.maxVectorLift())
		if same {
			score = Score(doc, other)
		} else {
			score += lift
		}
		align, partial := 0.0, false
		// 1단 — 통째 점수로 안 걸린 것만 문장 정렬로 다시 본다. 조각이 겹쳐
		// 들어온 후보만 잰다 (조각 곱셈 고삐 · 결정 35).
		if !same && score < floor && ss.sketchStamp[at] == round {
			info := alignOf(doc, other, AlignCut, alignNeed())
			align = info.Best
			partial = align >= AlignCut && (!alignTwoStage || passesStageTwo(info, true))
		}
		// 통째 점수로 걸린 것에도 2단을 건다. **거절선 위아래를 안 가린다** — 저장을
		// 막는 쪽(거절선 위)이 오히려 무방비였던 것이 리뷰 C1 이다.
		//
		//	① 통째 점수가 경고선을 넘은 **모든** 짝에 「같은 문장이 조금이라도 겹칠 것」
		//	   (softAlignFloor)을 요구한다. 이 자를 안 대면 남는 것은 「같은 바닥 낱말을
		//	   나눠 쓴 남남」이다 — 실데이터 40건에 22건, 새 프로젝트 3건에 2건이 그 꼴로
		//	   `duplicate-hard` 였다. 골든셋 대조군은 서로 주제가 멀어 이 구멍을 못 비춘다.
		//	② 거절(저장 막기)은 거기서 **2단까지** 통과해야 한다 — 닮은 짝 조각이 둘 이상,
		//	   숫자·경로·이름이 안 어긋나고, 확인 가능한 값을 나눠 갖는다. 부분 중복 길이
		//	   쓰던 것과 같은 자다. 2단을 못 넘으면 경고(`duplicate-soft`)로 내려간다.
		//	③ 본문이 글자까지 같은 것(Same)은 이 자를 안 탄다.
		hard := same
		if !same && score >= floor {
			info := alignOf(doc, other, AlignCut, alignNeed())
			switch {
			case factsOnScore && factsClash(info.Left, info.Right):
				score = 0
			case info.Best >= softAlignFloor:
				hard = score >= ss.table.Reject && (!alignTwoStage || passesStageTwo(info, false))
			case lift > 0:
				// 벡터만 손을 든 짝이다 — 낱말도 문장도 안 겹친다. 「다른 말로 쓴
				// 중복」일 수 있으니 버리지는 않되 **절대 거절하지 않는다**. 벡터는
				// 판정자가 아니다(결정 33). 경고로 남겨 사람이 본다.
			default:
				score = 0
			}
		}
		// 넓혀서 데려온 후보(요약 지문·문장 조각)는 **거절할 만큼 확신이 설 때만** 통째
		// 점수로 잡는다. 경고선만 겨우 넘는 것까지 잡으면 넓힌 값을 대조군 거짓
		// 경보로 치르게 된다 — 실측에서 그 자리에 딱 한 건이 있었다.
		if ss.narrowStamp[at] != round && !same && score < ss.table.Reject {
			score = 0
		}
		if score < floor && !same && !partial {
			continue
		}
		order := score
		if partial && order < floor {
			order = floor
		}
		matches = append(matches, Match{Doc: other, Score: score, Same: same,
			Align: align, Partial: partial, Hard: hard, order: order})
	}
	sort.Slice(matches, func(a, b int) bool {
		if matches[a].order != matches[b].order {
			return matches[a].order > matches[b].order
		}
		return matches[a].Doc.ID < matches[b].Doc.ID
	})
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}

// newSimilarFor 는 설정대로 닮음 표를 만든다. 후보 좁히기 값이 여러 자리에
// 흩어지지 않게 한 곳에서 만든다.
func newSimilarFor(opt Options) *Similar {
	table := NewSimilar(opt.Config.Quality.SimhashHamming)
	table.Reject = opt.Config.Quality.DupReject
	// 벡터 항은 **기본 꺼짐**이다 (설계 18-7 · `[quality] dup_vectors`).
	// 꺼져 있으면 후보원으로도 안 쓴다 — 후보만 데려와도 값은 그대로였고
	// (뒷정리-2a), 켜면 대조군 거짓 경보 3건 · 실데이터 오거절 15% 가 난다.
	if opt.Config.Quality.DupVectors {
		table.UseVectors(opt.Near)
	}
	return table
}
