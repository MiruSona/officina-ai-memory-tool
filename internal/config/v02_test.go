package config

import (
	"reflect"
	"strings"
	"testing"
)

// 설계 6-2 가 적은 새 키가 다 쓰이고 다시 읽히는지 본다.
func TestV02KeysRoundTrip(t *testing.T) {
	text := string(Encode(Default("mem")))
	for _, key := range []string{
		"[repo]", "scopes", "pin_max",
		"field_weights", "k1", "b ", "rerank_top", "abstain_floor", "syn_weight",
		"[embed]", "enabled", "floor", "rrf_weight",
		"[quality]", "dup_reject", "dup_warn", "simhash_hamming",
		"body_min_lines", "body_warn", "body_max", "tag_min", "tag_max",
		"tag_broad_ratio", "scope_skew_ratio", "cold_days",
		"precision_demote", "precision_off",
		"max_bytes", "lines_per_section", "fold_after_days", "db_max_mb",
	} {
		if !strings.Contains(text, key) {
			t.Fatalf("mem.toml 에 %q 가 안 쓰였다", key)
		}
	}
	back, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	want := Default("mem")
	if back.Search.FieldWeights != want.Search.FieldWeights {
		t.Fatalf("field_weights 가 왕복에서 깨졌다 : %v", back.Search.FieldWeights)
	}
	if back.Search.K1 != DefaultK1 || back.Search.B != DefaultB || back.Search.RerankTop != DefaultRerankTop {
		t.Fatalf("재순위 손잡이가 왕복에서 깨졌다 : %+v", back.Search)
	}
	if back.Quality != want.Quality {
		t.Fatalf("품질 문턱이 왕복에서 깨졌다 : %+v", back.Quality)
	}
	if back.Embed != want.Embed {
		t.Fatalf("임베딩 설정이 왕복에서 깨졌다 : %+v", back.Embed)
	}
	if !reflect.DeepEqual(back.Hook, want.Hook) {
		t.Fatalf("훅 설정이 왕복에서 깨졌다 : %+v", back.Hook)
	}
	if back.GC != want.GC {
		t.Fatalf("gc 설정이 왕복에서 깨졌다 : %+v", back.GC)
	}
}

// 결정 16·18·23 이 못박은 값이 기본값이어야 한다.
func TestV02Defaults(t *testing.T) {
	config := Default("mem")
	if config.Search.FieldWeights != DefaultFieldWeights || len(DefaultFieldWeights) != 4 {
		t.Fatalf("필드 가중이 설계가 못박은 네 값이 아니다 : %v", config.Search.FieldWeights)
	}
	if DefaultFieldWeights[0] <= DefaultFieldWeights[3] {
		t.Fatalf("제목이 본문보다 무겁지 않다 : %v", DefaultFieldWeights)
	}
	if config.Search.B != 0.4 || config.Search.K1 != 1.2 {
		t.Fatalf("재순위 bm25 가 k1=1.2 b=0.4 가 아니다 : %v %v", config.Search.K1, config.Search.B)
	}
	if config.Hook.MaxBytes != 8000 {
		t.Fatalf("훅 상한이 8,000바이트가 아니다 : %d", config.Hook.MaxBytes)
	}
	// 설계는 0.72/0.55 였지만 실측(206건 최대 S=0.248)에 맞춰 내렸다.
	if config.Quality.DupReject != DefaultDupReject || config.Quality.DupWarn != DefaultDupWarn {
		t.Fatalf("중복 문턱이 기본값과 다르다 : %v %v", config.Quality.DupReject, config.Quality.DupWarn)
	}
	if config.Quality.DupWarn >= config.Quality.DupReject {
		t.Fatalf("경고 문턱이 거절 문턱보다 낮아야 한다 : %v %v", config.Quality.DupWarn, config.Quality.DupReject)
	}
	if config.Quality.TagMin != 2 {
		t.Fatalf("태그 하한이 2 가 아니다 : %d", config.Quality.TagMin)
	}
	if config.Schema != 2 {
		t.Fatalf("규격 판이 2 가 아니다 : %d", config.Schema)
	}
}

// 사람이 적은 값이 기본값을 이긴다.
func TestV02KeysOverride(t *testing.T) {
	config, err := Parse(`
[search]
field_weights = [4, 3, 2, 1]
b = 0.75
rerank_top = 50

[quality]
dup_reject = 0.9
tag_min = 3

[hook]
max_bytes = 4000

[repo]
scopes = ["aimemorytool", "diagramtool"]
`)
	if err != nil {
		t.Fatal(err)
	}
	if config.Search.FieldWeights != [4]float64{4, 3, 2, 1} {
		t.Fatalf("field_weights 를 못 읽었다 : %v", config.Search.FieldWeights)
	}
	if config.Search.B != 0.75 || config.Search.RerankTop != 50 {
		t.Fatalf("검색 손잡이를 못 읽었다 : %+v", config.Search)
	}
	if config.Quality.DupReject != 0.9 || config.Quality.TagMin != 3 {
		t.Fatalf("품질 문턱을 못 읽었다 : %+v", config.Quality)
	}
	if config.Hook.MaxBytes != 4000 {
		t.Fatalf("훅 상한을 못 읽었다 : %d", config.Hook.MaxBytes)
	}
	if len(config.Repo.Scopes) != 2 || config.Repo.Scopes[1] != "diagramtool" {
		t.Fatalf("[repo] scopes 를 못 읽었다 : %v", config.Repo.Scopes)
	}
	// 안 적은 것은 기본값이 그대로 있어야 한다.
	if config.Search.K1 != DefaultK1 || config.Quality.DupWarn != DefaultDupWarn {
		t.Fatalf("안 적은 키가 기본값을 잃었다 : %+v %+v", config.Search, config.Quality)
	}
}

// 셋이 아니거나 숫자가 아닌 field_weights 는 기본값을 지킨다.
func TestFieldWeightsFallback(t *testing.T) {
	for _, text := range []string{
		"[search]\nfield_weights = [16, 6]\n",
		"[search]\nfield_weights = [\"a\", \"b\", \"c\"]\n",
		"[search]\nfield_weights = [16, -1, 1]\n",
	} {
		config, err := Parse(text)
		if err != nil {
			t.Fatal(err)
		}
		if config.Search.FieldWeights != DefaultFieldWeights {
			t.Fatalf("틀린 field_weights 를 그대로 썼다 : %q → %v", text, config.Search.FieldWeights)
		}
	}
}

// `[pin] max` 는 v0.1 이름이다. 손으로 고쳐 둔 쪽이 이긴다.
func TestPinMaxOldNameWins(t *testing.T) {
	config, err := Parse("[repo]\npin_max = 7\n\n[pin]\nmax = 3\n")
	if err != nil {
		t.Fatal(err)
	}
	if config.Pin.Max != 3 || config.Repo.PinMax != 3 {
		t.Fatalf("[pin] max 가 이겨야 한다 : %d / %d", config.Pin.Max, config.Repo.PinMax)
	}
	only, err := Parse("[repo]\npin_max = 7\n")
	if err != nil {
		t.Fatal(err)
	}
	if only.Pin.Max != 7 {
		t.Fatalf("[repo] pin_max 를 못 읽었다 : %d", only.Pin.Max)
	}
}

// 설정 검사 — 값을 조용히 고치지 않고 말로 알린다.
func TestConfigProblems(t *testing.T) {
	if problems := Default("mem").Problems(); len(problems) > 0 {
		t.Fatalf("기본값이 스스로 걸렸다 : %v", problems)
	}
	bad := Default("mem")
	bad.Quality.DupWarn = 0.9
	bad.Quality.TagMin = 9
	bad.Hook.MaxBytes = 10
	bad.Search.B = 2
	if problems := bad.Problems(); len(problems) < 4 {
		t.Fatalf("네 자리가 다 걸려야 한다 : %v", problems)
	}
}

// init 이 빠진 키를 채운다 — 새 키가 늘어도 옛 mem.toml 이 그대로 돌아야 한다.
func TestMissingKeysFindsV02Keys(t *testing.T) {
	old := "schema = 1\nname = \"mem\"\n\n[gc]\nwarm_count = 1000\n"
	missing := strings.Join(MissingKeys(old), " ")
	for _, key := range []string{"quality.dup_reject", "search.field_weights", "hook.max_bytes", "repo.scopes"} {
		if !strings.Contains(missing, key) {
			t.Fatalf("빠진 키 %q 를 못 찾았다 : %s", key, missing)
		}
	}
	if left := MissingKeys(string(Encode(Default("mem")))); len(left) > 0 {
		t.Fatalf("갓 쓴 mem.toml 에 빠진 키가 있다 : %v", left)
	}
}

// v0.3 까지의 mem.toml 은 세 값이다. 넷째(메타) 자리에 기본값을 채우고
// 조용히 넘기지 않는다 (v0.4 결정 6 · 불변조건 7).
func TestOldThreeFieldWeightsGetMetaDefault(t *testing.T) {
	said := ""
	before := Warn
	Warn = func(line string) { said = line }
	defer func() { Warn = before }()
	config, err := Parse("[search]\nfield_weights = [16, 6, 1]\n")
	if err != nil {
		t.Fatal(err)
	}
	want := [4]float64{16, DefaultFieldWeights[MetaWeightSlot], 6, 1}
	if config.Search.FieldWeights != want {
		t.Fatalf("옛 세 값을 잘못 옮겼다 : %v", config.Search.FieldWeights)
	}
	if said == "" {
		t.Fatal("경고 한 줄이 없다")
	}
}

// 값이 둘뿐이거나 다섯이면 반쯤 읽지 말고 기본값을 지킨다.
func TestBadFieldWeightsKeepDefault(t *testing.T) {
	defer func(before func(string)) { Warn = before }(Warn)
	Warn = func(string) {}
	for _, line := range []string{"field_weights = [16, 1]", "field_weights = [16, 6, 3, 1, 1]",
		`field_weights = [16, "가", 3, 1]`} {
		config, err := Parse("[search]\n" + line + "\n")
		if err != nil {
			t.Fatal(err)
		}
		if config.Search.FieldWeights != DefaultFieldWeights {
			t.Fatalf("%s 를 반쯤 읽었다 : %v", line, config.Search.FieldWeights)
		}
	}
}

// 값 개수는 맞는데 숫자가 아닌 것이 섞였으면 「몇 개」가 아니라 **몇 번째 값**을
// 짚어야 사람이 어디를 고칠지 안다 (v0.4 리뷰 A R6).
func TestBadFieldWeightValuePointsAtSlot(t *testing.T) {
	said := ""
	before := Warn
	Warn = func(line string) { said = line }
	defer func() { Warn = before }()
	if _, err := Parse("[search]\nfield_weights = [12, 3, \"넷\", 1]\n"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(said, "3번째") {
		t.Fatalf("몇 번째 값이 틀렸는지 안 짚는다 : %q", said)
	}
	if strings.Contains(said, "4개") {
		t.Fatalf("개수 이야기를 한다 : %q", said)
	}
}
