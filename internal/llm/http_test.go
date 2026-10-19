package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 307 을 따라가면 POST 몸통과 Bearer 키가 다른 곳으로 다시 실려 간다. 따라가지 않고 오류로 받아야 한다.
func TestPostJSONDoesNotFollowRedirect(t *testing.T) {
	reached := false
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))
	defer elsewhere.Close()
	judge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusTemporaryRedirect)
	}))
	defer judge.Close()

	_, err := postJSON(context.Background(), NewClient(5*time.Second), judge.URL, "secret-key", map[string]string{"a": "b"})
	var status StatusError
	if !errors.As(err, &status) || status.Code != http.StatusTemporaryRedirect {
		t.Fatalf("307 을 StatusError 로 받아야 한다 : %v", err)
	}
	if reached {
		t.Fatal("리다이렉트를 따라가 다른 서버에 닿았다")
	}
}
