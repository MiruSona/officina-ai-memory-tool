package main

// `mem auto report` 와 R3 건너뜀 경고 기록 (자동쌓기A1후속 2절 · 7절).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/retain"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

func writeJSONFile(t *testing.T, dir, name string, payload any) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(payload)
	if payload == nil {
		data = []byte("{깨짐")
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAutoReportCountsAndFilters(t *testing.T) {
	memory := autoRepo(t)
	if _, code := capture(t, func() int { return run(autoArgs("본문 한 줄")) }); code != exitOK {
		t.Fatal("자동 add 가 안 들어갔다")
	}
	now := time.Now()
	today, at, old := now.Format("2006-01-02"), now.Format(time.RFC3339), "2020-01-01T09:00:00Z"
	state := retain.LoadState(memory)
	state.Sessions["aaaaaaaa-0000-0000-0000-000000000001"] = &retain.Session{Seen: at, Nudges: 3, AutoAdds: 1}
	state.Sessions["aaaaaaaa-0000-0000-0000-000000000002"] = &retain.Session{Seen: at, Nudges: 2}
	if err := retain.SaveState(memory, state, now); err != nil {
		t.Fatal(err)
	}
	logText := "\n## 2020-01-01\n- 거절 자동 · stop · R1-quote · 옛것\n- 경고 자동 · stop · " + i18n.T(i18n.AutoJudgeSkipped) +
		"\n## " + today + "\n- 거절 자동 · stop · R2-fragment · 가\n- 거절 자동 · stop · R3-support · 나\n" +
		"- 경고 자동 · stop · " + i18n.T(i18n.AutoJudgeSkipped) + "\n- 경고 자동 · stop · 다른 알림\n"
	file, _ := os.OpenFile(store.LogPath(memory), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	file.WriteString(logText)
	file.Close()
	queue := retain.QueueDir(memory)
	writeJSONFile(t, queue, "a.json", retain.QueueEntry{Reason: "compact", At: at})
	writeJSONFile(t, queue, "b.json", retain.QueueEntry{Reason: "end", At: at, Covered: true})
	writeJSONFile(t, queue, "c.json", retain.QueueEntry{Reason: "end", At: old})
	writeJSONFile(t, queue, "broken.json", nil)
	undo := undoDir(memory)
	writeJSONFile(t, undo, "1.json", undoRecord{At: at, IDs: []string{"X1", "X2"}, RedoneIDs: []string{"X2"}})
	writeJSONFile(t, undo, "2.json", undoRecord{At: old, IDs: []string{"X3"}})
	writeJSONFile(t, undo, "3.json", undoRecord{At: at, IDs: []string{"X4"}, Redone: at})
	writeJSONFile(t, undo, "broken.json", nil)

	out, code := capture(t, func() int { return run([]string{"auto", "report", "--since", today, "--json", "--seed", "7"}) })
	if code != exitOK {
		t.Fatalf("종료 코드 %d : %s", code, out)
	}
	got := autoReportJSON{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json 을 못 읽었다 : %v\n%s", err, out)
	}
	want := map[string]any{"nudges": 5, "sessions": 2, "adds": 1, "r3": 1, "skipped": 1, "undone": 1, "auto": 2,
		"queued": 2, "missed": 1}
	have := map[string]any{"nudges": got.Nudges, "sessions": got.NudgeSession, "adds": got.AddSessions,
		"r3": got.R3Rejects, "skipped": got.R3Skipped, "undone": got.Undone, "auto": got.AutoCount,
		"queued": got.Queued, "missed": got.Missed}
	if !reflect.DeepEqual(want, have) {
		t.Errorf("숫자가 다르다 : %v", have)
	}
	if !reflect.DeepEqual(got.Rejects, map[string]int{"R2-fragment": 1, "R3-support": 1}) || got.MissedBy["compact"] != 1 {
		t.Errorf("규칙별·사유별이 다르다 : %v %v", got.Rejects, got.MissedBy)
	}
	if got.WarnSince != "2020-01-01" || got.Seed != 7 || len(got.Sample) != 1 || got.UndoRate == nil {
		t.Errorf("경고 시작·씨앗·표본·비율이 다르다 : %+v", got)
	}
	for _, key := range []string{`"since"`, `"auto_add_rate"`, `"undo_rate"`, `"missed_by_reason"`, `"sample"`} {
		if !strings.Contains(out, key) {
			t.Errorf("json 키 %s 가 없다", key)
		}
	}
}

func TestAutoReportEmptyRepo(t *testing.T) {
	newRepo(t)
	out, code := capture(t, func() int { return run([]string{"auto", "report", "--seed", "1"}) })
	if code != exitOK || !strings.Contains(out, "알림 0회") || !strings.Contains(out, "= —") {
		t.Fatalf("빈 저장소에서 0 과 — 를 찍어야 한다 : %d %s", code, out)
	}
	if _, code := capture(t, func() int { return run([]string{"auto", "report", "--sample", "x"}) }); code != exitUsage {
		t.Fatal("--sample 이 수가 아닌데 받았다")
	}
}

func TestAutoReportSampleSeed(t *testing.T) {
	pool := []*model.Memory{}
	for index := 0; index < 50; index++ {
		pool = append(pool, &model.Memory{ID: fmt.Sprintf("m%02d", index), Origin: "stop", Title: "제목\n둘째"})
	}
	first, again, other := reportSample(pool, 10, 1), reportSample(pool, 10, 1), reportSample(pool, 10, 2)
	if !reflect.DeepEqual(first, again) || reflect.DeepEqual(first, other) || len(first) != 10 {
		t.Fatal("같은 씨앗은 같고 다른 씨앗은 달라야 한다")
	}
	if strings.Contains(first[0].Title, "\n") {
		t.Fatal("제목이 한 줄로 안 눌렸다")
	}
	if len(reportSample(pool, 100, 1)) != 50 {
		t.Fatal("--sample 이 건수보다 크면 전부다")
	}
}

func TestNoteWarningsLogsOneLine(t *testing.T) {
	memory := newRepo(t)
	auto := autoAdd{Origin: "stop", Result: retain.Result{Warnings: []string{i18n.T(i18n.AutoJudgeSkipped) + "\n둘째 줄"}}}
	noteWarnings(store.Open(memory, false), auto)
	raw, _ := os.ReadFile(store.LogPath(memory))
	if !strings.Contains(string(raw), "- 경고 자동 · stop · "+i18n.T(i18n.AutoJudgeSkipped)+" 둘째 줄\n") {
		t.Fatalf("경고 줄이 한 줄로 안 남았다 :\n%s", raw)
	}
	added, rejected, _ := logCounts(store.Open(memory, false))
	if added != 0 || rejected != 0 {
		t.Fatal("status 가 경고를 들어옴·거절로 셌다")
	}
}
