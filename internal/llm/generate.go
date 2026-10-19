package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
)

// chatPath 는 OpenAI 호환 생성 끝 주소다.
const chatPath = "/chat/completions"

// generateMaxTokens 는 생성 답 상한이다 — 후보 3개 × 약 400 토큰 + 여유 (A2 retain 설계 4절).
const generateMaxTokens = 1500

// ErrNoContent 는 200 답인데 JSON 이 아니거나 choices 가 빈 것이다. 빈 content 는 오류가 아니다.
var ErrNoContent = errors.New("no-content")

// Generator 는 글을 만드는 서버(Strata) 하나다. 판정용 NLI 서버와 주소·시간 상한이 따로다.
type Generator struct {
	endpoint string
	key      string
	model    string
	http     *http.Client
}

// GenResult 는 생성 한 번의 답이다. 토큰 수는 서버가 usage 를 안 주면 0 이다.
type GenResult struct {
	Content          string
	PromptTokens     int
	CompletionTokens int
	MS               int64
}

// generateRequest 는 생성 요청 꼴이다. temperature 0 도 보내야 하므로 omitempty 를 안 단다.
type generateRequest struct {
	Model          string            `json:"model,omitempty"`
	Messages       []chatMessage     `json:"messages"`
	MaxTokens      int               `json:"max_tokens"`
	Temperature    float64           `json:"temperature"`
	Stream         bool              `json:"stream"`
	ResponseFormat map[string]string `json:"response_format"`
	Template       map[string]any    `json:"chat_template_kwargs"`
}

type generateResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Endpoint 는 적힌 주소를 chat/completions 주소로 맞춘다. 뿌리(`http://h:8080`) ·
// `/v1` · 끝 주소 셋 다 받는다.
func Endpoint(address string) string {
	address = strings.TrimRight(strings.TrimSpace(address), "/")
	switch {
	case strings.HasSuffix(address, chatPath):
		return address
	case strings.HasSuffix(address, "/v1"):
		return address + chatPath
	}
	return address + "/v1" + chatPath
}

// NewGenerator 는 생성 주소가 적혀 있을 때만 만든다. 없으면 (nil, false) 다.
func NewGenerator(settings config.LLMConfig) (*Generator, bool) {
	if settings.GenerateURL == "" {
		return nil, false
	}
	timeout := time.Duration(settings.GenerateTimeoutMS) * time.Millisecond
	return &Generator{endpoint: Endpoint(settings.GenerateURL), key: settings.Key,
		model: settings.GenerateModel, http: NewClient(timeout)}, true
}

// Generate 는 한 번 묻는다. 재시도는 없다 — JSON 이 깨졌을 때 다시 묻는 것은 부르는 쪽 몫이다.
// 생각을 끄고 response_format json_object 를 켠다 (10-19 Strata 가 둘 다 받는 것 확인).
func (g *Generator) Generate(ctx context.Context, system, user string) (GenResult, error) {
	if g == nil {
		return GenResult{}, ErrDisabled
	}
	payload := generateRequest{Model: g.model,
		Messages:  []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}},
		MaxTokens: generateMaxTokens, Temperature: 0,
		ResponseFormat: map[string]string{"type": "json_object"},
		Template:       map[string]any{"enable_thinking": false}}
	started := time.Now()
	data, err := postJSON(ctx, g.http, g.endpoint, g.key, payload)
	elapsed := time.Since(started).Milliseconds()
	if err != nil {
		return GenResult{MS: elapsed}, err
	}
	var parsed generateResponse
	if err := json.Unmarshal(data, &parsed); err != nil || len(parsed.Choices) == 0 {
		return GenResult{MS: elapsed}, ErrNoContent
	}
	return GenResult{Content: parsed.Choices[0].Message.Content, PromptTokens: parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens, MS: elapsed}, nil
}
