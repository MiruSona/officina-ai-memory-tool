package config

import (
	"strings"
	"testing"
)

// [canon] 은 [synonym] 옆, 곧 mem.toml 에 있다 (결정 25 · 조사E X6).
func TestCanonRoundTrip(t *testing.T) {
	want := Default("게임A")
	want.Canon = map[string]string{"인덱싱": "색인", "index": "색인"}
	text := string(Encode(want))
	if !strings.Contains(text, "[canon]") {
		t.Fatalf("[canon] 절이 있어야 한다 :\n%s", text)
	}
	got, err := Parse(text)
	if err != nil {
		t.Fatalf("다시 읽기 실패: %v", err)
	}
	if got.Canon["인덱싱"] != "색인" || got.Canon["index"] != "색인" {
		t.Fatalf("대표말 표가 %v 로 돌아왔다", got.Canon)
	}
}

// init 은 빈 [canon] 골격과 예시 주석을 쓴다. 손잡이가 파일에 안 보이면
// 아무도 못 채운다 ([synonym] 을 감췄다가 동의어가 0쌍이 된 자리와 같다).
func TestInitWritesEmptyCanonSkeleton(t *testing.T) {
	text := string(Encode(Default("게임A")))
	if !strings.Contains(text, "[canon]") {
		t.Fatalf("빈 표라도 절은 나와야 한다 :\n%s", text)
	}
	if !strings.Contains(text, "# \"인덱싱\"") {
		t.Fatalf("예시 주석이 있어야 한다 :\n%s", text)
	}
	got, err := Parse(text)
	if err != nil {
		t.Fatalf("다시 읽기 실패: %v", err)
	}
	if len(got.Canon) != 0 {
		t.Fatalf("주석은 낱말이 아니다: %v", got.Canon)
	}
}

// 표가 비면 정규화는 항등이고 아무것도 안 거절한다 (P12 「배우는 중」).
func TestEmptyCanonNormalizesToItself(t *testing.T) {
	config := Default("게임A")
	canon, err := config.CanonTable()
	if err != nil {
		t.Fatalf("빈 표는 오류가 아니다: %v", err)
	}
	if canon.Len() != 0 {
		t.Fatalf("빈 표인데 %d 줄이다", canon.Len())
	}
}

// 잘못 쓴 표는 조용히 넘기지 않는다 — 대표말이 어긋나면 색인과 질의가
// 다른 글자가 돼 「조용히 틀린 답」이 나온다.
func TestParseRejectsCanonCycle(t *testing.T) {
	if _, err := Parse("[canon]\n\"인덱싱\" = \"색인\"\n\"색인\" = \"index\"\n"); err == nil {
		t.Fatal("순환하는 표는 거절해야 한다")
	}
	if _, err := Parse("[canon]\n\"인덱싱\" = \"\"\n"); err == nil {
		t.Fatal("빈 대표말은 거절해야 한다")
	}
}
