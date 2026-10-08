package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
)

// Laya 판정 프로세스(판정 사다리 ② 단)에 손님으로 붙는다. 프로세스는 AIMemoryTool/judge-laya/
// 의 serve.py 이고, 끝점·응답 꼴은 자체판정프로그램설계(2026-10-08) 4절.

const (
	// LayaVersion 은 Laya 판 이름이다 (Verdict.Prompt 칸).
	LayaVersion = "laya-v2"
	layaPath    = "/judge"
	// layaProbSlack 은 a+b+c 가 1 에서 벗어나도 받아 주는 폭이다 (반올림 몫).
	layaProbSlack = 0.02
	// layaModelMax 는 응답의 model 칸을 받아 두는 길이 상한이다 (기록의 Profile 칸).
	layaModelMax = 64
)

// LayaClient 는 Laya 판정 프로세스 하나다. nil 이면 꺼진 것이다.
type LayaClient struct {
	endpoint string
	sure     float64
	http     *http.Client
}

// NewLaya 는 laya_url 이 적혀 있을 때만 LayaClient 를 만든다. 비었으면 nil 이다.
func NewLaya(settings config.LLMConfig) *LayaClient {
	if settings.LayaURL == "" {
		return nil
	}
	timeout := time.Duration(settings.LayaTimeoutMS) * time.Millisecond
	return &LayaClient{endpoint: strings.TrimRight(settings.LayaURL, "/") + layaPath,
		sure: settings.LayaSure, http: &http.Client{Timeout: timeout}}
}

// Sure 는 이 확률 아래면 다음 단으로 넘기는 선(laya_sure)이다.
func (c *LayaClient) Sure() float64 { return c.sure }

type layaRequest struct {
	Evidence string `json:"evidence"`
	Claim    string `json:"claim"`
}

type layaResponse struct {
	A     *float64 `json:"a"`
	B     *float64 `json:"b"`
	C     *float64 `json:"c"`
	Model string   `json:"model"`
	// MS 는 serve.py 가 소수로 준다 (42.0). 정수 칸이면 해석이 깨져 모든 답이 bad-answer 가 된다.
	MS float64 `json:"ms"`
}

// Judge 는 한 번 묻는다. model 은 응답의 모델 이름(SHA 앞 8자)이다. 재시도는 없다 —
// 오류는 laya: 로 시작하는 짧은 까닭이고 부르는 쪽이 이 단을 건너뛴다.
func (c *LayaClient) Judge(evidence, claim string) (Choice, string, error) {
	if c == nil {
		return Choice{}, "", errors.New("laya:off")
	}
	body, err := json.Marshal(layaRequest{Evidence: evidence, Claim: claim})
	if err != nil {
		return Choice{}, "", err
	}
	started := time.Now()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return Choice{}, "", errors.New("laya:bad-url")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return Choice{}, "", errors.New("laya:" + plainError(err).Error())
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, responseLimit))
	if err != nil {
		return Choice{}, "", errors.New("laya:" + plainError(err).Error())
	}
	if response.StatusCode != http.StatusOK {
		return Choice{}, "", errors.New("laya:" + StatusError{Code: response.StatusCode}.Error())
	}
	choice, model, err := readLaya(data)
	choice.MS = time.Since(started).Milliseconds()
	return choice, model, err
}

// readLaya 는 {"a","b","c","model","ms"} 를 Choice 로 바꾼다. 칸이 빠졌거나 0~1 밖이거나
// 합이 1 에서 멀면 받지 않는다.
func readLaya(data []byte) (Choice, string, error) {
	var parsed layaResponse
	if json.Unmarshal(data, &parsed) != nil || parsed.A == nil || parsed.B == nil || parsed.C == nil {
		return Choice{}, "", errors.New("laya:bad-answer")
	}
	probs := map[string]float64{LetterSupport: *parsed.A, LetterContradict: *parsed.B, LetterUnrelated: *parsed.C}
	choice := Choice{Probs: probs}
	total := 0.0
	for _, letter := range []string{LetterSupport, LetterContradict, LetterUnrelated} {
		prob := probs[letter]
		if math.IsNaN(prob) || prob < 0 || prob > 1 {
			return Choice{}, "", errors.New("laya:bad-answer")
		}
		total += prob
		if prob > choice.Prob {
			choice.Letter, choice.Prob = letter, prob
		}
	}
	if choice.Letter == "" || math.Abs(total-1) > layaProbSlack {
		return Choice{}, "", errors.New("laya:bad-answer")
	}
	model := strings.TrimSpace(parsed.Model)
	if len(model) > layaModelMax || model == "" {
		model = "-"
	}
	return choice, model, nil
}
