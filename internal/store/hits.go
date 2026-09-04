package store

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// dirLocal holds everything that must stay out of git: it is per machine and
// per person, so committing it would fight on every pull (design 검토 #5).
const dirLocal = "local"

// HitsFileName is the append only log of "an AI actually read this memory".
const HitsFileName = "hits.jsonl"

// HitsCompactLines is how long the log may grow before it is squeezed down to
// one line per memory.
const HitsCompactLines = 5000

// LocalDir is the git ignored folder of one repository.
func LocalDir(dir string) string {
	return filepath.Join(dir, dirLocal)
}

// LocalDir is the same folder for an opened store.
func (s *Store) LocalDir() string {
	return LocalDir(s.Dir)
}

// HitsPath is local/hits.jsonl of one repository.
func HitsPath(dir string) string {
	return filepath.Join(LocalDir(dir), HitsFileName)
}

// hitLine is one record. Count is left out when it is 1, which is the common case.
// Cmd 는 어느 명령이 읽었는지다 — 어떤 명령이 실제로 쓰이는지 셀 자료가 없어서
// 다음 판이 명령을 못 깎았다 (설계 18-3).
type hitLine struct {
	Cmd   string `json:"cmd,omitempty"`
	ID    string `json:"id"`
	At    int64  `json:"at"`
	Count int    `json:"n,omitempty"`
}

// HitTotal is what the index writes back into one memory row.
type HitTotal struct {
	Count  int
	LastAt int64
}

// AppendHit records one read. It is best effort: a memory that could not be
// counted is still a memory, so callers ignore the error.
func AppendHit(dir, command, id string) error {
	return appendHits(dir, []hitLine{{Cmd: command, ID: id, At: time.Now().Unix()}})
}

// AppendHit 은 열린 저장소에 대고 세는 쪽이다. 읽기 전용 저장소에는 한 글자도
// 안 쓴다 (리뷰 A #15).
func (s *Store) AppendHit(command, id string) error {
	if err := s.writable(); err != nil {
		return err
	}
	return AppendHit(s.Dir, command, id)
}

// CommandCounts 는 명령별로 몇 번 읽혔는지다. mem status 의 `사용 :` 줄이 쓴다.
func CommandCounts(dir string) (map[string]int, error) {
	file, err := os.Open(HitsPath(dir))
	if os.IsNotExist(err) {
		return map[string]int{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	counts := map[string]int{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := hitLine{}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil || line.Cmd == "" {
			continue
		}
		counts[line.Cmd] += max(line.Count, 1)
	}
	return counts, nil
}

func appendHits(dir string, lines []hitLine) error {
	if dir == "" || len(lines) == 0 {
		return nil
	}
	if err := os.MkdirAll(LocalDir(dir), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(HitsPath(dir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	for _, line := range lines {
		data, err := json.Marshal(line)
		if err != nil {
			return err
		}
		if _, err := file.Write(append(data, '\n')); err != nil {
			return err
		}
	}
	return nil
}

// ReadHits sums the log per memory id and also says how many lines it read, so
// the caller can decide to squeeze the file. A line it cannot read is skipped:
// a torn tail must not throw away the counts in front of it.
func ReadHits(dir string) (map[string]HitTotal, int, error) {
	file, err := os.Open(HitsPath(dir))
	if os.IsNotExist(err) {
		return map[string]HitTotal{}, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	totals := map[string]HitTotal{}
	count := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		text := scanner.Bytes()
		if len(text) == 0 {
			continue
		}
		line := hitLine{}
		if err := json.Unmarshal(text, &line); err != nil || line.ID == "" {
			// 명령 통계 줄(id 가 빈 줄)은 압축 문턱에 안 센다. 세면 기억
			// 수보다 먼저 문턱이 차서 필요 없는 압축이 돈다 (리뷰 A #20).
			continue
		}
		count++
		if line.Count <= 0 {
			line.Count = 1
		}
		total := totals[line.ID]
		total.Count += line.Count
		if line.At > total.LastAt {
			total.LastAt = line.At
		}
		totals[line.ID] = total
	}
	if err := scanner.Err(); err != nil {
		return totals, count, nil
	}
	return totals, count, nil
}

// CompactHits rewrites the log as one line per memory. The sums are the same,
// so the index reads back exactly what it read before.
func CompactHits(dir string, totals map[string]HitTotal) error {
	ids := make([]string, 0, len(totals))
	for id := range totals {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	lines := make([]hitLine, 0, len(ids))
	for _, id := range ids {
		lines = append(lines, hitLine{ID: id, At: totals[id].LastAt, Count: totals[id].Count})
	}
	if err := os.MkdirAll(LocalDir(dir), 0o755); err != nil {
		return err
	}
	tmp := HitsPath(dir) + ".tmp"
	if err := os.Remove(tmp); err != nil && !os.IsNotExist(err) {
		return err
	}
	data := []byte{}
	for _, line := range lines {
		encoded, err := json.Marshal(line)
		if err != nil {
			return err
		}
		data = append(data, encoded...)
		data = append(data, '\n')
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return renameRetry(tmp, HitsPath(dir))
}
