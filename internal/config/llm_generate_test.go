package config

import (
	"strings"
	"testing"
)

// 생성 칸 셋(A2 retain 설계 8절) — 빈 칸 · 상한 · 틀린 주소 · url 별칭 · 옛 프로필 칸 경고.
func TestLLMGenerateFields(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		url      string
		model    string
		timeout  int
		problems int
		warnHas  string
	}{
		{name: "빈 파일은 꺼짐 · 기본 시간", text: "", timeout: generateTimeoutMS},
		{name: "세 칸 그대로", text: "generate_url = \"http://10.0.0.2:8080\"\ngenerate_model = \" m1 \"\ngenerate_timeout_ms = 600000\n",
			url: "http://10.0.0.2:8080", model: "m1", timeout: 600000},
		{name: "url 별칭 — generate_url 이 비면 url 을 쓴다", text: "url = \"http://10.0.0.3:8080\"\n",
			url: "http://10.0.0.3:8080", timeout: generateTimeoutMS},
		{name: "generate_url 이 있으면 url 보다 먼저", text: "url = \"http://a:1\"\ngenerate_url = \"http://b:2\"\n",
			url: "http://b:2", timeout: generateTimeoutMS},
		{name: "틀린 generate_url 은 꺼짐 — 별칭으로 넘어가지 않는다", text: "url = \"http://a:1\"\ngenerate_url = \"file:///x\"\n",
			timeout: generateTimeoutMS, problems: 1, warnHas: "generate_url"},
		{name: "상한 넘음은 기본값", text: "generate_timeout_ms = 600001\n", timeout: generateTimeoutMS, problems: 1, warnHas: "600000"},
		{name: "0 은 기본값", text: "generate_timeout_ms = 0\n", timeout: generateTimeoutMS, problems: 1, warnHas: "generate_timeout_ms"},
		{name: "옛 프로필 칸 둘은 경고 한 줄", text: "judge_profile = \"j\"\ngenerate_profile = \"g\"\n",
			timeout: generateTimeoutMS, problems: 1, warnHas: "judge_profile"},
		{name: "옛 프로필 칸 하나도 경고", text: "generate_profile = \"g\"\n", timeout: generateTimeoutMS, problems: 1,
			warnHas: "지워도 된다"},
		{name: "옛 timeout_ms 도 안 쓰는 칸", text: "timeout_ms = 3000\n", timeout: generateTimeoutMS, problems: 1,
			warnHas: "timeout_ms 은 안 쓰는 칸"},
		{name: "틀린 url 별칭은 꺼짐", text: "url = \"file:///x\"\n", timeout: generateTimeoutMS, problems: 1,
			warnHas: "generate_url 로 옮긴다"},
	}
	for _, test := range cases {
		settings, problems := ParseLLM("x", test.text)
		if settings.GenerateURL != test.url || settings.GenerateModel != test.model ||
			settings.GenerateTimeoutMS != test.timeout || len(problems) != test.problems {
			t.Errorf("%s : %+v %v", test.name, settings, problems)
			continue
		}
		if test.warnHas != "" && !strings.Contains(problems[0], test.warnHas) {
			t.Errorf("%s : 문제 글에 %q 가 없다 : %v", test.name, test.warnHas, problems)
		}
	}
}

// 파일이 없어도 생성 시간 기본값은 채워진다.
func TestLLMGenerateDefaultWithoutFile(t *testing.T) {
	settings, _ := LoadLLM("")
	if settings.GenerateTimeoutMS != generateTimeoutMS || settings.GenerateURL != "" {
		t.Fatalf("파일이 없을 때 기본값이 아니다 : %+v", settings)
	}
}
