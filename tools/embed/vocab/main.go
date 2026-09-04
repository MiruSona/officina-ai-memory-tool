// vocab 은 증류 어휘를 뽑는 작은 도구다.
//
// 임베딩 표의 어휘는 **우리 토크나이저가 내는 조각과 글자 하나까지 같아야**
// 한다. 파이썬으로 다시 구현하면 언젠가 어긋나므로, 조각내기는 Go 쪽
// `internal/token` 이 하고 파이썬은 그 결과 파일만 읽는다 (설계 4-8).
//
// 쓰는 법 :
//
//	go run ./tools/embed/vocab -out vocab.tsv <말뭉치 폴더> [폴더...]
//	  -words <파일>   낱말 빈도표(`testdata/corpus-words.txt`)를 같이 센다.
//	                  여러 번 줄 수 있다
//
// 내는 꼴 : `<조각>\t<나온 횟수>` 한 줄씩, 많이 나온 차례.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/token"
)

// wordFiles 는 여러 번 줄 수 있는 -words 다.
type wordFiles []string

func (w *wordFiles) String() string { return strings.Join(*w, ",") }

func (w *wordFiles) Set(value string) error {
	*w = append(*w, value)
	return nil
}

func main() {
	out := flag.String("out", "vocab.tsv", "결과 파일")
	words := wordFiles{}
	flag.Var(&words, "words", "낱말 빈도표 파일 (`<낱말> <횟수>` 꼴). 여러 번 줄 수 있다")
	flag.Parse()
	counts := map[string]int{}
	for _, dir := range flag.Args() {
		if err := walkDir(dir, counts); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	for _, path := range words {
		if err := readWords(path, counts); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if err := write(*out, counts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("조각 %d 개를 %s 에 적었다\n", len(counts), *out)
}

// walkDir 은 폴더 아래 .md 를 다 읽어 조각을 센다.
func walkDir(dir string, counts map[string]int) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".md") {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		count(string(body), 1, counts)
		return nil
	})
}

// readWords 는 `<낱말> <횟수>` 표를 읽는다. `#` 로 시작하는 줄은 설명이다.
func readWords(path string, counts map[string]int) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		times := 1
		if len(fields) >= 2 {
			if parsed, err := strconv.Atoi(fields[len(fields)-1]); err == nil {
				times = parsed
				fields = fields[:len(fields)-1]
			}
		}
		count(strings.Join(fields, " "), times, counts)
	}
	return scanner.Err()
}

// count 는 글 한 덩어리를 색인과 똑같이 쪼개 센다.
func count(text string, times int, counts map[string]int) {
	for _, piece := range strings.Fields(token.ForIndex(text)) {
		counts[piece] += times
	}
}

func write(path string, counts map[string]int) error {
	type row struct {
		piece string
		times int
	}
	rows := make([]row, 0, len(counts))
	for piece, times := range counts {
		rows = append(rows, row{piece, times})
	}
	sort.Slice(rows, func(a, b int) bool {
		if rows[a].times != rows[b].times {
			return rows[a].times > rows[b].times
		}
		return rows[a].piece < rows[b].piece
	})
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	for _, item := range rows {
		fmt.Fprintf(writer, "%s\t%d\n", item.piece, item.times)
	}
	return writer.Flush()
}
