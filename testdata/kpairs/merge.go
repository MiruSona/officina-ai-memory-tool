package main

// merge 는 구조 라벨 · 규칙 · agent 표를 합쳐 채택 쌍과 Opus 묶음을 나눈다 (설계 5절 「세 투표 합치기」).
// Opus 물음 글 opus/PROMPT.md 는 손으로 둔다 (internal/llm 의 support-v3 글을 옮긴 것).

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// voteRow 는 mem judge support --file 결과 한 줄에서 투표에 쓰는 칸이다.
type voteRow struct {
	ID      string `json:"id"`
	Letter  string `json:"letter"`
	Unsure  bool   `json:"unsure"`
	Problem string `json:"problem"`
	Stage   string `json:"stage"`
}

// agreedRow 는 agreed.jsonl 한 줄이다. by 는 일치한 표 이름(struct·rules·agent)이다.
type agreedRow struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Evidence string   `json:"evidence"`
	Claim    string   `json:"claim"`
	Want     string   `json:"want"`
	SrcIDs   []string `json:"src_ids"`
	By       string   `json:"by"`
}

// opusRow 는 Opus 에게 보내는 한 줄이다. 투표는 안 보여 준다 (끌림 막기).
type opusRow struct {
	ID       string `json:"id"`
	Evidence string `json:"evidence"`
	Claim    string `json:"claim"`
}

var nameLetters = map[string]string{wantSupport: "A", wantContradict: "B", wantUnrelated: "C"}

func runMerge(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("merge", flag.ContinueOnError)
	candidatesPath := flags.String("candidates", "", "make2 의 candidates.jsonl")
	agentPath := flags.String("agent", "", "agent 표 (mem judge support --file … --stage semif 결과)")
	rulesPath := flags.String("rules", "", "규칙 표 (--stage rules 결과 · 없으면 건너뛴다)")
	out := flags.String("out", "", "출력 폴더 (agreed.jsonl · opus/)")
	batch := flags.Int("batch", 50, "Opus 묶음 하나의 쌍 수")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *candidatesPath == "" || *agentPath == "" || *out == "" || *batch < 1 {
		return errors.New("-candidates · -agent · -out 은 꼭 줘야 하고 -batch 는 1 이상")
	}
	candidates, err := readLines[candidate](*candidatesPath)
	if err != nil {
		return err
	}
	agent, err := readVotes(*agentPath, false)
	if err != nil {
		return err
	}
	rules := map[string]string{}
	if *rulesPath != "" {
		rules, err = readVotes(*rulesPath, true)
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(stdout, "규칙 표가 없어 두 표로만 합친다")
			rules = map[string]string{}
		} else if err != nil {
			return err
		}
	}
	agreed, toOpus := []agreedRow{}, []opusRow{}
	match, total := map[string]int{}, map[string]int{}
	for _, item := range candidates {
		structLetter := nameLetters[item.StructLabel]
		total[item.Kind]++
		if agent[item.ID] == structLetter {
			match[item.Kind]++
		}
		letter, by := majority(structLetter, rules[item.ID], agent[item.ID])
		if letter == "" || item.Kind == kindSupersede {
			toOpus = append(toOpus, opusRow{ID: item.ID, Evidence: item.Evidence, Claim: item.Claim})
			continue
		}
		agreed = append(agreed, agreedRow{ID: item.ID, Kind: item.Kind, Evidence: item.Evidence, Claim: item.Claim,
			Want: letterNames[letter], SrcIDs: item.SrcIDs, By: by})
	}
	if err := writeMergeOutputs(*out, agreed, toOpus, *batch); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "kind\tstruct=agent\ttotal")
	for _, kind := range kindOrder {
		fmt.Fprintf(stdout, "%s\t%d\t%d\n", kind, match[kind], total[kind])
	}
	batches := (len(toOpus) + *batch - 1) / *batch
	fmt.Fprintf(stdout, "채택 %d · Opus %d (묶음 %d)\n", len(agreed), len(toOpus), batches)
	return nil
}

// readVotes 는 id → 글자다. 문제 있음 · 「모른다」 · A·B·C 밖 글자는 표로 안 친다.
// 규칙 단이 낸 답은 규칙 표에서만 센다 — agent 표를 사다리 전체로 돌렸으면 규칙 답이 두 표가 된다.
func readVotes(path string, rulesVote bool) (map[string]string, error) {
	rows, err := readLines[voteRow](path)
	if err != nil {
		return nil, err
	}
	votes := map[string]string{}
	for _, row := range rows {
		if row.Problem != "" || row.Unsure || letterNames[row.Letter] == "" {
			continue
		}
		if (row.Stage == "rules") != rulesVote && row.Stage != "" {
			continue
		}
		votes[row.ID] = row.Letter
	}
	return votes, nil
}

// majority 는 표(빈 글은 기권) 중 둘 이상이 같은 글자를 고르면 그 글자와 표 이름을 돌려준다.
func majority(structLetter, rulesLetter, agentLetter string) (string, string) {
	names := []string{"struct", "rules", "agent"}
	letters := []string{structLetter, rulesLetter, agentLetter}
	for i, letter := range letters {
		if letter == "" {
			continue
		}
		by := []string{}
		for j, other := range letters {
			if other == letter {
				by = append(by, names[j])
			}
		}
		if len(by) >= 2 {
			return letters[i], strings.Join(by, "+")
		}
	}
	return "", ""
}

func writeMergeOutputs(dir string, agreed []agreedRow, toOpus []opusRow, batch int) error {
	opusDir := filepath.Join(dir, "opus")
	if err := os.MkdirAll(opusDir, 0o755); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(dir, "agreed.jsonl"), agreed); err != nil {
		return err
	}
	for start, number := 0, 1; start < len(toOpus); start, number = start+batch, number+1 {
		end := min(start+batch, len(toOpus))
		path := filepath.Join(opusDir, fmt.Sprintf("in-%02d.jsonl", number))
		if err := writeJSONL(path, toOpus[start:end]); err != nil {
			return err
		}
	}
	return nil
}
