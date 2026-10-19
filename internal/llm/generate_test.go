package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
)

const okReply = `{"choices":[{"message":{"role":"assistant","content":"{\"candidates\":[],\"nothing\":true}"}}],` +
	`"usage":{"prompt_tokens":120,"completion_tokens":9}}`

// generatorFor 는 시험 서버를 가리키는 생성기다. 주소는 뿌리만 줘 Endpoint 정규화도 같이 지난다.
func generatorFor(t *testing.T, address, model string, timeoutMS int) *Generator {
	t.Helper()
	generator, ok := NewGenerator(config.LLMConfig{GenerateURL: address, GenerateModel: model,
		GenerateTimeoutMS: timeoutMS, Key: "secret-key"})
	if !ok {
		t.Fatal("주소가 있는데 생성기를 못 만들었다")
	}
	return generator
}

func TestNewGeneratorNeedsAddress(t *testing.T) {
	if generator, ok := NewGenerator(config.LLMConfig{GenerateTimeoutMS: 1000}); ok || generator != nil {
		t.Fatal("주소가 없으면 꺼짐(nil, false)이어야 한다")
	}
}

// 정상 답 — 요청 꼴(설계 4절 표)과 받은 값을 같이 본다.
func TestGenerateRequestShapeAndResult(t *testing.T) {
	var got map[string]any
	var path, auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("요청이 JSON 이 아니다 : %v", err)
		}
		_, _ = w.Write([]byte(okReply))
	}))
	defer server.Close()

	result, err := generatorFor(t, server.URL, "strata-model", 5000).Generate(context.Background(), "시스템", "사용자")
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != `{"candidates":[],"nothing":true}` || result.PromptTokens != 120 || result.CompletionTokens != 9 ||
		result.MS < 0 {
		t.Fatalf("받은 값이 틀렸다 : %+v", result)
	}
	if path != "/v1/chat/completions" || auth != "Bearer secret-key" {
		t.Fatalf("주소·키가 틀렸다 : %s %s", path, auth)
	}
	format, _ := got["response_format"].(map[string]any)
	template, _ := got["chat_template_kwargs"].(map[string]any)
	if got["model"] != "strata-model" || got["temperature"] != 0.0 || got["max_tokens"] != 1500.0 ||
		format["type"] != "json_object" || template["enable_thinking"] != false || got["stream"] != false {
		t.Fatalf("요청 꼴이 설계 4절과 다르다 : %v", got)
	}
	messages, _ := got["messages"].([]any)
	if len(messages) != 2 || !strings.Contains(string(mustJSON(t, messages[0])), `"system"`) ||
		!strings.Contains(string(mustJSON(t, messages[1])), `"사용자"`) {
		t.Fatalf("messages 가 틀렸다 : %v", messages)
	}
}

// 모델 id 가 비면 model 칸을 아예 빼고 보낸다. 키가 비면 Authorization 도 없다.
func TestGenerateOmitsEmptyModel(t *testing.T) {
	var raw string
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		raw, auth = string(body), r.Header.Get("Authorization")
		_, _ = w.Write([]byte(okReply))
	}))
	defer server.Close()
	generator, _ := NewGenerator(config.LLMConfig{GenerateURL: server.URL + "/v1", GenerateTimeoutMS: 5000})
	if _, err := generator.Generate(context.Background(), "s", "u"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, `"model"`) || auth != "" {
		t.Fatalf("빈 model 칸·빈 키가 나갔다 : %s / %q", raw, auth)
	}
}

// 401 은 StatusError 로 돌려주고, 오류 글에 주소·키가 없다.
func TestGenerateStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	defer server.Close()
	_, err := generatorFor(t, server.URL, "", 5000).Generate(context.Background(), "s", "u")
	var status StatusError
	if !errors.As(err, &status) || status.Code != http.StatusUnauthorized {
		t.Fatalf("401 이 StatusError 가 아니다 : %v", err)
	}
	assertQuietError(t, err, server.URL)
}

// 200 이 아닌 답의 본문 앞 200자를 한 줄로 오류 글에 싣는다 (10-19 Strata 502 까닭 못 봄).
func TestGenerateStatusErrorCarriesBody(t *testing.T) {
	long := strings.Repeat("가", 300)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, "upstream closed\nafter stop "+long)
	}))
	defer server.Close()
	_, err := generatorFor(t, server.URL, "secret-key", 5000).Generate(context.Background(), "s", "u")
	var status StatusError
	if !errors.As(err, &status) || status.Code != http.StatusBadGateway {
		t.Fatalf("502 가 StatusError 가 아니다 : %v", err)
	}
	text := err.Error()
	if !strings.HasPrefix(text, "http-502: upstream closed after stop 가") || strings.Contains(text, "\n") {
		t.Fatalf("본문이 한 줄로 안 실렸다 : %q", text)
	}
	if len([]rune(status.Body)) > statusBodyClip || strings.Contains(text, "secret-key") {
		t.Fatalf("본문이 200자를 넘거나 키가 실렸다 : %d %q", len([]rune(status.Body)), text)
	}
	assertQuietError(t, err, server.URL)
}

// 시간 상한을 넘기면 "timeout" 하나로 줄인다.
func TestGenerateTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	}))
	defer server.Close()
	_, err := generatorFor(t, server.URL, "", 50).Generate(context.Background(), "s", "u")
	if err == nil || err.Error() != "timeout" {
		t.Fatalf("시간 넘김이 timeout 이 아니다 : %v", err)
	}
}

// 닿지 않는 주소는 "unreachable" 이고 주소가 안 찍힌다.
func TestGenerateUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	address := server.URL
	server.Close()
	_, err := generatorFor(t, address, "", 2000).Generate(context.Background(), "s", "u")
	if err == nil || err.Error() != "unreachable" {
		t.Fatalf("닿지 않는 주소가 unreachable 이 아니다 : %v", err)
	}
	assertQuietError(t, err, address)
}

// 1MB 를 넘는 답은 잘라 읽지 않고 오류로 돌려준다 — 잘린 JSON 을 「깨진 답」으로 오해하지 않게.
func TestGenerateResponseTooLarge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"`))
		_, _ = w.Write([]byte(strings.Repeat("a", responseLimit)))
		_, _ = w.Write([]byte(`"}}]}`))
	}))
	defer server.Close()
	_, err := generatorFor(t, server.URL, "", 5000).Generate(context.Background(), "s", "u")
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("1MB 넘는 답이 ErrResponseTooLarge 가 아니다 : %v", err)
	}
}

// 200 인데 choices 가 비거나 JSON 이 아니면 ErrNoContent 다.
func TestGenerateNoContent(t *testing.T) {
	for _, reply := range []string{`{"choices":[]}`, `not json`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(reply))
		}))
		_, err := generatorFor(t, server.URL, "", 5000).Generate(context.Background(), "s", "u")
		server.Close()
		if !errors.Is(err, ErrNoContent) {
			t.Errorf("%q : ErrNoContent 가 아니다 : %v", reply, err)
		}
	}
}

func assertQuietError(t *testing.T, err error, address string) {
	t.Helper()
	text := err.Error()
	if strings.Contains(text, "secret-key") || strings.Contains(text, strings.TrimPrefix(address, "http://")) {
		t.Fatalf("오류 글에 키나 주소가 찍혔다 : %s", text)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
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
