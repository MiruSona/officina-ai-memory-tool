package secret

import (
	"strings"
	"testing"
)

// A2 retain 가리기 — 막는 검사(ScanLine)와 같은 규칙·guard 로 가린다 (리뷰 13 #1).
func TestRedactHidesRealKeys(t *testing.T) {
	cases := []string{
		"키는 sk-ant-abcdefghijklmnopqrstuvwxyz0123 이다",
		"토큰 ghp_abcdefghijklmnopqrstuvwxyz0123 을 썼다",
		"AWS AKIAABCDEFGHIJKLMNOP 가 보였다",
		"슬랙 xoxb-1234567890abc 이다",
		"password = hunter2hunter2",
	}
	scanner := defaultScanner()
	for _, text := range cases {
		got, count := scanner.Redact(text)
		if count != 1 || !strings.Contains(got, Redacted) {
			t.Errorf("가려야 한다 : %q → %q (%d)", text, got, count)
		}
	}
}

func TestRedactHidesBuiltinWebhooks(t *testing.T) {
	cases := []string{
		"알림 https://hooks.slack.com/services/T0000000/B0000000/XXXXXXXXXXXXXXXXXXXXXXXX 로 보낸다",
		"https://discord.com/api/webhooks/000000000000000000/XXXXXXXXXXXXXXXXXXXXXXXX",
		"https://hooks.zapier.com/hooks/catch/123456/abcdef",
	}
	scanner := New(nil)
	for _, text := range cases {
		got, count := scanner.Redact(text)
		if count == 0 || strings.Contains(got, "XXXXXXXXXXXX") || strings.Contains(got, "abcdef") {
			t.Errorf("builtin 웹훅을 가려야 한다 : %q → %q (%d)", text, got, count)
		}
	}
}

func TestRedactKeepsLookalikes(t *testing.T) {
	cases := []string{
		"커밋 3f9a2c1b4d5e6f708192a3b4c5d6e7f809182736 을 봤다",
		"token = 1200 으로 줄였다",
		"max token: 1500 이 상한이다",
		"BattleFlowControllerFactoryProviderRegistry 를 고쳤다",
		"훅 지연이 길어서 상태 파일을 오프셋으로 읽게 바꾸자",
	}
	scanner := defaultScanner()
	for _, text := range cases {
		if got, count := scanner.Redact(text); got != text || count != 0 {
			t.Errorf("guard 를 못 넘는 글은 그대로다 : %q → %q (%d)", text, got, count)
		}
	}
}

func TestRedactCountsEachHitAcrossLines(t *testing.T) {
	text := "AKIAABCDEFGHIJKLMNOP 와 AKIAQRSTUVWXYZ234567 둘\n둘째 줄 ghp_abcdefghijklmnopqrstuvwxyz0123"
	got, count := defaultScanner().Redact(text)
	if count != 3 || strings.Contains(got, "AKIA") || strings.Contains(got, "ghp_") {
		t.Fatalf("셋 다 가리고 셋을 세야 한다 : %q %d", got, count)
	}
	if strings.Count(got, "\n") != 1 {
		t.Fatalf("줄바꿈은 그대로다 : %q", got)
	}
}
