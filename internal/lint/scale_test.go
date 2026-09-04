package lint

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// 규모 시험은 기본으로 작게 돈다. 크게 재려면 MEM_LINT_SIZES=1000,5000,20000 를
// 환경 변수로 준다 (설계 13-2 : 2만 건 10초 안).
const defaultSizes = "300"

// scaleBudget 은 한 건당 허용 시간이다. 2만 건 10초를 건당으로 나눈 값이라
// 작은 규모로 돌려도 같은 자로 잰다.
const scaleBudget = 10 * time.Second / 20000

// alike 는 서로 닮은 말뭉치를 만들 때 쓰는 문장 통이다. 우리 저장소가 꼭
// 이런 꼴이다 — 같은 결정을 여러 각도에서 적은 기억이 많다.
var alike = []string{
	"색인은 파생물이라 지워도 된다. mem index --full 로 완전히 복구된다.",
	"store 를 고쳐 쓰는 것은 index.lock 을 잡은 단 하나의 프로세스뿐이다.",
	"훅은 어떤 이유로도 세션을 막지 않는다. 못 하겠으면 아무것도 안 찍고 끝낸다.",
	"기억 파일은 머리말과 본문으로 나뉜다. 머리말은 yaml 로 쓴다.",
	"검색은 라우팅과 완화 사다리와 RRF 합치기로 돌아간다.",
	"0건은 반드시 왜 0건인지 말한다. 조용한 0건은 잘못을 숨긴다.",
	"gc 는 파일을 지우지 않는다. 본문만 접고 원본은 아카이브에 남긴다.",
	"태그는 영어 소문자 1~5개다. scope 는 아껴 쓴다.",
}

// TestLintScale 은 닮은 말뭉치와 안 닮은 말뭉치에서 벽시계를 잰다. v0.0 은
// 닮은 5,000건에 39.8초였다 (조사E 5-1).
func TestLintScale(t *testing.T) {
	sizes := os.Getenv("MEM_LINT_SIZES")
	if sizes == "" {
		sizes = defaultSizes
	}
	// 시간을 따지는 것은 규모를 손으로 준 때뿐이다. 기본 300건은 임시 폴더가
	// 바이러스 검사에 걸리는 기계에서 벽시계가 널뛰어 자로 쓸 수 없다.
	strict := os.Getenv("MEM_LINT_SIZES") != ""
	for _, text := range strings.Split(sizes, ",") {
		count, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil {
			t.Fatal(err)
		}
		measure(t, count, true, strict)
		measure(t, count, false, strict)
	}
}

func measure(t *testing.T, count int, similar, strict bool) {
	t.Helper()
	shape := "다름"
	if similar {
		shape = "닮음"
	}
	t.Run(fmt.Sprintf("%d건-%s", count, shape), func(t *testing.T) {
		opened := makeCorpus(t, count, similar)
		report := lintOnce(t, opened)
		// 규칙에 쓴 시간과 파일을 읽는 데 쓴 시간을 갈라 적는다. 바이러스
		// 검사가 도는 기계에서는 임시 폴더 읽기가 벽시계를 통째로 덮는다.
		rules := report.Elapsed - report.ReadElapsed
		t.Logf("%d건 %s : 모두 %.2f초 (읽기 %.2f초 · 규칙 %.2f초) · 걸린 것 %d건 %v",
			count, shape, report.Elapsed.Seconds(), report.ReadElapsed.Seconds(),
			rules.Seconds(), len(report.Problems), report.CountByRule())
		// 규모와 상관없는 붙박이 비용이 있어 1초를 얹어 준다. 예산은 규칙
		// 시간에 건다 — 디스크가 느린 것은 lint 가 고칠 수 있는 것이 아니다.
		budget := time.Duration(count)*scaleBudget + time.Second
		if strict && rules > budget {
			t.Fatalf("%d건 규칙 검사가 %.2f초 걸렸다. 예산 %.2f초를 넘었다",
				count, rules.Seconds(), budget.Seconds())
		}
	})
}

func makeCorpus(t *testing.T, count int, similar bool) *store.Store {
	t.Helper()
	dir := t.TempDir()
	opened := store.Open(filepath.Join(dir, "Memory"), false)
	if err := opened.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(opened.StoreDir(), "2026", "08")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	source := rand.New(rand.NewSource(int64(count)))
	for at := 0; at < count; at++ {
		id := fmt.Sprintf("20260820-%08x", at+1)
		text := frontMatterFor(id) + bodyFor(source, similar) + "\n"
		if err := os.WriteFile(filepath.Join(folder, id+".md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return opened
}

func frontMatterFor(id string) string {
	return "---\nid: " + id + "\ntype: history\ndate: 2026-08-20\n" +
		"summary: 규모 시험용으로 만든 가짜 기억이다. 요약은 규격이 요구하는 길이를 채우려고 이렇게 길게 쓴다\n" +
		"tags: [scale, test]\nsource: ai\nscope: mem-lint\n---\n\n"
}

// bodyFor 는 닮은 말뭉치와 안 닮은 말뭉치를 만든다. 닮은 쪽은 문장 통에서
// 골라 섞고, 안 닮은 쪽은 문서마다 다른 낱말을 쓴다.
func bodyFor(source *rand.Rand, similar bool) string {
	lines := []string{}
	for line := 0; line < 12; line++ {
		if similar {
			lines = append(lines, alike[source.Intn(len(alike))])
			continue
		}
		lines = append(lines, randomWords(source, 12))
	}
	return strings.Join(lines, "\n")
}

// randomWords 는 문서마다 겹치는 조각이 거의 없게 한글 음절을 골라 낱말을
// 만든다. 틀에 박힌 문장을 쓰면 "안 닮은 말뭉치" 인데도 3-gram 이 겹친다.
func randomWords(source *rand.Rand, count int) string {
	words := make([]string, 0, count)
	for at := 0; at < count; at++ {
		letters := make([]rune, 3)
		for index := range letters {
			letters[index] = rune(0xAC00 + source.Intn(11172))
		}
		words = append(words, string(letters))
	}
	return strings.Join(words, " ")
}

func lintOnce(t *testing.T, opened *store.Store) *Report {
	t.Helper()
	report, err := Run(Options{Store: opened, Config: config.Default("aimemorytool"), NoGit: true})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// TestCandidateLimit 은 닮은 것이 너무 많은 문서에서 비교를 그만두고 경고 한
// 줄로 끝내는지 본다 (설계 9-2 후보 상한 50).
func TestCandidateLimit(t *testing.T) {
	dir := t.TempDir()
	opened := store.Open(filepath.Join(dir, "Memory"), false)
	if err := opened.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(opened.StoreDir(), "2026", "08")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join(alike, "\n")
	for at := 0; at < 70; at++ {
		id := fmt.Sprintf("20260820-%08x", at+1)
		text := frontMatterFor(id) + body + fmt.Sprintf("\n%d 번 기억이다.\n", at)
		if err := os.WriteFile(filepath.Join(folder, id+".md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	report := lintOnce(t, opened)
	// 후보 상한(문서당 50)이 있어 70건이 서로 다 닮아도 O(n²) 로 안 간다.
	// 걸리기는 걸려야 한다 — 상한이 규칙을 통째로 끄면 안 된다.
	found := 0
	for _, problem := range report.Problems {
		if problem.Rule == quality.RuleDuplicateHard || problem.Rule == quality.RuleDuplicateSoft {
			found++
		}
	}
	if found == 0 {
		t.Fatalf("70건이 서로 닮았는데 중복이 하나도 안 걸렸다 : %d건", len(report.Problems))
	}
	if report.Elapsed > 5*time.Second {
		t.Fatalf("70건에 %.2f초 걸렸다", report.Elapsed.Seconds())
	}
}
