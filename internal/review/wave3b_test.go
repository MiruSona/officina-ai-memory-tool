package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// TestColdCountsIDsNotFindings 는 차가움 큐의 건수가 **기억 수**인지 본다.
// C09(아무도 안 봤다)와 C10(아무도 안 가리킨다)이 같은 기억에 같이 걸려서
// 206건 저장소가 337건으로 찍혔다 (N4 · 조사 E).
func TestColdCountsIDsNotFindings(t *testing.T) {
	opened := newRepo(t)
	ids := []string{"20260105-cccc0001", "20260105-cccc0002", "20260105-cccc0003"}
	for _, id := range ids {
		put(t, opened, id, "decision", "", "")
	}
	// 조회 기록이 있어야 C09 를 본다. 셋 다 오래전에 한 번 읽혔다.
	hits := map[string]time.Time{}
	for _, id := range ids {
		hits[id] = now.AddDate(-1, 0, 0)
	}
	report := runOn(t, opened, Options{Hits: hits, Kinds: []string{KindCold}})
	if report.Counts[KindCold] != len(ids) {
		t.Fatalf("차가움 건수가 기억 수와 다르다 : %d (기억 %d건)", report.Counts[KindCold], len(ids))
	}
	if report.Counts[KindCold] > len(report.Items) {
		t.Fatalf("건수(%d)가 줄 수(%d)보다 크다", report.Counts[KindCold], len(report.Items))
	}
}

// dirtySource 는 블록을 닫고 태그를 여는 근거 한 줄이다.
const dirtySource = "file:```<script>x"

// putDirty 는 근거 칸에 못 믿을 글이 든 기억을 놓는다. put 은 sources 를 이미
// 채워서 extra 로 덮으면 YAML 키가 겹친다.
func putDirty(t *testing.T, opened *store.Store, id string) {
	t.Helper()
	text := "---\n" +
		"id: " + id + "\n" +
		"type: decision\n" +
		"title: 훅 주입 상한을 바이트로 잰다\n" +
		"summary: 훅이 밀어 넣는 글은 글자 수가 아니라 UTF-8 바이트로 재야 한다. 한글은 한 글자가 세 바이트다\n" +
		"tags: [hook, korean]\n" +
		"scope: aimemorytool\n" +
		"date: 2026-01-05\n" +
		"author: human:mirusona\n" +
		"sources: [\"" + dirtySource + "\"]\n" +
		"---\n\n본문 한 줄이면 얇다는 경고가 나지만 이 시험은 근거만 본다.\n"
	dir := filepath.Join(opened.StoreDir(), "2026", "01")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestReviewSanitizesWhyAndRelated 는 못 믿을 글이 Why·Related 로 새는지 본다.
// review 출력은 백틱 안에 찍히고 AI 가 자주 읽는다 (불변조건 I3 · 결정 59).
func TestReviewSanitizesWhyAndRelated(t *testing.T) {
	opened := newRepo(t)
	putDirty(t, opened, "20260105-dddd0001")
	report := runOn(t, opened, Options{
		SourceChanged: func(_ *model.Memory, _ string) bool { return true },
		Kinds:         []string{KindStale},
	})
	if len(report.Items) == 0 {
		t.Fatal("낡음 큐가 비었다")
	}
	found := false
	for _, item := range report.Items {
		if item.Rule == quality.RuleStaleSourceChanged {
			found = true
		}
		if strings.Contains(item.Why, "```") || strings.Contains(item.Why, "<") {
			t.Fatalf("Why 가 안 씻겼다 : %q", item.Why)
		}
		for _, related := range item.Related {
			if strings.Contains(related, "```") || strings.Contains(related, "<") {
				t.Fatalf("Related 가 안 씻겼다 : %q", related)
			}
		}
	}
	if !found {
		t.Fatal("근거가 바뀐 기억이 큐에 없다")
	}
	if text := Markdown(report); strings.Contains(text, "<script>") {
		t.Fatal("화면에 태그가 그대로 나갔다")
	}
}

// TestNewRulesReachQueue 는 2D·2B 가 더한 규칙이 검토 큐에 뜨는지 본다.
// ruleKind 표에 없으면 한 건도 안 뜬다 (2D 넘김).
func TestNewRulesReachQueue(t *testing.T) {
	want := map[string]string{
		quality.RuleStaleAge:          KindStale,
		quality.RuleStaleConflictPair: KindStale,
		quality.RuleNotationDrift:     KindStale,
		quality.RuleLinkMissing:       KindLink,
		quality.RuleNoValue:           KindValue,
		quality.RuleDeadPath:          KindStale,
		quality.RuleDeadCommit:        KindStale,
	}
	for rule, kind := range want {
		if ruleKind[rule] != kind {
			t.Errorf("%s 가 %s 큐로 안 간다 (지금 %q)", rule, kind, ruleKind[rule])
		}
	}
	for _, kind := range []string{KindLink, KindValue} {
		if !KnownKind(kind) {
			t.Errorf("--kind %s 를 안 받는다", kind)
		}
	}
}
