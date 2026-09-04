package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

var now = time.Date(2026, 8, 23, 12, 0, 0, 0, time.Local)

func newRepo(t *testing.T) *store.Store {
	t.Helper()
	opened := store.Open(filepath.Join(t.TempDir(), "Memory"), false)
	if err := opened.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	return opened
}

// put 은 기억 하나를 store/ 에 놓는다. 규격은 다 지키고, 시험이 보려는 칸만
// extra 로 바꾼다.
func put(t *testing.T, opened *store.Store, id, kind, extra, body string) {
	t.Helper()
	text := "---\nid: " + id + "\ntype: " + kind + "\n" +
		"title: 훅 주입 상한을 바이트로 잰다\n" +
		"summary: 훅이 밀어 넣는 글은 글자 수가 아니라 UTF-8 바이트로 재야 한다. 한글은 한 글자가 세 바이트다\n" +
		"tags: [hook, korean]\nscope: aimemorytool\ndate: 2026-01-05\nauthor: human:mirusona\n" +
		"sources: [\"file:internal/hook/hook.go\"]\n" + extra + "---\n\n"
	if body == "" {
		body = "훅 상한은 바이트로 잰다. 글자 수로 재면 한글에서 세 배로 틀린다.\n넘치면 우리가 먼저 자른다.\n말없이 사라지는 것이 제일 나쁘다.\n"
	}
	dir := filepath.Join(opened.StoreDir(), "2026", "01")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(text+body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runOn(t *testing.T, opened *store.Store, options Options) *Report {
	t.Helper()
	options.Store = opened
	options.Config = config.Default("aimemorytool")
	options.Now = now
	report, err := Run(options)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func kinds(report *Report) map[string]int {
	found := map[string]int{}
	for _, item := range report.Items {
		found[item.Kind]++
	}
	return found
}

// TestExpiredGoesToQueue 는 다시 볼 날이 지난 기억이 큐에 오르는지 본다 (C07).
func TestExpiredGoesToQueue(t *testing.T) {
	opened := newRepo(t)
	put(t, opened, "20260105-aaaa0001", "decision", "stale_after: 2026-03-01\n", "")
	report := runOn(t, opened, Options{})
	if kinds(report)[KindExpired] != 1 {
		t.Fatalf("만료가 큐에 없다 : %+v", report.Items)
	}
	if len(report.Items[0].Next) == 0 {
		t.Fatal("다음에 칠 명령을 안 줬다")
	}
}

// TestConflictNeedsTwoLiveDecisions 는 같은 자리에 살아 있는 결정 둘을 사람에게
// 보내는지 본다 (C05). **판정은 안 한다.**
func TestConflictNeedsTwoLiveDecisions(t *testing.T) {
	opened := newRepo(t)
	put(t, opened, "20260105-bbbb0001", "decision", "", "")
	put(t, opened, "20260105-bbbb0002", "decision", "", "훅 상한을 8,000 바이트로 다시 정했다.\n앞의 결정과 값이 다르다.\n어느 쪽이 맞는지는 사람이 정한다.\n")
	report := runOn(t, opened, Options{})
	if kinds(report)[KindConflict] == 0 {
		t.Fatalf("모순 후보가 없다 : %+v", report.Items)
	}
	for _, item := range report.Items {
		if item.Kind == KindConflict && len(item.Related) == 0 {
			t.Fatal("어느 기억과 부딪히는지 안 알려줬다")
		}
	}
}

// TestKindFilters 는 --kind 가 그 큐만 남기는지 본다.
func TestKindFilters(t *testing.T) {
	opened := newRepo(t)
	put(t, opened, "20260105-cccc0001", "decision", "stale_after: 2026-03-01\n", "")
	put(t, opened, "20260105-cccc0002", "decision", "", "")
	report := runOn(t, opened, Options{Kinds: []string{KindExpired}})
	for _, item := range report.Items {
		if item.Kind != KindExpired {
			t.Fatalf("--kind expired 인데 %s 가 나왔다", item.Kind)
		}
	}
	if len(report.Items) == 0 {
		t.Fatal("expired 가 하나도 없다")
	}
}

// TestColdNeedsHitLog 는 조회 기록이 없으면 차가움을 안 말하는지 본다. 기록도
// 없는데 「아무도 안 본다」고 하면 저장소가 통째로 경고가 된다.
func TestColdNeedsHitLog(t *testing.T) {
	opened := newRepo(t)
	put(t, opened, "20260105-dddd0001", "history", "", "")
	report := runOn(t, opened, Options{})
	if kinds(report)[KindCold] != 0 {
		t.Fatalf("조회 기록이 없는데 차가움을 말했다 : %+v", report.Items)
	}
	if !strings.Contains(strings.Join(report.Notes, "\n"), "조회 기록") {
		t.Fatalf("안 본 것을 안 봤다고 안 적었다 : %v", report.Notes)
	}
	old := map[string]time.Time{"20260105-dddd0001": now.AddDate(0, 0, -400)}
	warm := runOn(t, opened, Options{Hits: old})
	if kinds(warm)[KindCold] == 0 {
		t.Fatalf("400일 안 읽힌 기억이 차가움에 안 들었다 : %+v", warm.Items)
	}
}

// TestSourceChangedGoesToStale 는 근거가 그 뒤에 바뀐 기억이 낡음 큐로 가는지
// 본다 (C06). git 은 부르는 쪽이 붙여 준다.
func TestSourceChangedGoesToStale(t *testing.T) {
	opened := newRepo(t)
	put(t, opened, "20260105-eeee0001", "decision", "", "")
	changed := func(m *model.Memory, source string) bool { return true }
	report := runOn(t, opened, Options{SourceChanged: changed})
	if kinds(report)[KindStale] == 0 {
		t.Fatalf("근거가 바뀌었는데 낡음 큐가 비었다 : %+v", report.Items)
	}
}

// TestMarkdownShowsNextCommand 는 화면이 다음 명령을 손에 쥐여 주는지 본다.
func TestMarkdownShowsNextCommand(t *testing.T) {
	opened := newRepo(t)
	put(t, opened, "20260105-ffff0001", "decision", "stale_after: 2026-03-01\n", "")
	text := Markdown(runOn(t, opened, Options{}))
	if !strings.Contains(text, "mem set 20260105-ffff0001") {
		t.Fatalf("다음 명령이 화면에 없다 :\n%s", text)
	}
	if !strings.Contains(text, "판정은 사람이 한다") {
		t.Fatal("판정을 안 한다는 말이 화면에 없다")
	}
}

// TestEmptyQueueSaysSo 는 볼 것이 없으면 그렇다고 말하는지 본다.
// 종류가 decision 인 것은 나이 임계(C12)가 1440일이라서다. history 는 180일이라
// 이 시험 자료(2026-01-05)가 230일 지난 것으로 잡힌다 — 시험이 재려는 것은
// 「빈 큐일 때의 말」이지 나이 규칙이 아니다.
func TestEmptyQueueSaysSo(t *testing.T) {
	opened := newRepo(t)
	put(t, opened, "20260105-0000ffff", "decision", "", "")
	text := Markdown(runOn(t, opened, Options{}))
	if !strings.Contains(text, "볼 것이 없다") {
		t.Fatalf("빈 큐인데 그 말을 안 했다 :\n%s", text)
	}
}

// TestQueuesAreGroupedInOrder 는 급한 것부터 큐가 놓이는지 본다.
func TestQueuesAreGroupedInOrder(t *testing.T) {
	opened := newRepo(t)
	put(t, opened, "20260105-1111aaaa", "decision", "stale_after: 2026-03-01\n", "")
	put(t, opened, "20260105-2222bbbb", "decision", "", "훅 상한을 8,000 바이트로 다시 정했다.\n앞의 결정과 값이 다르다.\n어느 쪽이 맞는지는 사람이 정한다.\n")
	changed := func(m *model.Memory, source string) bool { return m.ID == "20260105-2222bbbb" }
	report := runOn(t, opened, Options{SourceChanged: changed,
		Hits: map[string]time.Time{"20260105-1111aaaa": now.AddDate(0, 0, -400)}})
	seen := []string{}
	for _, item := range report.Items {
		if len(seen) == 0 || seen[len(seen)-1] != item.Kind {
			seen = append(seen, item.Kind)
		}
	}
	want := []string{KindExpired, KindConflict, KindStale, KindCold}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Fatalf("큐 차례가 %v 다. %v 라야 한다", seen, want)
	}
}
