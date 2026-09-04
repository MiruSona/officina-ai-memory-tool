// gen 은 검색을 재기 위한 가데이터 저장소를 만든다 (설계 13-1).
// 쓰는 법 : go run ./testdata/gen -n 5000 -seed 1 -out <폴더>
// 같은 씨앗이면 언제나 같은 말뭉치가 나온다.
package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// 종류 비율. 실제 저장소를 따라간다 (설계 13-1 · 조사D 4-7).
var kinds = []kindShare{
	{"decision", 36}, {"caution", 34}, {"history", 13},
	{"issue", 8}, {"howto", 5}, {"todo", 4},
}

// kindShare 는 종류 하나와 그 몫(백분율)이다.
type kindShare struct {
	name  string
	share int
}

// 본문 길이는 평균 735바이트 로그정규다 (설계 13-1 · 조사B 2-1).
const (
	bodyLogMean  = 6.4
	bodyLogSigma = 0.55
	minBodyLines = 15
	maxBodyLines = 120
)

// tagPool 은 태그로 쓸 영어 낱말이다. 머리말 규칙이 소문자·숫자·하이픈만 받는다.
var tagPool = []string{"search", "index", "hook", "gc", "lint", "eval", "store",
	"token", "budget", "secret", "design", "measure", "scale", "fieldtest"}

// scopePool 은 scope 값이다.
var scopePool = []string{"aimemorytool", "mem", "officina", "diagramtool", "codetool"}

// word 는 말뭉치 낱말 하나와 뽑힐 무게다.
type word struct {
	text   string
	weight int
}

// options 는 명령줄에서 받은 것이다.
type options struct {
	count int
	seed  int64
	out   string
	words string
}

func main() {
	settings := options{}
	flag.IntVar(&settings.count, "n", 1000, "몇 건을 만들지")
	flag.Int64Var(&settings.seed, "seed", 1, "씨앗. 같은 값이면 같은 말뭉치")
	flag.StringVar(&settings.out, "out", "", "만들 폴더 (Memory 저장소 뿌리)")
	flag.StringVar(&settings.words, "words", "", "낱말 목록 파일 (기본 testdata/corpus-words.txt)")
	flag.Parse()
	if settings.out == "" {
		fmt.Fprintln(os.Stderr, "-out 이 필요하다")
		os.Exit(1)
	}
	if err := build(settings); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func build(settings options) error {
	pool, err := readWords(wordsPath(settings))
	if err != nil {
		return err
	}
	dice := rand.New(rand.NewSource(settings.seed))
	if err := os.MkdirAll(filepath.Join(settings.out, "golden"), 0o755); err != nil {
		return err
	}
	cases := []string{}
	for at := 0; at < settings.count; at++ {
		id := makeID(at)
		unique := uniqueWord(at)
		text := oneMemory(dice, pool, id, unique, at)
		if err := writeFile(settings.out, id, text); err != nil {
			return err
		}
		if at%20 == 0 {
			cases = append(cases, goldenCase(unique, id, at))
		}
	}
	return writeGolden(settings.out, append(cases, abstainCases()...))
}

// abstainCases 는 「저장소에 없는 것을 물었을 때 없다고 답하나」 를 재는
// 질문이다. 예전에는 extract 뿐이라 abstain 분모가 0이었고, 분모 0인 지표가
// 1.000 합격으로 찍혀 **잰 적 없는 것이 합격으로 보였다** (리뷰B #22).
func abstainCases() []string {
	out := []string{}
	for _, word := range []string{"없는표식00001", "없는표식00002", "없는표식00003",
		"물리엔진 중력", "블렌더 리깅", "결제모듈", "광고SDK"} {
		out = append(out, "  - q: "+word+"\n    kind: abstain\n    lang: ko\n    expect: []\n")
	}
	return out
}

func wordsPath(settings options) string {
	if settings.words != "" {
		return settings.words
	}
	return filepath.Join("testdata", "corpus-words.txt")
}

// readWords 는 실제 저장소에서 뽑아 둔 낱말 분포를 읽는다. 무작위 글자로는
// 검색을 잴 수 없다 (설계 13-1).
func readWords(path string) ([]word, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pool := []word{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, count, found := strings.Cut(line, " ")
		if !found {
			continue
		}
		weight, err := strconv.Atoi(strings.TrimSpace(count))
		if err != nil || weight <= 0 {
			continue
		}
		pool = append(pool, word{text: name, weight: weight})
	}
	if len(pool) == 0 {
		return nil, fmt.Errorf("낱말 목록이 비었다 : %s", path)
	}
	sort.SliceStable(pool, func(a, b int) bool { return pool[a].text < pool[b].text })
	return pool, nil
}

// oneMemory 는 기억 파일 한 개의 글 전체다. 머리말은 store 규격을 따른다.
func oneMemory(dice *rand.Rand, pool []word, id, unique string, at int) string {
	kind := kinds[pickKind(at)].name
	out := strings.Builder{}
	out.WriteString("---\n")
	out.WriteString("id: " + id + "\n")
	out.WriteString("type: " + kind + "\n")
	out.WriteString("date: " + dateOf(at) + "\n")
	out.WriteString("summary: \"" + summaryOf(dice, pool, unique) + "\"\n")
	out.WriteString("tags: [" + strings.Join(tagsOf(dice), ", ") + "]\n")
	out.WriteString("source: ai\n")
	out.WriteString("scope: " + scopePool[dice.Intn(len(scopePool))] + "\n")
	out.WriteString(statusLine(kind, dice))
	out.WriteString("---\n\n")
	out.WriteString(bodyOf(dice, pool, unique))
	return out.String()
}

// summaryOf 는 30~120자 규칙 안에 드는 한 줄이다. 고유 낱말을 여기 심는다.
func summaryOf(dice *rand.Rand, pool []word, unique string) string {
	parts := []string{unique}
	for len([]rune(strings.Join(parts, " "))) < 40 {
		parts = append(parts, pick(dice, pool))
	}
	line := strings.Join(parts, " ")
	letters := []rune(line)
	if len(letters) > 110 {
		line = string(letters[:110])
	}
	return strings.ReplaceAll(line, "\"", "'")
}

// bodyOf 는 로그정규 길이의 본문이다. 함정(붙여쓰기·한영섞임·숫자단위·1글자)을
// 일부러 심는다 (설계 13-1).
func bodyOf(dice *rand.Rand, pool []word, unique string) string {
	lines := lineCount(dice)
	out := strings.Builder{}
	for at := 0; at < lines; at++ {
		out.WriteString(oneLine(dice, pool, at))
		out.WriteString("\n")
	}
	out.WriteString("\n고유 표시 : " + unique + "\n")
	return out.String()
}

func oneLine(dice *rand.Rand, pool []word, at int) string {
	parts := []string{}
	for count := 4 + dice.Intn(8); count > 0; count-- {
		parts = append(parts, pick(dice, pool))
	}
	switch at % 4 {
	case 0:
		parts = append(parts, pick(dice, pool)+pick(dice, pool))
	case 1:
		parts = append(parts, "unity"+pick(dice, pool))
	case 2:
		parts = append(parts, formatNumber(dice)+"건")
	case 3:
		parts = append(parts, oneLetters[dice.Intn(len(oneLetters))])
	}
	return "- " + strings.Join(parts, " ")
}

// oneLetters 는 한 글자 낱말 함정이다.
var oneLetters = []string{"훅", "표", "수", "값", "줄"}

// formatNumber 는 쉼표가 든 꼴과 없는 꼴을 섞는다.
func formatNumber(dice *rand.Rand) string {
	value := (dice.Intn(40) + 1) * 500
	if value < 1000 || dice.Intn(2) == 0 {
		return strconv.Itoa(value)
	}
	return strconv.Itoa(value/1000) + "," + fmt.Sprintf("%03d", value%1000)
}

func lineCount(dice *rand.Rand) int {
	size := math.Exp(bodyLogMean + bodyLogSigma*dice.NormFloat64())
	lines := int(size / 40)
	if lines < minBodyLines {
		return minBodyLines
	}
	if lines > maxBodyLines {
		return maxBodyLines
	}
	return lines
}

// pick 은 나온 횟수를 무게로 삼아 낱말 하나를 뽑는다.
func pick(dice *rand.Rand, pool []word) string {
	total := 0
	for _, item := range pool {
		total += item.weight
	}
	cut := dice.Intn(total)
	for _, item := range pool {
		cut -= item.weight
		if cut < 0 {
			return item.text
		}
	}
	return pool[0].text
}

func tagsOf(dice *rand.Rand) []string {
	count := 1 + dice.Intn(3)
	seen := map[string]bool{}
	out := []string{}
	for len(out) < count {
		name := tagPool[dice.Intn(len(tagPool))]
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func statusLine(kind string, dice *rand.Rand) string {
	if kind == "todo" {
		return "status: open\n"
	}
	if kind == "issue" || kind == "caution" {
		return "severity: " + []string{"low", "mid", "high"}[dice.Intn(3)] + "\n"
	}
	return ""
}

// pickKind 는 비율표를 그대로 따라간다. 무작위가 아니라 자리로 정해서 같은
// 씨앗이 아니어도 비율이 흔들리지 않는다.
func pickKind(at int) int {
	spot := at % 100
	sum := 0
	for place, item := range kinds {
		sum += item.share
		if spot < sum {
			return place
		}
	}
	return 0
}

// makeID 는 store 규격의 id 다 — YYYYMMDD-여덟자리.
func makeID(at int) string {
	return strings.ReplaceAll(dateOf(at), "-", "") + "-" + fmt.Sprintf("%08x", at*2654435761&0xffffffff)
}

// dateOf 는 만든 기억을 60일에 걸쳐 흩는다. 시간 필터를 잴 수 있어야 한다.
func dateOf(at int) string {
	day := 1 + at%28
	month := 7 + (at/28)%2
	return fmt.Sprintf("2026-%02d-%02d", month, day)
}

// uniqueWord 는 그 기억만 가진 낱말이다. 정답이 무엇인지 아는 유일한 길이다.
func uniqueWord(at int) string {
	return "고유표식" + fmt.Sprintf("%05d", at)
}

func writeFile(out, id, text string) error {
	dir := filepath.Join(out, "store", id[:4], id[4:6])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, id+".md"), []byte(text), 0o644)
}

// goldenCase 는 심은 고유 낱말 하나를 묻는 질문이다. 8월에 만든 자리는 시간
// 조건을 같이 걸어 temporal 로 만든다 — 유형이 하나뿐이면 자가 얕다 (리뷰B #22).
func goldenCase(unique, id string, at int) string {
	head := "  - q: " + unique + "\n"
	if recentDay(at) && at%80 == 0 {
		return head + "    kind: temporal\n    lang: ko\n    since: 30d\n    expect: [" + id + "]\n"
	}
	return head + "    kind: extract\n    lang: ko\n    expect: [" + id + "]\n"
}

// recentDay 는 dateOf 가 그 자리에 8월 날짜를 주는지다 (최근 30일 안).
func recentDay(at int) bool {
	return (at/28)%2 == 1
}

func writeGolden(out string, cases []string) error {
	text := "# 가데이터 골든셋. 생성기가 심은 고유 낱말 → id 다 (설계 9-3).\n" +
		"# 편향이 있어 품질을 재는 자가 아니라 회귀를 잡는 자다.\n" +
		"version: 2\ncases:\n" + strings.Join(cases, "")
	return os.WriteFile(filepath.Join(out, "golden", "goldenset.yaml"), []byte(text), 0o644)
}
