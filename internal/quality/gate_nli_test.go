package quality

// C04 decision-gate 의 NLI 안내 시험이다 (적용 설계 2절 · 시험 #8~#10). 판정기는 가짜다.

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// gateFixture 는 C04 에 걸리는 새 결정 하나와 상대 count 개다. 상대는 id 차례로 닮음이 줄어든다.
func gateFixture(count int) (*model.Memory, MemorySlice) {
	fresh := goodMemory()
	fresh.ID = "20260823-99999999"
	fresh.Title = "훅 예산을 세 줄로 줄인다"
	fresh.Summary = "훅 예산을 절마다 다섯 줄에서 세 줄로 줄인다. 20k 저장소에서 주입이 8,000바이트를 넘겨서다"
	repo := MemorySlice{}
	for at := 0; at < count; at++ {
		live := goodMemory()
		live.ID = fmt.Sprintf("20260822-000000a%d", at+1)
		live.Title = "훅 예산은 절마다 다섯 줄이다"
		live.Summary = fresh.Summary + strings.Repeat(" 그리고 긴 절부터 깎는다", at)
		repo = append(repo, live)
	}
	return fresh, repo
}

// fakeRival 은 정해진 답을 차례로 주고 물은 근거(상대 요약)를 모은다.
type fakeRival struct {
	answers []RivalVerdict
	errAt   int
	asked   []string
}

func (f *fakeRival) judge(evidence, claim string) (RivalVerdict, error) {
	f.asked = append(f.asked, evidence)
	if f.errAt > 0 && len(f.asked) == f.errAt {
		return RivalVerdict{}, errors.New("nli:timeout")
	}
	at := len(f.asked) - 1
	if at >= len(f.answers) {
		at = len(f.answers) - 1
	}
	return f.answers[at], nil
}

func gateWith(t *testing.T, fresh *model.Memory, repo MemorySlice, rival *fakeRival, mode string) *Finding {
	t.Helper()
	opt := testOptions()
	opt.Repo = repo
	opt.RivalMode = mode
	opt.RivalPass = 0.90
	if rival != nil {
		opt.RivalJudge = rival.judge
	}
	gate := findingOf(Gate(fresh, opt).Findings, RuleDecisionGate)
	if gate == nil {
		t.Fatal("decision-gate 가 안 걸렸다")
	}
	return gate
}

// #8 반대·지지·무관·보류마다 붙는 글.
func TestRivalNoteByLetter(t *testing.T) {
	fresh, repo := gateFixture(1)
	plain := gateWith(t, fresh, repo, nil, "")
	cases := []struct {
		name   string
		answer RivalVerdict
		want   string
	}{
		{"반대", RivalVerdict{Letter: RivalContradict, Prob: 0.91}, "NLI 반대 0.91 — `20260822-000000a1` 를 뒤집는 결정이면 `--by 20260822-000000a1`"},
		{"지지", RivalVerdict{Letter: RivalSupport, Prob: 0.93}, "NLI 지지 0.93 — `20260822-000000a1` 와 같은 결정을 다시 적은 것일 수 있다"},
		{"무관", RivalVerdict{Letter: RivalUnrelated, Prob: 0.97}, "NLI 무관 — 다른 주제면 `--new`"},
	}
	for _, one := range cases {
		gate := gateWith(t, fresh, repo, &fakeRival{answers: []RivalVerdict{one.answer}}, "")
		if gate.Reason != plain.Reason+" · "+one.want && !strings.HasPrefix(gate.Reason, plain.Reason+" · "+one.want) {
			t.Errorf("%s 안내가 다르다 : %s", one.name, gate.Reason)
		}
		if gate.Level != plain.Level || !reflect.DeepEqual(gate.Next, plain.Next) {
			t.Errorf("%s 에서 등급·다음 수 차례가 바뀌었다 : %v %v", one.name, gate.Level, gate.Next)
		}
	}
	unsure := gateWith(t, fresh, repo, &fakeRival{answers: []RivalVerdict{{Letter: RivalContradict, Prob: 0.6, Unsure: true}}}, "")
	if unsure.Reason != plain.Reason {
		t.Fatalf("보류면 덧붙임이 없어야 한다 : %s", unsure.Reason)
	}
}

// #8 상대는 닮음 순으로 3개까지만 묻고, 첫 오류 뒤에는 안 묻는다. 반대가 나오면 멈춘다.
func TestRivalNoteCapAndStops(t *testing.T) {
	fresh, repo := gateFixture(5)
	rival := &fakeRival{answers: []RivalVerdict{{Letter: RivalUnrelated, Prob: 0.95}}}
	gate := gateWith(t, fresh, repo, rival, "")
	if len(rival.asked) != rivalAskMax {
		t.Fatalf("상대 5개 중 %d개를 물었다 — 3개까지다", len(rival.asked))
	}
	if !strings.HasSuffix(gate.Reason, "NLI 무관 — 다른 주제면 `--new`") {
		t.Fatalf("물은 상대가 모두 무관이면 무관 안내다 : %s", gate.Reason)
	}
	// 가장 닮은(요약이 같은) 상대를 먼저 묻는다.
	if rival.asked[0] != repo[0].Summary {
		t.Fatalf("닮음 순이 아니다 : %q", rival.asked[0])
	}
	failing := &fakeRival{answers: []RivalVerdict{{Letter: RivalUnrelated, Prob: 0.95}}, errAt: 1}
	plain := gateWith(t, fresh, repo, nil, "")
	gate = gateWith(t, fresh, repo, failing, "")
	if len(failing.asked) != 1 || gate.Reason != plain.Reason {
		t.Fatalf("첫 오류 뒤에는 안 묻고 덧붙임도 없다 : %d번 · %s", len(failing.asked), gate.Reason)
	}
	stop := &fakeRival{answers: []RivalVerdict{{Letter: RivalContradict, Prob: 0.88}, {Letter: RivalUnrelated, Prob: 0.9}}}
	gateWith(t, fresh, repo, stop, "")
	if len(stop.asked) != 1 {
		t.Fatalf("반대 확신에서 멈춰야 한다 : %d번", len(stop.asked))
	}
}

// #8 `--new`·`--by` 면 관문이 안 돌아 묻지도 않는다.
func TestRivalNoteSkippedWithNewOrBy(t *testing.T) {
	fresh, repo := gateFixture(1)
	for _, set := range []func(*Options){
		func(opt *Options) { opt.AllowDuplicate = true },
		func(opt *Options) { opt.SupersedeOf = repo[0].ID },
	} {
		rival := &fakeRival{answers: []RivalVerdict{{Letter: RivalContradict, Prob: 0.9}}}
		opt := testOptions()
		opt.Repo = repo
		opt.RivalJudge = rival.judge
		set(&opt)
		if hasRule(Gate(fresh, opt).Findings, RuleDecisionGate) || len(rival.asked) != 0 {
			t.Fatalf("--new/--by 인데 관문이 돌았다 : 물음 %d", len(rival.asked))
		}
	}
}

// #9 판정기가 없으면(nil · off) 거절이 바이트까지 같다.
func TestRivalNoteNilKeepsVerdict(t *testing.T) {
	fresh, repo := gateFixture(2)
	opt := testOptions()
	opt.Repo = repo
	before := Gate(fresh, opt)
	opt.RivalMode = RivalPassMode
	opt.RivalPass = 0.5
	after := Gate(fresh, opt)
	if !reflect.DeepEqual(before.Findings, after.Findings) || before.Kind != after.Kind {
		t.Fatalf("RivalJudge 가 nil 인데 판정이 바뀌었다 :\n%+v\n%+v", before.Findings, after.Findings)
	}
}

// #10 pass — 4칸 답(nli4)이고 물은 상대가 모두 c ≥ 0.90 이면 경고로 낮춘다. 3칸 답이면 거절 그대로.
func TestRivalNotePassMode(t *testing.T) {
	fresh, repo := gateFixture(2)
	plain := gateWith(t, fresh, repo, nil, "")
	if plain.Level != GradeReject {
		t.Fatalf("기준 판정이 거절이 아니다 : %v", plain.Level)
	}
	sure4 := RivalVerdict{Letter: RivalUnrelated, Prob: 0.95, LabelSet: "nli4"}
	gate := gateWith(t, fresh, repo, &fakeRival{answers: []RivalVerdict{sure4}}, RivalPassMode)
	if gate.Level != GradeWarn || !strings.Contains(gate.Reason, "NLI 무관 확신이라 통과") {
		t.Fatalf("nli4 · c 0.95 는 경고로 낮춰야 한다 : %v %s", gate.Level, gate.Reason)
	}
	low4 := RivalVerdict{Letter: RivalUnrelated, Prob: 0.85, LabelSet: "nli4"}
	gate = gateWith(t, fresh, repo, &fakeRival{answers: []RivalVerdict{low4}}, RivalPassMode)
	if gate.Level != GradeReject {
		t.Fatalf("c 0.85 는 pass 선 아래라 거절 그대로다 : %v", gate.Level)
	}
	sure3 := RivalVerdict{Letter: RivalUnrelated, Prob: 0.99, LabelSet: "nli3"}
	gate = gateWith(t, fresh, repo, &fakeRival{answers: []RivalVerdict{sure3}}, RivalPassMode)
	if gate.Level != GradeReject || strings.Count(gate.Reason, "지금 판에서는 켜지지 않는다") != 1 {
		t.Fatalf("nli3 답이면 거절 그대로 + 알림 한 번 : %v %s", gate.Level, gate.Reason)
	}
	// 상대가 3개를 넘으면 다 못 물었으니 통과시키지 않는다.
	many, manyRepo := gateFixture(4)
	gate = gateWith(t, many, manyRepo, &fakeRival{answers: []RivalVerdict{sure4}}, RivalPassMode)
	if gate.Level != GradeReject {
		t.Fatalf("상대 4개면 pass 를 안 켠다 : %v", gate.Level)
	}
}

// pass 는 거절만 경고로 낮춘다. 설정이 이미 낮춘 등급(후보)을 경고로 올리지 않는다.
func TestRivalNotePassOnlyLowersReject(t *testing.T) {
	fresh, repo := gateFixture(1)
	opt := testOptions()
	opt.Repo = repo
	opt.Demoted = map[string]Grade{RuleDecisionGate: GradeCandidate}
	opt.RivalMode, opt.RivalPass = RivalPassMode, 0.90
	rival := &fakeRival{answers: []RivalVerdict{{Letter: RivalUnrelated, Prob: 0.97, LabelSet: "nli4"}}}
	opt.RivalJudge = rival.judge
	gate := findingOf(Gate(fresh, opt).Findings, RuleDecisionGate)
	if gate == nil || gate.Level != GradeCandidate {
		t.Fatalf("pass 가 후보 등급을 경고로 올렸다 : %+v", gate)
	}
}

// 자동 기억 길(hint-auto)은 pass 를 안 켜고 「다른 주제면 --new」 꼬리도 안 붙인다. 반대 안내는 그대로다.
func TestRivalNoteHintAuto(t *testing.T) {
	fresh, repo := gateFixture(1)
	plain := gateWith(t, fresh, repo, nil, "")
	sure4 := RivalVerdict{Letter: RivalUnrelated, Prob: 0.99, LabelSet: "nli4"}
	gate := gateWith(t, fresh, repo, &fakeRival{answers: []RivalVerdict{sure4}}, RivalHintAuto)
	if gate.Level != GradeReject || gate.Reason != plain.Reason {
		t.Fatalf("자동 길에서 무관이면 거절 그대로 · 꼬리 없음 : %v %s", gate.Level, gate.Reason)
	}
	gate = gateWith(t, fresh, repo, &fakeRival{answers: []RivalVerdict{{Letter: RivalContradict, Prob: 0.9}}}, RivalHintAuto)
	if !strings.Contains(gate.Reason, "NLI 반대 0.90") {
		t.Fatalf("자동 길도 반대 안내는 붙는다 : %s", gate.Reason)
	}
}
