package llm

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
)

// fakeServer 는 가짜 OpenAI 호환 서버다. answer 가 응답 몸통을 만든다.
type fakeServer struct {
	*httptest.Server
	hits atomic.Int32
	last atomic.Value // 마지막 요청 몸통 (map)
	auth atomic.Value // 마지막 Authorization 머리
}

func newFake(t *testing.T, answer func(w http.ResponseWriter)) *fakeServer {
	t.Helper()
	fake := &fakeServer{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		parsed := map[string]any{}
		json.Unmarshal(body, &parsed)
		fake.last.Store(parsed)
		fake.auth.Store(r.Header.Get("Authorization"))
		if r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		answer(w)
	}))
	t.Cleanup(fake.Close)
	return fake
}

// logprobsBody 는 첫 토큰 위 후보가 tops(토큰 → 확률)인 응답이다.
func logprobsBody(tops map[string]float64) string {
	list := []map[string]any{}
	for token, prob := range tops {
		list = append(list, map[string]any{"token": token, "logprob": math.Log(prob)})
	}
	data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
		"message":  map[string]any{"role": "assistant", "content": "A"},
		"logprobs": map[string]any{"content": []any{map[string]any{"token": "A", "top_logprobs": list}}},
	}}})
	return string(data)
}

func answerWith(body string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	}
}

func judgeFor(t *testing.T, address string, timeoutMS int) *Judge {
	t.Helper()
	settings := config.LLMConfig{URL: address, Key: "test-key", JudgeProfile: "judge-model", TimeoutMS: timeoutMS}
	return &Judge{Client: New(settings), Dir: filepath.Join(t.TempDir(), "judge")}
}

func TestSupportReadsFirstTokenLetters(t *testing.T) {
	fake := newFake(t, answerWith(logprobsBody(map[string]float64{"A": 0.90, "B": 0.06, " A": 0.02, "C": 0.01})))
	judge := judgeFor(t, fake.URL, 2000)
	verdict, err := judge.Support("훅은 상태 파일을 오프셋으로 읽는다", "훅이 오프셋으로 읽는다")
	if err != nil {
		t.Fatalf("정상 응답인데 판정을 못 받았다 : %v", err)
	}
	if verdict.Letter != LetterSupport || !verdict.Supported() || math.Abs(verdict.Prob-0.90) > 1e-9 {
		t.Fatalf("A 를 골라야 한다 : %+v", verdict)
	}
	// 쌍둥이(「 A」)는 합치지 않는다 — 정확 일치만.
	if math.Abs(verdict.Probs["A"]-0.90) > 1e-9 || len(verdict.Probs) != 3 {
		t.Fatalf("글자 확률을 정확 일치로 모아야 한다 : %v", verdict.Probs)
	}
	request := fake.last.Load().(map[string]any)
	if request["max_tokens"].(float64) != 1 || request["logprobs"] != true ||
		request["top_logprobs"].(float64) != 20 || request["temperature"].(float64) != 0 ||
		request["model"] != "judge-model" {
		t.Fatalf("SemIf 요청 꼴이 아니다 : %v", request)
	}
	kwargs := request["chat_template_kwargs"].(map[string]any)
	if kwargs["enable_thinking"] != false {
		t.Fatalf("생각을 꺼야 한다 : %v", kwargs)
	}
	if fake.auth.Load().(string) != "Bearer test-key" {
		t.Fatalf("키를 Bearer 로 보내야 한다 : %q", fake.auth.Load())
	}
	messages := request["messages"].([]any)
	user := messages[1].(map[string]any)["content"].(string)
	for _, want := range []string{`"evidence"`, `"criterion"`, `"options"`, `"letter":"A"`, `"letter":"C"`} {
		if !strings.Contains(user, want) {
			t.Errorf("사용자 턴 JSON 에 %s 가 없다 : %s", want, user)
		}
	}
}

func TestSupportCachesAndDoesNotAskAgain(t *testing.T) {
	fake := newFake(t, answerWith(logprobsBody(map[string]float64{"B": 0.8, "A": 0.15, "C": 0.05})))
	judge := judgeFor(t, fake.URL, 2000)
	first, err := judge.Support("근거", "주장")
	if err != nil || first.Cached || first.Supported() {
		t.Fatalf("첫 판정 : %+v %v", first, err)
	}
	files, _ := os.ReadDir(judge.Dir)
	if len(files) != 1 || !strings.HasSuffix(files[0].Name(), ".json") {
		t.Fatalf("판정 기록 파일이 하나 있어야 한다 : %v", files)
	}
	data, _ := os.ReadFile(filepath.Join(judge.Dir, files[0].Name()))
	if strings.Contains(string(data), fake.URL) || strings.Contains(string(data), "test-key") {
		t.Fatalf("기록에 주소·키가 들어갔다 : %s", data)
	}
	second, err := judge.Support("근거", "주장")
	if err != nil || !second.Cached || second.Letter != LetterContradict || fake.hits.Load() != 1 {
		t.Fatalf("같은 입력은 다시 묻지 않아야 한다 : %+v hits=%d", second, fake.hits.Load())
	}
	judge.Fresh = true
	if third, _ := judge.Support("근거", "주장"); third.Cached || fake.hits.Load() != 2 {
		t.Fatalf("--fresh 면 다시 물어야 한다 : hits=%d", fake.hits.Load())
	}
}

// 프로필이 비면 서버 쪽 모델이 바뀌어도 열쇠가 같으니 판정 기록을 쓰지도 읽지도 않는다.
func TestSupportWithoutProfileKeepsNoRecord(t *testing.T) {
	fake := newFake(t, answerWith(logprobsBody(map[string]float64{"A": 0.9, "B": 0.1})))
	settings := config.LLMConfig{URL: fake.URL, Key: "test-key", TimeoutMS: 2000}
	judge := &Judge{Client: New(settings), Dir: filepath.Join(t.TempDir(), "judge")}
	for range 2 {
		if verdict, err := judge.Support("근거", "주장"); err != nil || verdict.Cached {
			t.Fatalf("프로필 없이 기록을 읽었다 : %+v %v", verdict, err)
		}
	}
	if fake.hits.Load() != 2 {
		t.Fatalf("두 번 다 물어야 한다 : hits=%d", fake.hits.Load())
	}
	if files, _ := os.ReadDir(judge.Dir); len(files) != 0 {
		t.Fatalf("프로필 없이 기록을 남겼다 : %v", files)
	}
}

func TestSupportFailures(t *testing.T) {
	cases := []struct {
		name   string
		answer func(http.ResponseWriter)
		want   string
	}{
		{"logprobs 없음", answerWith(`{"choices":[{"message":{"role":"assistant","content":"A"}}]}`), ErrNoLogprobs.Error()},
		{"엉뚱한 글자", answerWith(logprobsBody(map[string]float64{"The": 0.7, "D": 0.2, "A": 0.01})), ErrNoLetter.Error()},
		{"500", func(w http.ResponseWriter) { w.WriteHeader(http.StatusInternalServerError) }, "http-500"},
		{"JSON 아님", answerWith(`<html>oops</html>`), ErrNoLogprobs.Error()},
		{"시간 넘김", func(w http.ResponseWriter) {
			time.Sleep(400 * time.Millisecond)
			answerWith(logprobsBody(map[string]float64{"A": 1}))(w)
		}, "timeout"},
	}
	for _, one := range cases {
		fake := newFake(t, one.answer)
		judge := judgeFor(t, fake.URL, 100)
		_, err := judge.Support("근거 문장", "주장 문장")
		if err == nil || err.Error() != one.want {
			t.Errorf("%s : %q 여야 하는데 %v", one.name, one.want, err)
		}
		supported, ok := judge.Supports("근거 문장", "주장 문장")
		if supported || ok || judge.Problem() != one.want {
			t.Errorf("%s : Supports 는 (false,false) · 까닭 %q 여야 한다 : %v %v %q", one.name, one.want, supported, ok, judge.Problem())
		}
		if files, _ := os.ReadDir(judge.Dir); len(files) != 0 {
			t.Errorf("%s : 못 받은 판정을 기록했다", one.name)
		}
		if fake.hits.Load() != 2 {
			t.Errorf("%s : 재시도 없이 부를 때마다 한 번이어야 한다 : %d", one.name, fake.hits.Load())
		}
	}
}

func TestDisabledAndUnreachable(t *testing.T) {
	if New(config.LLMConfig{}) != nil {
		t.Fatal("url 이 없으면 Client 가 nil 이어야 한다")
	}
	var nothing *Judge
	if _, err := nothing.Support("a", "b"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("nil 판정기는 꺼짐이다 : %v", err)
	}
	// Client 없는 판정기도 규칙 단은 돈다. 규칙에 안 걸리면 글자 빈 「모른다」다.
	if verdict, err := (&Judge{}).Support("a", "b"); err != nil || verdict.Letter != "" || !verdict.Unsure || verdict.Stage != StageRules {
		t.Fatalf("Client 없는 판정기는 규칙 단만 돈다 : %+v %v", verdict, err)
	}
	if _, err := (&Judge{Only: []string{StageSemIf}}).Support("a", "b"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("SemIf 만 고르고 Client 가 없으면 꺼짐이다 : %v", err)
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	address := closed.URL
	closed.Close()
	judge := judgeFor(t, address, 500)
	if _, err := judge.Support("a", "b"); err == nil || err.Error() != "unreachable" {
		t.Fatalf("닫힌 서버는 unreachable : %v", err)
	}
}

func TestRefusedTextIsNotSent(t *testing.T) {
	fake := newFake(t, answerWith(logprobsBody(map[string]float64{"A": 1})))
	judge := judgeFor(t, fake.URL, 1000)
	judge.Refuse = func(text string) bool { return strings.Contains(text, "SECRET") }
	if _, err := judge.Support("token SECRET here", "주장"); !errors.Is(err, ErrRefused) {
		t.Fatalf("비밀 꼴은 거절해야 한다 : %v", err)
	}
	if fake.hits.Load() != 0 {
		t.Fatal("비밀 꼴을 서버로 보냈다")
	}
}

func TestEndpointShapes(t *testing.T) {
	for given, want := range map[string]string{
		"http://127.0.0.1:8080":                     "http://127.0.0.1:8080/v1/chat/completions",
		"http://127.0.0.1:8080/":                    "http://127.0.0.1:8080/v1/chat/completions",
		"http://127.0.0.1:8080/v1":                  "http://127.0.0.1:8080/v1/chat/completions",
		"http://127.0.0.1:8080/v1/chat/completions": "http://127.0.0.1:8080/v1/chat/completions",
	} {
		if got := Endpoint(given); got != want {
			t.Errorf("%s → %s (기대 %s)", given, got, want)
		}
	}
}

// v3 — 최고 확률 0.40 아래거나 동점이면 「모른다」. 글자는 그대로 두고 Supports 는 경고로 흘린다.
func TestSupportUnsureIsWarningNotRejection(t *testing.T) {
	for name, one := range map[string]struct {
		tops   map[string]float64
		unsure bool
	}{
		"또렷":    {map[string]float64{"A": 0.80, "B": 0.10, "C": 0.10}, false},
		"선 위 동점": {map[string]float64{"A": 0.45, "B": 0.10, "C": 0.45}, true},
		"선 아래":   {map[string]float64{"A": 0.38, "B": 0.22, "C": 0.39}, true},
		"선 바로 위": {map[string]float64{"A": 0.41, "B": 0.20, "C": 0.39}, false},
	} {
		fake := newFake(t, answerWith(logprobsBody(one.tops)))
		judge := judgeFor(t, fake.URL, 2000)
		verdict, err := judge.Support("근거", "주장")
		if err != nil || verdict.Unsure != one.unsure || verdict.Letter == "" {
			t.Errorf("%s : unsure=%v 여야 한다 : %+v %v", name, one.unsure, verdict, err)
		}
		_, ok := judge.Supports("근거", "주장")
		if ok == one.unsure {
			t.Errorf("%s : Supports ok 는 %v 여야 한다", name, !one.unsure)
		}
		if one.unsure && !strings.HasPrefix(judge.Problem(), "unsure") {
			t.Errorf("%s : 경고 까닭이 unsure 로 시작해야 한다 : %q", name, judge.Problem())
		}
	}
}

// 물음 글 판은 이름으로 찾고, 판마다 해시가 갈려 기록이 섞이지 않는다.
func TestPromptVersionsKeepRecordsApart(t *testing.T) {
	for _, name := range []string{"v2", "v3", "v3-strict", "support-v3"} {
		if _, ok := PromptNamed(name); !ok {
			t.Errorf("%s 를 못 찾는다", name)
		}
	}
	if _, ok := PromptNamed("v9"); ok {
		t.Error("없는 판을 찾았다")
	}
	if DefaultPrompt.Version != PromptVersion {
		t.Errorf("기본 판 %s 와 PromptVersion %s 가 다르다", DefaultPrompt.Version, PromptVersion)
	}
	fake := newFake(t, answerWith(logprobsBody(map[string]float64{"A": 0.90, "B": 0.05, "C": 0.05})))
	judge := judgeFor(t, fake.URL, 2000)
	hashes := map[string]bool{}
	for _, prompt := range []Prompt{PromptV2, PromptV3, PromptV3Strict} {
		copied := prompt
		judge.Prompt = &copied
		verdict, err := judge.Support("근거", "주장")
		if err != nil || verdict.Cached || verdict.Prompt != prompt.Version {
			t.Fatalf("%s : %+v %v", prompt.Version, verdict, err)
		}
		hashes[verdict.Hash] = true
	}
	if len(hashes) != 3 || fake.hits.Load() != 3 {
		t.Fatalf("판 셋은 서로 다른 기록이어야 한다 : %d개 · hits=%d", len(hashes), fake.hits.Load())
	}
}
