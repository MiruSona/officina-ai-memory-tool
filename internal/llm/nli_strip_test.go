package llm

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 서버에는 꼬리표를 뗀 글이 간다 (quality.StripTags · JudgeModel strip_tags 와 같은 규칙).
func TestAskNLIStripsTags(t *testing.T) {
	got := make(chan nliRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request nliRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		got <- request
		io.WriteString(w, `{"a":0.9,"b":0.05,"c":0.05,"model":"abcd1234","ms":3.0}`)
	}))
	t.Cleanup(server.Close)
	verdict, err := (&Judge{NLI: nliFor(server.URL, 1000)}).Support("왜 : 스왑이 밀렸다\n근거 : 실측", "무엇을 : 문턱을 올렸다")
	if err != nil {
		t.Fatal(err)
	}
	request := <-got
	if request.Evidence != "스왑이 밀렸다\n실측" || request.Claim != "문턱을 올렸다" {
		t.Fatalf("꼬리표를 떼고 보내야 한다 : %+v", request)
	}
	if verdict.Evidence != request.Evidence || verdict.Claim != request.Claim {
		t.Fatalf("기록에는 보낸 글이 남아야 한다 : %+v", verdict)
	}
}
