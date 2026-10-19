package llm

import (
	"errors"
	"testing"
)

// mem retain 은 서버 쪽 오류면 판을 멈추고, 요청 하나의 오류(400 문맥 초과 등)면 그 세션만 접는다.
func TestServerDownSplitsGenerateErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		down bool
	}{
		{"안 닿음", errors.New("unreachable"), true},
		{"시간 넘김", errors.New("timeout"), true},
		{"502", StatusError{Code: 502, Body: "bad gateway"}, true},
		{"401", StatusError{Code: 401}, true},
		{"400 문맥 초과", StatusError{Code: 400, Body: "context length exceeded"}, false},
		{"413", StatusError{Code: 413}, false},
		{"답 너무 큼", ErrResponseTooLarge, false},
	}
	for _, one := range cases {
		if got := ServerDown(one.err); got != one.down {
			t.Errorf("%s : ServerDown = %v, 바라는 값 %v", one.name, got, one.down)
		}
	}
}
