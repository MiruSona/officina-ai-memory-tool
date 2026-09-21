package main

// 2026-09-21 코드 리뷰에서 나온 자리들 — --by 미리 검사 · 거절 가짓수 · JSONL
// 묶음 알림 · tags --list --json · 깨진 파일과 색인 대기 가르기.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// --by 가 id 꼴이 아니면 **큐에 넣기 전에** 막는다. 「저장됨」과 사용법 오류가
// 한 화면에 같이 뜨면 안 된다.
func TestAddChecksByBeforeQueueing(t *testing.T) {
	memory := newRepo(t)
	out, code := captureBoth(t, func() int {
		return run(append(addArgs("--by 를 잘못 준 본문이다"), "--by", "옛날거"))
	})
	if code != exitUsage {
		t.Fatalf("종료 코드가 1 이어야 한다 : %d (%s)", code, out)
	}
	if strings.Contains(out, "저장됨") {
		t.Fatalf("막힌 기억을 저장됐다고 말한다 : %s", out)
	}
	if queuedCount(t, memory) != 0 {
		t.Fatal("막힌 기억이 큐에 들어갔다")
	}
}

// 덮기가 실패했을 때 쓰는 끝줄은 「저장됨」과 「덮기는 실패」를 같이 말한다.
func TestQueuedNoByTellsBothSides(t *testing.T) {
	line := i18n.T(i18n.AddQueuedNoBy, "20260921-abcdabcd", "20260101-11112222")
	for _, want := range []string{"저장됨", "20260921-abcdabcd", "덮기", "실패", "20260101-11112222"} {
		if !strings.Contains(line, want) {
			t.Fatalf("`%s` 가 끝줄에 없다 : %s", want, line)
		}
	}
}

// 「거절됨 (n가지)」의 n 은 거절 등급만 센다. 경고까지 세면 고칠 가짓수를 부풀린다.
func TestRejectedNoteCountsRejectsOnly(t *testing.T) {
	newRepo(t)
	out, code := captureBoth(t, func() int {
		return run([]string{"add", "--type", "decision", "--scope", "mem-search",
			"--tags", "index,korean", "--sources", "note:그냥 메모",
			"--title", "짧", "--summary", "짧다", "--body", "한 줄"})
	})
	if code != exitCheck {
		t.Fatalf("거절이어야 한다 : %d (%s)", code, out)
	}
	want := i18n.T(i18n.AddRejectedNote, strings.Count(out, "거절 :"))
	if !strings.Contains(out, want) {
		t.Fatalf("거절 가짓수가 거절 줄 수와 다르다 : %s", out)
	}
}

// JSONL 은 줄마다 알리지 않고 묶음 끝에 몇 건을 고쳐 넣었는지 한 번만 말한다.
func TestJSONLNotesFixesOnce(t *testing.T) {
	newRepo(t)
	lines := []string{
		jsonlLine(t, map[string]any{"type": "caution", "severity": "medium", "scope": "mem-search",
			"tags": []string{"index", "korean"}, "sources": []string{"file:internal/token/token.go"},
			"title":   "두 글자 검색어를 조심한다",
			"summary": "두 글자 한글 검색어는 trigram 에 안 잡혀 0건이 되니 색인 쪽을 먼저 본다",
			"body":    jsonlBody("첫째 줄이다.")}),
		jsonlLine(t, map[string]any{"type": "todo", "scope": "mem-search",
			"tags": []string{"index", "korean"}, "sources": []string{"file:internal/token/token.go"},
			"title":   "낱말 자르기를 고친다",
			"summary": "두 글자 한글 검색어가 0건이 되는 자리를 trigram 쪽에서 고쳐야 한다",
			"body":    jsonlBody("둘째 줄이다.")}),
	}
	stdinOf(t, strings.Join(lines, "\n")+"\n")
	out, code := captureBoth(t, func() int { return run([]string{"add", "--jsonl"}) })
	if code != exitOK {
		t.Fatalf("묶음이 거절됐다 : %d (%s)", code, out)
	}
	if got := strings.Count(out, "고쳐 넣은 칸"); got != 1 {
		t.Fatalf("묶음 알림이 %d번 나왔다 : %s", got, out)
	}
	if !strings.Contains(out, "severity 별칭 1건") || !strings.Contains(out, "todo_status 기본값 1건") {
		t.Fatalf("몇 건을 고쳤는지 안 말한다 : %s", out)
	}
}

// jsonlLine 은 한 줄 JSON 하나를 만든다.
func jsonlLine(t *testing.T, fields map[string]any) string {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// tags --list --json 은 사람용 글이 아니라 한 줄 JSON 을 낸다.
func TestTagsListJSON(t *testing.T) {
	newRepo(t)
	out, code := capture(t, func() int { return run([]string{"tags", "--list", "--json"}) })
	if code != exitOK {
		t.Fatalf("종료 코드가 0 이어야 한다 : %d (%s)", code, out)
	}
	if strings.Contains(out, "표준 태그 —") {
		t.Fatalf("--json 인데 사람용 글이 섞였다 : %s", out)
	}
	listed := tagsListJSON{}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("JSON 이 아니다 : %v (%s)", err, out)
	}
	if listed.Parents != len(listed.Tags) || listed.Total < listed.Parents {
		t.Fatalf("상위·통틀어 셈이 안 맞는다 : %+v", listed)
	}
	if _, found := listed.Tags["index"]; !found {
		t.Fatalf("표준 태그가 빠졌다 : %+v", listed.Tags)
	}
	if !contains(listed.Scopes, "mem-search") {
		t.Fatalf("표준 scope 가 빠졌다 : %+v", listed.Scopes)
	}
}

// 깨진 파일은 「아직 색인 전」으로 뭉개지 않고 읽기 오류를 그대로 말한다.
func TestShowTellsBrokenFileApart(t *testing.T) {
	memory := newRepo(t)
	id := "20260921-abcdabcd"
	folder := filepath.Join(memory, "store", "2026", "09")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, id+".md"), []byte("머리말이 없는 글"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := captureBoth(t, func() int { return run([]string{"show", id, "--no-index"}) })
	if code != exitUsage {
		t.Fatalf("종료 코드가 1 이어야 한다 : %d (%s)", code, out)
	}
	if strings.Contains(out, "아직 색인 전") || strings.Contains(out, "그런 기억이 없다") {
		t.Fatalf("깨진 파일을 없는 기억으로 말한다 : %s", out)
	}
	if !strings.Contains(out, "머리말") {
		t.Fatalf("읽기 오류를 그대로 안 말한다 : %s", out)
	}
}
