package config

import (
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// toml 에 [type] 절이 없으면 기본 7종 그대로다.
func TestVocabTypesDefault(t *testing.T) {
	vocab, err := ParseVocab("[tag]\n\"build\" = [\"ci\"]\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(vocab.Types.Names(), " "); got != strings.Join(model.DefaultTypes().Names(), " ") {
		t.Fatalf("절이 없는데 표가 바뀌었다 : %s", got)
	}
}

// 적은 종류의 적은 칸만 바뀐다. 기본 7종은 못 지운다.
func TestVocabTypesOverride(t *testing.T) {
	vocab, err := ParseVocab(`
[type.history]
sources = "required"
stale_days = 90

[type.env]
label = "장비"
sources = "warn"
hook = "장비"
gc_keep = true
`)
	if err != nil {
		t.Fatal(err)
	}
	history, _ := vocab.Types.Get(model.TypeHistory)
	if history.Sources != model.SourcesRequired || history.StaleDays != 90 {
		t.Fatalf("적은 칸이 안 바뀌었다 : %+v", history)
	}
	// 안 적은 칸은 기본값 그대로다.
	if !history.Evidence || history.HalfLife != model.HalfLifeHistory {
		t.Fatalf("안 적은 칸까지 바뀌었다 : %+v", history)
	}
	if len(vocab.Types) != len(model.DefaultTypes())+1 {
		t.Fatalf("새 종류가 안 늘었다 : %v", vocab.Types.Names())
	}
	env, ok := vocab.Types.Get("env")
	if !ok || env.Label != "장비" || env.Hook != "장비" || !env.GCKeep {
		t.Fatalf("새 종류가 표에 제대로 안 들어갔다 : %+v", env)
	}
	// 새 종류의 안 적은 칸은 「아무것도 안 하는」 기본값이다.
	if env.Body != model.BodyFree || env.HalfLife != model.HalfLifeNone || env.Gate {
		t.Fatalf("새 종류 기본값이 다르다 : %+v", env)
	}
}

// 모르는 칸·모르는 값은 경고 한 줄 뒤에 기본값으로 돈다. 이름 규격 밖 절은 통째로 버린다.
func TestVocabTypesBadInput(t *testing.T) {
	defer Silence()()
	vocab, err := ParseVocab(`
[type.decision]
body = "표"
같이 = true
stale_days = "많이"

[type.Env]
label = "대문자"
`)
	if err != nil {
		t.Fatal(err)
	}
	decision, _ := vocab.Types.Get(model.TypeDecision)
	if decision.Body != model.BodyFree || decision.StaleDays != 1440 {
		t.Fatalf("못 읽은 값이 기본값으로 안 돌아갔다 : %+v", decision)
	}
	if vocab.Types.Has("env") || len(vocab.Types) != len(model.DefaultTypes()) {
		t.Fatalf("이름 규격 밖 절을 안 버렸다 : %v", vocab.Types.Names())
	}
}

// 왕복 — 쓴 것을 다시 읽고 또 써도 바이트가 같다.
func TestVocabTypesRoundTrip(t *testing.T) {
	seed := DefaultVocab()
	seed.Types = append(seed.Types, model.TypeSpec{Name: "env", Label: "장비",
		Sources: model.SourcesWarn, Body: model.BodyFree, HalfLife: model.HalfLifeNone, Hook: "장비"})
	text := string(EncodeVocab(seed))
	back, err := ParseVocab(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Types) != len(seed.Types) {
		t.Fatalf("왕복에서 종류 수가 달라졌다 : %v", back.Types.Names())
	}
	for at, spec := range back.Types {
		if spec != seed.Types[at] {
			t.Fatalf("%s 가 왕복에서 달라졌다 :\n%+v\n%+v", spec.Name, spec, seed.Types[at])
		}
	}
	if again := string(EncodeVocab(back)); again != text {
		t.Fatal("같은 표를 두 번 썼는데 바이트가 다르다")
	}
}
