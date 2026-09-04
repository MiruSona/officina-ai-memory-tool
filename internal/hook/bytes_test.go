package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 설계 결정 23 — 8,000바이트에서 우리가 먼저 자르고, 한글 가운데를 안 자른다.
func TestByteCapKeepsRunesWhole(t *testing.T) {
	huge := block{head: "머리\n", body: strings.Repeat("한글 줄 하나입니다\n", 900), foot: "꼬리\n"}
	trimmed, cut := byteCapped(huge, 8000)
	text := trimmed.text()
	if !cut {
		t.Fatal("잘랐다고 말하지 않았다")
	}
	if len(text) > 8000 {
		t.Fatalf("바이트 상한을 넘었다 : %d", len(text))
	}
	if !utf8.ValidString(text) {
		t.Fatal("글자 가운데가 잘렸다")
	}
	if !strings.HasPrefix(text, "머리\n") || !strings.HasSuffix(text, "꼬리\n") {
		t.Fatal("머리나 꼬리가 잘렸다")
	}
}

// 줄 하나가 통째로 상한을 넘어도 룬 경계에서만 자른다.
func TestByteCapOnOneHugeLine(t *testing.T) {
	one := block{head: "", body: strings.Repeat("가", 4000), foot: ""}
	trimmed, cut := byteCapped(one, 101)
	if !cut || len(trimmed.text()) > 101 || !utf8.ValidString(trimmed.text()) {
		t.Fatalf("룬 경계를 안 지켰다 : %d바이트", len(trimmed.text()))
	}
}

// 설계 5-1 — 잘랐으면 그 사실이 블록 맨 앞에 있고 꼬리에 실제 바이트가 적힌다.
func TestTruncationIsAnnouncedAtTheTop(t *testing.T) {
	dir := newRepo(t)
	for at := 0; at < 40; at++ {
		item := request(model.TypeCaution, "mem",
			padTo("바이트 상한을 넘기려고 넣는 아주 긴 주의 사항 번호 "+string(rune('가'+at)), 110))
		item.Body = "본문이 서로 달라야 승격이 안 합친다 : " + string(rune('가'+at))
		item.Severity = model.SeverityHigh
		addMemory(t, dir, item)
	}
	indexAll(t, dir)
	small := config.Default("시험")
	small.Hook.MaxBytes = 900
	writeConfig(t, dir, small)
	text := contextOf(t, runHook(t, []string{"session-start"}, startInput(dir)))
	if len(text) > 900 {
		t.Fatalf("상한 900바이트를 넘었다 : %d\n%s", len(text), text)
	}
	if !strings.Contains(strings.SplitN(text, "\n\n", 2)[0], "잘랐다") {
		t.Fatalf("자른 사실이 맨 앞에 없다 :\n%s", text)
	}
	if !strings.Contains(text, "블록 ") {
		t.Fatalf("꼬리에 바이트 수가 없다 :\n%s", text)
	}
}

// U2 — 바이트 상한이 토큰 예산보다 더 잘라도 머리말 「N건 중 M건」과
// --json 의 shown 이 실제로 실린 기억 줄 수와 같아야 한다. 잘리기 전 값을
// 그대로 찍으면 「51건 중 51건」인데 실제로는 45건만 실리는 일이 생긴다.
func TestHeaderShownMatchesAfterByteCut(t *testing.T) {
	dir := newRepo(t)
	for at := 0; at < 40; at++ {
		item := request(model.TypeCaution, "mem",
			padTo("바이트 상한을 넘기려고 넣는 아주 긴 주의 사항 번호 "+string(rune('가'+at)), 110))
		item.Body = "본문이 서로 달라야 승격이 안 합친다 : " + string(rune('가'+at))
		item.Severity = model.SeverityHigh
		addMemory(t, dir, item)
	}
	indexAll(t, dir)
	small := config.Default("시험")
	small.Hook.MaxBytes = 900
	writeConfig(t, dir, small)

	text := contextOf(t, runHook(t, []string{"session-start"}, startInput(dir)))
	actual := countEntries(text)
	claimed := headerShown(t, text)
	if claimed != actual {
		t.Fatalf("머리말은 %d건이라는데 실제로 실린 줄은 %d건이다 :\n%s", claimed, actual, text)
	}

	stats := jsonStats(t, runHook(t, []string{"session-start", "--json"}, startInput(dir)))
	if stats.Shown != actual {
		t.Fatalf("--json shown 은 %d인데 실제로 실린 줄은 %d건이다", stats.Shown, actual)
	}
}

// headerShown 은 「N건 중 M건」의 M 을 뽑는다.
func headerShown(t *testing.T, text string) int {
	t.Helper()
	match := regexp.MustCompile(`\d+건 중 (\d+)건`).FindStringSubmatch(text)
	if match == nil {
		t.Fatalf("머리말에 「N건 중 M건」이 없다 :\n%s", text)
	}
	value, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func jsonStats(t *testing.T, raw string) Stats {
	t.Helper()
	stats := Stats{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &stats); err != nil {
		t.Fatalf("잰 값이 JSON 이 아니다 : %v\n%s", err, raw)
	}
	return stats
}

// 설계 G3 — mem hook --json 이 bytes·tokens·ms 를 찍는다.
func TestJSONModeMeasures(t *testing.T) {
	dir := filled(t)
	raw := runHook(t, []string{"session-start", "--json"}, startInput(dir))
	stats := Stats{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &stats); err != nil {
		t.Fatalf("잰 값이 JSON 이 아니다 : %v\n%s", err, raw)
	}
	if stats.Bytes <= 0 || stats.Tokens <= 0 {
		t.Fatalf("바이트·토큰을 안 쟀다 : %+v", stats)
	}
	if stats.MaxBytes != config.DefaultHookMaxBytes {
		t.Fatalf("상한이 mem.toml 값이 아니다 : %d", stats.MaxBytes)
	}
	if stats.Shown == 0 || stats.Total == 0 {
		t.Fatalf("건수를 안 쟀다 : %+v", stats)
	}
}

// 설계 결정 3 — AI 가 쓴 줄과 훅이 쓴 줄에 표식이 붙고 사람 것에는 안 붙는다.
func TestAuthorMarks(t *testing.T) {
	cases := map[string]string{
		"human:mirusona":     "",
		"claude-code/opus5":  i18n.T(i18n.HookMarkAI),
		"hook:session-start": i18n.T(i18n.HookMarkHook),
		"import:v0.1":        i18n.T(i18n.HookMarkImport),
		"":                   "",
	}
	for author, want := range cases {
		if got := AuthorMark(author); got != want {
			t.Fatalf("%s → %q 여야 하는데 %q", author, want, got)
		}
	}
	row := index.HookRow{ID: "20260823-aaaabbbb", Summary: "요약", Author: "human:mirusona"}
	if strings.Contains(lineOf(row), i18n.T(i18n.HookMarkAI)) {
		t.Fatal("사람이 쓴 줄에 AI 표식이 붙었다")
	}
}

// 절당 줄 수는 mem.toml lines_per_section 을 따른다 (설계 5-1).
func TestLinesPerSectionFollowsConfig(t *testing.T) {
	dir := stuffed(t)
	settings := config.Default("시험")
	settings.Hook.LinesPerSection = 2
	writeConfig(t, dir, settings)
	text := contextOf(t, runHook(t, []string{"session-start"}, startInput(dir)))
	if strings.Contains(text, "## 고정 (3") || strings.Contains(text, "## 최근 결정 (3") {
		t.Fatalf("절당 줄 수가 2를 넘었다 :\n%s", text)
	}
}

// writeConfig 는 저장소 mem.toml 을 통째로 갈아 끼운다.
func writeConfig(t *testing.T, dir string, settings config.Config) {
	t.Helper()
	path := filepath.Join(dir, config.DirName, config.FileName)
	if err := os.WriteFile(path, config.Encode(settings), 0o644); err != nil {
		t.Fatal(err)
	}
}
