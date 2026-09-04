package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVocabRoundTrip(t *testing.T) {
	text := string(EncodeVocab(DefaultVocab()))
	back, err := ParseVocab(text)
	if err != nil {
		t.Fatalf("vocab.toml 을 다시 못 읽었다 : %v", err)
	}
	want := DefaultVocab()
	if len(back.Tags) != len(want.Tags) {
		t.Fatalf("태그 상위 수가 다르다 : %d vs %d", len(back.Tags), len(want.Tags))
	}
	if len(back.TagAlias) != len(want.TagAlias) {
		t.Fatalf("별칭 수가 다르다 : %d vs %d", len(back.TagAlias), len(want.TagAlias))
	}
	if len(back.TagDeny) != len(want.TagDeny) {
		t.Fatalf("금지 태그 수가 다르다 : %v", back.TagDeny)
	}
	if len(back.Scopes) != len(want.Scopes) || len(back.ScopeAlias) != len(want.ScopeAlias) {
		t.Fatalf("scope 표가 왕복에서 깨졌다 : %v / %v", back.Scopes, back.ScopeAlias)
	}
	// 두 번 써도 같은 바이트라야 init 이 파일을 헛되이 안 고친다.
	if again := string(EncodeVocab(back)); again != text {
		t.Fatal("같은 표를 두 번 썼는데 바이트가 다르다")
	}
}

// F09 — 별칭은 자동으로 치환된다.
func TestNormalizeTagAlias(t *testing.T) {
	vocab := DefaultVocab().normalized()
	cases := map[string]string{
		"docs":    "doc",
		"bugfix":  "bug",
		"unity3d": "unity",
		"qa":      "test",
		"DOCS":    "doc",
	}
	for from, want := range cases {
		got, standard, changed := vocab.NormalizeTag(from)
		if got != want || !standard || !changed {
			t.Fatalf("%q → %q(표준=%v 바뀜=%v) 여야 하는데 %q 였다", from, want, standard, changed, got)
		}
	}
	if got, standard, changed := vocab.NormalizeTag("bug"); got != "bug" || !standard || changed {
		t.Fatalf("표준 태그는 그대로여야 한다 : %q %v %v", got, standard, changed)
	}
	if _, standard, _ := vocab.NormalizeTag("없는태그"); standard {
		t.Fatal("목록 밖 태그가 표준이라고 나왔다")
	}
}

// F10 — 출처 태그는 태그로 못 쓴다.
func TestTagDeny(t *testing.T) {
	vocab := DefaultVocab().normalized()
	for _, tag := range []string{"impl", "poc", "fieldtest", "survey", "decision-table", "wip", "misc", "etc", "note"} {
		if !vocab.TagDenied(tag) {
			t.Fatalf("출처 태그 %q 가 안 걸렸다", tag)
		}
		if _, standard, _ := vocab.NormalizeTag(tag); standard {
			t.Fatalf("금지 태그 %q 가 표준 목록에 있다", tag)
		}
	}
	if vocab.TagDenied("bug") {
		t.Fatal("주제 태그가 금지 목록에 걸렸다")
	}
}

// F12 — 씨앗 목록에는 scope 가 하나도 없다. scope 는 프로젝트 이름이라 도구가
// 못 짐작한다 — 그래서 그동안은 「배우는 중」이다 (실데이터 시험 C5).
func TestDefaultVocabHasNoScope(t *testing.T) {
	vocab := DefaultVocab().normalized()
	if len(vocab.StandardScopes()) != 0 {
		t.Fatalf("씨앗 목록에 scope 가 박혀 있다 : %v", vocab.StandardScopes())
	}
	if !vocab.Learning() {
		t.Fatal("scope 가 없으면 배우는 중이어야 한다")
	}
	if _, standard, _ := vocab.NormalizeScope("mygame"); standard {
		t.Fatal("등록 안 한 scope 가 표준이라고 나왔다")
	}
}

// scope 를 하나라도 정하면 그때부터 목록이 선다.
func TestScopeConfirmedEndsLearning(t *testing.T) {
	vocab, err := ParseVocab(`[scope]
"mygame" = []

[scope.alias]
"mygame-ui" = "mygame"
`)
	if err != nil {
		t.Fatal(err)
	}
	if vocab.Learning() {
		t.Fatal("scope 를 정했는데 아직 배우는 중이라고 한다")
	}
	if got, standard, changed := vocab.NormalizeScope("mygame-ui"); got != "mygame" || !standard || !changed {
		t.Fatalf("scope 별칭이 안 먹었다 : %q %v %v", got, standard, changed)
	}
	if _, standard, _ := vocab.NormalizeScope("없는스코프"); standard {
		t.Fatal("등록 안 한 scope 가 표준이라고 나왔다")
	}
}

// 상하위는 한 겹만 둔다.
func TestVocabParentIsOneLayer(t *testing.T) {
	vocab := DefaultVocab().normalized()
	if got := vocab.Parent("unity"); got != "engine" {
		t.Fatalf("unity 의 상위는 engine 이어야 한다 : %q", got)
	}
	if got := vocab.Parent("engine"); got != "engine" {
		t.Fatalf("상위 태그의 상위는 자기 자신이다 : %q", got)
	}
	if got := vocab.Parent("없는태그"); got != "" {
		t.Fatalf("모르는 태그는 상위가 없다 : %q", got)
	}
	for parent, children := range vocab.Tags {
		for _, child := range children {
			if _, isParent := vocab.Tags[child]; isParent {
				t.Fatalf("%q 가 상위이면서 %q 의 하위다 — 겹이 둘이 됐다", child, parent)
			}
		}
	}
}

// 별칭이 표준 목록 밖을 가리키면 버린다 — 조용히 엉뚱한 태그로 바꾸는 것이 더 나쁘다.
func TestVocabDropsDanglingAlias(t *testing.T) {
	vocab, err := ParseVocab(`
[tag]
"search" = ["ranking"]

[tag.alias]
"scoring" = "없는것"
"relax" = "search"
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := vocab.TagAlias["scoring"]; ok {
		t.Fatal("표준 밖을 가리키는 별칭이 살아남았다")
	}
	if vocab.TagAlias["relax"] != "search" {
		t.Fatalf("멀쩡한 별칭이 버려졌다 : %v", vocab.TagAlias)
	}
}

func TestLoadVocabFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	vocab, err := LoadVocab(filepath.Join(dir, VocabFileName))
	if err != nil {
		t.Fatalf("파일이 없으면 기본 목록이어야 한다 : %v", err)
	}
	if len(vocab.StandardTags()) == 0 {
		t.Fatal("기본 태그 목록이 비었다")
	}
	path := filepath.Join(dir, VocabFileName)
	if err := os.WriteFile(path, EncodeVocab(DefaultVocab()), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadVocab(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.StandardTags()) != len(vocab.StandardTags()) {
		t.Fatal("파일에서 읽은 표가 기본과 다르다")
	}
}

func TestMissingVocabKeys(t *testing.T) {
	if missing := MissingVocabKeys(string(EncodeVocab(DefaultVocab()))); len(missing) > 0 {
		t.Fatalf("갓 쓴 vocab.toml 에 빠진 키가 있다 : %v", missing)
	}
	missing := MissingVocabKeys("[tag]\n\"search\" = [\"ranking\"]\n")
	if len(missing) == 0 || !strings.Contains(strings.Join(missing, " "), "tag.deny") {
		t.Fatalf("빠진 키를 못 찾았다 : %v", missing)
	}
}

// 리뷰 #1 — [scope] 키는 이름 규격을 지나야 목록에 앉는다. 큰따옴표 키에 넣은
// 개행이 훅의 신뢰 구역에 새 줄을 만들었다.
func TestVocabDropsBadScopeKeys(t *testing.T) {
	said := []string{}
	before := Warn
	Warn = func(line string) { said = append(said, line) }
	defer func() { Warn = before }()
	vocab, err := ParseVocab("[scope]\n\"good\" = []\n\"BAD SCOPE\" = []\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range vocab.StandardScopes() {
		if scope != "good" {
			t.Fatalf("규격 밖 scope 가 남았다 : %q", scope)
		}
	}
	if len(said) == 0 {
		t.Fatal("버렸다고 말하지 않았다")
	}
}
