package main

// retain 이 자식에 넘기기 전에 죽은 file: 근거를 거르는지 (A2 retain 설계 5절 R8 · 7절 dropped_files).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/retain"
)

func TestChildSourcesDropsDeadFiles(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	os.MkdirAll(filepath.Join(root, "cmd"), 0o755)
	os.WriteFile(filepath.Join(root, "cmd", "main.go"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(base, "outside.txt"), []byte("x"), 0o644)
	files := []string{"cmd/main.go", "cmd/main.go:12", `C:\Mirusona\Server\serve.ps1`,
		filepath.Join(root, "cmd", "main.go"), "../outside.txt", "없는.go", "a,b.go", ""}
	sources, dropped := childSources(root, "abcd1234", 2, files)
	want := []string{model.SourceNote + "transcript abcd1234#2", model.SourceFile + "cmd/main.go",
		model.SourceFile + "cmd/main.go:12"}
	if !reflect.DeepEqual(sources, want) || dropped != 5 {
		t.Fatalf("실재 상대 경로만 남아야 한다 : %v dropped=%d", sources, dropped)
	}
}

func TestRetainDryRunLogsDroppedFiles(t *testing.T) {
	memory := retainRepo(t)
	one := map[string]any{"type": "caution", "title": "한글 두 글자 검색 주의",
		"summary": "두 글자 한글 검색어는 trigram 색인에서 0건이 된다",
		"body":    "두 글자 한글 검색어는 trigram 색인에서 0건이 된다.\n증상은 검색 결과가 비는 것이다.\n짧은 검색어는 따로 다뤄야 한다.",
		"tags":    []string{"search", "korean"}, "scope": "aimemorytool", "quotes": []string{retainQuote},
		"files": []string{`C:\Mirusona\Server\serve.ps1`, "없는/파일.go"}}
	data, _ := json.Marshal(map[string]any{"candidates": []any{one}, "nothing": false})
	generate, _ := fakeGenerate(t, string(data))
	useRetainLLM(t, generate.URL, fakeNLI(t, 0.97, 0).URL)
	out, _ := captureBoth(t, func() int { return run([]string{"retain", "--dry-run"}) })
	lines := retainLogLines(t, memory)
	if len(lines) != 1 || lines[0].DroppedFiles != 2 || len(lines[0].Items) != 1 {
		t.Fatalf("dropped_files 가 안 셌다 : %+v\n%s", lines, out)
	}
	item := lines[0].Items[0]
	if item.Verdict != retainVerdictPass || !reflect.DeepEqual(item.Sources, []string{"note:transcript " + retainShort + "#1"}) {
		t.Fatalf("죽은 경로를 뺀 후보는 R8 을 지나야 하고 sources 가 보여야 한다 : %+v", item)
	}
}

func TestRetainDryRunLogsSummaryAndReasons(t *testing.T) {
	memory := retainRepo(t)
	generate, _ := fakeGenerate(t, goodCandidates())
	useRetainLLM(t, generate.URL, fakeNLI(t, 0.02, 0).URL)
	out, _ := captureBoth(t, func() int { return run([]string{"retain", "--dry-run"}) })
	lines := retainLogLines(t, memory)
	if len(lines) != 1 || len(lines[0].Items) != 1 {
		t.Fatalf("dry 후보 한 줄이 있어야 한다 : %+v\n%s", lines, out)
	}
	item := lines[0].Items[0]
	if item.Summary != "두 글자 한글 검색어는 trigram 색인에서 0건이 된다" {
		t.Fatalf("summary 가 안 실렸다 : %+v", item)
	}
	if len(item.Reasons) != 1 || !strings.HasPrefix(item.Reasons[0], retain.RuleSupport+": ") || len(item.Reasons[0]) <= len(retain.RuleSupport)+2 {
		t.Fatalf("R3 거절 까닭 글이 안 실렸다 : %+v", item)
	}
}
