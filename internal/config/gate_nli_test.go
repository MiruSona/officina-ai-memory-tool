package config

import (
	"strings"
	"testing"
)

// [quality] decision_gate_nli · decision_gate_pass (적용 설계 2절). 기본값이면 mem.toml 에 안 쓰이고,
// 바꾼 값은 왕복하며, 모르는 모드는 Problems 가 말한다.
func TestDecisionGateNLIKeys(t *testing.T) {
	plain := Default("mem")
	if plain.Quality.DecisionGateNLI != DecisionGateHint || plain.Quality.DecisionGatePass != 0.90 {
		t.Fatalf("기본값이 hint · 0.90 이 아니다 : %+v", plain.Quality)
	}
	if text := string(Encode(plain)); strings.Contains(text, "decision_gate") {
		t.Fatalf("기본값인데 mem.toml 에 적혔다 — 옛 저장소 파일이 바뀐다")
	}
	read, err := Parse("schema = 1\nname = \"x\"\n[quality]\ndecision_gate_nli = \" OFF \"\ndecision_gate_pass = 0.95\n")
	if err != nil {
		t.Fatal(err)
	}
	if read.Quality.DecisionGateNLI != DecisionGateOff || read.Quality.DecisionGatePass != 0.95 {
		t.Fatalf("읽은 값이 다르다 : %+v", read.Quality)
	}
	back, err := Parse(string(Encode(read)))
	if err != nil || back.Quality != read.Quality {
		t.Fatalf("왕복에서 깨졌다 : %+v %v", back.Quality, err)
	}
	read.Quality.DecisionGateNLI = "maybe"
	read.Quality.DecisionGatePass = 1.5
	problems := strings.Join(read.Problems(), "\n")
	if !strings.Contains(problems, "decision_gate_nli") || !strings.Contains(problems, "decision_gate_pass") {
		t.Fatalf("틀린 값을 안 알린다 : %s", problems)
	}
}
