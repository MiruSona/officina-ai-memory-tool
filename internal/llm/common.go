// Package llm 은 바깥 서버(OpenAI 호환 생성 서버 · JudgeModel NLI 서버)에 묻는 일을 맡는다.
// mem 은 서버를 띄우지 않고 llm.toml 에 적힌 주소에 손님으로 붙는다.
// 근거 지지 판정은 규칙 단과 NLI 단(JudgeModel) 둘로 한다 (judge.go).
package llm

import (
	"errors"
	"fmt"

	"github.com/mirusona/officina-ai-memory-tool/internal/safe"
)

// responseLimit 은 응답 몸통 상한이다.
const responseLimit = 1 << 20

// ErrDisabled 는 물을 서버·단이 없는 것이다. 부르는 쪽은 까닭만 알리고 건너뛴다.
var ErrDisabled = errors.New("llm-disabled")

// statusBodyClip 은 200 이 아닌 답의 본문을 오류 글에 실을 때의 글자 상한이다.
const statusBodyClip = 200

// StatusError 는 서버가 200 이 아닌 답을 준 것이다. Body 는 응답 본문 앞 200자를 한 줄로 만든 것이다
// (머리는 안 싣는다).
type StatusError struct {
	Code int
	Body string
}

func (e StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("http-%d", e.Code)
	}
	return fmt.Sprintf("http-%d: %s", e.Code, e.Body)
}

// newStatusError 는 본문을 한 줄로 납작하게 만들고 statusBodyClip 으로 자른다.
func newStatusError(code int, body []byte) StatusError {
	return StatusError{Code: code, Body: safe.Clip(safe.OneLine(string(body)), statusBodyClip)}
}

// Choice 는 서버가 준 판정이다. Probs 는 글자(A 지지 · B 반대 · C 무관)마다 받은 확률 그대로다.
type Choice struct {
	Letter string             `json:"letter"`
	Prob   float64            `json:"prob"`
	Probs  map[string]float64 `json:"probs"`
	MS     int64              `json:"ms"`
	// LabelSet 은 NLI 서버가 답한 응답 꼴(nli3 · nli4)이다.
	LabelSet string `json:"label_set,omitempty"`
	// Truncated 는 NLI 서버가 입력을 잘랐다고 답했는지다.
	Truncated bool `json:"truncated,omitempty"`
}

// chatMessage 는 OpenAI 호환 요청의 메시지 한 칸이다.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ServerDown 은 생성 요청 오류가 서버 쪽 문제(안 닿음 · 시간 넘김 · 5xx · 인증 · 주소 · 요청 과다)인지다.
// 그때는 다음 요청도 같으니 부르는 쪽이 판을 멈춘다. 그 밖의 4xx(문맥 초과 등)는 그 요청 하나의 문제다.
func ServerDown(err error) bool {
	var status StatusError
	if errors.As(err, &status) {
		switch status.Code {
		case 401, 403, 404, 429:
			return true
		}
		return status.Code >= 500
	}
	text := err.Error()
	return text == "timeout" || text == "unreachable"
}

// plainError 는 전송 오류를 짧은 까닭으로 줄인다. 주소가 든 원문은 화면에 안 낸다.
func plainError(err error) error {
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return errors.New("timeout")
	}
	return errors.New("unreachable")
}
