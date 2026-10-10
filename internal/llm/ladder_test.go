package llm

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
)

// 판정 사다리(규칙 → NLI → SemIf) 시험이다. 진짜 서버에는 아무것도 안 보낸다.

const (
	// 규칙에 안 걸리는 쌍 · 규칙 B · 규칙 C 쌍.
	plainEvidence     = "색인은 SQLite FTS5 로 만든다"
	plainClaim        = "색인은 SQLite FTS5 로 만들고 점수 순으로 보여 준다"
	negEvidence       = "새 기억을 넣을 때 중복 검사를 한다"
	negClaim          = "새 기억을 넣을 때 중복 검사를 안 한다"
	unrelatedEvidence = "고양이는 햇볕 아래에서 낮잠을 즐긴다"
	unrelatedClaim    = "서버 배포는 금요일 오후에 멈춘다"
)

// newNLI 는 가짜 NLI 판정 서버다. body 가 응답 몸통, status 가 0 이 아니면 그 코드로 답한다.
func newNLI(t *testing.T, body string, status int, delay time.Duration) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	hits := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		io.ReadAll(r.Body)
		if r.URL.Path != "/judge" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		time.Sleep(delay)
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server, hits
}

func nliFor(address string, timeoutMS int) *NLIClient {
	return NewNLI(config.LLMConfig{NLIURL: address, NLITimeoutMS: timeoutMS, NLISure: 0.70, NLISureSupport: 0.80})
}

// 응답 꼴은 JudgeModel 서버 그대로다 — ms 는 소수로 온다. nliFor 의 문턱은 지지 0.80 · 반대·무관 0.70.
const (
	nliSure   = `{"a": 0.85, "b": 0.1, "c": 0.05, "model": "abcd1234", "ms": 40.2}`
	nliUnsure = `{"a": 0.55, "b": 0.4, "c": 0.05, "model": "abcd1234", "ms": 40.0}`
)

func TestLadderRulesFinalStopsBeforeServers(t *testing.T) {
	nli, nliHits := newNLI(t, nliSure, 0, 0)
	semif := newFake(t, answerWith(logprobsBody(map[string]float64{"A": 0.9, "B": 0.05, "C": 0.05})))
	judge := judgeFor(t, semif.URL, 2000)
	judge.NLI = nliFor(nli.URL, 1000)
	judge.RulesFinal = []string{LetterContradict, LetterUnrelated}
	for _, pair := range [][3]string{{negEvidence, negClaim, LetterContradict}, {unrelatedEvidence, unrelatedClaim, LetterUnrelated}} {
		verdict, err := judge.Support(pair[0], pair[1])
		if err != nil || verdict.Letter != pair[2] || verdict.Stage != StageRules || verdict.Reason == "" ||
			verdict.Prompt != RulesVersion || verdict.Profile != RulesProfile || verdict.Unsure {
			t.Errorf("규칙이 최종이면 거기서 멈춘다 : %+v %v", verdict, err)
		}
	}
	if nliHits.Load() != 0 || semif.hits.Load() != 0 {
		t.Fatalf("규칙에서 끝났는데 서버를 불렀다 : nli=%d semif=%d", nliHits.Load(), semif.hits.Load())
	}
}

// retain 은 규칙 B 만 최종이다. 규칙 C 는 다음 단에 묻는다.
func TestLadderRetainAsksNextStageOnRuleC(t *testing.T) {
	semif := newFake(t, answerWith(logprobsBody(map[string]float64{"A": 0.9, "B": 0.05, "C": 0.05})))
	judge := judgeFor(t, semif.URL, 2000)
	judge.RulesFinal = []string{LetterContradict}
	verdict, err := judge.Support(unrelatedEvidence, unrelatedClaim)
	if err != nil || verdict.Stage != StageSemIf || verdict.Letter != LetterSupport || semif.hits.Load() != 1 {
		t.Fatalf("규칙 C 는 SemIf 에 넘겨야 한다 : %+v %v hits=%d", verdict, err, semif.hits.Load())
	}
	supported, ok := judge.Supports(negEvidence, negClaim)
	if supported || !ok || semif.hits.Load() != 1 {
		t.Fatalf("규칙 B 는 retain 에서도 최종 거절이다 : %v %v hits=%d", supported, ok, semif.hits.Load())
	}
}

// 다음 단이 없으면 규칙 C 는 「판정 못 받음」 경고로 흐르고, 안 걸림은 조용히 넘긴다.
func TestLadderWithoutServers(t *testing.T) {
	judge := &Judge{RulesFinal: []string{LetterContradict}}
	supported, ok := judge.Supports(unrelatedEvidence, unrelatedClaim)
	if supported || ok || judge.Skipped() || !strings.HasPrefix(judge.Problem(), "unsure C") ||
		!strings.Contains(judge.Problem(), "unrelated:") {
		t.Fatalf("규칙 C 뒤 단이 없으면 경고다 : %v %v skipped=%v %q", supported, ok, judge.Skipped(), judge.Problem())
	}
	supported, ok = judge.Supports(plainEvidence, plainClaim)
	if supported || ok || !judge.Skipped() {
		t.Fatalf("규칙에 안 걸리고 물을 단이 없으면 조용히 넘긴다 : %v %v skipped=%v", supported, ok, judge.Skipped())
	}
	if supported, ok := judge.Supports(negEvidence, negClaim); supported || !ok {
		t.Fatalf("서버 없이도 규칙 B 는 거절이다 : %v %v", supported, ok)
	}
}

func TestLadderNLISureStops(t *testing.T) {
	nli, nliHits := newNLI(t, nliSure, 0, 0)
	semif := newFake(t, answerWith(logprobsBody(map[string]float64{"B": 0.9, "A": 0.05, "C": 0.05})))
	judge := judgeFor(t, semif.URL, 2000)
	judge.NLI = nliFor(nli.URL, 1000)
	verdict, err := judge.Support(plainEvidence, plainClaim)
	if err != nil || verdict.Stage != StageNLI || verdict.Letter != LetterSupport || verdict.Prompt != NLIVersion ||
		verdict.Profile != "abcd1234" || verdict.Unsure || nliHits.Load() != 1 || semif.hits.Load() != 0 {
		t.Fatalf("NLI 가 확신하면 멈춘다 : %+v %v nli=%d semif=%d", verdict, err, nliHits.Load(), semif.hits.Load())
	}
}

func TestLadderNLIUnsureGoesToSemIf(t *testing.T) {
	nli, _ := newNLI(t, nliUnsure, 0, 0)
	semif := newFake(t, answerWith(logprobsBody(map[string]float64{"B": 0.9, "A": 0.05, "C": 0.05})))
	judge := judgeFor(t, semif.URL, 2000)
	judge.NLI = nliFor(nli.URL, 1000)
	verdict, err := judge.Support(plainEvidence, plainClaim)
	if err != nil || verdict.Stage != StageSemIf || verdict.Letter != LetterContradict || semif.hits.Load() != 1 {
		t.Fatalf("nli_sure_support 아래면 SemIf 로 넘긴다 : %+v %v", verdict, err)
	}
	// SemIf 가 없으면 NLI 답이 「모른다」로 남는다.
	judge.Client = nil
	verdict, err = judge.Support(plainEvidence, plainClaim)
	if err != nil || verdict.Stage != StageNLI || !verdict.Unsure || verdict.Letter != LetterSupport {
		t.Fatalf("마지막 단도 확신이 없으면 그 글자에 모른다 : %+v %v", verdict, err)
	}
}

func TestLadderNLIFailuresSkipStage(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		status int
		delay  time.Duration
		want   string
	}{
		{"500", "", http.StatusInternalServerError, 0, "nli:http-500"},
		{"시간 넘김", nliSure, 0, 300 * time.Millisecond, "nli:timeout"},
		{"칸 빠짐", `{"a":0.9,"b":0.1}`, 0, 0, "nli:bad-answer"},
		{"합이 1 아님", `{"a":0.9,"b":0.9,"c":0.9}`, 0, 0, "nli:bad-answer"},
		{"JSON 아님", `<html>`, 0, 0, "nli:bad-answer"},
		{"음수", `{"a":1.05,"b":-0.05,"c":0}`, 0, 0, "nli:bad-answer"},
		{"1 넘음", `{"a":1.5,"b":0,"c":0}`, 0, 0, "nli:bad-answer"},
		{"NaN", `{"a":NaN,"b":0.5,"c":0.5}`, 0, 0, "nli:bad-answer"},
	}
	for _, one := range cases {
		nli, _ := newNLI(t, one.body, one.status, one.delay)
		semif := newFake(t, answerWith(logprobsBody(map[string]float64{"A": 0.9, "B": 0.05, "C": 0.05})))
		judge := judgeFor(t, semif.URL, 2000)
		judge.NLI = nliFor(nli.URL, 100)
		verdict, err := judge.Support(plainEvidence, plainClaim)
		if err != nil || verdict.Stage != StageSemIf || judge.Problem() != one.want {
			t.Errorf("%s : NLI 를 건너뛰고 SemIf 로 · 까닭 %q : %+v %v %q", one.name, one.want, verdict, err, judge.Problem())
		}
		judge.Client = nil
		if _, ok := judge.Supports(plainEvidence, plainClaim); ok || judge.Problem() != one.want || judge.Skipped() {
			t.Errorf("%s : 다음 단이 없으면 경고 %q : %q", one.name, one.want, judge.Problem())
		}
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	address := closed.URL
	closed.Close()
	judge := &Judge{NLI: nliFor(address, 500)}
	if _, err := judge.Support(plainEvidence, plainClaim); err == nil || err.Error() != "nli:unreachable" {
		t.Fatalf("닫힌 NLI 는 nli:unreachable : %v", err)
	}
}

// 비밀 꼴은 규칙 단만 돌고 NLI 로도 안 보낸다.
func TestLadderRefuseBeforeNLI(t *testing.T) {
	nli, nliHits := newNLI(t, nliSure, 0, 0)
	judge := &Judge{NLI: nliFor(nli.URL, 1000), RulesFinal: []string{LetterContradict},
		Refuse: func(text string) bool { return strings.Contains(text, "SECRET") }}
	if _, err := judge.Support(plainEvidence+" SECRET", plainClaim); err != ErrRefused {
		t.Fatalf("비밀 꼴은 거절해야 한다 : %v", err)
	}
	verdict, err := judge.Support(negEvidence+" SECRET", negClaim)
	if err != nil || verdict.Letter != LetterContradict || verdict.Stage != StageRules {
		t.Fatalf("규칙 단은 비밀 꼴이어도 돈다 : %+v %v", verdict, err)
	}
	if nliHits.Load() != 0 {
		t.Fatal("비밀 꼴을 NLI 로 보냈다")
	}
}

// --stage 로 고른 단만 돈다.
func TestLadderOnlyChosenStages(t *testing.T) {
	nli, nliHits := newNLI(t, nliSure, 0, 0)
	semif := newFake(t, answerWith(logprobsBody(map[string]float64{"A": 0.9, "B": 0.05, "C": 0.05})))
	judge := judgeFor(t, semif.URL, 2000)
	judge.NLI = nliFor(nli.URL, 1000)
	judge.Only = []string{StageRules}
	verdict, err := judge.Support(plainEvidence, plainClaim)
	if err != nil || verdict.Letter != "" || !verdict.Unsure || verdict.Stage != StageRules {
		t.Fatalf("rules 만이면 안 걸림은 글자 빈 모른다 : %+v %v", verdict, err)
	}
	if nliHits.Load() != 0 || semif.hits.Load() != 0 {
		t.Fatalf("--stage rules 인데 서버를 불렀다 : nli=%d semif=%d", nliHits.Load(), semif.hits.Load())
	}
	judge.Only = []string{StageSemIf}
	verdict, err = judge.Support(negEvidence, negClaim)
	if err != nil || verdict.Stage != StageSemIf || nliHits.Load() != 0 {
		t.Fatalf("semif 만이면 규칙·NLI 를 건너뛴다 : %+v %v", verdict, err)
	}
}

// NLI 판정은 기록을 남기고, 판 이름·프로필이 달라 SemIf 기록과 안 섞인다.
func TestLadderNLIWritesOwnRecord(t *testing.T) {
	nli, _ := newNLI(t, nliSure, 0, 0)
	judge := &Judge{NLI: nliFor(nli.URL, 1000), Dir: filepath.Join(t.TempDir(), "judge")}
	verdict, err := judge.Support(plainEvidence, plainClaim)
	if err != nil {
		t.Fatal(err)
	}
	saved, ok := judge.read(verdict.Hash)
	if !ok || saved.Prompt != NLIVersion || saved.Stage != StageNLI || saved.Profile != "abcd1234" {
		t.Fatalf("NLI 판정 기록이 없다 : %+v %v", saved, ok)
	}
	semifHash := hashOf(KindSupport, PromptV3.Version, "abcd1234", plainEvidence, plainClaim)
	if semifHash == verdict.Hash {
		t.Fatal("NLI 와 SemIf 의 열쇠가 같다")
	}
}

// 문턱 둘 — 지지(A)는 nli_sure_support, 반대·무관(B·C)은 nli_sure 로 확정한다 (길1 NLI 설계 7절).
// 같은 0.80 이라도 지지는 보류되고 반대는 확정된다 — 지지 쪽만 더 엄하다.
func TestLadderNLIThresholdsPerLetter(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		letter string
		unsure bool
		sure   float64 // 반대·무관 선. 0 이면 0.70
	}{
		{"지지 0.80 < 0.90 → 보류", `{"a": 0.80, "b": 0.15, "c": 0.05, "model": "abcd1234", "ms": 40.0}`, LetterSupport, true, 0},
		{"지지 0.95 ≥ 0.90 → 확정", `{"a": 0.95, "b": 0.03, "c": 0.02, "model": "abcd1234", "ms": 40.0}`, LetterSupport, false, 0},
		{"지지 0.90 = 선 → 확정", `{"a": 0.90, "b": 0.05, "c": 0.05, "model": "abcd1234", "ms": 40.0}`, LetterSupport, false, 0},
		{"반대 0.80 ≥ 0.70 → 확정", `{"a": 0.15, "b": 0.80, "c": 0.05, "model": "abcd1234", "ms": 40.0}`, LetterContradict, false, 0},
		{"무관 0.75 ≥ 0.70 → 확정", `{"a": 0.05, "b": 0.20, "c": 0.75, "model": "abcd1234", "ms": 40.0}`, LetterUnrelated, false, 0},
		{"반대 0.65 < 0.70 → 보류", `{"a": 0.30, "b": 0.65, "c": 0.05, "model": "abcd1234", "ms": 40.0}`, LetterContradict, true, 0},
		// 선을 0.34 로 낮춰도 1·2등 차가 tieGap(0.01) 안이면 동점이라 보류다 — 문턱이 아니라 동점이 막는다.
		{"동점 0.475·0.47 (선 0.34) → 보류", `{"a": 0.47, "b": 0.055, "c": 0.475, "model": "abcd1234", "ms": 40.0}`, LetterUnrelated, true, 0.34},
		{"지지 0.8999 < 0.90 → 보류", `{"a": 0.8999, "b": 0.05, "c": 0.0501, "model": "abcd1234", "ms": 40.0}`, LetterSupport, true, 0},
	}
	for _, one := range cases {
		sure := one.sure
		if sure == 0 {
			sure = 0.70
		}
		server, _ := newNLI(t, one.body, 0, 0)
		client := NewNLI(config.LLMConfig{NLIURL: server.URL, NLITimeoutMS: 1000, NLISure: sure, NLISureSupport: 0.90})
		if client.Sure() != sure || client.SureSupport() != 0.90 {
			t.Fatalf("문턱을 설정에서 받아야 한다 : %v %v", client.Sure(), client.SureSupport())
		}
		// 다음 단 없음 — 확신 아래면 그 글자에 Unsure 로 남는다 (보류).
		judge := &Judge{NLI: client}
		verdict, err := judge.Support(plainEvidence, plainClaim)
		if err != nil || verdict.Stage != StageNLI || verdict.Letter != one.letter || verdict.Unsure != one.unsure {
			t.Errorf("%s : %+v %v", one.name, verdict, err)
		}
		// 다음 단 있음 — 확신 아래면 SemIf 로 넘기고, 확신이면 SemIf 를 안 부른다.
		semif := newFake(t, answerWith(logprobsBody(map[string]float64{"B": 0.9, "A": 0.05, "C": 0.05})))
		withSemIf := judgeFor(t, semif.URL, 2000)
		withSemIf.NLI = client
		verdict, err = withSemIf.Support(plainEvidence, plainClaim)
		wantStage, wantHits := StageNLI, int32(0)
		if one.unsure {
			wantStage, wantHits = StageSemIf, 1
		}
		if err != nil || verdict.Stage != wantStage || semif.hits.Load() != wantHits {
			t.Errorf("%s : SemIf 넘김이 틀렸다 %+v %v hits=%d", one.name, verdict, err, semif.hits.Load())
		}
	}
}

// 보류된 NLI 답은 retain 관문에서 「모른다」 경고로 흐른다 (거절도 통과도 아니다).
func TestLadderNLIUnsureIsHeld(t *testing.T) {
	server, _ := newNLI(t, `{"a": 0.80, "b": 0.15, "c": 0.05, "model": "abcd1234", "ms": 40.0}`, 0, 0)
	judge := &Judge{NLI: NewNLI(config.LLMConfig{NLIURL: server.URL, NLITimeoutMS: 1000, NLISure: 0.70, NLISureSupport: 0.90}),
		RulesFinal: []string{LetterContradict}}
	supported, ok := judge.Supports(plainEvidence, plainClaim)
	if supported || ok || judge.Skipped() || !strings.HasPrefix(judge.Problem(), "unsure A=0.80") {
		t.Fatalf("지지 문턱 아래는 보류 : %v %v skipped=%v %q", supported, ok, judge.Skipped(), judge.Problem())
	}
}
