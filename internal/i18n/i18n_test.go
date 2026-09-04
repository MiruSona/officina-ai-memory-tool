package i18n

import (
	"strings"
	"testing"
)

// 표에 든 문장은 전부 한글이어야 하고, 서식 자리가 남아 있으면 안 된다.
func TestEveryMessageIsKorean(t *testing.T) {
	for key, text := range messages {
		if strings.TrimSpace(text) == "" {
			t.Errorf("%s 에 문장이 없다", key)
		}
		// 파일 이름·경로처럼 낱말 하나짜리 값은 한글일 수 없다.
		if strings.Contains(text, " ") && !strings.ContainsFunc(text, isHangul) {
			t.Errorf("%s 가 한글이 아니다 : %s", key, text)
		}
	}
}

func isHangul(letter rune) bool {
	return letter >= '가' && letter <= '힣'
}

func TestFillsArguments(t *testing.T) {
	got := T(BadTagCount, 7)
	if !strings.Contains(got, "7") || strings.Contains(got, "%d") {
		t.Fatalf("숫자가 안 들어갔다 : %s", got)
	}
	if plain := T(BadFrontMatter); strings.Contains(plain, "%") {
		t.Fatalf("인자 없는 문장이 망가졌다 : %s", plain)
	}
	if unknown := T(Key("없는-키")); unknown != "없는-키" {
		t.Fatalf("모르는 키는 이름 그대로여야 한다 : %s", unknown)
	}
}
