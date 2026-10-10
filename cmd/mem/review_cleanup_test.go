package main

// mem review 의 점검·정리 출력 꼴 — --scope · --table · --ids · --all · --json 의 scope 칸
// (점검·정리 설계 6절).

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// putExpired 는 다시 볼 날이 지난 기억 하나를 놓는다 (C07 → expired 큐).
func putExpired(t *testing.T, memory, id, scope string) {
	t.Helper()
	one := basisMemory(id, model.TypeDecision, "다시 볼 날이 지난 결정 "+id)
	one.Scope = scope
	one.StaleAfter = "2026-01-01"
	putMemory(t, memory, one)
}

// --scope 는 그 scope 의 기억만 큐에 담는다.
func TestReviewScopeFilters(t *testing.T) {
	memory := newRepo(t)
	putExpired(t, memory, "20260822-aaaa0001", "aimemorytool")
	putExpired(t, memory, "20260822-aaaa0002", "mem-search")
	out := mustRun(t, "review", "--kind", "expired", "--scope", "mem-search")
	if !strings.Contains(out, "20260822-aaaa0002") || strings.Contains(out, "20260822-aaaa0001") {
		t.Fatalf("--scope 가 거르지 않았다 : %s", out)
	}
}

// --table 은 갈래 머리 아래 scope 머리와 표 머리를 찍는다.
func TestReviewTableHeads(t *testing.T) {
	memory := newRepo(t)
	putExpired(t, memory, "20260822-aaaa0001", "aimemorytool")
	putExpired(t, memory, "20260822-aaaa0002", "mem-search")
	putExpired(t, memory, "20260822-aaaa0003", "mem-search")
	out := mustRun(t, "review", "--kind", "expired", "--table")
	for _, want := range []string{"## 다시 볼 날이 지난 것 (3건)", "### aimemorytool (1건)", "### mem-search (2건)",
		"| 날짜 | id | 제목 | 까닭 | 같이 볼 것 | 다음 |", "| 2026-08-22 | 20260822-aaaa0003 |"} {
		if !strings.Contains(out, want) {
			t.Fatalf("`%s` 가 없다 : %s", want, out)
		}
	}
	if strings.Index(out, "### aimemorytool") > strings.Index(out, "### mem-search") {
		t.Fatalf("scope 가 이름 차례가 아니다 : %s", out)
	}
}

// --ids 는 한 줄에 「갈래 TAB 규칙 TAB id TAB 같이 볼 것」이고 머리말이 없다.
func TestReviewIDsLines(t *testing.T) {
	memory := newRepo(t)
	putExpired(t, memory, "20260822-aaaa0001", "aimemorytool")
	out := mustRun(t, "review", "--kind", "expired", "--ids")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("한 줄이어야 한다 : %q", out)
	}
	fields := strings.Split(lines[0], "\t")
	if len(fields) != 4 || fields[0] != "expired" || fields[2] != "20260822-aaaa0001" || fields[1] == "" {
		t.Fatalf("줄 꼴이 다르다 : %q", lines[0])
	}
}

// 상한을 넘으면 기본은 자르고 「못 본 것」에 적는다. --all 은 다 준다.
func TestReviewAllLiftsLimit(t *testing.T) {
	memory := newRepo(t)
	for at := 0; at < 25; at++ {
		putExpired(t, memory, fmt.Sprintf("20260822-aaaa%04d", at), "aimemorytool")
	}
	cut := mustRun(t, "review", "--kind", "expired", "--ids")
	if got := strings.Count(cut, "\n"); got != 20 {
		t.Fatalf("기본 상한은 20줄이다 : %d", got)
	}
	all := mustRun(t, "review", "--kind", "expired", "--ids", "--all")
	if got := strings.Count(all, "\n"); got != 25 {
		t.Fatalf("--all 이면 25줄이다 : %d", got)
	}
	table := mustRun(t, "review", "--kind", "expired", "--table", "--limit", "5")
	if !strings.Contains(table, "25줄 중 5줄만") {
		t.Fatalf("(갈래, scope) 상한 넘침을 안 알린다 : %s", table)
	}
}

// --json 은 줄마다 scope 를, 전체에 by_scope 를 준다.
func TestReviewJSONScope(t *testing.T) {
	memory := newRepo(t)
	putExpired(t, memory, "20260822-aaaa0001", "mem-search")
	out := mustRun(t, "review", "--kind", "expired", "--json")
	var report struct {
		Items []struct {
			Scope string `json:"scope"`
		} `json:"items"`
		ByScope map[string]map[string]int `json:"by_scope"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Items) != 1 || report.Items[0].Scope != "mem-search" || report.ByScope["mem-search"]["expired"] != 1 {
		t.Fatalf("scope 칸이 없다 : %s", out)
	}
}

// --nli 를 줬는데 nli_url 이 없으면 규칙 층만 보고 「못 본 것」에 적는다. 멈추지 않는다.
func TestReviewNLIWithoutServer(t *testing.T) {
	memory := newRepo(t)
	t.Setenv("MEM_LLM_CONFIG", memory+"/없는-llm.toml")
	putExpired(t, memory, "20260822-aaaa0001", "mem-search")
	out := mustRun(t, "review", "--kind", "contradict", "--nli", "--nli-pairs", "3")
	if !strings.Contains(out, "검토 큐") || !strings.Contains(out, "NLI 를 못 써서 규칙 층만 봤다") {
		t.Fatalf("규칙 층만 봤다는 줄이 없다 : %s", out)
	}
	if _, code := capture(t, func() int { return run([]string{"review", "--nli-pairs", "영"}) }); code != exitUsage {
		t.Fatalf("--nli-pairs 가 수가 아니면 사용법 잘못이다 : %d", code)
	}
}

// --nli-pairs 는 1 이상이어야 한다. 0·음수는 기본값으로 몰래 바꾸지 않고 사용법 잘못이다.
func TestReviewNLIPairsMustBePositive(t *testing.T) {
	newRepo(t)
	for _, bad := range []string{"0", "-3"} {
		if _, code := capture(t, func() int { return run([]string{"review", "--nli-pairs", bad}) }); code != exitUsage {
			t.Fatalf("--nli-pairs %s 는 사용법 잘못이다 : %d", bad, code)
		}
	}
}

// 없는 scope 를 주면 큐가 비고, 「못 본 것」에 그 이름을 적는다.
func TestReviewUnknownScopeIsNoted(t *testing.T) {
	memory := newRepo(t)
	putExpired(t, memory, "20260822-aaaa0001", "mem-search")
	out := mustRun(t, "review", "--kind", "expired", "--scope", "mem-serch")
	if strings.Contains(out, "20260822-aaaa0001") || !strings.Contains(out, "mem-serch") || !strings.Contains(out, "인 기억이 없다") {
		t.Fatalf("없는 scope 를 안 알린다 : %s", out)
	}
}

// --limit 과 --all 을 같이 주면 --all 이 이긴다 — 자르지 않는다.
func TestReviewAllBeatsLimit(t *testing.T) {
	memory := newRepo(t)
	for at := 0; at < 4; at++ {
		putExpired(t, memory, fmt.Sprintf("20260822-aaaa%04d", at), "aimemorytool")
	}
	out := mustRun(t, "review", "--kind", "expired", "--ids", "--limit", "2", "--all")
	if got := strings.Count(out, "\n"); got != 4 {
		t.Fatalf("--all 이 --limit 을 이겨야 한다 : %d줄", got)
	}
}
