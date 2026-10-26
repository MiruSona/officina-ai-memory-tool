package config

import (
	"strings"
	"testing"
)

// retain_sure_support 는 retain:* R3 만 쓰는 지지 선이다. 칸이 없으면 공용 nli_sure_support 를 따른다.
func TestRetainSureSupport(t *testing.T) {
	settings, problems := ParseLLM("x", "nli_sure_support = 0.89\n")
	if len(problems) != 0 || settings.RetainSureSupport != 0.89 {
		t.Fatalf("칸이 없으면 공용 값이어야 한다 : %+v %v", settings, problems)
	}
	if settings, _ := ParseLLM("x", ""); settings.RetainSureSupport != nliSureSupport {
		t.Fatalf("둘 다 없으면 코드 기본값이다 : %v", settings.RetainSureSupport)
	}
	settings, problems = ParseLLM("x", "nli_sure_support = 0.89\nretain_sure_support = 0.85\n")
	if len(problems) != 0 || settings.RetainSureSupport != 0.85 || settings.NLISureSupport != 0.89 {
		t.Fatalf("칸이 있으면 그 값 · 공용 값은 그대로 : %+v %v", settings, problems)
	}
	settings, problems = ParseLLM("x", "nli_sure_support = 0.89\nretain_sure_support = 0.2\n")
	if len(problems) != 1 || !strings.Contains(problems[0], "retain_sure_support") || settings.RetainSureSupport != 0.89 {
		t.Fatalf("범위 밖이면 공용 값과 문제 한 줄 : %+v %v", settings, problems)
	}
}

// retain_sure_support 가 nli_sure 아래면 거짓 지지를 막는 뜻이 뒤집힌다 — 문제 한 줄과 공용 값이다.
func TestRetainSureBelowNLISureFallsBack(t *testing.T) {
	settings, problems := ParseLLM("x", "nli_sure = 0.70\nnli_sure_support = 0.89\nretain_sure_support = 0.50\n")
	if len(problems) != 1 || !strings.Contains(problems[0], "retain_sure_support") || settings.RetainSureSupport != 0.89 {
		t.Fatalf("역전이면 문제 한 줄과 공용 값이어야 한다 : %+v %v", settings, problems)
	}
	if settings, problems := ParseLLM("x", "nli_sure = 0.70\nretain_sure_support = 0.70\n"); len(problems) != 0 || settings.RetainSureSupport != 0.70 {
		t.Fatalf("같은 값은 역전이 아니다 : %+v %v", settings, problems)
	}
}
