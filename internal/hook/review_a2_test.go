package hook

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 리뷰 A(2회차) H-1 마지막 방어선 — 색인에 어떻게 들어왔든 비밀정보가 든
// 요약은 훅 블록에 한 줄도 안 실린다. 줄을 만드는 자리를 바로 부른다.
func TestSecretSummaryNeverBecomesALine(t *testing.T) {
	scanner := secretScanner(config.Default("시험").Secret)
	rows := []index.HookRow{
		{ID: "20260823-11112222", Type: model.TypeDecision, Date: "2026-08-23", Scope: "openapi",
			Summary: "배포에는 ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123 을 쓰기로 정했다"},
		{ID: "20260823-33334444", Type: model.TypeDecision, Date: "2026-08-23", Scope: "openapi",
			Summary: "이 결정은 멀쩡해서 블록에 실려야 한다"},
	}
	lines := linesOf(rows, map[string]bool{}, 5, scanner)
	if len(lines) != 1 {
		t.Fatalf("한 줄만 남아야 한다 : %v", lines)
	}
	if strings.Contains(lines[0], "ghp_") {
		t.Fatalf("비밀정보가 줄에 실렸다 : %s", lines[0])
	}
	if !strings.Contains(lines[0], "멀쩡해서") {
		t.Fatalf("멀쩡한 기억까지 빠졌다 : %s", lines[0])
	}
}

// 설계 6-1 `--budget` — 손으로 준 예산이 계기별 기본값을 이긴다.
func TestBudgetFlagOverridesTheEvent(t *testing.T) {
	dir := filled(t)
	out := bytes.Buffer{}
	if code := Run([]string{"session-start", "--json", "--budget", "120"},
		strings.NewReader(startInput(dir)), &out); code != 0 {
		t.Fatalf("훅은 늘 0 이어야 한다 : %d", code)
	}
	if !strings.Contains(out.String(), `"budget":120`) {
		t.Fatalf("--budget 이 안 먹었다 : %s", out.String())
	}
}

// 사람이 잘못 친 이벤트 이름은 말없이 빈 출력이 아니라 사용법 오류다. 등록된
// 훅 경로(stdin 에 hook_event_name 이 있는 것)는 그대로 0 이다.
func TestHandTypedEventNameFails(t *testing.T) {
	dir := newRepo(t)
	out := bytes.Buffer{}
	if code := Run([]string{"startup"}, strings.NewReader(""), &out); code != 1 {
		t.Fatalf("사람이 잘못 친 것은 1 이어야 한다 : %d", code)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout 은 비어 있어야 한다 : %q", out.String())
	}
	out.Reset()
	if code := Run([]string{"startup"}, strings.NewReader(startInput(dir)), &out); code != 0 {
		t.Fatalf("훅 JSON 이 있으면 0 이어야 한다 : %d", code)
	}
	out.Reset()
	if code := Run([]string{"help"}, strings.NewReader(""), &out); code != 0 {
		t.Fatalf("도움말은 0 이어야 한다 : %d", code)
	}
	if out.Len() != 0 {
		t.Fatalf("도움말이 stdout 으로 갔다 : %q", out.String())
	}
}

// 콘솔에서 손으로 치면 표준입력이 EOF 를 안 줘서 매달렸다. 콘솔이면 안 읽는다.
func TestTerminalStdinIsNotRead(t *testing.T) {
	if isTerminal(strings.NewReader("x")) {
		t.Fatal("파이프를 콘솔로 봤다")
	}
	file, err := os.Open(os.DevNull)
	if err != nil {
		t.Skip("os.DevNull 을 못 연다")
	}
	defer file.Close()
	// NUL 은 문자 장치라 콘솔과 같은 갈래로 잡힌다 — 판정이 도는지만 본다.
	_ = isTerminal(file)
}
