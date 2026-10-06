package quality

import (
	"fmt"
	"strings"
	"testing"
)

// urlToken 은 34자 섞인 토막이다 — 엔트로피 경고 문턱을 넘는다.
const urlToken = "Xk9Qm2Lp7Rt4Vw8Zb3Nc6Hd1Jf5Gs0Ya2E"

func entropyHit(sources []string, body string) bool {
	memory := goodMemory()
	memory.Sources = append(memory.Sources, sources...)
	memory.Body += body
	return hasRule(checkSecurity(memory, testOptions().normalized()), RuleSecretEntropy)
}

func TestEntropySkipsURLPath(t *testing.T) {
	if entropyHit(nil, "") {
		t.Fatal("바탕 기억이 이미 경고를 낸다 — 시험이 무의미하다")
	}
	if entropyHit([]string{"url:https://example.com/docs/" + urlToken + "/view"}, "") {
		t.Fatal("url 경로 토막은 엔트로피 경고를 안 내야 한다")
	}
}

func TestEntropyKeepsURLTails(t *testing.T) {
	cases := map[string][]string{
		"쿼리":       {"url:https://example.com/docs?key=" + urlToken},
		"조각":       {"url:https://example.com/docs#" + urlToken},
		"userinfo": {"url:https://user:" + urlToken + "@example.com/path"},
	}
	for name, sources := range cases {
		if !entropyHit(sources, "") {
			t.Errorf("%s 의 토막은 경고가 나야 한다", name)
		}
	}
	if !entropyHit(nil, "\n참고 https://example.com/docs/"+urlToken) {
		t.Error("본문 안 url 경로는 지금처럼 검사해야 한다")
	}
}

func TestURLPathStillRejectsSecretPattern(t *testing.T) {
	memory := goodMemory()
	memory.Sources = append(memory.Sources, "url:https://example.com/ghp_"+strings.Repeat("a1B2", 6)+"/x")
	if kind := Gate(memory, testOptions()).Kind; kind != KindSecurityReject {
		t.Fatalf("url 경로의 거절 패턴은 여전히 거절이어야 한다 : %v", kind)
	}
}

func TestEntropySourcesKeepsLines(t *testing.T) {
	in := []string{"file:a.go", "url:https://example.com/a/b?q=1#z", "url:no-scheme/path", "note:x"}
	want := []string{"file:a.go", "url:https://example.com?q=1#z", "url:no-scheme/path", "note:x"}
	got := entropySources(in)
	if len(got) != len(in) {
		t.Fatalf("줄 수가 바뀌었다 : %d → %d", len(in), len(got))
	}
	for at := range want {
		if got[at] != want[at] {
			t.Errorf("%d번째 : %q, 바란 것 %q", at, got[at], want[at])
		}
	}
}

// fixedFinder 는 정해 둔 닮음 하나를 돌려준다.
type fixedFinder struct{ match Match }

func (finder fixedFinder) Nearest(*Doc, float64, int) []Match { return []Match{finder.match} }

func TestDuplicateReasonShowsScoreAndThreshold(t *testing.T) {
	opt := testOptions()
	warn, reject := opt.Config.Quality.DupWarn, opt.Config.Quality.DupReject
	memory := goodMemory()
	rival := &Doc{ID: "20260801-aaaaaaaa"}
	cases := []struct {
		rule  string
		match Match
		want  []string
	}{
		{RuleDuplicateHard, Match{Doc: rival, Score: reject + 0.011, Hard: true},
			[]string{fmt.Sprintf("닮음 %.3f", reject+0.011), fmt.Sprintf("막는 문턱 %.3f", reject)}},
		{RuleDuplicateSoft, Match{Doc: rival, Score: (warn + reject) / 2},
			[]string{fmt.Sprintf("닮음 %.3f", (warn+reject)/2), fmt.Sprintf("경고 문턱 %.3f", warn),
				fmt.Sprintf("막는 문턱 %.3f", reject)}},
	}
	for _, one := range cases {
		found := DuplicateFindings(NewDoc(memory), fixedFinder{one.match}, memory, opt)
		if len(found) != 1 || found[0].Rule != one.rule {
			t.Fatalf("%s 하나가 나와야 한다 : %+v", one.rule, found)
		}
		for _, piece := range one.want {
			if !strings.Contains(found[0].Reason, piece) {
				t.Errorf("%s 이유에 %q 가 없다 : %s", one.rule, piece, found[0].Reason)
			}
		}
	}
}
