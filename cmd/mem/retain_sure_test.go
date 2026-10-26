package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/retain"
)

// retain_sure_support 는 retain:* R3 의 지지 선만 바꾼다. 서버 truncated 는 retain-log 후보 줄에 남는다.
func TestRetainSureSupportAndTruncatedLog(t *testing.T) {
	nli := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		if r.URL.Path == "/health" {
			io.WriteString(w, `{"ok":true}`)
			return
		}
		io.WriteString(w, `{"a":0.860,"b":0.140,"c":0,"model":"fake0001","ms":1.0,"label_set":"nli3","truncated":true}`)
	}))
	t.Cleanup(nli.Close)
	for _, one := range []struct {
		extra, want string
	}{{"", retain.RuleSupport}, {"retain_sure_support = 0.85\n", retainVerdictPass}} {
		memory := retainRepo(t)
		generate, _ := fakeGenerate(t, goodCandidates())
		path := filepath.Join(t.TempDir(), "llm.toml")
		text := fmt.Sprintf("generate_url = %q\nnli_url = %q\nnli_sure_support = 0.89\n%s", generate.URL, nli.URL, one.extra)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("MEM_LLM_CONFIG", path)
		captureBoth(t, func() int { return run([]string{"retain", "--dry-run"}) })
		lines := retainLogLines(t, memory)
		if len(lines) != 1 || len(lines[0].Items) != 1 {
			t.Fatalf("후보 줄이 하나여야 한다 : %+v", lines)
		}
		item := lines[0].Items[0]
		if item.Verdict != one.want || !item.Truncated {
			t.Fatalf("%q : 판정 %q (기대 %q) · truncated %v", one.extra, item.Verdict, one.want, item.Truncated)
		}
	}
}
