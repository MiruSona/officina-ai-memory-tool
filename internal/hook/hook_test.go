package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/budget"
	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// 시험용 저장소는 폴더 이름이 곧 scope 짐작값이 된다 (설계 7-2).
const projectFolder = "openapi"

// newRepo 는 프로젝트 저장소 하나를 만들고 홈은 빈 곳으로 치운다.
func newRepo(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	dir := filepath.Join(t.TempDir(), projectFolder)
	makeRepo(t, dir)
	return dir
}

func makeRepo(t *testing.T, dir string) {
	t.Helper()
	memory := filepath.Join(dir, config.DirName)
	if err := os.MkdirAll(memory, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := config.Default("시험")
	if err := os.WriteFile(filepath.Join(memory, config.FileName), config.Encode(settings), 0o644); err != nil {
		t.Fatal(err)
	}
	opened := store.Open(memory, false)
	if err := opened.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
}

// setBudget 은 저장소 mem.toml 의 [budget] 만 갈아 끼운다.
func setBudget(t *testing.T, dir string, want config.BudgetConfig) {
	t.Helper()
	memory := filepath.Join(dir, config.DirName)
	settings := config.Default("시험")
	settings.Budget = want
	if err := os.WriteFile(filepath.Join(memory, config.FileName), config.Encode(settings), 0o644); err != nil {
		t.Fatal(err)
	}
}

func addMemory(t *testing.T, dir string, request store.AddRequest) {
	t.Helper()
	opened := store.Open(filepath.Join(dir, config.DirName), false)
	if _, err := opened.WriteAdd(request); err != nil {
		t.Fatal(err)
	}
}

func indexAll(t *testing.T, dir string) {
	t.Helper()
	settings := config.Default("시험")
	opened := store.Open(filepath.Join(dir, config.DirName), false)
	_, err := index.Run(index.Options{Store: opened, GC: settings.GC, Secret: settings.Secret, Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
}

// request 는 시험용 기억 한 건이다. 요약은 30~120자여야 통과한다.
func request(memoryType, scope, summary string) store.AddRequest {
	return store.AddRequest{Op: store.OpAdd, Type: memoryType, Date: "2026-08-22",
		Summary: summary, Tags: []string{"mem", "hook"}, Source: model.LegacySourceAI,
		Scope: scope, Body: "본문은 여기에 둔다. 훅은 본문을 안 읽는다."}
}

func longEnough(text string) string {
	for len([]rune(text)) < 30 {
		text += " 그리고 이것은 시험용으로 길이를 채우려고 붙인 말이다"
	}
	return text
}

func startInput(dir string) string {
	raw, err := json.Marshal(Input{HookEventName: EventName, Source: KindStartup, Cwd: dir})
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func runHook(t *testing.T, argv []string, input string) string {
	t.Helper()
	out := bytes.Buffer{}
	code := Run(argv, strings.NewReader(input), &out)
	if code != 0 {
		t.Fatalf("훅은 늘 0 을 내야 한다 : %d", code)
	}
	return out.String()
}

// contextOf 는 훅이 낸 JSON 에서 주입 본문을 꺼낸다.
func contextOf(t *testing.T, raw string) string {
	t.Helper()
	answer := output{}
	if err := json.Unmarshal([]byte(raw), &answer); err != nil {
		t.Fatalf("유효한 JSON 이 아니다 : %v\n%s", err, raw)
	}
	if answer.HookSpecificOutput.HookEventName != EventName {
		t.Fatalf("이벤트 이름이 다르다 : %s", answer.HookSpecificOutput.HookEventName)
	}
	return answer.HookSpecificOutput.AdditionalContext
}

func filled(t *testing.T) string {
	t.Helper()
	dir := newRepo(t)
	pinned := request(model.TypeDecision, "mem", longEnough("저장소는 프로젝트 하나 더하기 기계 전역 하나로 간다"))
	pinned.Pinned = true
	addMemory(t, dir, pinned)
	todo := request(model.TypeTodo, "mem", longEnough("실측 없이 잡은 임계값을 한 달 굴려 보고 다시 잡는다"))
	todo.Status = model.StatusOpen
	addMemory(t, dir, todo)
	addMemory(t, dir, request(model.TypeDecision, projectFolder,
		longEnough("한글 검색은 바이그램 stems 구절에 영문 낱말을 더해서 찾는다")))
	caution := request(model.TypeCaution, "claude-code",
		longEnough("훅 stdout JSON 이 잘리면 그 문자열이 그대로 세션에 주입된다"))
	caution.Severity = model.SeverityHigh
	addMemory(t, dir, caution)
	indexAll(t, dir)
	return dir
}

// 설계 7-7 · 12-3 「훅 안전」 — DB 를 일부러 깨도 stdout 이 완전히 비고 0 이다.
func TestBrokenIndexSaysNothing(t *testing.T) {
	dir := filled(t)
	broken := filepath.Join(dir, config.DirName, index.FileName)
	if err := os.WriteFile(broken, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	if out := runHook(t, []string{"session-start"}, startInput(dir)); out != "" {
		t.Fatalf("깨진 DB 에서 뭔가 냈다 : %q", out)
	}
}

// 설계 7-7 — 저장소가 없으면 침묵한다.
func TestNoRepositoryIsSilent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	empty := t.TempDir()
	if out := runHook(t, []string{"session-start"}, startInput(empty)); out != "" {
		t.Fatalf("저장소 없이 뭔가 냈다 : %q", out)
	}
}

// 설계 7-3 — 출력은 유효한 JSON 한 덩어리이고 본문은 10,000자 안이다.
func TestOutputIsOneValidJSON(t *testing.T) {
	dir := filled(t)
	raw := runHook(t, []string{"session-start"}, startInput(dir))
	text := contextOf(t, raw)
	if len([]rune(text)) > hardCharCap {
		t.Fatalf("글자 하드컷을 넘었다 : %d", len([]rune(text)))
	}
	if strings.Count(strings.TrimSpace(raw), "\n") != 0 {
		t.Fatalf("JSON 은 한 줄이어야 한다 : %q", raw)
	}
}

// 설계 7-5 — 줄바꿈과 마크다운 기호가 든 요약도 한 줄 · 160룬 안이다.
func TestLinesAreFlatAndShort(t *testing.T) {
	dir := newRepo(t)
	hostile := request(model.TypeDecision, "mem", longEnough("# 제목\n- 항목 <b> 그리고 아주 긴 말이 계속 이어진다"))
	hostile.Pinned = true
	addMemory(t, dir, hostile)
	indexAll(t, dir)
	text := contextOf(t, runHook(t, []string{"session-start"}, startInput(dir)))
	for _, line := range strings.Split(text, "\n") {
		if len([]rune(line)) > lineMaxRunes {
			t.Fatalf("줄이 너무 길다 (%d) : %s", len([]rune(line)), line)
		}
	}
	if !strings.Contains(text, "제목") || strings.Contains(text, "# 제목") {
		t.Fatalf("줄머리 기호가 안 떨어졌다 :\n%s", text)
	}
	if strings.Contains(text, "<b>") {
		t.Fatalf("태그가 살아 있다 :\n%s", text)
	}
}

// 설계 7-5 — 자료 표시 두 줄은 늘 있고 잘리는 것은 그 사이뿐이다 (불변조건 7).
func TestGuardLinesSurviveTheCut(t *testing.T) {
	dir := newRepo(t)
	for at := 0; at < 12; at++ {
		item := request(model.TypeCaution, "mem",
			longEnough("아주 긴 주의 사항을 여러 건 넣어 예산을 넘겨 본다 그러면 잘려야 한다"))
		item.Severity = model.SeverityHigh
		addMemory(t, dir, item)
	}
	indexAll(t, dir)
	text := contextOf(t, runHook(t, []string{"session-start"}, startInput(dir)))
	head := i18n.T(i18n.HookGuardHead)
	foot := i18n.T(i18n.HookGuardFoot)
	if !strings.Contains(text, head) || !strings.Contains(text, foot) {
		t.Fatalf("자료 표시가 사라졌다 :\n%s", text)
	}
	if strings.Index(text, head) > strings.Index(text, foot) {
		t.Fatal("자료 표시 순서가 뒤집혔다")
	}
}

// stuffed 는 절마다 자리가 넘치도록 긴 기억을 채운 저장소다.
func stuffed(t *testing.T) string {
	t.Helper()
	dir := newRepo(t)
	addMany(t, dir, model.TypeDecision, "mem", 6, false)
	addMany(t, dir, model.TypeTodo, "mem", 6, false)
	addMany(t, dir, model.TypeCaution, "mem", 6, false)
	addMany(t, dir, model.TypeDecision, "mem", 6, true)
	indexAll(t, dir)
	return dir
}

// addMany 는 서로 다른 본문으로 여러 건을 넣는다. 같은 본문은 승격이 합쳐 버린다.
func addMany(t *testing.T, dir, memoryType, scope string, count int, pinned bool) {
	t.Helper()
	for at := 0; at < count; at++ {
		mark := strconv.Itoa(at)
		item := request(memoryType, scope,
			padTo("번호 "+mark+" 예산이 실제로 줄을 자르는지 보려고 넣은 긴 요약이다", 100))
		item.Body = "본문 " + mark + " : " + memoryType + " 마다 내용이 달라야 중복으로 안 합쳐진다."
		item.Pinned = pinned
		if memoryType == model.TypeTodo {
			item.Status = model.StatusOpen
		}
		if memoryType == model.TypeCaution {
			item.Severity = model.SeverityHigh
		}
		addMemory(t, dir, item)
	}
}

func padTo(text string, want int) string {
	for len([]rune(text)) < want {
		text += " 그리고 길이를 채우려고 붙인 말이다"
	}
	letters := []rune(text)
	if len(letters) > 120 {
		letters = letters[:120]
	}
	return string(letters)
}

// 설계 7-4 — 계기마다 예산이 다르다. resume 은 startup 보다 작아야 한다.
func TestBudgetDiffersByKind(t *testing.T) {
	dir := stuffed(t)
	setBudget(t, dir, config.BudgetConfig{Startup: 3000, Clear: 3000, Resume: 200, Compact: 800, Fork: 200})
	raw, err := json.Marshal(Input{HookEventName: EventName, Source: KindResume, Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	long := contextOf(t, runHook(t, []string{"session-start"}, startInput(dir)))
	short := contextOf(t, runHook(t, []string{"session-start"}, string(raw)))
	if budget.Estimate(short) >= budget.Estimate(long) {
		t.Fatalf("resume 이 startup 보다 작지 않다 : %d / %d", budget.Estimate(short), budget.Estimate(long))
	}
}

// 설계 7-4 — 절은 세션 scope 를 먼저 채우고 남는 자리를 무필터로 채운다.
func TestScopeFillsFirst(t *testing.T) {
	dir := newRepo(t)
	for at := 0; at < 6; at++ {
		addMemory(t, dir, request(model.TypeDecision, "other",
			longEnough("딴 범위의 결정을 여러 건 먼저 넣어 자리를 다투게 한다")))
	}
	addMemory(t, dir, request(model.TypeDecision, projectFolder,
		longEnough("이 세션의 범위에 딱 맞는 결정 한 건은 맨 앞에 와야 한다")))
	indexAll(t, dir)
	text := contextOf(t, runHook(t, []string{"session-start"}, startInput(dir)))
	mine := strings.Index(text, "("+projectFolder+" ·")
	other := strings.Index(text, "(other ·")
	if mine < 0 {
		t.Fatalf("내 범위 기억이 안 들어갔다 :\n%s", text)
	}
	if other >= 0 && other < mine {
		t.Fatalf("딴 범위가 먼저 나왔다 :\n%s", text)
	}
}

// 설계 7-4a · 12-3 「DB 열기 횟수」 — 전역 저장소를 없앤 뒤로 훅 한 번에
// 색인을 한 번만 연다.
func TestOpenCountIsAtMostOne(t *testing.T) {
	dir := filled(t)
	before := index.OpenCount()
	runHook(t, []string{"session-start"}, startInput(dir))
	if opened := index.OpenCount() - before; opened > 1 {
		t.Fatalf("색인을 %d 번 열었다 (상한 1)", opened)
	}
}

// 설계 7-6 — Gemini 입력도 같은 이벤트 이름이고 CLAUDE_PROJECT_DIR 별칭을 준다.
func TestGeminiInputWorks(t *testing.T) {
	dir := filled(t)
	t.Setenv("CLAUDE_PROJECT_DIR", dir)
	raw, err := json.Marshal(Input{HookEventName: EventName, SessionStartKind: KindStartup})
	if err != nil {
		t.Fatal(err)
	}
	text := contextOf(t, runHook(t, []string{"session-start"}, string(raw)))
	if !strings.Contains(text, "## 고정") {
		t.Fatalf("절이 안 만들어졌다 :\n%s", text)
	}
}

// 설계 8-1 #7 — --dry-run 은 사람이 읽는 Markdown 만 낸다.
func TestDryRunPrintsMarkdown(t *testing.T) {
	dir := filled(t)
	out := runHook(t, []string{"session-start", "--dry-run"}, startInput(dir))
	if strings.Contains(out, "hookSpecificOutput") {
		t.Fatalf("--dry-run 이 JSON 을 냈다 :\n%s", out)
	}
	for _, want := range []string{"# 기억 (mem", "## 고정", "## 최근 결정", "## 열린 이슈·할 일"} {
		if !strings.Contains(out, want) {
			t.Fatalf("%s 절이 없다 :\n%s", want, out)
		}
	}
}

// 설계 7-1 — 모르는 이벤트는 침묵한다.
func TestUnknownEventIsSilent(t *testing.T) {
	dir := filled(t)
	raw, err := json.Marshal(Input{HookEventName: "PreToolUse", Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if out := runHook(t, []string{"pre-tool-use"}, string(raw)); out != "" {
		t.Fatalf("모르는 이벤트에 뭔가 냈다 : %q", out)
	}
}

// 설계 7-4 — 알림 절은 두 줄까지다. inbox/bad 가 있으면 그 줄이 뜬다.
func TestNoticeSaysBadInbox(t *testing.T) {
	dir := filled(t)
	bad := filepath.Join(dir, config.DirName, "inbox", "bad", "깨진것.json")
	if err := os.WriteFile(bad, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	text := contextOf(t, runHook(t, []string{"session-start"}, startInput(dir)))
	if !strings.Contains(text, "inbox/bad") {
		t.Fatalf("알림 줄이 없다 :\n%s", text)
	}
}

// 설계 7-4 — 예산이 잘라도 규칙·제목·자료 표시 줄은 그대로 남는다.
func TestHeadSurvivesTheBudgetCut(t *testing.T) {
	dir := stuffed(t)
	raw, err := json.Marshal(Input{HookEventName: EventName, Source: KindResume, Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	text := contextOf(t, runHook(t, []string{"session-start"}, string(raw)))
	for _, want := range []string{"# 기억 (mem", i18n.T(i18n.HookGuardHead), i18n.T(i18n.HookGuardFoot)} {
		if !strings.Contains(text, want) {
			t.Fatalf("머리나 꼬리가 잘려 나갔다 (%s) :\n%s", want, text)
		}
	}
	if strings.Count(text, "\n- ") >= 20 {
		t.Fatalf("resume 인데 줄이 안 줄었다 :\n%s", text)
	}
}

// 설계 7-4 — 글자 하드컷은 본문만 자르고 자료 표시는 안 건드린다.
func TestCharCapCutsBodyOnly(t *testing.T) {
	huge := block{head: "머리\n", body: strings.Repeat("긴 줄 하나\n", 4000), foot: "꼬리\n"}
	trimmed, _ := charCapped(huge, false)
	text := trimmed.text()
	if len([]rune(text)) > hardCharCap {
		t.Fatalf("하드컷을 넘었다 : %d", len([]rune(text)))
	}
	if !strings.HasPrefix(text, "머리\n") || !strings.HasSuffix(text, "꼬리\n") {
		t.Fatalf("머리나 꼬리가 잘렸다 :\n%s", text[:80])
	}
}

// 설계 7-5 — 잘린 자리에 반쪽 줄이 남으면 안 된다.
func TestCutLeavesNoHalfLine(t *testing.T) {
	huge := block{head: "머리\n", body: strings.Repeat("긴 줄 하나입니다\n", 4000), foot: "꼬리\n"}
	trimmed, _ := charCapped(huge, false)
	for _, line := range strings.Split(trimmed.text(), "\n") {
		if line != "" && line != "머리" && line != "꼬리" && line != "긴 줄 하나입니다" &&
			line != i18n.T(i18n.HookTruncated) {
			t.Fatalf("반쪽 줄이 남았다 : %q", line)
		}
	}
}
