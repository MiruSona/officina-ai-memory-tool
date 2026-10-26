package main

// retain 후보 하나를 자식 `mem add --origin retain:strata` 로 관문에 건다 (A2 retain 설계 5절
// 「부르는 방식」). 저장 배선(큐·락·색인)을 다시 짜지 않으려고 프로세스를 하나 띄운다.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
	"github.com/mirusona/officina-ai-memory-tool/internal/retain"
	"github.com/mirusona/officina-ai-memory-tool/internal/safe"
)

// retainOrigin · retainAuthor 는 retain 이 넣는 기억의 출처 칸이다 (설계 6절).
const (
	retainOrigin = "retain:strata"
	retainAuthor = "mem-retain/1"
)

// retainSeverity 는 caution 후보에 다는 severity 기본값이다.
const retainSeverity = "mid"

// childErrClip 은 자식 오류 글을 retain-log 에 실을 때의 글자 상한이다.
const childErrClip = 200

// childExecutable 은 자식으로 띄울 mem 실행 파일이다. 시험에서는 시험 바이너리를 가리키고,
// TestMain 이 MEM_TEST_CHILD=1 을 보고 명령으로 돈다.
var childExecutable = os.Executable

// childResult 는 자식 add 한 번의 결과다. Passed 면 저장됐거나(--check 면 통과) ID 가 붙는다.
type childResult struct {
	Passed  bool
	ID      string
	Rejects []quality.Finding
	// Truncated 는 자식 R3 의 NLI 답 중 하나라도 입력을 잘랐다고 했는지다 (retain-log 칸).
	Truncated bool
}

// childJSON 은 add --json 이 내는 두 꼴(addVerdictJSON · autoVerdictJSON)을 한 번에 읽는 꼴이다.
type childJSON struct {
	Kind     quality.Kind      `json:"kind"`
	Findings []quality.Finding `json:"findings"`
	Auto     retain.Result     `json:"auto"`
	ID       string            `json:"id"`
}

// childSources 는 후보 하나의 sources 다. note:transcript 한 칸은 늘 넣고, 생성이 적은 files 중
// root 기준 실재 상대 경로만 file: 로 싣는다 (R8 과 같은 판정). 뺀 수를 같이 돌려준다.
func childSources(root, short string, chunk int, files []string) ([]string, int) {
	sources := []string{model.SourceNote + "transcript " + short + "#" + strconv.Itoa(chunk)}
	dropped := 0
	for _, file := range files {
		if strings.TrimSpace(file) == "" {
			continue
		}
		// sources 는 쉼표로 가르므로 쉼표가 든 경로는 실을 수 없다.
		if strings.Contains(file, ",") || !retain.PathExists(root, file) {
			dropped++
			continue
		}
		sources = append(sources, model.SourceFile+strings.TrimSpace(file))
	}
	return sources, dropped
}

// childArgs 는 후보 하나의 add 인자다. 본문은 인자 대신 표준입력으로 넘긴다
// (Windows 인자 길이·따옴표 문제).
func childArgs(repoDir, short string, candidate retain.GenCandidate, sources []string, dry bool) []string {
	args := []string{"add", "--repo", repoDir, "--origin", retainOrigin, "--session", short,
		"--json", "--stdin", "--quotes", strings.Join(childQuotes(candidate.Quotes), quoteSeparator),
		"--author", retainAuthor, "--type", candidate.Type, "--title", candidate.Title,
		"--summary", candidate.Summary, "--tags", strings.Join(candidate.Tags, ","),
		"--scope", candidate.Scope, "--sources", strings.Join(sources, ",")}
	// caution 은 severity 가 꼭 있어야 하는데 생성 꼴(설계 4절)에는 그 칸이 없다 — 가운데 값으로 둔다.
	if candidate.Type == model.TypeCaution {
		args = append(args, "--severity", retainSeverity)
	}
	if dry {
		args = append(args, "--check")
	}
	return args
}

// testChildEnv 는 시험 바이너리가 자식 명령으로 돌지 가르는 시험 전용 변수다. MEM_RETAIN_CHILD 는
// 진짜 자식 세션에도 켜져 있어, 그 셸에서 go test 를 돌리면 시험 대신 명령이 돈다.
const testChildEnv = "MEM_TEST_CHILD"

// 자식 한 판의 시간 여유·하한과, `--quotes` 인자에 싣는 인용 합계 글자 상한(Windows 명령 줄 32K 안)이다.
const (
	childSlack       = 60 * time.Second
	childMinDuration = 90 * time.Second
	childQuoteRoom   = 2000
)

// childTimeout 은 자식 하나를 기다릴 상한이다. R3 판정이 인용 수 + 2 번(묶음 · 제목)까지 간다.
func childTimeout(quotes, nliTimeoutMS int) time.Duration {
	limit := time.Duration(nliTimeoutMS)*time.Millisecond*time.Duration(quotes+2) + childSlack
	if limit < childMinDuration {
		return childMinDuration
	}
	return limit
}

// childQuotes 는 인용 합계가 childQuoteRoom 을 넘지 않게 자른다. 넘는 인용은 남은 자리만큼 앞을
// 남긴다 — 글자 그대로인 문장의 앞부분이라 R1 대조는 그대로 맞는다.
func childQuotes(quotes []string) []string {
	kept := []string{}
	room := childQuoteRoom
	for _, quote := range quotes {
		runes := []rune(quote)
		if len(runes) > room {
			runes = runes[:room]
		}
		if len(runes) == 0 {
			break
		}
		kept = append(kept, string(runes))
		room -= len(runes)
	}
	return kept
}

// runChild 는 자식 add 를 돌려 결과를 읽는다. 종료 0 은 저장(또는 --check 통과), 2·4 는 거절이다.
// 그 밖의 종료 코드나 JSON 이 없는 답 · 시간 넘김은 오류다 — 부르는 쪽이 retain-log 에 남긴다.
func runChild(args []string, body string, timeout time.Duration) (childResult, error) {
	executable, err := childExecutable()
	if err != nil {
		return childResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, executable, args...)
	command.Env = append(os.Environ(), retain.ChildEnv+"=1", testChildEnv+"=1")
	command.Stdin = strings.NewReader(body)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	code := exitOK
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return childResult{}, fmt.Errorf("child timeout %s", timeout)
		}
		exitErr := &exec.ExitError{}
		if !errors.As(err, &exitErr) {
			return childResult{}, err
		}
		code = exitErr.ExitCode()
	}
	parsed, found := lastJSON(stdout.String())
	if !found || (code != exitOK && code != exitCheck && code != exitSecurity) {
		return childResult{}, fmt.Errorf("child exit %d: %s", code, safe.Clip(safe.OneLine(stderr.String()), childErrClip))
	}
	if code == exitOK {
		return childResult{Passed: true, ID: parsed.ID, Truncated: parsed.Auto.Truncated}, nil
	}
	return childResult{Rejects: rejectsOf(parsed, code), Truncated: parsed.Auto.Truncated}, nil
}

// lastJSON 은 stdout 에서 마지막으로 읽히는 JSON 줄을 고른다. 자식 --json 은 판정 JSON 한 줄을
// 내고, 저장 때는 그 뒤에 id 한 줄이 붙는다. 앞에 사람용 글이 섞여도 판정 줄을 잡는다.
func lastJSON(text string) (childJSON, bool) {
	lines := strings.Split(text, "\n")
	for at := len(lines) - 1; at >= 0; at-- {
		line := strings.TrimSpace(lines[at])
		if !strings.HasPrefix(line, "{") {
			continue
		}
		parsed := childJSON{}
		if json.Unmarshal([]byte(line), &parsed) == nil {
			return parsed, true
		}
	}
	return childJSON{}, false
}

// rejectsOf 는 자동 관문 거절과 기존 관문의 거절 등급 판정을 한 목록으로 모은다.
// 거절 종료인데 둘 다 비면 종료 코드를 규칙 이름으로 남긴다 — 셈에서 사라지지 않게.
func rejectsOf(parsed childJSON, code int) []quality.Finding {
	rejects := []quality.Finding{}
	for _, one := range parsed.Auto.Rejects {
		rejects = append(rejects, quality.Finding{Rule: one.Rule, Reason: one.Reason})
	}
	for _, one := range parsed.Findings {
		if one.Level == quality.GradeReject {
			rejects = append(rejects, one)
		}
	}
	if len(rejects) == 0 {
		rejects = append(rejects, quality.Finding{Rule: "exit-" + strconv.Itoa(code)})
	}
	return rejects
}
