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

// NLI 판정 서버(판정 사다리 ② 단)에 HTTP 손님으로 붙는다. 서버는 이 툴 밖에 있고 JudgeModel 의
// 응답 꼴을 따른다 — POST /judge {"evidence","claim","label_set"} → {"a","b","c","model","ms"}
// (a 지지 · b 반대 · c 무관 확률, model 은 가중치 SHA 앞 8자). 덧칸 label_set·labels·model_version 은 받아만 둔다.

const (
	// NLIVersion 은 NLI 단의 판 이름이다 (Verdict.Prompt 칸). 모델 자체는 Profile 칸(model)이 가른다.
	NLIVersion = "nli-v1"
	nliPath    = "/judge"
	// nliProbSlack 은 a+b+c 가 1 에서 벗어나도 받아 주는 폭이다 (반올림 몫).
	nliProbSlack = 0.02
	// nliModelMax 는 응답의 model 칸을 받아 두는 길이 상한이다 (기록의 Profile 칸).
	nliModelMax = 64
	// NLILabelSet 은 요청에 싣는 응답 꼴이다. 서버가 4칸(d)으로 바뀌어도 3칸으로 답하게 못 박는다.
	NLILabelSet = "nli3"
	// NLILabelSet4 는 4칸 서버가 그대로 답했다는 표시다 (C04 pass 모드만 본다).
	NLILabelSet4 = "nli4"
)

// NLIClient 는 NLI 판정 서버 하나다. nil 이면 꺼진 것이다.
type NLIClient struct {
	endpoint    string
	sure        float64
	sureSupport float64
	http        *http.Client
}

// NewNLI 는 nli_url 이 적혀 있을 때만 NLIClient 를 만든다. 비었으면 nil 이다.
func NewNLI(settings config.LLMConfig) *NLIClient {
	if settings.NLIURL == "" {
		return nil
	}
	timeout := time.Duration(settings.NLITimeoutMS) * time.Millisecond
	return &NLIClient{endpoint: strings.TrimRight(settings.NLIURL, "/") + nliPath,
		sure: settings.NLISure, sureSupport: settings.NLISureSupport, http: NewClient(timeout)}
}

// Sure 는 반대·무관(B·C) 답을 확정하는 선(nli_sure)이다. 이 아래면 다음 단으로 넘긴다.
func (c *NLIClient) Sure() float64 { return c.sure }

// SureSupport 는 지지(A) 답을 확정하는 선(nli_sure_support)이다. 막을 실수가 거짓 지지라
// 보통 Sure 보다 높다.
func (c *NLIClient) SureSupport() float64 { return c.sureSupport }

// sureFor 는 그 글자를 확정하는 선이다.
func (c *NLIClient) sureFor(letter string) float64 {
	if letter == LetterSupport {
		return c.sureSupport
	}
	return c.sure
}

type nliRequest struct {
	Evidence string `json:"evidence"`
	Claim    string `json:"claim"`
	LabelSet string `json:"label_set"`
}

type nliResponse struct {
	A *float64 `json:"a"`
	B *float64 `json:"b"`
	C *float64 `json:"c"`
	// D 는 4칸 서버의 「같은 대상, 다른 사실」이다. 글자로는 안 쓰고 합 검사에만 넣는다.
	D     *float64 `json:"d"`
	Model string   `json:"model"`
	// MS 는 서버가 소수로 준다 (42.0). 정수 칸이면 해석이 깨져 모든 답이 bad-answer 가 된다.
	MS float64 `json:"ms"`
	// 덧칸이다. 없어도 되고, labels·model_version 은 꼴만 받아 둔다 (값을 안 쓴다).
	LabelSet     string          `json:"label_set"`
	Labels       json.RawMessage `json:"labels"`
	ModelVersion json.RawMessage `json:"model_version"`
}

// Judge 는 한 번 묻는다. model 은 응답의 모델 이름(SHA 앞 8자)이다. 재시도는 없다 —
// 오류는 nli: 로 시작하는 짧은 까닭이고 부르는 쪽이 이 단을 건너뛴다.
func (c *NLIClient) Judge(evidence, claim string) (Choice, string, error) {
	if c == nil {
		return Choice{}, "", errors.New("nli:off")
	}
	body, err := json.Marshal(nliRequest{Evidence: evidence, Claim: claim, LabelSet: NLILabelSet})
	if err != nil {
		return Choice{}, "", err
	}
	started := time.Now()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return Choice{}, "", errors.New("nli:bad-url")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return Choice{}, "", errors.New("nli:" + plainError(err).Error())
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, responseLimit))
	if err != nil {
		return Choice{}, "", errors.New("nli:" + plainError(err).Error())
	}
	if response.StatusCode != http.StatusOK {
		return Choice{}, "", errors.New("nli:" + StatusError{Code: response.StatusCode}.Error())
	}
	choice, model, err := readNLI(data)
	choice.MS = time.Since(started).Milliseconds()
	return choice, model, err
}

// readNLI 는 {"a","b","c","model","ms"} 를 Choice 로 바꾼다. 칸이 빠졌거나 0~1 밖이거나
// 합이 1 에서 멀면 받지 않는다. label_set 은 짧은 이름일 때만 Choice 에 옮긴다.
func readNLI(data []byte) (Choice, string, error) {
	var parsed nliResponse
	if json.Unmarshal(data, &parsed) != nil || parsed.A == nil || parsed.B == nil || parsed.C == nil {
		return Choice{}, "", errors.New("nli:bad-answer")
	}
	probs := map[string]float64{LetterSupport: *parsed.A, LetterContradict: *parsed.B, LetterUnrelated: *parsed.C}
	choice := Choice{Probs: probs}
	total := 0.0
	for _, letter := range []string{LetterSupport, LetterContradict, LetterUnrelated} {
		prob := probs[letter]
		if math.IsNaN(prob) || prob < 0 || prob > 1 {
			return Choice{}, "", errors.New("nli:bad-answer")
		}
		total += prob
		if prob > choice.Prob {
			choice.Letter, choice.Prob = letter, prob
		}
	}
	if parsed.D != nil {
		if math.IsNaN(*parsed.D) || *parsed.D < 0 || *parsed.D > 1 {
			return Choice{}, "", errors.New("nli:bad-answer")
		}
		total += *parsed.D
	}
	if choice.Letter == "" || math.Abs(total-1) > nliProbSlack {
		return Choice{}, "", errors.New("nli:bad-answer")
	}
	choice.LabelSet = labelSetOf(parsed.LabelSet)
	model := strings.TrimSpace(parsed.Model)
	if len(model) > nliModelMax || model == "" {
		model = "-"
	}
	return choice, model, nil
}

// labelSetOf 는 응답의 label_set 을 받아 둘 꼴로 고른다. 모르는 값은 기록에 남기지 않는다.
func labelSetOf(value string) string {
	value = strings.TrimSpace(value)
	if value == NLILabelSet || value == NLILabelSet4 {
		return value
	}
	return ""
}
