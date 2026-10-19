package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
)

// C04 NLI 판정기 꽂기 (적용 설계 2절 · 시험 #9). nli_url 이 없거나 off 면 안 꽂혀 거절문이 지금과 같다.
func TestAttachRivalJudge(t *testing.T) {
	memory := newRepo(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		io.WriteString(w, `{"a":0.02,"b":0.95,"c":0.03,"model":"fake0001","ms":2.0,"label_set":"nli3"}`)
	}))
	t.Cleanup(server.Close)
	repository := &config.Repository{Dir: memory, Config: config.Default("시험")}

	t.Setenv("MEM_LLM_CONFIG", filepath.Join(memory, "없는-llm.toml"))
	gate := quality.Options{}
	attachRivalJudge(&gate, repository)
	if gate.RivalJudge != nil {
		t.Fatal("nli_url 이 없는데 판정기를 꽂았다")
	}

	llmPath := filepath.Join(memory, "llm.toml")
	if err := os.WriteFile(llmPath, []byte("nli_url = \""+server.URL+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEM_LLM_CONFIG", llmPath)
	attachRivalJudge(&gate, repository)
	if gate.RivalJudge == nil || gate.RivalMode != quality.RivalHint {
		t.Fatalf("기본(hint)에서 판정기가 안 꽂혔다 : %q", gate.RivalMode)
	}
	verdict, err := gate.RivalJudge("훅은 세션 시작에만 붙인다", "훅을 모든 자리에 붙인다")
	if err != nil || verdict.Letter != quality.RivalContradict || verdict.Unsure || verdict.LabelSet != "nli3" {
		t.Fatalf("판정기 답이 이어지지 않았다 : %+v %v", verdict, err)
	}

	repository.Config.Quality.DecisionGateNLI = config.DecisionGatePass
	gate = quality.Options{}
	attachRivalJudge(&gate, repository)
	if gate.RivalMode != quality.RivalPassMode || gate.RivalPass != 0.90 {
		t.Fatalf("pass 모드가 안 넘어갔다 : %q %.2f", gate.RivalMode, gate.RivalPass)
	}

	repository.Config.Quality.DecisionGateNLI = config.DecisionGateOff
	gate = quality.Options{}
	attachRivalJudge(&gate, repository)
	if gate.RivalJudge != nil {
		t.Fatal("off 인데 판정기를 꽂았다")
	}
}

// 자동 기억 길은 pass 설정이어도 hint-auto 로 묶인다. 판정기가 없으면 안 건드린다.
func TestPinAutoRival(t *testing.T) {
	gate := quality.Options{RivalMode: quality.RivalPassMode}
	pinAutoRival(&gate)
	if gate.RivalMode != quality.RivalPassMode {
		t.Fatalf("판정기가 없는데 모드를 바꿨다 : %q", gate.RivalMode)
	}
	gate.RivalJudge = func(string, string) (quality.RivalVerdict, error) { return quality.RivalVerdict{}, nil }
	pinAutoRival(&gate)
	if gate.RivalMode != quality.RivalHintAuto {
		t.Fatalf("자동 길이 hint-auto 로 안 묶였다 : %q", gate.RivalMode)
	}
}
