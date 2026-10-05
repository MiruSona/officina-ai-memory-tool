// Package llm 은 바깥 LLM(OpenAI 호환 HTTP 서버)에 묻는 일을 맡는다 (자동쌓기설계
// 1-3 · 5절 K). mem 은 서버를 띄우지 않고 llm.toml 에 적힌 주소에 손님으로 붙는다.
//
// 판정은 SemIf 방식이다 — 선택지를 영문 글자로 주고, 생각을 끈 채 `max_tokens 1` 로
// 물어 **첫 토큰의 위 20개 확률**에서 글자 토큰만 정확 일치로 모은다. 글을 한 글자도
// 안 만들게 하므로 지어낼 자리가 없다.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
)

const (
	chatPath = "/chat/completions"
	// topLogprobs 는 서버에 달라는 첫 토큰 후보 수다. 실측에서 영문 글자는 이 안에 늘 들었다.
	topLogprobs = 20
	// letterMassMin 은 글자 토큰에 간 확률 합의 바닥이다. 이보다 적으면 모델이 글자가
	// 아닌 것(생각 글·딴소리)을 내려 한 것이라 판정을 못 받은 것으로 친다.
	letterMassMin = 0.5
	// responseLimit 은 응답 몸통 상한이다. 한 토큰 답이라 이만큼이면 넉넉하다.
	responseLimit = 1 << 20
)

// 판정을 못 받은 까닭들이다. 부르는 쪽은 까닭만 알리고 건너뛴다.
var (
	ErrDisabled   = errors.New("llm-disabled")
	ErrNoLogprobs = errors.New("no-logprobs")
	ErrNoLetter   = errors.New("no-letter")
)

// StatusError 는 서버가 200 이 아닌 답을 준 것이다.
type StatusError struct{ Code int }

func (e StatusError) Error() string { return fmt.Sprintf("http-%d", e.Code) }

// Client 는 서버 하나다. nil 이면 꺼진 것이다.
type Client struct {
	endpoint string
	key      string
	profile  string
	http     *http.Client
}

// New 는 설정이 켜져 있을 때만 Client 를 만든다. 꺼졌으면 nil 이다.
func New(settings config.LLMConfig) *Client {
	if !settings.Enabled() {
		return nil
	}
	timeout := time.Duration(settings.TimeoutMS) * time.Millisecond
	return &Client{endpoint: Endpoint(settings.URL), key: settings.Key,
		profile: settings.JudgeProfile, http: &http.Client{Timeout: timeout}}
}

// Profile 은 판정에 쓰는 프로필(요청의 model 칸) 이름이다. 비면 "-" 다.
func (c *Client) Profile() string {
	if c == nil || c.profile == "" {
		return "-"
	}
	return c.profile
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

// Question 은 글자 하나로 답할 물음이다.
type Question struct {
	System  string
	User    string
	Letters []string
}

// Choice 는 받은 판정이다. Probs 는 글자마다 exp(logprob) 그대로다 (다시 맞추지 않음).
type Choice struct {
	Letter string             `json:"letter"`
	Prob   float64            `json:"prob"`
	Probs  map[string]float64 `json:"probs"`
	MS     int64              `json:"ms"`
}

// chatRequest 는 SemIf 요청 꼴이다. 생각 끄기는 llama.cpp 의 chat_template_kwargs 로 준다.
type chatRequest struct {
	Model       string         `json:"model,omitempty"`
	Messages    []chatMessage  `json:"messages"`
	MaxTokens   int            `json:"max_tokens"`
	Temperature float64        `json:"temperature"`
	Logprobs    bool           `json:"logprobs"`
	TopLogprobs int            `json:"top_logprobs"`
	Stream      bool           `json:"stream"`
	Template    map[string]any `json:"chat_template_kwargs"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Logprobs *struct {
			Content []struct {
				Token       string `json:"token"`
				TopLogprobs []struct {
					Token   string  `json:"token"`
					Logprob float64 `json:"logprob"`
				} `json:"top_logprobs"`
			} `json:"content"`
		} `json:"logprobs"`
	} `json:"choices"`
}

// Choose 는 한 번 묻는다. 재시도는 없다 — 안 되면 오류를 돌려주고 부르는 쪽이 건너뛴다.
func (c *Client) Choose(question Question) (Choice, error) {
	if c == nil {
		return Choice{}, ErrDisabled
	}
	body, err := json.Marshal(chatRequest{Model: c.profile,
		Messages: []chatMessage{{Role: "system", Content: question.System},
			{Role: "user", Content: question.User}},
		MaxTokens: 1, Temperature: 0, Logprobs: true, TopLogprobs: topLogprobs,
		Template: map[string]any{"enable_thinking": false}})
	if err != nil {
		return Choice{}, err
	}
	started := time.Now()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return Choice{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	if c.key != "" {
		request.Header.Set("Authorization", "Bearer "+c.key)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return Choice{}, plainError(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, responseLimit))
	if err != nil {
		return Choice{}, plainError(err)
	}
	if response.StatusCode != http.StatusOK {
		return Choice{}, StatusError{Code: response.StatusCode}
	}
	choice, err := readChoice(data, question.Letters)
	choice.MS = time.Since(started).Milliseconds()
	return choice, err
}

// readChoice 는 첫 토큰 위 20개에서 글자 토큰만 **정확 일치**로 모은다. 「 A」 같은
// 쌍둥이는 합치지 않는다 — 합쳐도 답이 바뀐 요청이 0 이었고, 합치다 덮어쓰는 실수가
// 알려진 사고다 (판정모델 실측 2-3).
func readChoice(data []byte, letters []string) (Choice, error) {
	var parsed chatResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Choice{}, ErrNoLogprobs
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Logprobs == nil ||
		len(parsed.Choices[0].Logprobs.Content) == 0 ||
		len(parsed.Choices[0].Logprobs.Content[0].TopLogprobs) == 0 {
		return Choice{}, ErrNoLogprobs
	}
	choice := Choice{Probs: map[string]float64{}}
	mass := 0.0
	for _, top := range parsed.Choices[0].Logprobs.Content[0].TopLogprobs {
		for _, letter := range letters {
			if top.Token != letter {
				continue
			}
			if _, seen := choice.Probs[letter]; seen {
				continue
			}
			prob := math.Exp(top.Logprob)
			choice.Probs[letter] = prob
			mass += prob
			if prob > choice.Prob {
				choice.Letter, choice.Prob = letter, prob
			}
		}
	}
	if choice.Letter == "" || mass < letterMassMin {
		return choice, ErrNoLetter
	}
	return choice, nil
}

// plainError 는 전송 오류를 짧은 까닭으로 줄인다. 주소가 든 원문은 화면에 안 낸다.
func plainError(err error) error {
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return errors.New("timeout")
	}
	return errors.New("unreachable")
}
