package llm

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// #7 요청에는 늘 label_set "nli3" 가 실린다 — 서버가 4칸으로 바뀌어도 응답 꼴이 3칸으로 고정된다.
func TestNLIRequestCarriesLabelSet(t *testing.T) {
	got := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		_ = json.NewDecoder(r.Body).Decode(&request)
		got <- request
		io.WriteString(w, `{"a":0.9,"b":0.05,"c":0.05,"model":"abcd1234","ms":3.0}`)
	}))
	t.Cleanup(server.Close)
	if _, _, err := nliFor(server.URL, 1000).Judge("근거", "주장"); err != nil {
		t.Fatal(err)
	}
	request := <-got
	if request["label_set"] != "nli3" {
		t.Fatalf("label_set 이 안 실렸다 : %v", request)
	}
}

// #7 덧칸(label_set · labels · model_version)이 있어도 받는다. 4칸 답의 d 는 합 검사에만 든다.
func TestReadNLIAcceptsExtraFields(t *testing.T) {
	body := `{"a":0.1,"b":0.8,"c":0.1,"model":"abcd1234","ms":4.5,"label_set":"nli3",` +
		`"labels":["a","b","c"],"model_version":"nli-studio-v10"}`
	choice, model, err := readNLI([]byte(body))
	if err != nil || choice.Letter != LetterContradict || model != "abcd1234" || choice.LabelSet != "nli3" {
		t.Fatalf("덧칸 있는 답을 못 받았다 : %+v %q %v", choice, model, err)
	}
	four := `{"a":0.05,"b":0.05,"c":0.5,"d":0.4,"model":"abcd1234","ms":4.0,"label_set":"nli4",` +
		`"labels":{"a":"지지"},"model_version":10}`
	choice, _, err = readNLI([]byte(four))
	if err != nil || choice.Letter != LetterUnrelated || choice.LabelSet != "nli4" {
		t.Fatalf("4칸 답을 못 받았다 : %+v %v", choice, err)
	}
	strange := `{"a":0.9,"b":0.05,"c":0.05,"model":"abcd1234","ms":1.0,"label_set":"뭔가 긴 이름"}`
	choice, _, err = readNLI([]byte(strange))
	if err != nil || choice.LabelSet != "" {
		t.Fatalf("모르는 label_set 은 비워 둔다 : %+v %v", choice, err)
	}
	if _, _, err := readNLI([]byte(`{"a":0.5,"b":0.1,"c":0.1,"d":0.9,"model":"x","ms":1}`)); err == nil {
		t.Fatal("d 까지 더한 합이 1 에서 멀면 받지 않는다")
	}
}

// 판정 기록(Verdict)에도 응답 꼴이 남는다 — C04 pass 모드가 이것을 본다.
func TestVerdictKeepsLabelSet(t *testing.T) {
	server, _ := newNLI(t, `{"a":0.05,"b":0.9,"c":0.05,"model":"abcd1234","ms":2.0,"label_set":"nli3"}`, 0, 0)
	verdict, err := (&Judge{NLI: nliFor(server.URL, 1000), Only: []string{StageNLI}}).Support(plainEvidence, plainClaim)
	if err != nil || verdict.LabelSet != "nli3" || verdict.Letter != LetterContradict {
		t.Fatalf("기록에 label_set 이 없다 : %+v %v", verdict, err)
	}
}
