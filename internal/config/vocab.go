package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/safe"
)

// VocabFileName 은 태그·scope 표준 목록이 사는 파일이다. mem.toml 과 나란히
// `Memory/` 안에 있고 git 에 들어간다 (설계 6-3).
const VocabFileName = "vocab.toml"

// Vocab 은 저장소 하나의 낱말 표준이다.
//
//	[tag]        상위 태그 = [하위 태그…]     — 상하위는 한 겹만 둔다 (SKOS 얕게)
//	[tag.alias]  별칭 = 표준 태그             — add·lint 가 조용히 치환한다
//	[tag.deny]   words = [출처 태그…]         — 주제가 아니라 출처라 태그로 못 쓴다
//	[scope]      표준 scope = [메모]          — 툴 이름 통째 하나
//	[scope.alias] 별칭 = 표준 scope
//
// 근거 : 품질규칙표 3절 · 규칙 F09·F10·F12.
type Vocab struct {
	// Tags 는 상위 태그 → 하위 태그다. 하위가 없으면 빈 목록이다.
	Tags map[string][]string
	// TagAlias 는 별칭 → 표준 태그다.
	TagAlias map[string]string
	// TagDeny 는 태그로 못 쓰는 낱말이다 (규칙 F10).
	TagDeny []string
	// Scopes 는 표준 scope 다. 값은 사람이 적는 메모라 검사에 안 쓴다.
	Scopes map[string][]string
	// ScopeAlias 는 별칭 → 표준 scope 다.
	ScopeAlias map[string]string
	// Types 는 이 저장소가 아는 기억 종류 표다 (`[type.*]`). 비면 기본 7종이다.
	Types model.TypeTable
}

// DefaultVocab 은 `mem init` 이 깔아 주는 씨앗 목록이다 (품질규칙표 3절).
//
// **어느 프로젝트에나 있는 말만 담는다.** 이 도구를 만든 저장소의 낱말
// (`aimemory` `fts5` `officina` …)을 여기 박아 두면 새 프로젝트는 첫
// `mem add` 부터 전부 거절당한다 (외부 말뭉치 시험 C5 · 툴 폴더 규칙).
//
// **scope 는 하나도 안 넣는다.** scope 는 프로젝트마다 다른 이름이라 도구가
// 못 짐작한다. 그래서 scope 가 비어 있는 동안은 「아직 안 정한 저장소」로 보고
// F09·F12 를 거절이 아니라 경고로 다룬다 (Learning 참고).
func DefaultVocab() Vocab {
	return Vocab{
		Tags: map[string][]string{
			// 게임 개발 공통
			"build":    {"ci", "packaging", "pipeline"},
			"engine":   {"unity", "unreal", "godot"},
			"gameplay": {"input", "camera", "physics", "ai"},
			"level":    {"tilemap", "levelgen", "spawn"},
			"art":      {"animation", "shader", "texture", "audio"},
			"ui":       {"hud", "localization"},
			"perf":     {"profiling", "memory", "loadtime"},
			"platform": {"android", "ios", "windows", "console"},
			"data":     {"save", "schema", "config"},
			"net":      {"multiplayer", "server"},
			// 일하는 방식 (구조 태그)
			"tool":     {"editor", "cli", "install"},
			"test":     {"unittest", "stress", "testdata"},
			"bug":      {},
			"security": {"secret", "permission"},
			"design":   {"architecture", "plan"},
			"doc":      {"guide"},
			"process":  {"effort", "review", "release"},
		},
		TagAlias: map[string]string{
			"unity3d":       "unity",
			"ux":            "ui",
			"sfx":           "audio",
			"anim":          "animation",
			"loc":           "localization",
			"optimization":  "perf",
			"network":       "net",
			"qa":            "test",
			"bugfix":        "bug",
			"docs":          "doc",
			"estimate":      "effort",
			"design-review": "design",
		},
		TagDeny:    []string{"impl", "poc", "fieldtest", "survey", "decision-table", "wip", "misc", "etc", "note"},
		Scopes:     map[string][]string{},
		ScopeAlias: map[string]string{},
		Types:      model.DefaultTypes(),
	}
}

// TypeTable 은 이 저장소의 종류 표다. 비어 있으면 기본 7종이다 — Vocab 을
// 손으로 지어 쓰는 자리(시험·옛 호출부)가 표 없이 돌아도 되게 한다.
func (v Vocab) TypeTable() model.TypeTable {
	if len(v.Types) == 0 {
		return defaultTypeTable
	}
	return v.Types
}

// defaultTypeTable 은 표를 안 든 자리가 쓰는 기본표다. 부를 때마다 새로 지으면
// 저장소를 훑는 lint 가 기억마다 표를 한 벌씩 만든다.
var defaultTypeTable = model.DefaultTypes()

// Learning 은 이 저장소가 제 낱말을 아직 안 정한 상태다. scope 쪽 기준이라
// ScopeLearning 과 같다. 옛 이름을 부르는 자리를 위해 남겨 둔다.
func (v Vocab) Learning() bool { return v.ScopeLearning() }

// ScopeLearning 은 표준 scope 를 아직 안 정한 상태다. scope 는 프로젝트 이름이라
// 도구가 못 짐작해서 씨앗을 못 준다 — 그래서 하나도 없으면 F12(표준 밖 scope)를
// 경고로만 낸다. `mem tags --add-scope <이름>` 하나면 끝난다.
func (v Vocab) ScopeLearning() bool { return len(v.Scopes) == 0 }

// TagLearning 은 태그 표준을 아직 안 정한 상태다 — 씨앗 목록 그대로라는 뜻이다.
//
// **scope 와 따로 본다 (리뷰 D7).** 둘을 하나로 묶어 뒀더니 scope 이름 하나만
// 정하려던 사람이 다음 `add` 부터 좋은 기억을 6/10 거절당했다. 씨앗 목록은
// 「어느 프로젝트에나 있는 말」뿐이라 제 프로젝트 낱말(`index`·`hook`·`search` …)이
// 다 목록 밖이기 때문이다. 태그 표준은 `mem tags --add <태그>` 를 한 번이라도
// 써서 **사람이 제 목록을 만들기 시작했을 때** 선다.
func (v Vocab) TagLearning() bool { return sameTagTable(v.Tags, seedTags()) }

// seedTags 는 씨앗 태그 표다. 견줄 때마다 DefaultVocab 을 다시 지으면 표를
// 통째로 새로 만들게 된다.
var seedTags = sync.OnceValue(func() map[string][]string { return DefaultVocab().Tags })

// ParseVocab 은 vocab.toml 을 읽는다. 없는 절은 기본값 그대로 둔다 — 사람이
// 태그 한 줄을 지웠다고 표준 목록이 통째로 비면 안 된다.
func ParseVocab(text string) (Vocab, error) {
	vocab := DefaultVocab()
	file, err := parseTOML(text)
	if err != nil {
		return vocab, err
	}
	if tags := file.mapOfLists("tag"); len(tags) > 0 {
		vocab.Tags = tags
	}
	if alias := file.mapOf("tag.alias"); len(alias) > 0 {
		vocab.TagAlias = alias
	}
	vocab.TagDeny = file.listOr("tag.deny", "words", vocab.TagDeny)
	if scopes := file.mapOfLists("scope"); len(scopes) > 0 {
		vocab.Scopes = scopes
	}
	if alias := file.mapOf("scope.alias"); len(alias) > 0 {
		vocab.ScopeAlias = alias
	}
	vocab.Types = mergeTypes(vocab.Types, file)
	return vocab.normalized(), nil
}

// normalized 는 표에 든 낱말을 다 소문자로 맞추고, 별칭이 표준 목록을 가리키는지
// 본다. 별칭이 가리키는 곳이 표준이 아니면 그 별칭은 버린다 — 조용히 엉뚱한
// 태그로 바꿔 놓는 것이 목록이 없는 것보다 나쁘다.
func (v Vocab) normalized() Vocab {
	out := Vocab{
		Tags: lowerLists(v.Tags), TagAlias: map[string]string{},
		TagDeny: lowerAll(v.TagDeny), Scopes: goodScopes(lowerLists(v.Scopes)),
		ScopeAlias: map[string]string{}, Types: v.TypeTable(),
	}
	for from, to := range v.TagAlias {
		from, to = strings.ToLower(from), strings.ToLower(to)
		if out.standardTag(to) {
			out.TagAlias[from] = to
		}
	}
	for from, to := range v.ScopeAlias {
		from, to = strings.ToLower(from), strings.ToLower(to)
		if _, ok := out.Scopes[to]; ok {
			out.ScopeAlias[from] = to
		}
	}
	return out
}

// goodScopes 는 이름 규격 밖 scope 키를 버린다. 규격 밖 이름은 어차피 어느
// 기억의 scope 와도 못 맞고, 훅이 이 목록을 서브에이전트 프롬프트에 그대로
// 실으므로 개행이 든 키가 지시문 한 줄로 풀린다 (불변조건 I3).
func goodScopes(table map[string][]string) map[string][]string {
	out := map[string][]string{}
	for key, list := range table {
		if !model.IsScope(key) {
			Warn(i18n.T(i18n.VocabScopeName, safe.Summary(key, scopeShowRunes)))
			continue
		}
		out[key] = list
	}
	return out
}

// scopeShowRunes 는 경고에 적는 나쁜 키의 길이다. 못 믿을 글이라 살균하고 자른다.
const scopeShowRunes = 40

func lowerLists(table map[string][]string) map[string][]string {
	out := map[string][]string{}
	for key, list := range table {
		out[strings.ToLower(key)] = lowerAll(list)
	}
	return out
}

func lowerAll(list []string) []string {
	out := make([]string, 0, len(list))
	for _, one := range list {
		out = append(out, strings.ToLower(one))
	}
	return out
}

// standardTag 은 상위든 하위든 표준 목록에 있는 태그인지다.
func (v Vocab) standardTag(tag string) bool {
	if _, ok := v.Tags[tag]; ok {
		return true
	}
	for _, children := range v.Tags {
		for _, child := range children {
			if child == tag {
				return true
			}
		}
	}
	return false
}

// NormalizeTag 은 태그 하나를 표준 이름으로 바꾼다.
// 돌려주는 값 : 바뀐 이름 · 표준 목록 안인가 · 별칭이라 바뀌었는가 (규칙 F09).
func (v Vocab) NormalizeTag(tag string) (string, bool, bool) {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if to, ok := v.TagAlias[tag]; ok {
		return to, true, to != tag
	}
	return tag, v.standardTag(tag), false
}

// TagDenied 는 출처 태그처럼 태그로 못 쓰는 낱말인지다 (규칙 F10).
func (v Vocab) TagDenied(tag string) bool {
	tag = strings.ToLower(strings.TrimSpace(tag))
	for _, one := range v.TagDeny {
		if one == tag {
			return true
		}
	}
	return false
}

// NormalizeScope 은 scope 를 표준 이름으로 바꾼다 (규칙 F12).
func (v Vocab) NormalizeScope(scope string) (string, bool, bool) {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if to, ok := v.ScopeAlias[scope]; ok {
		return to, true, to != scope
	}
	_, ok := v.Scopes[scope]
	return scope, ok, false
}

// StandardTags 는 상위·하위를 다 편 표준 태그 목록이다. 차례는 늘 같다.
func (v Vocab) StandardTags() []string {
	seen := map[string]bool{}
	for parent, children := range v.Tags {
		seen[parent] = true
		for _, child := range children {
			seen[child] = true
		}
	}
	return sortedStrings(seen)
}

// StandardScopes 는 표준 scope 목록이다.
func (v Vocab) StandardScopes() []string {
	seen := map[string]bool{}
	for scope := range v.Scopes {
		seen[scope] = true
	}
	return sortedStrings(seen)
}

// Parent 는 하위 태그의 상위를 돌려준다. 상위 태그를 넣으면 자기 자신이다.
// 상하위는 한 겹뿐이라 더 올라갈 곳이 없다.
func (v Vocab) Parent(tag string) string {
	tag = strings.ToLower(tag)
	if _, ok := v.Tags[tag]; ok {
		return tag
	}
	for parent, children := range v.Tags {
		for _, child := range children {
			if child == tag {
				return parent
			}
		}
	}
	return ""
}

// EncodeVocab 은 vocab.toml 을 LF·BOM 없이 쓴다. 같은 표를 두 번 쓰면 같은
// 바이트가 나와야 `init` 이 파일을 헛되이 안 고친다.
func EncodeVocab(v Vocab) []byte {
	out := strings.Builder{}
	out.WriteString("# 이 저장소가 쓰는 낱말 표준이다. 여기 있는 것은 씨앗일 뿐이라\n")
	out.WriteString("# 프로젝트에 맞게 지우고 늘려서 쓴다.\n")
	out.WriteString("#\n")
	out.WriteString("# [scope] 가 비어 있는 동안은 「아직 안 정했다」로 보고 표준 밖 태그·scope 를\n")
	out.WriteString("# 경고로만 알린다. mem tags --add-scope <이름> 으로 첫 scope 를 정하면\n")
	out.WriteString("# 그때부터 목록 밖은 거절이다.\n\n")
	out.WriteString("# 태그 표준 목록. 왼쪽이 상위, 오른쪽이 하위다 (한 겹만 둔다).\n")
	out.WriteString("# 늘리려면 mem tags --add <태그> 를 쓴다.\n")
	out.WriteString("[tag]\n")
	for _, parent := range sortedListKeys(v.Tags) {
		writeList(&out, quote(parent), v.Tags[parent])
	}
	out.WriteString("\n# 별칭 → 표준 태그. add·lint 가 조용히 바꿔 준다.\n[tag.alias]\n")
	for _, from := range sortedKeys(v.TagAlias) {
		out.WriteString(quote(from) + " = " + quote(v.TagAlias[from]) + "\n")
	}
	out.WriteString("\n# 태그로 못 쓰는 낱말. 주제가 아니라 출처라서다 (author·sources 칸이 맡는다).\n[tag.deny]\n")
	writeList(&out, "words", v.TagDeny)
	out.WriteString("\n# scope 표준 목록. 툴·부품 이름 통째 하나만 쓴다. 주제를 붙이지 않는다.\n")
	out.WriteString("# 늘리려면 mem tags --add-scope <이름> 을 쓴다.\n[scope]\n")
	for _, scope := range sortedListKeys(v.Scopes) {
		writeList(&out, quote(scope), v.Scopes[scope])
	}
	out.WriteString("\n[scope.alias]\n")
	for _, from := range sortedKeys(v.ScopeAlias) {
		out.WriteString(quote(from) + " = " + quote(v.ScopeAlias[from]) + "\n")
	}
	writeTypes(&out, v.TypeTable())
	return []byte(out.String())
}

// vocabSections 는 vocab.toml 이 가져야 할 절이다.
var vocabSections = []string{"tag", "tag.alias", "tag.deny", "scope", "scope.alias"}

// MissingVocabKeys 는 이 파일에 없는 절 이름이다.
//
// mem.toml 과 달리 **낱말 하나하나는 안 본다.** vocab.toml 은 설정이 아니라
// 사람이 정한 목록이라, `init` 이 씨앗 낱말을 남의 목록에 도로 밀어 넣으면
// 지운 태그가 살아 돌아온다. 절만 있으면 그 파일은 온전한 것이다.
func MissingVocabKeys(text string) []string {
	have := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			have[strings.Trim(line, "[]")] = true
		}
	}
	missing := []string{}
	for _, section := range vocabSections {
		if !have[section] {
			missing = append(missing, section)
		}
	}
	return missing
}

// LoadVocab 은 vocab.toml 하나를 읽는다. 파일이 없으면 기본 목록을 쓴다 —
// 목록이 없다고 저장소를 못 쓰게 만들면 첫 `init` 전에 아무것도 못 한다.
func LoadVocab(path string) (Vocab, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultVocab(), nil
		}
		return DefaultVocab(), err
	}
	return ParseVocab(strings.TrimPrefix(string(data), "\ufeff"))
}

// TypesIn 은 저장소 폴더 하나의 기억 종류 표다. 파일이 없거나 못 읽으면 기본
// 7종이다 — 목록이 없다고 색인이 멈추면 안 된다.
func TypesIn(dir string) model.TypeTable {
	vocab, err := LoadVocab(filepath.Join(dir, VocabFileName))
	if err != nil {
		return model.DefaultTypes()
	}
	return vocab.TypeTable()
}

// sameTagTable 은 두 태그 표가 낱말까지 같은지다. 씨앗 그대로인지 보는 자리라
// 상위·하위를 다 견준다.
func sameTagTable(left, right map[string][]string) bool {
	if len(left) != len(right) {
		return false
	}
	for top, subs := range left {
		other, found := right[strings.ToLower(top)]
		if !found && !hasKeyFold(right, top) {
			return false
		}
		if !found {
			other = right[strings.ToLower(top)]
		}
		if !sameWords(subs, other) {
			return false
		}
	}
	return true
}

func hasKeyFold(table map[string][]string, key string) bool {
	_, found := table[strings.ToLower(key)]
	return found
}

// sameWords 는 두 낱말 목록이 차례와 상관없이 같은지다.
func sameWords(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]int, len(right))
	for _, one := range right {
		seen[strings.ToLower(one)]++
	}
	for _, one := range left {
		key := strings.ToLower(one)
		if seen[key] == 0 {
			return false
		}
		seen[key]--
	}
	return true
}
