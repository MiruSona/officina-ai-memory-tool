package config

import (
	"reflect"
	"testing"
)

// UnreadLines 는 parseTOML 이 경고하고 건너뛴 줄 번호(1부터)를 돌려준다.
// 세 경우 — 이름이 틀린 절 머리 · 키 = 값 이 아닌 줄 · 끝까지 안 닫힌 목록.
func TestUnreadLines(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []int
	}{
		{"멀쩡한 파일", "[tag]\n\"a\" = [\"b\"]\n", nil},
		{"이름이 틀린 절 머리", "[tag]\n\"a\" = [\"b\"]\n[alias\n", []int{3}},
		{"키 = 값 이 아닌 줄", "[tag]\n그냥 글\n\"a\" = [\"b\"]\n", []int{2}},
		{"안 닫힌 목록", "[tag]\n\"a\" = [\"b\"]\n\"c\" = [\"d\",\n\"e\"\n", []int{3}},
		{"여럿 · CRLF", "[tag]\r\n그냥 글\r\n\"a\" = [\"b\"]\r\n또 글\r\n", []int{2, 4}},
	}
	for _, one := range cases {
		got := UnreadLines(one.text)
		if len(got) == 0 && len(one.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, one.want) {
			t.Errorf("%s : 줄 번호 %v, 바란 것 %v", one.name, got, one.want)
		}
	}
}

// UnreadLines 는 경고를 내지 않고, 끝나면 Warn 을 되돌려 둔다.
func TestUnreadLinesIsSilent(t *testing.T) {
	said := 0
	before := Warn
	Warn = func(string) { said++ }
	defer func() { Warn = before }()
	UnreadLines("그냥 글\n")
	if said != 0 {
		t.Fatalf("경고를 %d 번 냈다", said)
	}
	ParseVocab("그냥 글\n")
	if said != 1 {
		t.Fatalf("Warn 이 되돌아오지 않았다 (경고 %d 번)", said)
	}
}
