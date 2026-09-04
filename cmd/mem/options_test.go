package main

import (
	"strings"
	"testing"
)

// 모르는 옵션은 다음 인자를 값으로 삼키지 않고 바로 실패한다. 조용히 먹으면
// 사람이 진짜 원인을 못 찾는다 (실데이터 시험 7절 #8).
func TestUnknownOptionIsRefused(t *testing.T) {
	_, err := parseOptions([]string{"--no-global", "--type", "decision"}, addBools, addValues)
	if err == nil {
		t.Fatal("모르는 옵션을 받아 줬다")
	}
	if !strings.Contains(err.Error(), "--no-global") {
		t.Fatalf("어느 옵션이 잘못됐는지 안 알려준다 : %v", err)
	}
	if _, err := parseOptions([]string{"--모름=1"}, addBools, addValues); err == nil {
		t.Fatal("`--이름=값` 꼴도 가려야 한다")
	}
}

// 명령마다 적어 둔 옵션은 전부 통과해야 한다. 하나라도 빠지면 멀쩡한 명령이
// "모르는 옵션" 으로 죽는다.
func TestEveryDeclaredOptionParses(t *testing.T) {
	for name, item := range registry {
		argv := []string{}
		for _, flag := range item.bools {
			argv = append(argv, "--"+flag)
		}
		for _, value := range item.values {
			argv = append(argv, "--"+value, "x")
		}
		if _, err := parseOptions(argv, item.bools, item.values); err != nil {
			t.Fatalf("%s : 자기가 적어 둔 옵션을 못 읽는다 : %v", name, err)
		}
	}
}

// 값 옵션과 불리언 옵션이 겹치면 안 된다 — 어느 쪽으로 읽을지 갈린다.
func TestBoolAndValueDoNotOverlap(t *testing.T) {
	for name, item := range registry {
		for _, flag := range item.bools {
			if contains(item.values, flag) {
				t.Fatalf("%s : `--%s` 가 불리언이면서 값 옵션이다", name, flag)
			}
		}
	}
}

// 13개 명령이 다 붙어 있고 저마다 옵션 목록을 적어 뒀는지.
func TestEveryCommandDeclaresOptions(t *testing.T) {
	want := []string{"install", "init", "add", "set", "search", "show", "hook",
		"index", "gc", "lint", "eval", "status"}
	for _, name := range want {
		item, found := registry[name]
		if !found {
			t.Errorf("%s 가 등록이 안 됐다", name)
			continue
		}
		if item.bools == nil && item.values == nil {
			t.Errorf("%s 가 옵션 목록을 안 적었다", name)
		}
	}
}
