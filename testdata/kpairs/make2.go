package main

// make2 는 Laya 다시 학습용 쌍 후보를 만든다. 갈래·몫은 Docs/Design/2026-10-08-자체판정프로그램설계.md 5절.
// make(K 재현)와 달리 한 기억을 여러 갈래에서 쓸 수 있다 (train). measure 는 한 기억을 한 번만 쓴다.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/secret"
)

// 갈래 이름 (candidate.Kind).
const (
	kindSame      = "same"
	kindLong      = "long"
	kindLink      = "link"
	kindNum       = "num"
	kindSupersede = "supersede"
	kindFlip      = "flip"
	kindSScope    = "sscope"
	kindXScope    = "xscope"
)

// kindOrder 는 MANIFEST·표에 찍는 차례다.
var kindOrder = []string{kindSame, kindLong, kindLink, kindNum, kindSupersede, kindFlip, kindSScope, kindXScope}

var trainQuota = map[string]int{
	kindSame: 220, kindLong: 110, kindLink: 110, kindNum: 110,
	kindSupersede: 65, kindFlip: 200, kindSScope: 300, kindXScope: 150,
}

// measureQuota 의 반대 30 은 덮음 먼저, 모자라면 뒤집기로 채운다 (contradictWant).
var measureQuota = map[string]int{
	kindSame: 15, kindLink: 8, kindNum: 7,
	kindSupersede: 30, kindSScope: 20, kindXScope: 10,
}

// contradictWant 는 measure 의 반대 몫이다 — 덮음 몫 + 뒤집기 몫. 덮음이 모자란 만큼 뒤집기로 채운다.
// 기본 measureQuota 는 덮음 30 · 뒤집기 0 이라 30 이다.
func contradictWant(quota map[string]int) int {
	return quota[kindSupersede] + quota[kindFlip]
}

// parseQuota 는 -quota 값 「kind=N,kind=N」 을 읽는다. 적지 않은 갈래는 0 이다.
func parseQuota(text string) (map[string]int, error) {
	known := map[string]bool{}
	for _, kind := range kindOrder {
		known[kind] = true
	}
	quota := map[string]int{}
	for _, part := range strings.Split(text, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kind, value, ok := strings.Cut(part, "=")
		if !ok || !known[kind] {
			return nil, fmt.Errorf("-quota 칸을 못 읽음: %q (kind=N 꼴, kind 는 %s)", part, strings.Join(kindOrder, " "))
		}
		count, err := strconv.Atoi(value)
		if err != nil || count < 0 {
			return nil, fmt.Errorf("-quota 수가 이상함: %q", part)
		}
		quota[kind] = count
	}
	if len(quota) == 0 {
		return nil, errors.New("-quota 가 비었다")
	}
	return quota, nil
}

// candidate 는 candidates.jsonl 한 줄이다. want 는 struct_label 과 같은 값 — mem judge 가 채점에 쓴다.
type candidate struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Rule        string   `json:"rule,omitempty"`
	Evidence    string   `json:"evidence"`
	Claim       string   `json:"claim"`
	Want        string   `json:"want"`
	StructLabel string   `json:"struct_label"`
	SrcIDs      []string `json:"src_ids"`
}

func newCandidate(kind, label, evidence, claim string, ids ...string) candidate {
	return candidate{Kind: kind, Evidence: evidence, Claim: claim, Want: label, StructLabel: label, SrcIDs: ids}
}

// ledger 는 「이미 쓴 기억」 장부다. measure 는 모든 칸이 한 장부를 같이 쓴다.
// 링크·덮음은 train 에선 같은 기억이 여러 쌍에 나와도 되고(shared 거짓), measure 에선 안 된다.
type ledger struct {
	shared                                          bool
	support, flip, link, supersede, evidence, claim map[string]bool
}

func newLedger(shared bool) ledger {
	if shared {
		one := map[string]bool{}
		return ledger{true, one, one, one, one, one, one}
	}
	return ledger{false, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}}
}

func runMake2(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("make2", flag.ContinueOnError)
	store := flags.String("store", "", "기억 저장소 폴더 (Memory/store)")
	seed := flags.Int64("seed", 1, "씨앗")
	out := flags.String("out", "", "출력 폴더")
	exclude := flags.String("exclude", "", "뺄 기억 id 파일 (한 줄에 하나 · K 원천 id)")
	deny := flags.String("deny", "", "금지어 파일 (한 줄에 낱말 하나)")
	set := flags.String("set", "train", "train(스튜디오 store 만) 또는 measure(다른 store 만)")
	flips := flags.Int("flips", 0, "0 보다 크면 뒤집기 쌍만 이만큼 낸다 (flips.jsonl · 쓴 기억은 -exclude 로 뺀다)")
	moreTypes := flags.String("more-types", "", "measure 에서 decision·caution·howto 밖에 더 쓸 종류 「history,todo,…」 (쓸 만한 기억이 다 쓰였을 때)")
	avoid := flags.String("avoid", "", "measure 에서 근거로 다시 안 쓸 쌍 파일들 「a.jsonl,b.jsonl」 — 그 evidence 문장을 건너뛴다")
	quotaText := flags.String("quota", "", "measure 몫을 바꾼다 「kind=N,…」 (measure-draft.jsonl · 반대는 덮음 먼저, 모자라면 뒤집기)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *store == "" || *out == "" {
		return errors.New("-store 와 -out 은 꼭 줘야 한다")
	}
	if *set != "train" && *set != "measure" {
		return fmt.Errorf("-set 은 train 이나 measure: %s", *set)
	}
	studio := isStudioStore(*store)
	if *set == "train" && !studio {
		return errors.New("-set train 은 스튜디오 Memory/store 만 받는다 (누수 막기 ④)")
	}
	if *set == "measure" && studio {
		return errors.New("-set measure 는 스튜디오가 아닌 store 만 받는다 (누수 막기 ④)")
	}
	opts := make2Opts{}
	if *moreTypes != "" {
		if *set != "measure" {
			return errors.New("-more-types 는 -set measure 에서만 쓴다")
		}
		opts.extraTypes = map[string]bool{}
		for _, kind := range strings.Split(*moreTypes, ",") {
			if kind = strings.TrimSpace(kind); kind != "" {
				opts.extraTypes[kind] = true
			}
		}
	}
	if *avoid != "" {
		if *set != "measure" {
			return errors.New("-avoid 는 -set measure 에서만 쓴다")
		}
		sentences, err := readEvidenceSet(*avoid)
		if err != nil {
			return err
		}
		opts.avoid = sentences
	}
	denyWords, err := readDenyWords(*deny)
	if err != nil {
		return err
	}
	excluded, err := readIDSet(*exclude)
	if err != nil {
		return err
	}
	memories, err := loadAll(*store, denyWords, excluded)
	if err != nil {
		return err
	}
	quota, prefix, name := trainQuota, "t%04d", "candidates.jsonl"
	if *set == "measure" {
		quota, prefix, name = measureQuota, "g%03d", "game90-draft.jsonl"
	}
	if *quotaText != "" {
		if *set != "measure" || *flips > 0 {
			return errors.New("-quota 는 -set measure 에서만, -flips 없이 쓴다")
		}
		if quota, err = parseQuota(*quotaText); err != nil {
			return err
		}
		if opts.avoid != nil && quota[kindLong] > 0 {
			return errors.New("-avoid 와 -quota long=N 은 같이 못 쓴다 — long 은 본문 전체가 근거라 avoid 가 안 먹힌다")
		}
		prefix, name = "m%03d", "measure-draft.jsonl"
	}
	var got []candidate
	if *flips > 0 {
		quota, prefix, name = map[string]int{kindFlip: *flips}, "f%03d", "flips.jsonl"
		got = flipOnly(memories, *seed, *flips, opts)
	} else {
		got = makeCandidates(memories, *seed, quota, *set == "measure", opts)
	}
	for i := range got {
		got[i].ID = fmt.Sprintf(prefix, i+1)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	path := filepath.Join(*out, name)
	if err := writeJSONL(path, got); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	storeKind := "other"
	if studio {
		storeKind = "studio"
	}
	head := fmt.Sprintf("set: %s\nstore: %s\nseed: %d\nmemories: %d\nexcluded: %d\nfile: %s\nsha256: %s\n",
		*set, storeKind, *seed, len(memories), len(excluded), name, sha256Hex(data))
	if *quotaText != "" || *moreTypes != "" {
		head += fmt.Sprintf("quota: %s\nmore-types: %s\n", *quotaText, *moreTypes)
	}
	if *avoid != "" {
		head += fmt.Sprintf("avoid: %s (%d 문장)\n", *avoid, len(opts.avoid))
	}
	manifest := head + countTable(got, quota, *set == "measure" && *flips == 0)
	fmt.Fprint(stdout, manifest)
	return writeNew(filepath.Join(*out, "MANIFEST.txt"), []byte(manifest))
}

// isStudioStore 는 store 가 <스튜디오>/Memory/store 인지 본다 — 스튜디오 뿌리엔 AIMemoryTool/ 폴더가 있다.
func isStudioStore(store string) bool {
	abs, err := filepath.Abs(store)
	if err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(abs, "..", "..", "AIMemoryTool"))
	return err == nil && info.IsDir()
}

// readIDSet 은 한 줄에 id 하나인 파일을 읽는다. 빈 줄과 # 줄은 건너뛴다. 경로가 비면 빈 집합.
func readIDSet(path string) (map[string]bool, error) {
	set := map[string]bool{}
	if path == "" {
		return set, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			set[line] = true
		}
	}
	return set, nil
}

// loadAll 은 loadMemories 와 같은 거름(뺄 scope · 비밀 · 금지어 · 요약 없음)을 하되 종류는 안 거른다.
// 링크·덮음은 history 같은 종류도 쓰고, 나머지 갈래는 usable 로 다시 거른다.
func loadAll(store string, denyWords []string, excluded map[string]bool) ([]memory, error) {
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
		if !ok || skipScopes[item.scope] || item.summary == "" || excluded[item.id] {
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

// make2Opts 는 make2 한 판의 선택이다. 전역 대신 넘겨 시험끼리 서로 오염되지 않게 한다.
type make2Opts struct {
	avoid      map[string]bool // -avoid 로 읽은 「이미 근거로 쓴 문장」 — bestSentenceFor 가 건너뛴다. 평소엔 nil
	extraTypes map[string]bool // -more-types 로 이번 한 번만 더 쓰는 종류. 평소엔 nil
}

// readEvidenceSet 은 쉼표로 나눈 jsonl 파일들의 evidence 칸을 모은다.
func readEvidenceSet(paths string) (map[string]bool, error) {
	set := map[string]bool{}
	for _, path := range strings.Split(paths, ",") {
		if path = strings.TrimSpace(path); path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var row struct {
				Evidence string `json:"evidence"`
			}
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			set[row.Evidence] = true
		}
	}
	return set, nil
}

func (o make2Opts) usable(item memory) bool {
	return item.origin == "stop" || usableTypes[item.kind] || o.extraTypes[item.kind]
}

// makeCandidates 는 갈래마다 몫까지 뽑아 섞는다. 몫을 못 채우면 지어내지 않고 있는 만큼만 낸다.
func makeCandidates(memories []memory, seed int64, quota map[string]int, measure bool, opts make2Opts) []candidate {
	rng := rand.New(rand.NewSource(seed))
	pick := []memory{}
	for _, item := range memories {
		if opts.usable(item) {
			pick = append(pick, item)
		}
	}
	pool := orderPool(pick, rng)
	everyone := orderPool(memories, rng)
	byID := map[string]memory{}
	for _, item := range memories {
		byID[item.id] = item
	}
	book := newLedger(measure)
	all := []candidate{}
	// 반대가 가장 모자라니 먼저 고른다. measure 는 덮음이 모자란 만큼 뒤집기로 채운다.
	all = append(all, supersedePairs(everyone, byID, book, quota[kindSupersede], opts)...)
	flipWant := quota[kindFlip]
	if measure {
		flipWant = contradictWant(quota) - len(all)
	}
	all = append(all, flipPairs(pool, rng, book, flipWant, opts)...)
	all = append(all, numPairs(pool, book, quota[kindNum], opts)...)
	all = append(all, linkPairs(everyone, byID, book, quota[kindLink], opts)...)
	all = append(all, longPairs(pool, book, quota[kindLong])...)
	all = append(all, samePairs(pool, book, quota[kindSame], opts)...)
	all = append(all, unrelatedCandidates(pool, book, true, quota[kindXScope], opts)...)
	all = append(all, unrelatedCandidates(pool, book, false, quota[kindSScope], opts)...)
	rng.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
	return all
}

// flipOnly 는 뒤집기 쌍만 want 개 뽑는다. 덮음이 많아 measure 반대를 뒤집기로 따로 채울 때 쓴다.
// 한 기억은 한 번만 쓴다. 이미 쓴 기억은 부르는 쪽이 -exclude 로 미리 뺀다.
func flipOnly(memories []memory, seed int64, want int, opts make2Opts) []candidate {
	rng := rand.New(rand.NewSource(seed))
	pick := []memory{}
	for _, item := range memories {
		if opts.usable(item) {
			pick = append(pick, item)
		}
	}
	return flipPairs(orderPool(pick, rng), rng, newLedger(true), want, opts)
}

// supersedePairs : 덮은 기억 요약(주장) ↔ 덮인 기억 본문 중 그 요약과 겹침 최고 문장(근거). 구조 라벨은 반대.
func supersedePairs(everyone []memory, byID map[string]memory, book ledger, want int, opts make2Opts) []candidate {
	out := []candidate{}
	for _, old := range everyone {
		if len(out) == want {
			break
		}
		next, ok := byID[old.supersededBy]
		if !ok || next.id == old.id {
			continue
		}
		if book.shared && (book.supersede[old.id] || book.supersede[next.id]) {
			continue
		}
		evidence, _ := bestSentenceFor(old, next.summary, nil, opts.avoid)
		if evidence == "" {
			continue
		}
		out = append(out, newCandidate(kindSupersede, wantContradict, evidence, next.summary, old.id, next.id))
		book.supersede[old.id], book.supersede[next.id] = true, true
	}
	return out
}

// flipPairs 는 K 의 num·neg 뒤집기를 그대로 쓴다.
func flipPairs(pool []memory, rng *rand.Rand, book ledger, want int, opts make2Opts) []candidate {
	out := []candidate{}
	for _, item := range pool {
		if len(out) >= want {
			break
		}
		if book.flip[item.id] {
			continue
		}
		options := contradictions(item, opts.avoid)
		if len(options) == 0 {
			continue
		}
		chosen := options[rng.Intn(len(options))]
		flipped := newCandidate(kindFlip, wantContradict, chosen.Evidence, chosen.Claim, item.id)
		flipped.Rule = chosen.Rule
		out = append(out, flipped)
		book.flip[item.id] = true
	}
	return out
}

// numPairs : 요약에 값 숫자가 있고, 숫자 든 본문 문장 중 겹침 최고(낱말 2개 이상)를 근거로 쓴다.
func numPairs(pool []memory, book ledger, want int, opts make2Opts) []candidate {
	out := []candidate{}
	for _, item := range pool {
		if len(out) == want {
			break
		}
		if book.support[item.id] || !hasPlainNumber(item.summary) {
			continue
		}
		evidence, score := bestSentence(item, hasPlainNumber, opts.avoid)
		if score < minOverlap {
			continue
		}
		out = append(out, newCandidate(kindNum, wantSupport, evidence, item.summary, item.id))
		book.support[item.id] = true
	}
	return out
}

func hasPlainNumber(text string) bool {
	for _, loc := range numberPattern.FindAllStringIndex(text, -1) {
		if _, ok := plainNumber(text, loc); ok {
			return true
		}
	}
	return false
}

// linkPairs : 가리키는 기억 요약(주장) ↔ 가리킨 기억 본문 중 그 요약과 겹침 최고 문장(근거). 낱말 1개 이상 겹쳐야 한다.
func linkPairs(everyone []memory, byID map[string]memory, book ledger, want int, opts make2Opts) []candidate {
	out := []candidate{}
	seen := map[string]bool{}
	for _, from := range everyone {
		for _, id := range from.links {
			if len(out) == want {
				return out
			}
			to, ok := byID[id]
			key := from.id + "+" + id
			if !ok || to.id == from.id || seen[key] {
				continue
			}
			if book.shared && (book.link[from.id] || book.link[to.id]) {
				continue
			}
			evidence, score := bestSentenceFor(to, from.summary, nil, opts.avoid)
			if score < 1 {
				continue
			}
			seen[key] = true
			out = append(out, newCandidate(kindLink, wantSupport, evidence, from.summary, to.id, from.id))
			book.link[from.id], book.link[to.id] = true, true
		}
	}
	return out
}

func longPairs(pool []memory, book ledger, want int) []candidate {
	out := []candidate{}
	for _, item := range pool {
		if len(out) == want {
			break
		}
		size := len([]rune(item.body))
		if book.support[item.id] || size < minLongBody || size > maxLongBody {
			continue
		}
		out = append(out, newCandidate(kindLong, wantSupport, item.body, item.summary, item.id))
		book.support[item.id] = true
	}
	return out
}

func samePairs(pool []memory, book ledger, want int, opts make2Opts) []candidate {
	out := []candidate{}
	for _, item := range pool {
		if len(out) == want {
			break
		}
		if book.support[item.id] {
			continue
		}
		if evidence, score := bestSentence(item, nil, opts.avoid); score >= minOverlap {
			out = append(out, newCandidate(kindSame, wantSupport, evidence, item.summary, item.id))
			book.support[item.id] = true
		}
	}
	return out
}

// unrelatedCandidates : X 의 문장(근거) ↔ Y 의 요약(주장). 겹침 < 0.1 · 서로 링크·덮음 없음.
// train 은 한 기억을 근거로 한 번 · 주장으로 한 번까지 쓴다.
func unrelatedCandidates(pool []memory, book ledger, crossScope bool, want int, opts make2Opts) []candidate {
	kind := kindSScope
	if crossScope {
		kind = kindXScope
	}
	out := []candidate{}
	for _, x := range pool {
		if len(out) == want {
			break
		}
		if book.evidence[x.id] || len(x.sentences) == 0 {
			continue
		}
		evidence, _ := bestSentence(x, nil, opts.avoid)
		if evidence == "" {
			continue // -avoid 가 X 의 문장을 다 걸렀다 — 빈 근거 쌍을 내지 않는다
		}
		for _, y := range pool {
			if y.id == x.id || book.claim[y.id] || (y.scope != x.scope) != crossScope || related(x, y) {
				continue
			}
			if jaccard(evidence, y.summary) >= maxJaccard {
				continue
			}
			out = append(out, newCandidate(kind, wantUnrelated, evidence, y.summary, x.id, y.id))
			book.evidence[x.id], book.claim[y.id] = true, true
			break
		}
	}
	return out
}

func related(x, y memory) bool {
	if x.supersededBy == y.id || y.supersededBy == x.id {
		return true
	}
	for _, id := range x.links {
		if id == y.id {
			return true
		}
	}
	for _, id := range y.links {
		if id == x.id {
			return true
		}
	}
	return false
}

// countTable 은 갈래별 「뽑음/몫」과 라벨별 수를 글로 만든다.
func countTable(got []candidate, quota map[string]int, measure bool) string {
	counts, labels := map[string]int{}, map[string]int{}
	for _, item := range got {
		counts[item.Kind]++
		labels[item.StructLabel]++
	}
	var b strings.Builder
	b.WriteString("kind\tgot\tquota\n")
	for _, kind := range kindOrder {
		want := quota[kind]
		if measure && kind == kindFlip {
			want = contradictWant(quota) - counts[kindSupersede]
		}
		mark := ""
		if counts[kind] < want {
			mark = "\t모자람"
		}
		fmt.Fprintf(&b, "%s\t%d\t%d%s\n", kind, counts[kind], want, mark)
	}
	fmt.Fprintf(&b, "label\tsupport %d\tcontradict %d\tunrelated %d\ntotal\t%d\n",
		labels[wantSupport], labels[wantContradict], labels[wantUnrelated], len(got))
	return b.String()
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// writeNew 는 이미 있는 파일·링크를 덮지 않고 새로 쓴다 (writeJSONL 과 같은 까닭).
func writeNew(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s 가 이미 있어 안 덮는다 — 다시 만들려면 그 파일을 지우고 돌린다", path)
	}
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
