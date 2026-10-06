package main

import (
	"strings"
	"testing"
)

// 자동으로 쌓인 기억을 되돌리면 0건 안내가 「자동 되돌림」 줄로 따로 말하고,
// 되살린 뒤에는 그 줄이 사라진다.
func TestSearchEmptyNamesAutoUndone(t *testing.T) {
	autoRepo(t)
	out, code := capture(t, func() int { return run(autoArgs("본문 한 줄")) })
	if code != exitOK {
		t.Fatalf("자동 add 실패 : %s", out)
	}
	before, _ := capture(t, func() int { return run([]string{"search", "trigram"}) })
	if strings.Contains(before, "자동 되돌림") {
		t.Fatalf("되돌림 기록이 없는데 자동 되돌림 줄이 나왔다 :\n%s", before)
	}
	if _, code := capture(t, func() int { return run([]string{"auto", "undo", "--origin", "stop", "--apply"}) }); code != exitOK {
		t.Fatal("undo 실패")
	}
	found, _ := capture(t, func() int { return run([]string{"search", "trigram"}) })
	if !strings.Contains(found, "자동 되돌림 1건") {
		t.Fatalf("undo 뒤 0건 안내에 자동 되돌림 1건이 없다 :\n%s", found)
	}
	if _, code := capture(t, func() int { return run([]string{"auto", "redo", "--apply"}) }); code != exitOK {
		t.Fatal("redo 실패")
	}
	again, _ := capture(t, func() int { return run([]string{"search", "trigram"}) })
	if strings.Contains(again, "자동 되돌림") {
		t.Fatalf("redo 뒤에도 자동 되돌림 줄이 나온다 :\n%s", again)
	}
}

// 되돌림 기록이 없는 저장소는 undoneIDs 가 nil 이라 0건 안내가 예전과 같다.
func TestUndoneIDsEmptyWithoutRecords(t *testing.T) {
	memory := newRepo(t)
	if ids := undoneIDs(memory); ids != nil {
		t.Fatalf("기록이 없는데 %v", ids)
	}
	if ids := undoneIDs(""); ids != nil {
		t.Fatalf("빈 자리인데 %v", ids)
	}
}
