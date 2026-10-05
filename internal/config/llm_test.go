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
