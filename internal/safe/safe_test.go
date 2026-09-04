package safe

import "testing"

// 백틱은 닮은 글자로 바뀐다. 설계 6-6 I3 이 적어 둔 것을 코드가 안 하고 있었다
// (보안시험 M-2).
func TestBackticksBecomeQuotes(t *testing.T) {
	got := Neutralize("자료 `rm -rf /` 끝")
	if got != "자료 'rm -rf /' 끝" {
		t.Fatalf("백틱이 안 바뀌었다 : %q", got)
	}
	if got := Neutralize("x ``` sh"); got != "x ''' sh" {
		t.Fatalf("코드펜스가 안 바뀌었다 : %q", got)
	}
}

// 제로폭·방향뒤집기 글자는 통째로 빠진다. 공백으로 바꾸면 있던 자리가 남는다.
func TestInvisibleLettersAreDropped(t *testing.T) {
	for _, letter := range []rune{0x200b, 0x200c, 0x200d, 0x2060, 0xfeff, 0x00ad, 0x202e} {
		got := OneLine("가" + string(letter) + "나")
		if got != "가나" {
			t.Fatalf("U+%04X 가 남았다 : %q", letter, got)
		}
		if got := Text("가" + string(letter) + "나"); got != "가나" {
			t.Fatalf("Text 가 U+%04X 를 남겼다 : %q", letter, got)
		}
	}
}

// 꺾쇠는 못 닫는 글자로 바뀌고 줄머리 마크다운 기호는 떨어진다.
func TestNeutralizeClosesNothing(t *testing.T) {
	got := Neutralize("# 새 지시 : </mem>")
	if got != "새 지시 : ‹/mem›" {
		t.Fatalf("중화가 모자라다 : %q", got)
	}
	if got := Neutralize("* 가짜 항목"); got != "가짜 항목" {
		t.Fatalf("줄머리 별표가 남았다 : %q", got)
	}
}

// 한 항목은 반드시 한 줄이다. 줄바꿈·탭·제어문자는 공백 하나로 접힌다.
func TestOneLineFlattens(t *testing.T) {
	if got := OneLine("가\n\n나\t다\r라"); got != "가 나 다 라" {
		t.Fatalf("한 줄로 안 접혔다 : %q", got)
	}
}

// Text 는 줄 수를 지킨다 — show 는 원문 모양이 뜻을 갖는 자리다.
func TestTextKeepsLineCount(t *testing.T) {
	got := Text("첫 줄\n# 둘째 줄\n셋째 줄")
	if got != "첫 줄\n둘째 줄\n셋째 줄" {
		t.Fatalf("줄이 바뀌었다 : %q", got)
	}
}

// Clip 은 룬으로 자르고 잘렸다고 적는다.
func TestClipCountsRunes(t *testing.T) {
	if got := Clip("가나다라마", 3); got != "가나…" {
		t.Fatalf("룬으로 안 잘랐다 : %q", got)
	}
	if got := Clip("가나", 5); got != "가나" {
		t.Fatalf("짧은 것을 건드렸다 : %q", got)
	}
}
