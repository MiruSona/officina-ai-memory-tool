package model

import (
	"strings"
	"testing"
)

// origin 칸은 자동 기억의 출처다 (자동쌓기설계 2-1). 쓰고 읽어도 같아야 하고,
// 틀린 값은 검사에서 걸려야 한다.
func TestOriginRoundTrip(t *testing.T) {
	memory := &Memory{ID: "20261005-aaaa0001", Type: TypeHowto, Date: "2026-10-05",
		Title: "자동 기억 출처 칸", Summary: "요약", Tags: []string{"index", "korean"}, Scope: "mem",
		Author: "mem/0.5.0", Spec: SpecV2, Origin: "retain:qwen3.6", OriginSession: "c0ffee00",
		Body: "본문"}
	encoded := Encode(memory)
	if !strings.Contains(string(encoded), "origin: retain:qwen3.6") {
		t.Fatalf("origin 이 안 쓰였다 :\n%s", encoded)
	}
	back, err := Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if back.Origin != memory.Origin || back.OriginSession != memory.OriginSession {
		t.Fatalf("다시 읽은 값이 다르다 : %q %q", back.Origin, back.OriginSession)
	}
	plain := *memory
	plain.Origin, plain.OriginSession = "", ""
	if strings.Contains(string(Encode(&plain)), "origin") {
		t.Fatal("사람 기억에 origin 칸이 생겼다")
	}
}

func TestOriginValues(t *testing.T) {
	for _, good := range []string{"stop", "card", "retain:qwen3.6", "consolidate:flash-next"} {
		if !IsOrigin(good) {
			t.Errorf("%s 를 못 받았다", good)
		}
	}
	for _, bad := range []string{"", "robot", "retain:", "retain:../x", "stop "} {
		if IsOrigin(bad) {
			t.Errorf("%q 를 받았다", bad)
		}
	}
}
