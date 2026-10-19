package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

// ErrResponseTooLarge 는 답 몸통이 responseLimit 을 넘은 것이다. 잘라 읽으면 깨진 JSON 처럼
// 보여 「모델이 틀렸다」로 오해하므로 따로 알린다.
var ErrResponseTooLarge = errors.New("response-too-large")

// NewClient 는 리다이렉트를 따라가지 않는 http.Client 다. 루프백 판정·생성 서버가 307 을 주면
// POST 몸통과 Bearer 키가 다른 곳으로 다시 실려 가므로, 답을 그대로 받아 200 이 아닌 것으로 다룬다.
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// postJSON 은 payload 를 JSON 으로 POST 하고 200 답의 몸통을 돌려준다. key 가 있으면 Bearer 로 싣는다.
// 오류 글에는 주소·키를 안 싣는다 — 전송 오류는 plainError 로, 200 이 아니면 본문 앞 200자를 단 StatusError 로 줄인다.
func postJSON(ctx context.Context, client *http.Client, endpoint, key string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, plainError(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, plainError(err)
	}
	defer response.Body.Close()
	data, err := readLimited(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, newStatusError(response.StatusCode, data)
	}
	return data, nil
}

// readLimited 는 몸통을 responseLimit 까지 읽는다. 한 바이트라도 더 있으면 ErrResponseTooLarge 다.
func readLimited(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, responseLimit+1))
	if err != nil {
		return nil, plainError(err)
	}
	if len(data) > responseLimit {
		return nil, ErrResponseTooLarge
	}
	return data, nil
}
