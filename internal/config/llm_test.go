package config

import (
	"os"
	"path/filepath"
	"strings"
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

// NLI 네 칸 — 없으면 기본값(주소 빔 · 1000ms · 0.70 · 0.90), 적으면 그대로, 틀리면 기본값과 문제 한 줄씩.
func TestLLMNLIFields(t *testing.T) {
	settings, problems := ParseLLM("x", "")
	if len(problems) != 0 || settings.NLIURL != "" || settings.NLITimeoutMS != nliTimeoutMS ||
		settings.NLISure != nliSure || settings.NLISureSupport != nliSureSupport {
		t.Fatalf("기본값이 아니다 : %+v %v", settings, problems)
	}
	if settings, _ := LoadLLM(""); settings.NLITimeoutMS != nliTimeoutMS || settings.NLISure != nliSure ||
		settings.NLISureSupport != nliSureSupport {
		t.Fatalf("파일이 없어도 기본값이다 : %+v", settings)
	}
	settings, problems = ParseLLM("x", "nli_url = \"http://127.0.0.1:8092\"\nnli_timeout_ms = 500\nnli_sure = 0.8\nnli_sure_support = 0.95\n")
	if len(problems) != 0 || settings.NLIURL != "http://127.0.0.1:8092" || settings.NLITimeoutMS != 500 ||
		settings.NLISure != 0.8 || settings.NLISureSupport != 0.95 || settings.Enabled() {
		t.Fatalf("값을 잘못 읽었다 (NLI 만 적으면 SemIf 는 꺼짐) : %+v %v", settings, problems)
	}
	settings, problems = ParseLLM("x", "nli_url = \"file:///x\"\nnli_timeout_ms = 5001\nnli_sure = 1\nnli_sure_support = 0.2\n")
	if len(problems) != 4 || settings.NLIURL != "" || settings.NLITimeoutMS != nliTimeoutMS ||
		settings.NLISure != nliSure || settings.NLISureSupport != nliSureSupport {
		t.Fatalf("틀린 칸은 버리고 문제 넷 : %+v %v", settings, problems)
	}
	if !strings.Contains(problems[3], "nli_sure_support") || !strings.Contains(problems[2], "nli_sure(") {
		t.Errorf("문턱 문제 글에 칸 이름이 있어야 한다 : %v", problems)
	}
	for _, edge := range []string{"nli_timeout_ms = 1\nnli_sure = 0.34\nnli_sure_support = 0.34\n",
		"nli_timeout_ms = 5000\nnli_sure = 0.99\nnli_sure_support = 0.99\n"} {
		if _, problems := ParseLLM("x", edge); len(problems) != 0 {
			t.Errorf("경계값은 받는다 : %q %v", edge, problems)
		}
	}
	if _, problems := ParseLLM("x", "nli_sure = 0.33\nnli_sure_support = 1.0\nnli_timeout_ms = 0\n"); len(problems) != 3 {
		t.Errorf("경계 밖은 문제다 : %v", problems)
	}
	if settings, problems := ParseLLM("x", "nli_sure = nan\nnli_sure_support = nan\n"); len(problems) != 2 ||
		settings.NLISure != nliSure || settings.NLISureSupport != nliSureSupport {
		t.Errorf("nan 은 범위 밖이다 : %+v %v", settings, problems)
	}
	if settings, problems := ParseLLM("x", "nli_sure = 0.9\nnli_sure_support = 0.8\n"); len(problems) != 1 ||
		!strings.Contains(problems[0], "nli_sure_support(0.80)") || settings.NLISure != 0.9 || settings.NLISureSupport != 0.8 {
		t.Errorf("지지 선이 더 낮으면 경고 한 줄, 값은 그대로다 : %+v %v", settings, problems)
	}
}

// 옛 laya_* 칸은 별칭으로 받지 않는다 — 읽지 않고 문제 한 줄만 낸다 (몇 칸이 있어도 한 줄).
func TestLLMLayaKeysRemoved(t *testing.T) {
	settings, problems := ParseLLM("x", "laya_url = \"http://127.0.0.1:8091\"\nlaya_timeout_ms = 500\nlaya_sure = 0.8\n")
	if len(problems) != 1 || !strings.Contains(problems[0], "laya_*") || !strings.Contains(problems[0], "nli_url") {
		t.Fatalf("옛 키는 경고 한 줄이다 : %v", problems)
	}
	if settings.NLIURL != "" || settings.NLITimeoutMS != nliTimeoutMS || settings.NLISure != nliSure {
		t.Fatalf("옛 값을 읽었다 : %+v", settings)
	}
	settings, problems = ParseLLM("x", "laya_url = \"http://127.0.0.1:8091\"\nnli_url = \"http://127.0.0.1:8092\"\n")
	if len(problems) != 1 || settings.NLIURL != "http://127.0.0.1:8092" {
		t.Fatalf("새 키는 옛 키와 같이 있어도 읽는다 : %+v %v", settings, problems)
	}
	// 절 안의 laya_ 키는 맨 위 칸이 아니라 상관없다.
	if _, problems := ParseLLM("x", "[other]\nlaya_url = \"x\"\n"); len(problems) != 0 {
		t.Errorf("다른 절의 키는 경고하지 않는다 : %v", problems)
	}
}
