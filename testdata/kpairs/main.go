// kpairs 는 K(근거가 주장을 지지·반대·무관 중 무엇인가) 실물 측정용 쌍 파일을 만들고 채점한다.
// 쓰는 법 :
//
//	go run ./testdata/kpairs make -store <Memory/store> -seed 1 -out <폴더> [-deny <금지어 파일>]
//	go run ./testdata/kpairs score -pairs <pairs.jsonl> -result <mem judge support --file 의 stdout>
//	make2 · leak · merge 는 Laya 학습 쌍용이다 (make2.go · leak.go · merge.go).
//
// 같은 씨앗이면 언제나 바이트까지 같은 파일이 나온다. LLM 은 부르지 않는다.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/secret"
)

// 갈래마다 쌍 수와 그 안의 규칙별 몫.
const (
	perBranch    = 16
	longSupport  = 4  // 지지 중 본문 전체를 근거로 쓰는 쌍
	xscopeCount  = 10 // 무관 중 scope 가 다른 쌍
	minSentence  = 15
	maxSentence  = 300
	minLongBody  = 300  // 긴 입력으로 쓸 본문 최소 글자 수
	maxLongBody  = 6000 // 너무 길면 judge 한 줄 상한에 걸릴 수 있다
	maxJaccard   = 0.1
	minOverlap   = 2 // 지지 근거 문장이 요약과 겹쳐야 할 낱말 수
	passAccuracy = 0.85
	passP95MS    = 5000
	weakBranch   = 11 // 갈래 정답이 이 수 이하면 「조건부」
)

// 갈래 이름. mem judge 의 want 가 받는 이름 그대로다 (cmd_judge.go letterOf).
const (
	wantSupport    = "support"
	wantContradict = "contradict"
	wantUnrelated  = "unrelated"
)

var branches = []string{wantSupport, wantContradict, wantUnrelated}

// 빼는 scope. 공개하지 않을 기획 내용이 들어 있다.
var skipScopes = map[string]bool{"gamedesigntool": true, "prototool": true}

// 자동 기억(origin: stop)이 아니면 이 종류만 쓴다.
var usableTypes = map[string]bool{"decision": true, "caution": true, "howto": true}

// flips 는 반대 주장을 만드는 극성표다. 긴 꼴을 먼저 본다 ("안 한다" 가 "한다" 보다 먼저).
var flips = [][2]string{
	{"기본 켬", "기본 끔"}, {"기본 끔", "기본 켬"},
	{"안 한다", "한다"}, {" 한다", " 안 한다"},
	{"안 된다", "된다"}, {" 된다", " 안 된다"},
	{"켠다", "끈다"}, {"끈다", "켠다"},
	{"있다", "없다"}, {"없다", "있다"},
	{"넣는다", "뺀다"}, {"뺀다", "넣는다"},
	{"늘린다", "줄인다"}, {"줄인다", "늘린다"},
	{"늘었다", "줄었다"}, {"줄었다", "늘었다"},
	{"늘어", "줄어"}, {"줄어", "늘어"},
	{"합격", "미달"}, {"미달", "합격"},
}

var numberPattern = regexp.MustCompile(`\d+`)

// memory 는 기억 md 하나에서 쓰는 칸만 모은 것이다.
type memory struct {
	id, kind, title, summary, scope, origin, body string
	sentences                                     []string
	// make2 만 쓰는 칸이다. supersededBy 는 이 기억을 덮은 기억 · links 는 links 와 sources 의 mem: 을 합친 것.
	supersededBy string
	links        []string
}

// pair 는 pairs.jsonl 한 줄이다. src · rule 은 mem judge 가 무시하는 칸이다.
type pair struct {
	ID       string `json:"id"`
	Evidence string `json:"evidence"`
	Claim    string `json:"claim"`
	Want     string `json:"want"`
	Src      string `json:"src"`
	Rule     string `json:"rule"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run 은 하위 명령을 고르고 종료 코드를 돌려준다.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "쓰는 법: kpairs make|score|make2|leak|merge ...")
		return 2
	}
	var err error
	switch args[0] {
	case "make":
		err = runMake(args[1:])
	case "score":
		err = runScore(args[1:], stdout)
	case "make2":
		err = runMake2(args[1:], stdout)
	case "leak":
		err = runLeak(args[1:], stdout)
	case "merge":
		err = runMerge(args[1:], stdout)
	default:
		err = fmt.Errorf("모르는 하위 명령: %s", args[0])
	}
	if err != nil {
		fmt.Fprintln(stderr, "kpairs:", err)
		return 1
	}
	return 0
}

func runMake(args []string) error {
	flags := flag.NewFlagSet("make", flag.ContinueOnError)
	store := flags.String("store", "", "기억 저장소 폴더 (Memory/store)")
	seed := flags.Int64("seed", 1, "씨앗")
	out := flags.String("out", "", "출력 폴더")
	deny := flags.String("deny", "", "금지어 파일 (한 줄에 낱말 하나)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *store == "" || *out == "" {
		return errors.New("-store 와 -out 은 꼭 줘야 한다")
	}
	denyWords, err := readDenyWords(*deny)
	if err != nil {
		return err
	}
	memories, err := loadMemories(*store, denyWords)
	if err != nil {
		return err
	}
	pairs, err := makePairs(memories, *seed)
	if err != nil {
		return err
	}
	return writeOutputs(*out, pairs)
}

// readDenyWords 는 금지어를 소문자로 읽는다. 빈 줄과 # 줄은 건너뛴다.
func readDenyWords(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	words := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		words = append(words, strings.ToLower(line))
	}
	return words, nil
}

// loadMemories 는 쓸 수 있는 기억만 id 순으로 돌려준다. 비밀·금지어·뺄 scope 는 여기서 걸러진다.
func loadMemories(store string, denyWords []string) ([]memory, error) {
	scanner := secret.New(config.DefaultSecretPatterns())
	memories := []memory{}
	err := filepath.WalkDir(store, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		item, ok := parseMemory(string(data))
		if !ok || skipScopes[item.scope] || item.summary == "" {
			return nil
		}
		if item.origin != "stop" && !usableTypes[item.kind] {
			return nil
		}
		if scanner.ScanText(secret.MemoryText(item.title, item.summary, nil, item.body)) != nil {
			return nil
		}
		if hasDenyWord(item.title+"\n"+item.summary+"\n"+item.body, denyWords) {
			return nil
		}
		item.sentences = splitSentences(item.body)
		memories = append(memories, item)
		return nil
	})
	sort.Slice(memories, func(i, j int) bool { return memories[i].id < memories[j].id })
	return memories, err
}

func hasDenyWord(text string, words []string) bool {
	lower := strings.ToLower(text)
	for _, word := range words {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// parseMemory 는 머리말(--- 사이)의 한 줄 칸과 본문을 읽는다.
// 목록 칸은 links([a, b] 또는 - 줄)와 sources 의 「- mem:<id>」만 읽는다.
func parseMemory(text string) (memory, bool) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return memory{}, false
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return memory{}, false
	}
	head, body := text[4:4+end], text[4+end+4:]
	item := memory{body: strings.TrimSpace(body)}
	listKey := ""
	for _, line := range strings.Split(head, "\n") {
		if entry, isEntry := strings.CutPrefix(strings.TrimSpace(line), "- "); isEntry {
			item.links = append(item.links, listLink(listKey, entry)...)
			continue
		}
		listKey = ""
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch strings.TrimSpace(key) {
		case "id":
			item.id = value
		case "type":
			item.kind = value
		case "title":
			item.title = value
		case "summary":
			item.summary = value
		case "scope":
			item.scope = value
		case "origin":
			item.origin = value
		case "superseded_by":
			item.supersededBy = value
		case "links":
			listKey = "links"
			for _, id := range strings.Split(strings.Trim(value, "[]"), ",") {
				item.links = append(item.links, listLink(listKey, id)...)
			}
		case "sources":
			listKey = "sources"
		}
	}
	return item, item.id != ""
}

// listLink 는 목록 한 칸에서 기억 id 를 꺼낸다. links 는 칸 그대로, sources 는 「mem:」 붙은 것만.
func listLink(listKey, entry string) []string {
	entry = strings.Trim(strings.TrimSpace(entry), `"'`)
	switch listKey {
	case "links":
		if entry != "" {
			return []string{entry}
		}
	case "sources":
		if id, ok := strings.CutPrefix(entry, "mem:"); ok && id != "" {
			return []string{strings.TrimSpace(id)}
		}
	}
	return nil
}

// splitSentences 는 본문을 줄과 문장 끝(. ! ? 뒤 빈칸)으로 자르고 15~300자만 남긴다.
func splitSentences(body string) []string {
	out := []string{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimLeft(strings.TrimSpace(line), "-*#>| ")
		for _, part := range splitLine(line) {
			part = strings.TrimSpace(part)
			if n := len([]rune(part)); n >= minSentence && n <= maxSentence {
				out = append(out, part)
			}
		}
	}
	return out
}

func splitLine(line string) []string {
	parts := []string{}
	start := 0
	runes := []rune(line)
	for i := 0; i+1 < len(runes); i++ {
		if strings.ContainsRune(".!?", runes[i]) && runes[i+1] == ' ' {
			parts = append(parts, string(runes[start:i+1]))
			start = i + 1
		}
	}
	return append(parts, string(runes[start:]))
}

// words 는 글을 소문자 낱말 집합으로 바꾼다. 한 글자 낱말은 뺀다.
func words(text string) map[string]bool {
	set := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len([]rune(word)) >= 2 {
			set[word] = true
		}
	}
	return set
}

func overlap(a, b map[string]bool) int {
	count := 0
	for word := range a {
		if b[word] {
			count++
		}
	}
	return count
}

func jaccard(a, b string) float64 {
	setA, setB := words(a), words(b)
	both := overlap(setA, setB)
	union := len(setA) + len(setB) - both
	if union == 0 {
		return 0
	}
	return float64(both) / float64(union)
}

// bestSentence 는 요약과 낱말이 가장 많이 겹치는 문장을 고른다. keep 이 거르면 그 문장만 본다.
// 겹침이 같으면 먼저 나온 문장이다.
// avoid 에 든 문장은 건너뛴다 (make2 -avoid · 평소엔 nil).
func bestSentence(item memory, keep func(string) bool, avoid map[string]bool) (string, int) {
	return bestSentenceFor(item, item.summary, keep, avoid)
}

// bestSentenceFor 는 bestSentence 와 같되 겹침을 잴 글(claim)을 따로 받는다 (다른 기억의 요약 등).
func bestSentenceFor(item memory, claim string, keep func(string) bool, avoid map[string]bool) (string, int) {
	target := words(claim)
	best, bestScore := "", -1
	for _, sentence := range item.sentences {
		if (keep != nil && !keep(sentence)) || avoid[sentence] {
			continue
		}
		if score := overlap(words(sentence), target); score > bestScore {
			best, bestScore = sentence, score
		}
	}
	return best, bestScore
}

// contradiction 은 요약을 뒤집은 주장과 그 뒤집힌 말이 든 근거 문장 후보를 모두 돌려준다. avoid 문장은 근거로 안 쓴다.
func contradictions(item memory, avoid map[string]bool) []pair {
	options := []pair{}
	for _, loc := range numberPattern.FindAllStringIndex(item.summary, -1) {
		number := item.summary[loc[0]:loc[1]]
		value, err := strconv.Atoi(number)
		unit, ok := plainNumber(item.summary, loc)
		if err != nil || !ok {
			continue
		}
		evidence, _ := bestSentence(item, func(s string) bool { return hasNumber(s, number, unit) }, avoid)
		if evidence == "" {
			continue
		}
		changed := value * 2
		if value == 0 || value >= 1000 {
			changed = value + 1
		}
		claim := item.summary[:loc[0]] + strconv.Itoa(changed) + item.summary[loc[1]:]
		options = append(options, pair{Evidence: evidence, Claim: claim, Rule: "num"})
		break // 숫자는 첫 번째로 쓸 수 있는 것 하나만
	}
	for _, flip := range flips {
		if !strings.Contains(item.summary, flip[0]) {
			continue
		}
		evidence, _ := bestSentence(item, func(s string) bool { return strings.Contains(s, flip[0]) }, avoid)
		if evidence == "" {
			continue
		}
		claim := strings.Replace(item.summary, flip[0], flip[1], 1)
		options = append(options, pair{Evidence: evidence, Claim: claim, Rule: "neg"})
		break
	}
	return options
}

// plainNumber 는 숫자가 「값」으로 쓰였는지 보고 바로 뒤 글자(단위)를 돌려준다.
// 낱말에 붙은 숫자(in_sum2·e422148)와 날짜·판·범위(10-06·1.2·4~5·12:30)는 뒤집지 않는다.
func plainNumber(text string, loc []int) (rune, bool) {
	const joiners = "-.~:/_"
	before, after := rune(0), rune(0)
	if loc[0] > 0 {
		before = []rune(text[:loc[0]])[len([]rune(text[:loc[0]]))-1]
	}
	if loc[1] < len(text) {
		after = []rune(text[loc[1]:])[0]
	}
	if before < unicode.MaxASCII && (unicode.IsLetter(before) || strings.ContainsRune(joiners, before)) && before != 0 {
		return 0, false
	}
	if strings.ContainsRune(joiners, after) && after != 0 {
		return 0, false
	}
	return after, true
}

// hasNumber 는 같은 숫자가 같은 단위로 「값」으로 쓰였는지 본다 (5만 과 4~5배 는 다르다).
func hasNumber(text, number string, unit rune) bool {
	for _, loc := range numberPattern.FindAllStringIndex(text, -1) {
		if text[loc[0]:loc[1]] != number {
			continue
		}
		if after, ok := plainNumber(text, loc); ok && after == unit {
			return true
		}
	}
	return false
}

// makePairs 는 세 갈래 16쌍씩을 만들어 섞고 id 를 붙인다. 하나라도 모자라면 오류다.
func makePairs(memories []memory, seed int64) ([]pair, error) {
	rng := rand.New(rand.NewSource(seed))
	pool := orderPool(memories, rng)
	used := map[string]bool{}
	byBranch := map[string][]pair{}

	// 반대가 가장 까다로우니 먼저 고른다.
	for _, item := range pool {
		if len(byBranch[wantContradict]) == perBranch {
			break
		}
		options := contradictions(item, nil)
		if len(options) == 0 {
			continue
		}
		chosen := options[rng.Intn(len(options))]
		chosen.Want, chosen.Src = wantContradict, item.id
		byBranch[wantContradict] = append(byBranch[wantContradict], chosen)
		used[item.id] = true
	}

	long, same := 0, 0
	for _, item := range pool {
		if used[item.id] || long+same == perBranch {
			continue
		}
		size := len([]rune(item.body))
		if long < longSupport && size >= minLongBody && size <= maxLongBody {
			byBranch[wantSupport] = append(byBranch[wantSupport],
				pair{Evidence: item.body, Claim: item.summary, Want: wantSupport, Src: item.id, Rule: "long"})
			used[item.id] = true
			long++
			continue
		}
		if same == perBranch-longSupport {
			continue
		}
		if evidence, score := bestSentence(item, nil, nil); score >= minOverlap {
			byBranch[wantSupport] = append(byBranch[wantSupport],
				pair{Evidence: evidence, Claim: item.summary, Want: wantSupport, Src: item.id, Rule: "same"})
			used[item.id] = true
			same++
		}
	}

	byBranch[wantUnrelated] = append(unrelatedPairs(pool, used, true, xscopeCount),
		unrelatedPairs(pool, used, false, perBranch-xscopeCount)...)

	short := []string{}
	for _, branch := range branches {
		if missing := perBranch - len(byBranch[branch]); missing > 0 {
			short = append(short, fmt.Sprintf("%s %d개 모자람", branch, missing))
		}
	}
	if len(short) > 0 {
		return nil, fmt.Errorf("쌍을 다 못 채웠다 (지어내지 않는다): %s", strings.Join(short, ", "))
	}
	all := append(append(byBranch[wantSupport], byBranch[wantContradict]...), byBranch[wantUnrelated]...)
	rng.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
	for i := range all {
		all[i].ID = fmt.Sprintf("k%03d", i+1)
	}
	return all, nil
}

// orderPool 은 자동 기억을 앞에, 나머지를 뒤에 두고 각 무리 안을 씨앗으로 섞는다.
func orderPool(memories []memory, rng *rand.Rand) []memory {
	stop, rest := []memory{}, []memory{}
	for _, item := range memories {
		if item.origin == "stop" {
			stop = append(stop, item)
		} else {
			rest = append(rest, item)
		}
	}
	rng.Shuffle(len(stop), func(i, j int) { stop[i], stop[j] = stop[j], stop[i] })
	rng.Shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] })
	return append(stop, rest...)
}

// unrelatedPairs 는 X 의 문장과 Y 의 요약을 붙인다. 둘 다 쓴 것으로 셈한다.
func unrelatedPairs(pool []memory, used map[string]bool, crossScope bool, want int) []pair {
	rule := "sscope"
	if crossScope {
		rule = "xscope"
	}
	out := []pair{}
	for _, x := range pool {
		if len(out) == want {
			break
		}
		if used[x.id] || len(x.sentences) == 0 {
			continue
		}
		evidence, _ := bestSentence(x, nil, nil)
		for _, y := range pool {
			if y.id == x.id || used[y.id] || (y.scope != x.scope) != crossScope {
				continue
			}
			if jaccard(evidence, y.summary) >= maxJaccard {
				continue
			}
			out = append(out, pair{Evidence: evidence, Claim: y.summary, Want: wantUnrelated,
				Src: x.id + "+" + y.id, Rule: rule})
			used[x.id], used[y.id] = true, true
			break
		}
	}
	return out
}

// writeOutputs 는 pairs.jsonl(전부)과 smoke.jsonl(갈래마다 첫 줄)을 쓴다.
func writeOutputs(dir string, pairs []pair) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	smoke := []pair{}
	for _, branch := range branches {
		for _, item := range pairs {
			if item.Want == branch {
				smoke = append(smoke, item)
				break
			}
		}
	}
	if err := writeJSONL(filepath.Join(dir, "pairs.jsonl"), pairs); err != nil {
		return err
	}
	return writeJSONL(filepath.Join(dir, "smoke.jsonl"), smoke)
}

func writeJSONL[T any](path string, items []T) error {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	for _, item := range items {
		if err := encoder.Encode(item); err != nil {
			return err
		}
	}
	// 산출물에 기억 본문이 들어간다. 이미 있는 파일 · 링크는 덮지 않는다 (링크를 따라가 엉뚱한 파일을 덮지 않게).
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s 가 이미 있어 안 덮는다 — 다시 만들려면 그 파일을 지우고 돌린다", path)
	}
	if err != nil {
		return err
	}
	if _, err := file.Write(buffer.Bytes()); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// resultRow 는 mem judge --file 결과 한 줄에서 채점에 쓰는 칸이다.
type resultRow struct {
	ID      string `json:"id"`
	Problem string `json:"problem"`
	Letter  string `json:"letter"`
	MS      int64  `json:"ms"`
}

var letterNames = map[string]string{"A": wantSupport, "B": wantContradict, "C": wantUnrelated}

func runScore(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("score", flag.ContinueOnError)
	pairsPath := flags.String("pairs", "", "pairs.jsonl")
	resultPath := flags.String("result", "", "mem judge support --file 의 stdout jsonl")
	if err := flags.Parse(args); err != nil {
		return err
	}
	pairs, err := readLines[pair](*pairsPath)
	if err != nil {
		return err
	}
	rows, err := readLines[resultRow](*resultPath)
	if err != nil {
		return err
	}
	score(pairs, rows, stdout)
	return nil
}

// readLines 는 jsonl 을 줄마다 T 로 읽는다. 빈 줄은 건너뛴다.
func readLines[T any](path string) ([]T, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	out := []T{}
	for number := 1; scanner.Scan(); number++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item T
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, fmt.Errorf("%s %d줄: %w", path, number, err)
		}
		out = append(out, item)
	}
	return out, scanner.Err()
}

// score 는 표를 찍고 판정(통과·조건부·미달)을 돌려준다.
// 문제 있음·답 없음·결과 줄 없음은 틀린 것으로 센다.
func score(pairs []pair, rows []resultRow, w io.Writer) string {
	byID := map[string]resultRow{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	got := func(item pair) string {
		row, ok := byID[item.ID]
		if !ok || row.Problem != "" || letterNames[row.Letter] == "" {
			return "none"
		}
		return letterNames[row.Letter]
	}
	right, missing := 0, 0
	branchRight, branchTotal := map[string]int{}, map[string]int{}
	confusion := map[string]map[string]int{}
	latencies, longLines := []int64{}, []string{}
	for _, item := range pairs {
		answer := got(item)
		if confusion[item.Want] == nil {
			confusion[item.Want] = map[string]int{}
		}
		confusion[item.Want][answer]++
		branchTotal[item.Want]++
		if answer == "none" {
			missing++
		} else {
			latencies = append(latencies, byID[item.ID].MS)
		}
		if answer == item.Want {
			right++
			branchRight[item.Want]++
		}
		if item.Rule == "long" {
			longLines = append(longLines, fmt.Sprintf("  %s  %s  %dms", item.ID, answer, byID[item.ID].MS))
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	slow := 0
	for _, ms := range latencies {
		if ms > passP95MS {
			slow++
		}
	}
	accuracy := 0.0
	if len(pairs) > 0 {
		accuracy = float64(right) / float64(len(pairs))
	}
	p95 := percentile(latencies, 95)
	fmt.Fprintf(w, "전체 정확도  %d/%d = %.3f\n", right, len(pairs), accuracy)
	fmt.Fprintln(w, "갈래별")
	for _, branch := range branches {
		fmt.Fprintf(w, "  %-10s %d/%d\n", branch, branchRight[branch], branchTotal[branch])
	}
	fmt.Fprintln(w, "혼동표 (줄=want, 칸=받은 답)")
	fmt.Fprintf(w, "  %-10s %10s %10s %10s %6s\n", "", wantSupport, wantContradict, wantUnrelated, "none")
	for _, branch := range branches {
		cells := confusion[branch]
		fmt.Fprintf(w, "  %-10s %10d %10d %10d %6d\n", branch,
			cells[wantSupport], cells[wantContradict], cells[wantUnrelated], cells["none"])
	}
	fmt.Fprintf(w, "못 받은 수  %d\n", missing)
	fmt.Fprintf(w, "지연  p50 %dms · p95 %dms · %dms 넘음 %d\n", percentile(latencies, 50), p95, passP95MS, slow)
	fmt.Fprintln(w, "긴 입력(long) 지연")
	for _, line := range longLines {
		fmt.Fprintln(w, line)
	}
	verdict := "미달"
	weak := false
	for _, branch := range branches {
		if branchRight[branch] <= weakBranch {
			weak = true
		}
	}
	switch {
	case accuracy >= passAccuracy && p95 <= passP95MS:
		verdict = "통과"
	case weak:
		verdict = "조건부"
	}
	fmt.Fprintf(w, "판정  %s (전체 ≥ %.2f 그리고 p95 ≤ %dms 면 통과)\n", verdict, passAccuracy, passP95MS)
	return verdict
}

// percentile 은 정렬된 값의 nearest-rank 백분위다. 값이 없으면 0.
func percentile(sorted []int64, p int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := (p*len(sorted) + 99) / 100
	if rank < 1 {
		rank = 1
	}
	return sorted[rank-1]
}
