package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLLMMissingFileIsOff(t *testing.T) {
	settings, problems := LoadLLM(filepath.Join(t.TempDir(), "없음.toml"))
	if settings.Enabled() || len(problems) != 0 || settings.TimeoutMS != llmTimeoutMS {
		t.Fatalf("파일이 없으면 조용히 꺼짐이어야 한다 : %+v %v", settings, problems)
	}
	if settings, _ := LoadLLM(""); settings.Enabled() {
		t.Fatal("자리가 비면 꺼짐이다")
	}
}

func TestLLMParse(t *testing.T) {
	path := filepath.Join(t.TempDir(), LLMFileName)
	text := "\ufeff# 이 기계\nurl = \"http://127.0.0.1:8080\"\nkey = \"k\"\njudge_profile = \"judge\"\n" +
		"generate_profile = \"gen\"\ntimeout_ms = 3000\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	settings, problems := LoadLLM(path)
	if len(problems) != 0 || !settings.Enabled() || settings.URL != "http://127.0.0.1:8080" ||
		settings.Key != "k" || settings.JudgeProfile != "judge" || settings.GenerateProfile != "gen" ||
		settings.TimeoutMS != 3000 || settings.Path != path {
		t.Fatalf("값을 잘못 읽었다 : %+v %v", settings, problems)
	}
}

func TestLLMBadValues(t *testing.T) {
	settings, problems := ParseLLM("x", "url = \"file:///etc/passwd\"\ntimeout_ms = 0\n")
	if settings.Enabled() || settings.TimeoutMS != llmTimeoutMS || len(problems) != 2 {
		t.Fatalf("틀린 url 은 꺼짐 · 틀린 시간은 기본값이어야 한다 : %+v %v", settings, problems)
	}
	if settings, _ := ParseLLM("x", "url = \"127.0.0.1:8080\"\n"); settings.Enabled() {
		t.Fatal("http 가 없는 주소는 받지 않는다")
	}
}

func TestLLMPathEnv(t *testing.T) {
	t.Setenv(LLMPathEnv, "C:/somewhere/llm.toml")
	if LLMPath() != "C:/somewhere/llm.toml" {
		t.Fatalf("환경 변수 자리를 따라야 한다 : %s", LLMPath())
	}
}

// Laya 세 칸 — 없으면 기본값(주소 빔 · 1000ms · 0.70), 적으면 그대로, 틀리면 기본값과 문제 한 줄씩.
func TestLLMLayaFields(t *testing.T) {
	settings, problems := ParseLLM("x", "")
	if len(problems) != 0 || settings.LayaURL != "" || settings.LayaTimeoutMS != layaTimeoutMS || settings.LayaSure != layaSure {
		t.Fatalf("기본값이 아니다 : %+v %v", settings, problems)
	}
	if settings, _ := LoadLLM(""); settings.LayaTimeoutMS != layaTimeoutMS || settings.LayaSure != layaSure {
		t.Fatalf("파일이 없어도 기본값이다 : %+v", settings)
	}
	settings, problems = ParseLLM("x", "laya_url = \"http://127.0.0.1:8091\"\nlaya_timeout_ms = 500\nlaya_sure = 0.8\n")
	if len(problems) != 0 || settings.LayaURL != "http://127.0.0.1:8091" || settings.LayaTimeoutMS != 500 ||
		settings.LayaSure != 0.8 || settings.Enabled() {
		t.Fatalf("값을 잘못 읽었다 (Laya 만 적으면 SemIf 는 꺼짐) : %+v %v", settings, problems)
	}
	settings, problems = ParseLLM("x", "laya_url = \"file:///x\"\nlaya_timeout_ms = 5001\nlaya_sure = 1\n")
	if len(problems) != 3 || settings.LayaURL != "" || settings.LayaTimeoutMS != layaTimeoutMS || settings.LayaSure != layaSure {
		t.Fatalf("틀린 칸은 버리고 문제 셋 : %+v %v", settings, problems)
	}
	for _, edge := range []string{"laya_timeout_ms = 1\nlaya_sure = 0.34\n", "laya_timeout_ms = 5000\nlaya_sure = 0.99\n"} {
		if _, problems := ParseLLM("x", edge); len(problems) != 0 {
			t.Errorf("경계값은 받는다 : %q %v", edge, problems)
		}
	}
	if _, problems := ParseLLM("x", "laya_sure = 0.33\nlaya_timeout_ms = 0\n"); len(problems) != 2 {
		t.Errorf("경계 밖은 문제다 : %v", problems)
	}
	if settings, problems := ParseLLM("x", "laya_sure = nan\n"); len(problems) != 1 || settings.LayaSure != layaSure {
		t.Errorf("nan 은 범위 밖이다 : %v %v", settings.LayaSure, problems)
	}
}
