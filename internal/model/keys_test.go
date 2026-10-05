package model

import (
	"strings"
	"testing"
)

// keys 칸(C1) — 머리말에 한 줄로 쓰고 그대로 읽힌다. 비면 줄 자체가 없다.
func TestKeysRoundTrip(t *testing.T) {
	memory := &Memory{ID: "20261005-aabbccdd", Type: TypeHistory, Title: "keys 칸 왕복 시험",
		Summary: "keys 칸이 머리말에 한 줄로 쓰이고 다시 읽을 때 그대로 돌아오는지 보는 시험",
		Tags:    []string{"mem", "search"}, Keys: []string{"reindex", "재빌드 절차"}, Scope: "mem",
		Date: "2026-10-05", Author: "human:tester", Body: "본문", Spec: SpecV2}
	data := Encode(memory)
	if !strings.Contains(string(data), "keys: [reindex, 재빌드 절차]") {
		t.Fatalf("keys 가 한 줄로 안 쓰였다 :\n%s", data)
	}
	back, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Keys) != 2 || back.Keys[1] != "재빌드 절차" {
		t.Fatalf("keys 가 안 돌아왔다 : %v", back.Keys)
	}
	memory.Keys = nil
	if strings.Contains(string(Encode(memory)), "keys:") {
		t.Fatal("빈 keys 가 줄로 남았다 — 옛 기억 파일이 말없이 바뀐다")
	}
}

func TestKeyProblems(t *testing.T) {
	good := []string{"ab", strings.Repeat("가", 30), "두 낱말"}
	if problems := KeyProblems(good); len(problems) != 0 {
		t.Fatalf("멀쩡한 keys 를 막았다 : %v", problems)
	}
	for _, bad := range [][]string{{"a"}, {strings.Repeat("가", 31)}, {"가,나"}, {"줄\n바꿈"}, {" 앞공백"},
		{"a1", "b2", "c3", "d4", "e5", "f6", "g7"}} {
		if problems := KeyProblems(bad); len(problems) == 0 {
			t.Fatalf("틀린 keys %q 를 통과시켰다", bad)
		}
	}
}
