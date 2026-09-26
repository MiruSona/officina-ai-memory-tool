package lint

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// fixtureDir 은 규칙마다 하나씩 일부러 어긴 파일이 든 자리다.
const fixtureDir = "../../testdata/lint"

var idLine = regexp.MustCompile(`(?m)^id:\s*(\S+)`)

// newRepo 는 시험용 저장소 하나를 만들고 fixture 파일을 store/ 에 넣는다.
func newRepo(t *testing.T, fixtures ...string) *store.Store {
	t.Helper()
	dir := t.TempDir()
	opened := store.Open(filepath.Join(dir, "Memory"), false)
	if err := opened.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	for _, name := range fixtures {
		copyFixture(t, opened, name)
	}
	return opened
}

func copyFixture(t *testing.T, opened *store.Store, name string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	writeMemoryFile(t, opened, idOf(string(raw)), raw)
}

// copyFixtureCRLF 는 fixture 를 CRLF 줄끝으로 바꿔 넣는다. 시험 자료는
// `.gitattributes` 로 어느 OS 에서 받아도 LF 라서, CRLF 는 여기서 바이트로
// 만든다 — 체크아웃이 줄끝을 바꿔 주는 데 기대면 리눅스에서 깨진다.
// 먼저 LF 로 되돌리는 것은 옛 Windows 사본(CRLF 로 받은 것)에서 `\r\r\n` 이
// 생기지 않게 하려는 것이다.
func copyFixtureCRLF(t *testing.T, opened *store.Store, name string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\n", "\r\n")
	writeMemoryFile(t, opened, idOf(text), []byte(text))
}

func idOf(text string) string {
	if found := idLine.FindStringSubmatch(text); found != nil {
		return found[1]
	}
	return "20260820-00000001"
}

func writeMemoryFile(t *testing.T, opened *store.Store, id string, raw []byte) {
	t.Helper()
	dir := filepath.Join(opened.StoreDir(), "2026", "08")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".md"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// lintVocab 은 시험 자료가 쓰는 낱말 표준이다. 씨앗 목록에는 scope 가 하나도
// 없어서 그대로 두면 표준 밖 태그·scope 가 경고로만 나온다 (Vocab.Learning).
func lintVocab() config.Vocab {
	vocab := config.DefaultVocab()
	vocab.Tags["index"] = []string{"fts5"}
	vocab.Tags["search"] = []string{"ranking"}
	vocab.TagAlias["scoring"] = "ranking"
	vocab.Scopes["aimemorytool"] = []string{}
	return vocab
}

// goodMemory 는 v0.2 규격을 다 지킨 기억 하나다. 규칙을 하나만 어기게 하려면
// 여기서 한 칸만 바꾼다 — 그래야 「그 규칙이 걸렸다」가 진짜 그 규칙 때문이다.
func goodMemory(id, extra, body string) []byte {
	head := "---\nid: " + id + "\ntype: history\n" +
		"title: 색인은 파생물이라 지워도 된다\n" +
		"summary: 색인은 언제든 지우고 다시 만들 수 있는 파생물이라, 망가지면 통째로 버리고 mem index --full 로 되살린다\n" +
		"tags: [index, test]\nscope: aimemorytool\ndate: 2026-08-20\nauthor: human:mirusona\n" +
		"sources: [\"commit:0123456789abcdef\"]\n" + extra + "---\n\n"
	if body == "" {
		body = "색인은 파생물이라 지워도 원본 md 가 그대로 남는다.\n망가지면 통째로 버린다.\n다시 만드는 명령은 `mem index --full` 하나다.\n"
	}
	return []byte(head + body)
}

func runOn(t *testing.T, opened *store.Store, fix bool) *Report {
	t.Helper()
	return runWith(t, Options{Store: opened, Config: config.Default("aimemorytool"), Vocab: lintVocab(), Fix: fix,
		Now: time.Date(2026, 8, 22, 12, 0, 0, 0, time.Local), NoGit: true})
}

func runWith(t *testing.T, options Options) *Report {
	t.Helper()
	if options.Now.IsZero() {
		options.Now = time.Date(2026, 8, 22, 12, 0, 0, 0, time.Local)
	}
	report, err := Run(options)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func has(report *Report, rule string) bool {
	for _, problem := range report.Problems {
		if problem.Rule == rule {
			return true
		}
	}
	return false
}

func ruleLevel(report *Report, rule string) Level {
	for _, problem := range report.Problems {
		if problem.Rule == rule {
			return problem.Level
		}
	}
	return ""
}

// TestCatalogIsTheRuleList 는 lint 가 제 규칙표를 안 따로 들고 있는지 본다.
// 규칙이 두 곳에 적히면 반드시 어긋난다 (설계 3절).
func TestCatalogIsTheRuleList(t *testing.T) {
	if len(Rules) != len(quality.Catalog) {
		t.Fatalf("규칙이 %d개다. 카탈로그는 %d개다", len(Rules), len(quality.Catalog))
	}
	for _, name := range Rules {
		if _, ok := quality.Lookup(name); !ok {
			t.Fatalf("카탈로그에 없는 규칙 %s 를 lint 가 들고 있다", name)
		}
	}
}

// lintOnlyCase 는 lint 가 혼자 맡는 규칙 하나와 그것을 걸리게 하는 자리다.
// 나머지 33개는 quality 관문이 보고 `mem eval --quality` 가 정밀도까지 잰다.
type lintOnlyCase struct {
	Rule  string
	Level Level
	Setup func(t *testing.T, opened *store.Store)
}

func lintOnlyCases() []lintOnlyCase {
	return []lintOnlyCase{
		{quality.RuleFrontMatter, LevelError, func(t *testing.T, s *store.Store) {
			writeMemoryFile(t, s, "20260820-11110000", []byte("머리말이 없다\n"))
		}},
		{quality.RuleEncoding, LevelWarn, func(t *testing.T, s *store.Store) {
			copyFixture(t, s, "encoding.cp949")
		}},
		{quality.RuleBOM, LevelWarn, func(t *testing.T, s *store.Store) {
			writeMemoryFile(t, s, "20260820-11110002",
				append([]byte{0xEF, 0xBB, 0xBF}, goodMemory("20260820-11110002", "", "")...))
		}},
		{quality.RuleCRLF, LevelWarn, func(t *testing.T, s *store.Store) {
			raw := goodMemory("20260820-11110003", "", "")
			writeMemoryFile(t, s, "20260820-11110003",
				[]byte(strings.ReplaceAll(string(raw), "\n", "\r\n")))
		}},
		{quality.RuleDeadMemLink, LevelError, func(t *testing.T, s *store.Store) {
			writeMemoryFile(t, s, "20260820-11110004",
				goodMemory("20260820-11110004", "links: [20260820-deadbeef]\n", ""))
		}},
		{quality.RuleDeadPath, LevelWarn, func(t *testing.T, s *store.Store) {
			writeMemoryFile(t, s, "20260820-11110005",
				goodMemory("20260820-11110005", "", "없는 파일을 가리킨다.\n[여기](src/없는파일.go) 를 보라는데 그런 것은 없다.\n한 줄 더 적어 본문 하한을 채운다.\n"))
		}},
		{quality.RuleShadowExe, LevelError, putShadowExe},
		{quality.RuleInboxBad, LevelError, putInboxBad},
	}
}

func putShadowExe(t *testing.T, opened *store.Store) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(filepath.Dir(opened.Dir), "mem.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func putInboxBad(t *testing.T, opened *store.Store) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(opened.InboxBadDir(), "broken.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func putMissLog(t *testing.T, opened *store.Store) {
	t.Helper()
	dir := store.LocalDir(opened.Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"q":"기억 수정","words":["기억","수정"],"at":1755820800}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, MissFileName), []byte(strings.Repeat(line, 3)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLintOnlyRulesFire 는 lint 가 혼자 맡는 규칙이 저마다 실제로 걸리는지 본다.
func TestLintOnlyRulesFire(t *testing.T) {
	for _, item := range lintOnlyCases() {
		t.Run(item.Rule, func(t *testing.T) {
			opened := newRepo(t)
			item.Setup(t, opened)
			report := runOn(t, opened, false)
			if !has(report, item.Rule) {
				t.Fatalf("규칙 %s 가 안 걸렸다 : %+v", item.Rule, report.Problems)
			}
			if ruleLevel(report, item.Rule) != item.Level {
				t.Fatalf("규칙 %s 등급이 %s 가 아니라 %s 다", item.Rule, item.Level, ruleLevel(report, item.Rule))
			}
		})
	}
}

// TestGateRulesReachLint 는 quality 관문 규칙이 lint 보고에도 그대로 나오는지
// 본다 — 같은 검사가 두 곳에 따로 적혀 있으면 안 된다.
func TestGateRulesReachLint(t *testing.T) {
	opened := newRepo(t)
	writeMemoryFile(t, opened, "20260820-22220000",
		[]byte("---\nid: 20260820-22220000\ntype: decision\nsummary: 너무 짧은 요약\ntags: [x]\nscope: 몰라\ndate: 2026-08-20\nauthor: 이상함\n---\n\n한 줄.\n"))
	report := runOn(t, opened, false)
	for _, rule := range []string{quality.RuleRequiredField, quality.RuleSummaryLen,
		quality.RuleTagCount, quality.RuleScopeStandard, quality.RuleAuthorShape,
		quality.RuleSourcesRequired, quality.RuleBodyThin} {
		if !has(report, rule) {
			t.Errorf("관문 규칙 %s 가 lint 에 안 나왔다", rule)
		}
	}
}

// TestCleanFileHasNoProblem 은 규격을 다 지킨 파일에 아무 소리도 안 나는지 본다.
func TestCleanFileHasNoProblem(t *testing.T) {
	opened := newRepo(t)
	writeMemoryFile(t, opened, "20260820-33330000", goodMemory("20260820-33330000", "", ""))
	report := runOn(t, opened, false)
	if len(report.Problems) != 0 {
		t.Fatalf("깨끗한 파일에 %d건이 걸렸다 : %+v", len(report.Problems), report.Problems)
	}
}

// TestRuleFilterKeepsOne 은 --rule 이 그 규칙만 남기는지 본다.
func TestRuleFilterKeepsOne(t *testing.T) {
	opened := newRepo(t)
	putShadowExe(t, opened)
	putInboxBad(t, opened)
	report := runWith(t, Options{Store: opened, Config: config.Default("aimemorytool"), Vocab: lintVocab(),
		NoGit: true, Rule: quality.RuleShadowExe})
	if len(report.Problems) != 1 || report.Problems[0].Rule != quality.RuleShadowExe {
		t.Fatalf("--rule 이 하나만 안 남겼다 : %+v", report.Problems)
	}
}

// TestSynonymCandidates 는 --synonym 이 0건 기록으로 후보를 뽑는지 본다.
func TestSynonymCandidates(t *testing.T) {
	opened := newRepo(t)
	putMissLog(t, opened)
	report := runWith(t, Options{Store: opened, Config: config.Default("aimemorytool"), Vocab: lintVocab(),
		NoGit: true, Synonym: true})
	if len(report.Synonyms) == 0 {
		t.Fatalf("동의어 후보가 하나도 없다 : %v", report.Notes)
	}
	quiet := runOn(t, opened, false)
	if len(quiet.Synonyms) != 0 {
		t.Fatal("--synonym 없이도 후보를 뽑았다")
	}
}

// TestDemotedGradeGoesDown 은 강등표가 등급을 실제로 내리는지 본다 (결정 14).
func TestDemotedGradeGoesDown(t *testing.T) {
	opened := newRepo(t)
	writeMemoryFile(t, opened, "20260820-44440000",
		goodMemory("20260820-44440000", "", "한 줄뿐인 본문.\n"))
	before := runOn(t, opened, false)
	if ruleLevel(before, quality.RuleBodyThin) != LevelError {
		t.Fatalf("body-thin 이 오류가 아니다 : %+v", before.Problems)
	}
	after := runWith(t, Options{Store: opened, Config: config.Default("aimemorytool"), Vocab: lintVocab(), NoGit: true,
		Demoted: map[string]quality.Grade{quality.RuleBodyThin: quality.GradeWarn}})
	if ruleLevel(after, quality.RuleBodyThin) != LevelWarn {
		t.Fatalf("강등표를 읽고도 등급이 안 내려갔다 : %+v", after.Problems)
	}
}

// TestSecretValueNeverPrinted 는 걸린 값이 메시지에 안 실리는지 본다
// (불변조건 10절).
func TestSecretValueNeverPrinted(t *testing.T) {
	report := runOn(t, newRepo(t, "secret.md"), false)
	for _, problem := range report.Problems {
		if strings.Contains(problem.Text, "AKIA") {
			t.Fatalf("비밀정보 값이 찍혔다 : %s", problem.Text)
		}
	}
	if !has(report, quality.RuleSecretPattern) {
		t.Fatalf("비밀정보를 못 찾았다 : %+v", report.Problems)
	}
}

// TestFixOnlyReversible 은 --fix 가 되돌릴 수 있는 것만 고치는지 본다.
func TestFixOnlyReversible(t *testing.T) {
	opened := newRepo(t, "bom.md", "field.md")
	copyFixtureCRLF(t, opened, "crlf.md")
	// 규격 밖 칸이 있는 파일도 고칠 거리(CRLF)가 있어야 「건너뜀」으로 잡힌다.
	// 고칠 것이 없으면 건너뛸 것도 없다.
	copyFixtureCRLF(t, opened, "extra-field.md")
	report := runOn(t, opened, true)
	if len(report.Fixes) != 2 {
		t.Fatalf("고친 것이 2건이어야 한다 : %+v", report.Fixes)
	}
	if len(report.Held) != 1 {
		t.Fatalf("규격 밖 칸이 있는 파일 1건을 건너뛰어야 한다 : %+v", report.Held)
	}
	short := readStore(t, opened, "20260820-22222222.md")
	if !strings.Contains(short, "너무 짧은 요약") {
		t.Fatal("요약이 짧은 파일은 손대지 않아야 한다")
	}
	if strings.Contains(readStore(t, opened, "20260820-ffff6666.md"), "\r\n") {
		t.Fatal("줄끝이 아직 CRLF 다")
	}
	if strings.HasPrefix(readStore(t, opened, "20260820-ffff5555.md"), string([]byte{0xEF, 0xBB, 0xBF})) {
		t.Fatal("BOM 이 안 지워졌다")
	}
}

// TestLintFixturesAreLF 는 받은 시험 자료가 LF 인지 본다. `.gitattributes` 가
// `testdata/** text eol=lf` 로 못박는다 — 여기 CRLF 가 보이면 그 파일을 넣기 전에
// 받은 옛 사본이다. CRLF 시험은 copyFixtureCRLF 가 바이트를 만든다.
func TestLintFixturesAreLF(t *testing.T) {
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(fixtureDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "\r\n") {
			t.Fatalf("%s 가 CRLF 다. .gitattributes 전에 받은 사본이면 testdata 를 다시 받는다", entry.Name())
		}
	}
}

// TestFixSwapsAliasTag 는 --fix 가 별칭 태그를 제 이름으로 바꾸는지 본다 (F09).
func TestFixSwapsAliasTag(t *testing.T) {
	opened := newRepo(t)
	raw := goodMemory("20260820-55550000", "", "")
	raw = []byte(strings.Replace(string(raw), "tags: [index, test]", "tags: [scoring, test]", 1))
	writeMemoryFile(t, opened, "20260820-55550000", raw)
	report := runOn(t, opened, true)
	if len(report.Fixes) != 1 {
		t.Fatalf("별칭 태그를 안 고쳤다 : %+v (걸린 것 %+v)", report.Fixes, report.Problems)
	}
	after := readStore(t, opened, "20260820-55550000.md")
	if strings.Contains(after, "scoring") || !strings.Contains(after, "ranking") {
		t.Fatalf("별칭이 안 바뀌었다 :\n%s", after)
	}
}

// TestFixWithoutLockOnlyChecks 는 남이 락을 쥐고 있으면 검사만 하는지 본다
// (불변조건 2).
func TestFixWithoutLockOnlyChecks(t *testing.T) {
	opened := newRepo(t)
	copyFixtureCRLF(t, opened, "crlf.md")
	lock := filepath.Join(opened.Dir, "index.lock")
	line := "999999 1 " + timeNow() + " othermachine\n"
	if err := os.WriteFile(lock, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	report := runOn(t, opened, true)
	if !report.FixLocked {
		t.Fatal("락을 못 잡았는데 FixLocked 가 아니다")
	}
	if len(report.Fixes) != 0 {
		t.Fatalf("락 없이 고쳤다 : %+v", report.Fixes)
	}
	if !strings.Contains(readStore(t, opened, "20260820-ffff6666.md"), "\r\n") {
		t.Fatal("파일이 고쳐졌다")
	}
}

// TestFixReadOnlyStore 는 읽기 전용 저장소에서 검사만 하는지 본다.
func TestFixReadOnlyStore(t *testing.T) {
	opened := newRepo(t)
	copyFixtureCRLF(t, opened, "crlf.md")
	readOnly := store.Open(opened.Dir, true)
	report := runWith(t, Options{Store: readOnly, Config: config.Default("test"), Vocab: lintVocab(), Fix: true, NoGit: true})
	if !report.FixReadOnly || len(report.Fixes) != 0 {
		t.Fatalf("읽기 전용인데 고치려 들었다 : %+v", report)
	}
}

// TestNotesSayWhatWasNotChecked 는 안 본 것을 안 봤다고 말하는지 본다
// (설계 8-1 신뢰도).
func TestNotesSayWhatWasNotChecked(t *testing.T) {
	opened := newRepo(t)
	writeMemoryFile(t, opened, "20260820-66660000", goodMemory("20260820-66660000", "", ""))
	report := runOn(t, opened, false)
	if !strings.Contains(strings.Join(report.Notes, "\n"), "조회 기록") {
		t.Fatalf("조회 기록이 없는데 그 사실을 안 적었다 : %v", report.Notes)
	}
}

func readStore(t *testing.T, opened *store.Store, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(opened.StoreDir(), "2026", "08", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func timeNow() string {
	return strconv.FormatInt(time.Now().Unix(), 10)
}
