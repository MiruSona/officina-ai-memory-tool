package retain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadUtterancesKeepsTalkInOrder(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "s.jsonl")
	writeTranscript(t, record, 0, 0,
		transcriptLine(t, "user", "<system-reminder>숨은 글 ZZZ</system-reminder>첫 사용자 말이다"),
		transcriptLine(t, "assistant", []any{map[string]any{"type": "text", "text": "첫 AI 답이다"},
			toolUse("Bash", map[string]any{"command": "echo 도구입력 555"})}),
		transcriptLine(t, "user", []any{map[string]any{"type": "tool_result", "content": "도구 결과 777"}}),
		transcriptLine(t, "assistant", "이어진 AI 답이다"),
		transcriptLine(t, "user", "둘째 사용자 말이다"))
	got, err := ReadUtterances(record)
	if err != nil {
		t.Fatal(err)
	}
	roles := []string{}
	all := ""
	for _, one := range got {
		roles = append(roles, one.Role)
		all += one.Text + "\n"
	}
	if strings.Join(roles, ",") != "user,assistant,user" {
		t.Fatalf("역할 차례가 틀렸다 (같은 역할 이어짐은 하나로) : %v", roles)
	}
	if strings.Contains(all, "ZZZ") {
		t.Fatal("system-reminder 덩이가 남았다")
	}
	if strings.Contains(all, "777") || strings.Contains(all, "555") {
		t.Fatal("도구 입력·결과는 빠져야 한다")
	}
	if !strings.Contains(got[1].Text, "첫 AI 답이다") || !strings.Contains(got[1].Text, "이어진 AI 답이다") {
		t.Fatalf("이어진 AI 글을 잃었다 : %q", got[1].Text)
	}
}

func TestReadUtterancesReadsTheEnd(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "big.jsonl")
	text := strings.Builder{}
	for at := range 200 {
		text.WriteString(transcriptLine(t, "user", fmt.Sprintf("앞쪽 채움 글 %03d 번째 줄", at)))
	}
	text.WriteString(transcriptLine(t, "assistant", "맨 끝의 AI 답"))
	if err := os.WriteFile(record, []byte(text.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	saved := wholeRoom
	wholeRoom = 2000
	defer func() { wholeRoom = saved }()
	got, err := ReadUtterances(record)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[len(got)-1].Text != "맨 끝의 AI 답" {
		t.Fatalf("끝 글이 빠졌다 : %v", got)
	}
	if strings.Contains(got[0].Text, "000 번째") {
		t.Fatal("상한보다 앞 글을 읽었다")
	}
}

func TestReadUtterancesRefusesNonTranscript(t *testing.T) {
	if _, err := ReadUtterances(filepath.Join(t.TempDir(), "a.txt")); err == nil {
		t.Fatal(".jsonl 아닌 파일을 읽었다")
	}
}

// 문맥 압축 뒤 하네스가 넣는 요약 줄(isCompactSummary)은 사람 말이 아니라 뺀다 — 실제 기록에 있다 (리뷰 13 #9).
func TestReadUtterancesSkipsCompactSummary(t *testing.T) {
	record := filepath.Join(t.TempDir(), "s.jsonl")
	summary := `{"type":"user","isCompactSummary":true,"message":{"role":"user","content":"This session is being continued 요약 QQQ"}}` + "\n"
	writeTranscript(t, record, 0, 0, summary,
		transcriptLine(t, "user", "압축 뒤 사용자 말이다"),
		transcriptLine(t, "assistant", "압축 뒤 AI 답이다"))
	got, err := ReadUtterances(record)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || strings.Contains(got[0].Text, "QQQ") || got[0].Text != "압축 뒤 사용자 말이다" {
		t.Fatalf("압축 요약 줄이 남았다 : %+v", got)
	}
}
