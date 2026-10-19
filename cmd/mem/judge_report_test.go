package main

// mem judge report 시험이다 (v1.2 적용 설계 4절 · 시험 #14 · #15). NLI 서버는 가짜다.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 옮김 검사용 보고 셋이다. 기대값은 JudgeModel/measure/use_sites/reports.py 의 extract_claims 를
// 같은 글에 돌려 받은 것을 그대로 적었다 (2026-10-19). 보고 글의 ~ 는 백틱으로 바꿔 읽는다.
const reportFixtureOne = `## 만든 것

1. ~internal/review/near.go~ — 몫 나누기

## 이번에 정한 것

| 자리 | 정한 것 | 까닭 |
| --- | --- | --- |
| 몫 | C19 45 · C13 20 · C20 20 · obs 15 로 나눈다 | 설계 3절 |
| 2 | 짧음 | 숫자 줄 |
| C04 안내 | **NLI 반대면** ~--by~ 를 권한다 | 거절은 그대로 |
|  | — | 빈 줄 |

정함 : 서버가 없으면 규칙 층만 보고 한 줄 남긴다
- **정함**: [링크 글](http://x.y) 를 따라 고친다
정함 : 짧다
`

const reportFixtureTwo = `결정 묶음

| # | 무엇 | 어떻게 | 왜 |
|:--|:--|--:|--|
| 1 | 판정기 고르기 | 설정 nli 를 먼저 본다 | 빠르다 |
| 2 | 주소 검사 | 루프백만 받는다 | 안전 |
| 3 | 3.5 | 12 | 숫자 |

| 이름 | 값 |
| --- | --- |
| 이건 결정 표가 아니다 | 그래서 안 뽑는다 |

본문에서 mem add --type decision --scope officina --title "제목만 있는 결정 줄이다" --tags x
그리고 mem add --type=decision --summary '요약이 제목보다 먼저 쓰인다' --title "안 쓰이는 제목이다"
mem add --type fact --summary "사실은 안 뽑는다 이 줄은"
`

// reportFixtureThree 는 파이썬 쪽과 같은 식으로 만든다 — 표 15줄(12개 상한) + 201·200자 정함 줄 + 겹친 줄.
func reportFixtureThree() string {
	lines := []string{"| 결정 0 | 까닭 |", "| --- | --- |"}
	for at := 0; at < 15; at++ {
		lines = append(lines, fmt.Sprintf("| 결정 문장 번호 %02d 은 여덟 글자를 넘는다 | 까닭 %d |", at, at))
	}
	lines = append(lines, "정함 : "+strings.Repeat("가", 201), "정함 : "+strings.Repeat("나", 200),
		"정함 : 결정 문장 번호 03 은 여덟 글자를 넘는다")
	return strings.Join(lines, "\n") + "\n"
}

// #14 파이썬 extract_claims 와 같은 문장 · 같은 종류 · 같은 차례가 나온다.
func TestReportClaimsMatchPython(t *testing.T) {
	three := []reportClaim{}
	for at := 0; at < 12; at++ {
		three = append(three, reportClaim{fmt.Sprintf("결정 문장 번호 %02d 은 여덟 글자를 넘는다", at), claimTable})
	}
	cases := []struct {
		name string
		text string
		want []reportClaim
	}{
		{"표·정함", strings.ReplaceAll(reportFixtureOne, "~", "`"), []reportClaim{
			{"몫 : C19 45 · C13 20 · C20 20 · obs 15 로 나눈다", claimTable},
			{"C04 안내 : NLI 반대면 --by 를 권한다", claimTable},
			{"서버가 없으면 규칙 층만 보고 한 줄 남긴다", claimDecided},
			{"링크 글 를 따라 고친다", claimDecided},
		}},
		{"제목 표·mem add", reportFixtureTwo, []reportClaim{
			{"판정기 고르기 : 설정 nli 를 먼저 본다", claimTable},
			{"주소 검사 : 루프백만 받는다", claimTable},
			{"제목만 있는 결정 줄이다", claimMemAdd},
			{"요약이 제목보다 먼저 쓰인다", claimMemAdd},
		}},
		{"12개 상한", reportFixtureThree(), three},
	}
	for _, one := range cases {
		if got := extractClaims(one.text); !reflect.DeepEqual(got, one.want) {
			t.Errorf("%s : 파이썬 판과 다르다\n got  %v\n want %v", one.name, got, one.want)
		}
	}
}

// #14 길이 거름(8~200자) · 겹침 거름 · CRLF 줄 끝.
func TestReportClaimsLengthAndDedupe(t *testing.T) {
	text := "정함 : 일곱글자다요\r\n정함 : 여덟 글자 문장\r\n정함 : " + strings.Repeat("나", 200) +
		"\r\n정함 : " + strings.Repeat("가", 201) + "\r\n- 정함 : 여덟 글자 문장\r\n"
	got := extractClaims(text)
	if len(got) != 2 || got[0].Text != "여덟 글자 문장" || got[1].Text != strings.Repeat("나", 200) {
		t.Fatalf("길이·겹침 거름이 다르다 : %v", got)
	}
}

// #15 scope 고르기 — 준 것 · 보고 속 id 가 한 scope · 여럿 · 없음.
func TestReportScopeChoice(t *testing.T) {
	scopes := map[string]string{"20261001-aaaaaaa1": "alpha", "20261001-aaaaaaa2": "alpha", "20261001-bbbbbbb1": "beta"}
	lookup := func(id string) *model.Memory {
		if scope, ok := scopes[id]; ok {
			return &model.Memory{ID: id, Scope: scope}
		}
		return nil
	}
	if scope, _, err := reportScope(" gamma ", "20261001-bbbbbbb1", lookup); err != nil || scope != "gamma" {
		t.Fatalf("--scope 가 먼저다 : %q %v", scope, err)
	}
	if scope, _, err := reportScope("", "근거 20261001-aaaaaaa1 · 20261001-aaaaaaa2 · 20261001-ccccccc9", lookup); err != nil || scope != "alpha" {
		t.Fatalf("id 들의 scope 가 하나면 그것이다 : %q %v", scope, err)
	}
	if _, seen, err := reportScope("", "20261001-aaaaaaa1 20261001-bbbbbbb1", lookup); err == nil || len(seen) != 2 {
		t.Fatalf("scope 가 여럿이면 못 고른다 : %v %v", seen, err)
	}
	if _, _, err := reportScope("", "id 없음", lookup); err == nil {
		t.Fatal("id 가 없으면 못 고른다")
	}
}

// reportServer 는 가짜 NLI 서버다. GET 은 /health, POST 는 근거에 「다섯」이 들면 확신 반대,
// 「지지」가 들면 확신 지지, 아니면 확신 무관이다. 받은 주장을 모은다.
func reportServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var lock sync.Mutex
	claims := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"ok":true}`)
			return
		}
		var request struct {
			Evidence string `json:"evidence"`
			Claim    string `json:"claim"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		lock.Lock()
		claims = append(claims, request.Claim)
		lock.Unlock()
		switch {
		case strings.Contains(request.Evidence, "다섯"):
			io.WriteString(w, `{"a":0.02,"b":0.93,"c":0.05,"model":"fake0001","ms":2.0,"label_set":"nli3"}`)
		case strings.Contains(request.Evidence, "지지"):
			io.WriteString(w, `{"a":0.95,"b":0.02,"c":0.03,"model":"fake0001","ms":2.0,"label_set":"nli3"}`)
		default:
			io.WriteString(w, `{"a":0.02,"b":0.03,"c":0.95,"model":"fake0001","ms":2.0,"label_set":"nli3"}`)
		}
	}))
	t.Cleanup(server.Close)
	return server, &claims
}

// reportRepo 는 결정 둘(반대가 날 것 · 무관이 날 것)을 둔 저장소와 llm.toml 이다.
func reportRepo(t *testing.T, nliURL string) string {
	t.Helper()
	memory := newRepo(t)
	budget := basisMemory("20261001-aaaaaaa1", model.TypeDecision, "훅 예산은 절마다 다섯 줄이다")
	budget.Summary = "훅 예산은 절마다 다섯 줄로 둔다. 훅 주입은 바이트로 잰다"
	other := basisMemory("20261001-aaaaaaa2", model.TypeDecision, "훅 로그는 하루치만 남긴다")
	other.Summary = "훅 로그는 하루치만 남기고 지운다. 훅 주입 기록도 같다"
	elsewhere := basisMemory("20261001-bbbbbbb1", model.TypeDecision, "훅 예산 다섯 줄 다른 scope")
	elsewhere.Scope = "mem-search"
	elsewhere.Summary = "훅 예산은 다섯 줄이다 (다른 scope 라 안 묻는다)"
	for _, one := range []*model.Memory{budget, other, elsewhere} {
		putMemory(t, memory, one)
	}
	mustRun(t, "index")
	llmPath := filepath.Join(memory, "llm-test.toml")
	text := ""
	if nliURL != "" {
		text = "nli_url = \"" + nliURL + "\"\n"
	}
	if err := os.WriteFile(llmPath, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEM_LLM_CONFIG", llmPath)
	return memory
}

func writeReport(t *testing.T, memory, text string) string {
	t.Helper()
	path := filepath.Join(filepath.Dir(memory), "report.md")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// #15 반대 확신만 찍히고 끝에 셈 한 줄이 나온다. 다른 scope 결정은 안 묻는다. 종료 0.
func TestJudgeReportPrintsOnlyContradictions(t *testing.T) {
	server, claims := reportServer(t)
	memory := reportRepo(t, server.URL)
	path := writeReport(t, memory, "## 이번에 정한 것\n\n정함 : 훅 예산을 절마다 세 줄로 줄인다 (훅 주입)\n")
	out, code := capture(t, func() int {
		return run([]string{"judge", "report", "--scope", "aimemorytool", "--file", path})
	})
	if code != exitOK {
		t.Fatalf("종료 코드가 0 이 아니다 : %d %s", code, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "어긋날 수 있다 — 보고 「훅 예산을 절마다 세 줄로") ||
		!strings.Contains(lines[0], "`20261001-aaaaaaa1`") || !strings.HasSuffix(lines[0], "반대 .93") {
		t.Fatalf("반대 줄 하나 + 셈 줄이어야 한다 :\n%s", out)
	}
	if !strings.Contains(lines[1], "물음 2 · 반대 1 · 지지 0 · 무관 1") {
		t.Fatalf("셈 줄이 다르다 : %s", lines[1])
	}
	if len(*claims) != 2 {
		t.Fatalf("같은 scope 결정 둘만 물어야 한다 : %d번", len(*claims))
	}
	// --pairs 1 이면 하나만 묻고 나머지는 「밀림」으로 센다. --json 은 쌍을 다 싣는다.
	out, code = capture(t, func() int {
		return run([]string{"judge", "report", "--scope", "aimemorytool", "--file", path, "--pairs", "1", "--json", "--fresh"})
	})
	var row judgeReportJSON
	if code != exitOK || json.Unmarshal([]byte(out), &row) != nil || row.Counts.Asked != 1 || row.Counts.Skipped != 1 ||
		len(row.Pairs) != 1 || len(row.Claims) != 1 {
		t.Fatalf("--pairs 상한 · --json 이 다르다 : %d %s", code, out)
	}
}

// #15 scope 를 안 주면 보고 속 mem id 의 scope 를 쓴다. 못 고르면 종료 2.
func TestJudgeReportScopeFromIDs(t *testing.T) {
	server, _ := reportServer(t)
	memory := reportRepo(t, server.URL)
	path := writeReport(t, memory, "근거 [mem:20261001-aaaaaaa2]\n정함 : 훅 예산을 절마다 세 줄로 줄인다 (훅 주입)\n")
	out, code := capture(t, func() int { return run([]string{"judge", "report", "--file", path}) })
	if code != exitOK || !strings.Contains(out, "scope aimemorytool") {
		t.Fatalf("id 의 scope 를 못 골랐다 : %d %s", code, out)
	}
	mixed := writeReport(t, memory, "20261001-aaaaaaa2 · 20261001-bbbbbbb1\n정함 : 훅 예산을 절마다 세 줄로 줄인다\n")
	if _, code := capture(t, func() int { return run([]string{"judge", "report", "--file", mixed}) }); code != exitCheck {
		t.Fatalf("scope 가 둘이면 종료 2 여야 한다 : %d", code)
	}
	if _, code := capture(t, func() int { return run([]string{"judge", "report"}) }); code != exitUsage {
		t.Fatalf("--file 없이는 사용법 잘못이다 : %d", code)
	}
	if _, code := capture(t, func() int { return run([]string{"judge", "report", "--file", path, "--pairs", "0"}) }); code != exitUsage {
		t.Fatalf("--pairs 0 은 사용법 잘못이다 : %d", code)
	}
}

// #15 서버가 없으면(주소 없음 · /health 안 닿음) 한 줄 + 종료 0. 문장이 없으면 그것도 한 줄 + 0.
func TestJudgeReportWithoutServer(t *testing.T) {
	memory := reportRepo(t, "")
	path := writeReport(t, memory, "정함 : 훅 예산을 절마다 세 줄로 줄인다\n")
	out, code := capture(t, func() int { return run([]string{"judge", "report", "--scope", "aimemorytool", "--file", path}) })
	if code != exitOK || strings.TrimSpace(out) != "NLI 서버 없음 — 대조 안 함" {
		t.Fatalf("주소가 없을 때 : %d %q", code, out)
	}
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	if err := os.WriteFile(os.Getenv("MEM_LLM_CONFIG"), []byte("nli_url = \""+deadURL+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code = capture(t, func() int { return run([]string{"judge", "report", "--scope", "aimemorytool", "--file", path}) })
	if code != exitOK || strings.TrimSpace(out) != "NLI 서버 없음 — 대조 안 함" {
		t.Fatalf("서버가 안 닿을 때 : %d %q", code, out)
	}
	empty := writeReport(t, memory, "결정 문장이 하나도 없는 보고다\n")
	out, code = capture(t, func() int { return run([]string{"judge", "report", "--file", empty}) })
	if code != exitOK || strings.TrimSpace(out) != "결정 문장 없음 — 대조 안 함" {
		t.Fatalf("문장이 없을 때 : %d %q", code, out)
	}
}

// #15 비밀 꼴이 든 문장은 서버로 안 나간다 — 실패로 센다.
func TestJudgeReportRefusesSecrets(t *testing.T) {
	server, claims := reportServer(t)
	memory := reportRepo(t, server.URL)
	path := writeReport(t, memory, "정함 : 훅 예산 열쇠는 "+fakeKey+" 로 둔다 (훅 주입)\n")
	out, code := capture(t, func() int { return run([]string{"judge", "report", "--scope", "aimemorytool", "--file", path}) })
	if code != exitOK {
		t.Fatalf("종료 코드가 0 이 아니다 : %d %s", code, out)
	}
	for _, claim := range *claims {
		if strings.Contains(claim, "sk-ant") {
			t.Fatalf("비밀 꼴이 서버로 나갔다 : %s", claim)
		}
	}
	if strings.Contains(out, "sk-ant") || !strings.Contains(out, "실패 2") {
		t.Fatalf("거절을 실패로 세야 하고 출력에 비밀이 없어야 한다 : %s", out)
	}
}

// 보고 글이 1 MiB 를 넘으면 사용 오류(1)다. 서버에는 아무것도 안 묻는다.
func TestJudgeReportTooBig(t *testing.T) {
	server, claims := reportServer(t)
	memory := reportRepo(t, server.URL)
	path := writeReport(t, memory, "정함 : 훅 예산을 절마다 세 줄로 줄인다\n"+strings.Repeat("가", reportReadLimit/3+1))
	if _, code := capture(t, func() int { return run([]string{"judge", "report", "--scope", "aimemorytool", "--file", path}) }); code != exitUsage {
		t.Fatalf("1 MiB 넘는 보고는 종료 1 이어야 한다 : %d", code)
	}
	if len(*claims) != 0 {
		t.Fatalf("상한을 넘었는데 %d번 물었다", len(*claims))
	}
}
