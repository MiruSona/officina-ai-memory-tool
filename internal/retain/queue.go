package retain

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// 큐 항목 처리 결과 (A2 설계 2절 `result` 칸).
const (
	ResultSaved        = "saved"
	ResultNothing      = "nothing"
	ResultRejected     = "rejected"
	ResultNoTranscript = "no-transcript"
	// ResultError 는 생성 요청이 그 세션 때문에 거절된 것이다 (400 문맥 초과 등). 다시 안 돈다.
	ResultError = "error"
)

// QueueItem 은 큐 파일 하나와 그 안의 표다. 표시·prune 이 Path 를 쓴다.
type QueueItem struct {
	Path  string
	Entry QueueEntry
}

// ReadQueue 는 Memory 폴더 dir 의 큐 표를 다 읽는다. 폴더가 없으면 빈 목록이고,
// 깨진 파일은 건너뛴다 — 훅이 쓰다 만 파일 하나로 retain 이 멈추면 안 된다.
func ReadQueue(dir string) ([]QueueItem, error) {
	paths, err := filepath.Glob(filepath.Join(QueueDir(dir), "*.json"))
	if err != nil {
		return nil, err
	}
	items := []QueueItem{}
	for _, path := range paths {
		entry, err := readEntry(path)
		if err != nil {
			continue
		}
		items = append(items, QueueItem{Path: path, Entry: entry})
	}
	return items, nil
}

func readEntry(path string) (QueueEntry, error) {
	entry := QueueEntry{}
	data, err := os.ReadFile(path)
	if err != nil {
		return entry, err
	}
	err = json.Unmarshal(data, &entry)
	return entry, err
}

// Pick 은 이번 판에 처리할 항목을 고른다 (A2 설계 1절 ③) : covered 아님 · done_at 없음 ·
// transcript 파일 있음 · queue_days 안. transcript 가 며칠 뒤 사라지므로 오래된 것부터
// max 건이다. max 가 0 이하면 다 준다.
func Pick(items []QueueItem, now time.Time, queueDays, max int) []QueueItem {
	picked := []QueueItem{}
	for _, item := range items {
		entry := item.Entry
		if entry.Covered || entry.DoneAt != "" || !transcriptExists(entry.TranscriptPath) {
			continue
		}
		if olderThan(entry.At, now, queueDays) {
			continue
		}
		picked = append(picked, item)
	}
	sort.SliceStable(picked, func(a, b int) bool {
		first, _ := time.Parse(time.RFC3339, picked[a].Entry.At)
		second, _ := time.Parse(time.RFC3339, picked[b].Entry.At)
		return first.Before(second)
	})
	if max > 0 && len(picked) > max {
		picked = picked[:max]
	}
	return picked
}

// MarkDone 은 큐 표에 처리 결과를 더해 원자 쓰기한다. 다른 칸은 그대로 둔다. pickedAt 은 고를 때
// 읽은 At 이다 — 처리하는 사이 훅이 같은 세션을 다시 적었으면(At 이 다름) 새 글이 생긴 것이라
// 표시하지 않고 marked 거짓을 준다. 다음 판에 다시 처리한다.
func MarkDone(path, pickedAt, result string, saved []string, now time.Time) (marked bool, err error) {
	entry, err := readEntry(path)
	if err != nil {
		return false, err
	}
	if entry.At != pickedAt {
		return false, nil
	}
	entry.DoneAt = now.Format(time.RFC3339)
	entry.Result = result
	entry.Saved = saved
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return false, err
	}
	return true, writeAtomic(path, data)
}

// PrunePlan 은 `mem retain --prune` 이 치울 항목이다 (A2 설계 2절) : transcript 파일이
// 없는 것 · 쌓인 지 queue_days 지난 것 · 처리한 지 queue_days 지난 것.
func PrunePlan(items []QueueItem, now time.Time, queueDays int) []QueueItem {
	plan := []QueueItem{}
	for _, item := range items {
		entry := item.Entry
		gone := !transcriptExists(entry.TranscriptPath)
		stale := olderThan(entry.At, now, queueDays)
		doneLong := entry.DoneAt != "" && olderThan(entry.DoneAt, now, queueDays)
		if gone || stale || doneLong {
			plan = append(plan, item)
		}
	}
	return plan
}

// Prune 은 큐 파일을 지운다. 이미 없는 파일은 넘어가고, 나머지 오류는 모아 준다.
func Prune(paths []string) error {
	problems := []error{}
	for _, path := range paths {
		err := os.Remove(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}

// FindQueueTranscript 는 세션 앞 8자로 큐 표의 transcript_path 를 찾는다. 관문이
// retain-state.json 에서 세션을 못 찾았을 때 쓰는 대체 길이다 (A2 설계 5절).
func FindQueueTranscript(dir, short string) (string, bool) {
	if len(short) != ShortLen || !sessionPattern.MatchString(short) {
		return "", false
	}
	entry, err := readEntry(filepath.Join(QueueDir(dir), short+".json"))
	if err != nil || entry.TranscriptPath == "" {
		return "", false
	}
	return entry.TranscriptPath, true
}

func transcriptExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// olderThan 은 at(RFC3339)이 now 보다 days 일 넘게 앞인지다. 못 읽는 때는 오래된 것으로
// 친다. days 가 0 이하면 기한을 끈 것이다.
func olderThan(at string, now time.Time, days int) bool {
	if days <= 0 {
		return false
	}
	when, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return true
	}
	return when.Before(now.AddDate(0, 0, -days))
}
