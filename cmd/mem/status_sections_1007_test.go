package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
)

// 키 없는 절 머리 [canon] 만 빠진 mem.toml 을 status 가 빠진 것 1 로 본다.
func TestStatusCountsMissingSection(t *testing.T) {
	memory := newRepo(t)
	path := filepath.Join(memory, config.FileName)
	// newRepo 의 mem.toml 은 짧아서 기본 글 전체로 바꾼 뒤 머리 하나만 뺀다.
	raw := config.Encode(config.Default("시험"))
	header := regexp.MustCompile(`(?m)^\[canon\]\r?\n`)
	if !header.Match(raw) {
		t.Fatalf("기본 mem.toml 에 [canon] 이 없다 :\n%s", raw)
	}
	if err := os.WriteFile(path, header.ReplaceAll(raw, nil), 0o644); err != nil {
		t.Fatal(err)
	}
	capture(t, func() int { return run([]string{"index", "--quiet"}) })
	out, _ := capture(t, func() int { return run([]string{"status", "--json"}) })
	if !strings.Contains(out, `"missing_sections"`) {
		t.Fatalf("--json 에 missing_sections 가 없다 :\n%s", out)
	}
	data := statusData{}
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		t.Fatalf("JSON 이 아니다 : %v\n%s", err, out)
	}
	if data.MissingKey+data.MissingSection != 1 || data.MissingSection != 1 {
		t.Fatalf("빠진 것 1 이어야 한다 : 키 %d · 절 %d", data.MissingKey, data.MissingSection)
	}
	text, _ := capture(t, func() int { return run([]string{"status"}) })
	if !strings.Contains(text, "빠진 키 0 · 절 1") {
		t.Fatalf("설정 줄에 빠진 것 1 이 안 찍혔다 :\n%s", text)
	}
}
