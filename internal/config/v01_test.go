package config

import (
	"reflect"
	"testing"
)

// 설계 4-4 · 7-4 — v0.1 이 새로 읽는 두 자리다.
func TestStopwordAndHookKeys(t *testing.T) {
	got, err := Parse("[stopword]\nwords = [\"왜\", \"지금\"]\n[hook]\nuser_prompt = true\n")
	if err != nil {
		t.Fatalf("읽다가 죽었다 : %v", err)
	}
	if len(got.Stopword.Words) != 2 || got.Stopword.Words[0] != "왜" {
		t.Fatalf("불용어를 못 읽었다 : %v", got.Stopword.Words)
	}
	if !got.Hook.UserPrompt {
		t.Fatal("[hook] user_prompt 를 못 읽었다")
	}
	fallback, err := Parse("name = \"x\"\n")
	if err != nil {
		t.Fatalf("읽다가 죽었다 : %v", err)
	}
	if fallback.Hook.UserPrompt {
		t.Fatal("user_prompt 는 기본이 꺼짐이어야 한다")
	}
	if len(fallback.Stopword.Words) != len(DefaultStopwords()) {
		t.Fatalf("불용어 기본값이 안 들어왔다 : %v", fallback.Stopword.Words)
	}
}

// mem init 은 같은 바이트를 두 번 써야 한다. 새 절이 그걸 안 깨는지 본다.
func TestEncodeKeepsNewSections(t *testing.T) {
	text := string(Encode(Default("게임A")))
	got, err := Parse(text)
	if err != nil {
		t.Fatalf("다시 못 읽었다 : %v\n%s", err, text)
	}
	// [hook] 에 subagent_skip 목록이 생겨 == 로 못 견준다. 재는 뜻은 그대로다.
	if !reflect.DeepEqual(got.Hook, Default("게임A").Hook) {
		t.Fatalf("[hook] 이 바뀌었다 : %+v", got.Hook)
	}
	if len(got.Stopword.Words) != len(DefaultStopwords()) {
		t.Fatalf("[stopword] 가 바뀌었다 : %v", got.Stopword.Words)
	}
	if string(Encode(got)) != text {
		t.Fatal("두 번 쓴 바이트가 다르다")
	}
	if len(MissingKeys(text)) != 0 {
		t.Fatalf("갓 쓴 mem.toml 에 빠진 키가 있다 : %v", MissingKeys(text))
	}
}
