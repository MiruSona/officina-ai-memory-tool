package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// newRepo 는 시험용 저장소를 만들고 그 안에서 돌게 한다. 집 폴더도 옮겨서
// 진짜 전역 저장소를 절대 안 건드린다.
func newRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	memory := filepath.Join(root, "Memory")
	if err := os.MkdirAll(memory, 0o755); err != nil {
		t.Fatal(err)
	}
	toml := "schema = 1\nname = \"시험\"\n"
	if err := os.WriteFile(filepath.Join(memory, "mem.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	// 시험 저장소는 낱말 표준을 **정해 둔** 상태로 시작한다. 씨앗 목록에는
	// scope 가 하나도 없어서, 안 정해 두면 표준 밖 태그·scope 가 경고로만
	// 나온다 (config.Vocab.Learning). 거절까지 재는 시험이라 여기서 못 박는다.
	if err := os.WriteFile(filepath.Join(memory, "vocab.toml"), []byte(testVocabTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Chdir(root)
	return memory
}

// testVocabTOML 은 시험이 쓰는 태그·scope 표준 목록이다.
const testVocabTOML = `[tag]
"index" = ["fts5"]
"korean" = []
"eval" = []
"search" = ["ranking"]
"doc" = []
"test" = []
"hook" = []
"security" = []

[tag.alias]
"scoring" = "ranking"
"sqlite" = "index"

[tag.deny]
words = ["impl", "poc", "note"]

[scope]
"mem-search" = []
"aimemorytool" = []

[scope.alias]
"mem" = "mem-search"
`

// capture 는 명령이 stdout 에 찍은 것을 돌려준다.
func capture(t *testing.T, action func() int) (string, int) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	before := os.Stdout
	os.Stdout = writer
	code := action()
	os.Stdout = before
	writer.Close()
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

// addArgs 는 v0.2 규격을 다 갖춘 add 다. 관문이 기본으로 켜져 있어 필수 칸을
// 하나라도 빼면 거절이라, 시험도 진짜 기억처럼 갖춰서 넣는다 (설계 3-1).
func addArgs(body string) []string {
	return []string{"add", "--type", "issue", "--severity", "high", "--scope", "mem-search",
		"--tags", "index,korean", "--sources", "file:internal/token/token.go",
		"--author", "human:tester",
		"--summary", "두 글자 한글 검색어에 trigram 이 조용히 0건을 돌려줘 검색이 죽었다",
		"--title", "trigram 이 0건을 낸다", "--body", issueBody(body)}
}

// issueBody 는 issue 본문 규격(첫 줄 결론 · 증상 · 해결)을 갖춘 본문이다.
// 시험이 넘긴 글은 둘째 문단에 그대로 들어가 검색·비교에 쓰인다.
func issueBody(head string) string {
	return "두 글자 한글 검색어가 색인에 있는데도 0건으로 나오는 버그를 잡았다.\n\n" + head +
		"\n\n## 증상\n검색이 0건을 돌려준다.\n색인에는 그 낱말이 든 기억이 열두 건 있다.\n" +
		"\n## 해결\n질의 토크나이저가 두 글자를 안 버리게 고쳤다.\n회귀 시험을 하나 박아 뒀다."
}

// 설계 5-2 — add 는 store/ 를 안 건드리고 큐에만 쓴다. 그리고 최종 id 를 찍는다.
func TestAddQueuesAndPrintsID(t *testing.T) {
	memory := newRepo(t)
	out, code := capture(t, func() int { return run(addArgs("본문 한 줄")) })
	if code != 0 {
		t.Fatalf("add 가 실패했다 : %s", out)
	}
	id := strings.TrimSpace(out)
	if !model.IsID(id) {
		t.Fatalf("id 꼴이 아니다 : %q", id)
	}
	queued, err := os.ReadDir(filepath.Join(memory, "inbox", "new"))
	if err != nil || len(queued) != 1 {
		t.Fatalf("큐에 한 건이 있어야 한다 : %v %v", queued, err)
	}
	if _, err := os.Stat(filepath.Join(memory, "store", id[:4])); err == nil {
		t.Fatal("add 는 store/ 를 만들면 안 된다")
	}
	// 같은 큐 파일이면 같은 id 라야 승격이 두 벌을 안 만든다.
	again := model.QueueID(queued[0].Name(), issueBody("본문 한 줄"), id[:4]+"-"+id[4:6]+"-"+id[6:8])
	if again != id {
		t.Fatalf("id 가 큐 파일에서 다시 안 나온다 : %s %s", again, id)
	}
}

// 규격을 어긴 add 는 「쓰는 법 잘못(1)」 이 아니라 「품질 관문 거절(2)」 이다
// (설계 6-1 종료 코드 표).
func TestAddRefusesShortSummary(t *testing.T) {
	newRepo(t)
	code := run([]string{"add", "--type", "history", "--scope", "mem", "--tags", "mem", "--summary", "짧다", "--body", "본문"})
	if code != exitCheck {
		t.Fatalf("짧은 요약은 품질 관문 거절(2)이라야 한다 : %d", code)
	}
}

func TestShowReadsStoreFileByPath(t *testing.T) {
	memory := newRepo(t)
	wanted := model.Memory{ID: "20260822-3f9a2c1b", Type: model.TypeHistory, Date: "2026-08-22",
		Summary: strings.Repeat("가", 40), Tags: []string{"mem"}, LegacySource: model.LegacySourceAI,
		Scope: "mem", Body: "본문 한 줄"}
	path := storeFile(memory, wanted.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, model.Encode(&wanted), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := capture(t, func() int { return run([]string{"show", wanted.ID}) })
	if code != 0 || !strings.Contains(out, "본문 한 줄") {
		t.Fatalf("show 가 본문을 못 냈다 : %d %q", code, out)
	}
	if code := run([]string{"show", "../../AGENTS"}); code != 1 {
		t.Fatalf("경로 탈출은 막아야 한다 : %d", code)
	}
	if code := run([]string{"show", "20260822-aaaaaaaa"}); code != 1 {
		t.Fatalf("없는 기억은 1 이라야 한다 : %d", code)
	}
}

func TestStatusCountsStoreAndInbox(t *testing.T) {
	memory := newRepo(t)
	if _, code := capture(t, func() int { return run(addArgs("본문")) }); code != 0 {
		t.Fatal("add 가 실패했다")
	}
	// 훅도 PATH 도 없는 시험 저장소라 색인 건강은 나쁘다 (설계 9-5).
	out, code := capture(t, func() int { return run([]string{"status"}) })
	if code != exitCheck {
		t.Fatalf("status 종료 코드가 %d 다 : %s", code, out)
	}
	if !strings.Contains(out, memory) {
		t.Fatalf("저장소 자리가 안 나온다 : %s", out)
	}
	if !strings.Contains(out, "미승격 1") {
		t.Fatalf("대기 건수가 안 맞는다 : %s", out)
	}
}

// 설계 8-1 — 종료 코드. 아직 없는 명령과 모르는 명령을 갈라서 말한다.
func TestExitCodes(t *testing.T) {
	newRepo(t)
	if code := run([]string{"show"}); code != 1 {
		t.Fatalf("사용법이 잘못이면 1 이라야 한다 : %d", code)
	}
	if code := run([]string{"frobnicate"}); code != 1 {
		t.Fatalf("모르는 명령은 1 이라야 한다 : %d", code)
	}
	if _, code := capture(t, func() int { return run([]string{"help"}) }); code != 0 {
		t.Fatalf("help 는 0 이라야 한다 : %d", code)
	}
	if _, code := capture(t, func() int { return run([]string{"help", "add"}) }); code != 0 {
		t.Fatalf("help add 는 0 이라야 한다 : %d", code)
	}
	if code := run([]string{"help", "frobnicate"}); code != 1 {
		t.Fatalf("모르는 명령의 도움말은 1 이라야 한다 : %d", code)
	}
}

func TestStatusWithoutRepositoryIsThree(t *testing.T) {
	root := t.TempDir()
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Chdir(root)
	if code := run([]string{"status"}); code != 3 {
		t.Fatalf("저장소가 없으면 3 이라야 한다 : %d", code)
	}
}
