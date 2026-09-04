package index

import (
	"math"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mirusona/officina-ai-memory-tool/internal/token"
)

// 재순위 재료 (설계 4-1b · 4-3).
//
// FTS5 의 bm25 는 k1·b 를 못 바꾼다. 그래서 검색은 상위 몇 건만 우리 손으로
// 다시 매기는데(k1=1.2, b=0.4) 그때 필요한 것이 넷이다.
//   ① 문서마다 필드별 낱말 수  → memories.n_title/n_meta/n_sum/n_body
//   ② 말뭉치 평균 필드 길이     → CorpusStats
//   ③ 낱말마다 몇 건에 나오나   → DocFreq (vocab_ko)
//   ④ 문서 안 낱말 셈(tf)      → RerankRows 가 원문에서 조각을 다시 만든다
//
// ④ 를 DB 에 담지 않는 이유 : 조각 글은 본문의 2.5배라 20k 에서 그것만
// 30MB 가 넘는다. 상위 200건만 다시 만드는 값이 훨씬 싸다.

// RerankRow 는 문서 하나의 재순위 재료다.
type RerankRow struct {
	Docid int64
	ID    string
	// Path 는 store 상대 경로다. 본문 원본이 있는 자리다.
	Path string
	// 네 필드의 색인 조각. 색인에 넣은 것과 같은 함수를 지나서 짝이 맞는다.
	TitleKO   string
	MetaKO    string
	SummaryKO string
	BodyKO    string
	// Lens 는 색인할 때 센 필드별 낱말 수다 (title·meta·summary·body 순).
	Lens [4]int
	// Body 는 구절 가산이 쓸 원문이다. DB 가 아니라 md 에서 그때 읽는다.
	// 파일이 없어졌으면 빈 글이다.
	Body string
}

// rerankWorkers 는 본문을 같이 읽을 일꾼 수다. 파일 읽기가 섞여 있어 코어 수를
// 넘겨도 손해가 아니지만, 검색 하나가 기계를 다 먹으면 안 되니 여기서 끊는다.
const rerankWorkers = 8

// fillRerankBodies 는 본문을 읽어 조각을 만든다. 자리 번호로 갈라 맡기므로
// 일꾼이 몇이든 결과가 같다.
func fillRerankBodies(d *DB, rows []RerankRow, raw []rerankText) {
	workers := rerankWorkers
	if workers > len(rows) {
		workers = len(rows)
	}
	if workers <= 1 {
		for at := range rows {
			fillOneRerank(d, &rows[at], raw[at])
		}
		return
	}
	next := atomic.Int64{}
	wait := sync.WaitGroup{}
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for {
				at := int(next.Add(1)) - 1
				if at >= len(rows) {
					return
				}
				fillOneRerank(d, &rows[at], raw[at])
			}
		}()
	}
	wait.Wait()
}

// rerankText 는 DB 에서 읽어 온 원문 칸이다. 조각은 일꾼이 만든다.
type rerankText struct {
	Title   string
	Meta    string
	Summary string
}

func fillOneRerank(d *DB, row *RerankRow, raw rerankText) {
	row.Body = d.BodyOf(row.Path)
	row.TitleKO = token.ForIndex(raw.Title)
	row.MetaKO = token.ForIndex(raw.Meta)
	row.SummaryKO = token.ForIndex(raw.Summary)
	row.BodyKO = token.ForIndex(row.Body)
}

// CorpusStats 는 말뭉치 전체 눈금이다.
type CorpusStats struct {
	Docs int
	// AvgLen 은 필드별 평균 낱말 수다. 0 건이면 1 로 둔다 — 나누기 자리다.
	AvgLen [4]float64
}

// RerankRows 는 docid 몇 개의 재순위 재료를 준다. 순서는 준 차례를 지킨다.
func (d *DB) RerankRows(docids []int64) ([]RerankRow, error) {
	if len(docids) == 0 {
		return nil, nil
	}
	places := make([]string, len(docids))
	args := make([]any, len(docids))
	for at, docid := range docids {
		places[at] = "?"
		args[at] = docid
	}
	// 제목·태그·scope 는 memories 에 이미 따로 있다. head 열을 따로 두면 같은
	// 글을 두 번 담는 것이라 v0.4 에서 뺐다.
	rows, err := d.sql.Query(`SELECT docid, id, path, title, tags, scope, summary,
		n_title, n_meta, n_sum, n_body
		FROM memories WHERE docid IN (`+strings.Join(places, ",")+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := []RerankRow{}
	raw := []rerankText{}
	for rows.Next() {
		row := RerankRow{}
		title, tags, scope, summary := "", "", "", ""
		if err := rows.Scan(&row.Docid, &row.ID, &row.Path, &title, &tags, &scope, &summary,
			&row.Lens[0], &row.Lens[1], &row.Lens[2], &row.Lens[3]); err != nil {
			return nil, err
		}
		found = append(found, row)
		raw = append(raw, rerankText{Title: title,
			Meta: strings.TrimSpace(tags) + " " + scope, Summary: summary})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 본문은 DB 에 없다. 상위 몇 건이라 여기서 파일을 열어도 싸지만, 200건이면
	// 파일 200개를 열고 조각을 200번 만든다 — 검색 꼬리 지연이 다 여기다.
	// 자리 번호로 갈라 같이 읽는다. 답은 차례가 그대로라 안 바뀐다.
	fillRerankBodies(d, found, raw)
	byDocid := make(map[int64]RerankRow, len(found))
	for _, row := range found {
		byDocid[row.Docid] = row
	}
	out := make([]RerankRow, 0, len(docids))
	for _, docid := range docids {
		if row, ok := byDocid[docid]; ok {
			out = append(out, row)
		}
	}
	return out, nil
}

// CorpusStats 는 건수와 필드별 평균 길이다. 한 질의에 한 번만 부르면 된다.
func (d *DB) CorpusStats() (CorpusStats, error) {
	stats := CorpusStats{AvgLen: [4]float64{1, 1, 1, 1}}
	title, meta, sum, body := 0.0, 0.0, 0.0, 0.0
	err := d.sql.QueryRow(`SELECT COUNT(*), COALESCE(AVG(n_title), 0), COALESCE(AVG(n_meta), 0),
		COALESCE(AVG(n_sum), 0), COALESCE(AVG(n_body), 0) FROM memories`).
		Scan(&stats.Docs, &title, &meta, &sum, &body)
	if err != nil {
		return stats, err
	}
	for at, value := range [4]float64{title, meta, sum, body} {
		if value > 0 {
			stats.AvgLen[at] = value
		}
	}
	return stats, nil
}

// DocFreq 는 낱말마다 몇 건에 나오는지다 (idf 의 재료). vocab_ko 는 한글
// 조각 표라서 영문 낱말도 같이 들어 있다 — fts_ko 가 둘 다 담기 때문이다.
// 색인에 없는 낱말은 아예 안 담아서 부르는 쪽이 "없는 낱말" 을 가릴 수 있다.
func (d *DB) DocFreq(terms []string) (map[string]int, error) {
	out := map[string]int{}
	if len(terms) == 0 {
		return out, nil
	}
	places := make([]string, 0, len(terms))
	args := make([]any, 0, len(terms))
	seen := map[string]bool{}
	for _, term := range terms {
		lowered := strings.ToLower(term)
		if lowered == "" || seen[lowered] {
			continue
		}
		seen[lowered] = true
		places = append(places, "?")
		args = append(args, lowered)
	}
	if len(places) == 0 {
		return out, nil
	}
	rows, err := d.sql.Query("SELECT term, doc FROM vocab_ko WHERE term IN ("+
		strings.Join(places, ",")+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		term := ""
		docs := 0
		if err := rows.Scan(&term, &docs); err != nil {
			return nil, err
		}
		out[term] = docs
	}
	return out, rows.Err()
}

// TermCounts 는 조각 한 줄에서 낱말별 셈을 낸다. 재순위가 tf 를 셀 때 쓴다.
func TermCounts(indexed string) map[string]int {
	counts := map[string]int{}
	for _, piece := range strings.Fields(indexed) {
		counts[piece]++
	}
	return counts
}

// BM25 는 우리 손으로 매기는 필드 가중 bm25 다. FTS5 것과 달리 k1·b 를
// 고를 수 있다 (설계 결정 18 — k1=1.2, b=0.4).
// 값이 클수록 좋다. FTS5 bm25 와 부호가 반대인 것에 주의한다.
func BM25(row RerankRow, terms []string, freq map[string]int, stats CorpusStats,
	weights [4]float64, k1, b float64) float64 {
	fields := [4]map[string]int{
		TermCounts(row.TitleKO), TermCounts(row.MetaKO),
		TermCounts(row.SummaryKO), TermCounts(row.BodyKO),
	}
	total := float64(stats.Docs)
	score := 0.0
	for _, term := range terms {
		lowered := strings.ToLower(term)
		docs := float64(freq[lowered])
		if docs <= 0 {
			continue
		}
		// 거의 모든 문서에 있는 낱말은 idf 가 0 이하로 내려간다. FTS5 bm25 와
		// 같이 아주 작은 값으로 눕혀서 순위가 뒤집히지만 않게 한다.
		idf := math.Log((total - docs + 0.5) / (docs + 0.5))
		if idf <= 0 {
			idf = 1e-6
		}
		for at := 0; at < 4; at++ {
			tf := float64(fields[at][lowered])
			if tf == 0 {
				continue
			}
			length := float64(row.Lens[at])
			norm := 1 - b + b*length/stats.AvgLen[at]
			score += weights[at] * idf * tf * (k1 + 1) / (tf + k1*norm)
		}
	}
	return score
}
