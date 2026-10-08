package main

// leak 은 학습 쌍과 측정 쌍 사이 누수를 네 가지로 본다 (설계 5절 ①~④). 하나라도 걸리면 exit 1.

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// claimGram 은 ③ 주장 n-gram 길이(빈칸 뺀 글자 수)다.
const claimGram = 8

// stockClaims : train 주장 이만큼 이상에 나오는 조각은 상투 문구(「(사용자 확정)」 등)로 보고 ③ 에서 뺀다.
const stockClaims = 3

// leakShow 는 검사마다 찍는 걸림 줄 상한이다. 수는 다 센다.
const leakShow = 20

// leakRow 는 쌍 파일 한 줄에서 누수 검사에 쓰는 칸이다. K pairs.jsonl 은 src("a+b"), make2 는 src_ids.
type leakRow struct {
	ID     string   `json:"id"`
	Claim  string   `json:"claim"`
	Src    string   `json:"src"`
	SrcIDs []string `json:"src_ids"`
}

func (row leakRow) ids() []string {
	out := append([]string{}, row.SrcIDs...)
	if row.Src != "" {
		out = append(out, strings.Split(row.Src, "+")...)
	}
	return out
}

// listFlag 는 -test 를 여러 번 받는다.
type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }

func (l *listFlag) Set(value string) error {
	*l = append(*l, value)
	return nil
}

func runLeak(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("leak", flag.ContinueOnError)
	trainPath := flags.String("train", "", "학습 쌍 jsonl (make2 -set train)")
	exclude := flags.String("exclude", "", "K 원천 id 파일")
	var tests listFlag
	flags.Var(&tests, "test", "측정 쌍 jsonl (여러 번 줄 수 있다)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *trainPath == "" || len(tests) == 0 {
		return errors.New("-train 과 -test 는 꼭 줘야 한다")
	}
	train, err := readLines[leakRow](*trainPath)
	if err != nil {
		return err
	}
	excluded, err := readIDSet(*exclude)
	if err != nil {
		return err
	}
	hits := 0
	hits += report(stdout, "① K 원천 id 가 train 에", excludedHits(train, excluded))
	trainIDs := idOwners(train)
	trainGrams := gramOwners(train)
	for _, path := range tests {
		test, err := readLines[leakRow](path)
		if err != nil {
			return err
		}
		name := filepath.Base(path)
		hits += report(stdout, "② 기억 id 겹침 train ↔ "+name, idHits(test, trainIDs))
		hits += report(stdout, fmt.Sprintf("③ 주장 %d글자 겹침 train ↔ %s", claimGram, name), gramHits(test, trainGrams))
	}
	hits += report(stdout, "④ store 갈래", storeHits(*trainPath, tests))
	if hits > 0 {
		return fmt.Errorf("누수 %d건", hits)
	}
	fmt.Fprintln(stdout, "누수 0")
	return nil
}

// report 는 걸린 것을 상한까지 찍고 수를 돌려준다.
func report(w io.Writer, title string, found []string) int {
	fmt.Fprintf(w, "%s : %d\n", title, len(found))
	for i, line := range found {
		if i == leakShow {
			fmt.Fprintf(w, "  … %d건 더\n", len(found)-leakShow)
			break
		}
		fmt.Fprintln(w, "  "+line)
	}
	return len(found)
}

func excludedHits(train []leakRow, excluded map[string]bool) []string {
	found := []string{}
	for _, row := range train {
		for _, id := range row.ids() {
			if excluded[id] {
				found = append(found, row.ID+" "+id)
			}
		}
	}
	return found
}

// idOwners 는 기억 id → 그 id 를 쓴 첫 쌍 id 다.
func idOwners(rows []leakRow) map[string]string {
	owners := map[string]string{}
	for _, row := range rows {
		for _, id := range row.ids() {
			if _, ok := owners[id]; !ok {
				owners[id] = row.ID
			}
		}
	}
	return owners
}

func idHits(test []leakRow, owners map[string]string) []string {
	found := []string{}
	for _, row := range test {
		for _, id := range row.ids() {
			if owner, ok := owners[id]; ok {
				found = append(found, fmt.Sprintf("%s ↔ %s (%s)", row.ID, owner, id))
			}
		}
	}
	return found
}

// claimGrams 는 주장에서 빈칸을 뺀 글자로 claimGram 글자 조각을 모두 뽑는다.
// 영문·숫자가 든 조각은 뺀다 — 툴 이름·경로·날짜는 두 저장소가 같이 쓰는 낱말이지 주장 누수가 아니다.
func claimGrams(claim string) []string {
	runes := []rune{}
	for _, r := range claim {
		if !unicode.IsSpace(r) {
			runes = append(runes, r)
		}
	}
	grams := []string{}
	for i := 0; i+claimGram <= len(runes); i++ {
		gram := runes[i : i+claimGram]
		if !hasASCIIAlnum(gram) {
			grams = append(grams, string(gram))
		}
	}
	return grams
}

func hasASCIIAlnum(runes []rune) bool {
	for _, r := range runes {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return true
		}
	}
	return false
}

// gramOwners 는 조각 → 그 조각을 쓴 첫 쌍 id 다. 서로 다른 주장 stockClaims 개 이상에 나온 조각은 뺀다.
func gramOwners(rows []leakRow) map[string]string {
	owners, claims := map[string]string{}, map[string]map[string]bool{}
	for _, row := range rows {
		for _, gram := range claimGrams(row.Claim) {
			if _, ok := owners[gram]; !ok {
				owners[gram] = row.ID
				claims[gram] = map[string]bool{}
			}
			claims[gram][row.Claim] = true
		}
	}
	for gram, set := range claims {
		if len(set) >= stockClaims {
			delete(owners, gram)
		}
	}
	return owners
}

// gramHits 는 쌍마다 첫 겹침 조각 하나만 적는다 (본문 조각을 많이 찍지 않게 쌍 id 만).
func gramHits(test []leakRow, owners map[string]string) []string {
	found := []string{}
	for _, row := range test {
		for _, gram := range claimGrams(row.Claim) {
			if owner, ok := owners[gram]; ok {
				found = append(found, fmt.Sprintf("%s ↔ %s", row.ID, owner))
				break
			}
		}
	}
	return found
}

// storeHits : train 옆 MANIFEST 는 set train · store studio 여야 하고,
// MANIFEST 가 있는 test 는 store other 여야 한다 (K pairs 처럼 MANIFEST 없는 test 는 건너뛴다).
func storeHits(trainPath string, tests []string) []string {
	found := []string{}
	fields, err := readManifest(filepath.Join(filepath.Dir(trainPath), "MANIFEST.txt"))
	if err != nil {
		found = append(found, "train MANIFEST 를 못 읽음: "+err.Error())
	} else if fields["set"] != "train" || fields["store"] != "studio" {
		found = append(found, fmt.Sprintf("train MANIFEST 가 set %q · store %q", fields["set"], fields["store"]))
	}
	for _, path := range tests {
		fields, err := readManifest(filepath.Join(filepath.Dir(path), "MANIFEST.txt"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			found = append(found, filepath.Base(path)+" MANIFEST 를 못 읽음: "+err.Error())
			continue
		}
		if fields["store"] != "other" {
			found = append(found, fmt.Sprintf("%s MANIFEST 가 store %q", filepath.Base(path), fields["store"]))
		}
	}
	return found
}

// readManifest 는 「이름: 값」 줄만 읽는다.
func readManifest(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	fields := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ": ")
		if ok {
			fields[key] = strings.TrimSpace(value)
		}
	}
	return fields, scanner.Err()
}
