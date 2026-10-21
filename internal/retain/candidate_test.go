package retain

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
)

var testVocab = config.Vocab{
	Scopes:     map[string][]string{"aimemorytool": {}, "officina": {}},
	ScopeAlias: map[string]string{"mem": "aimemorytool"},
	Tags:       map[string][]string{"hook": {}, "retain": {}, "test": {}},
}

// genItem 은 생성 결과 후보 하나를 JSON 으로 쓴다. edit 로 칸을 바꾸거나 뺀다.
func genItem(edit func(map[string]any)) string {
	item := map[string]any{"type": "caution", "title": "훅 지연", "summary": "Stop 훅은 오프셋 뒤만 읽는다",
		"body": "본문 한 줄", "tags": []any{"hook", "nope"}, "scope": "aimemorytool",
		"quotes": []any{"훅 지연이 길어서 상태 파일을 오프셋으로 읽게 바꾸자"}, "files": []any{"cmd/mem/main.go"},
		"supersedes": nil}
	if edit != nil {
		edit(item)
	}
	return mustJSON(item)
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func TestParseCandidatesGoodAndFence(t *testing.T) {
	raw := "```json\n{\"candidates\":[" + genItem(nil) + "],\"nothing\":false}\n```"
	cands, nothing, dropped, err := ParseCandidates(raw, testVocab, "", 3)
	if err != nil || nothing || len(dropped) != 0 || len(cands) != 1 {
		t.Fatalf("울타리를 벗기고 하나를 읽어야 한다 : %v %v %v %d", err, nothing, dropped, len(cands))
	}
	got := cands[0]
	if got.Type != "caution" || strings.Join(got.Tags, ",") != "hook" || got.Files[0] != "cmd/mem/main.go" {
		t.Fatalf("칸이 틀렸다 (표준 밖 태그는 빠져야 한다) : %+v", got)
	}
}

func TestParseCandidatesNothingAndBroken(t *testing.T) {
	cands, nothing, _, err := ParseCandidates(`{"candidates":[],"nothing":true}`, testVocab, "", 3)
	if err != nil || !nothing || len(cands) != 0 {
		t.Fatalf("없음 답을 읽어야 한다 : %v %v", err, nothing)
	}
	if _, _, _, err := ParseCandidates(`{"candidates":[`, testVocab, "", 3); err == nil {
		t.Fatal("깨진 JSON 은 오류다 (부르는 쪽이 한 번 다시 묻는다)")
	}
	if _, _, _, err := ParseCandidates("", testVocab, "", 3); err == nil {
		t.Fatal("빈 답은 오류다")
	}
}

func TestParseCandidatesDropsBadOnes(t *testing.T) {
	items := []string{
		genItem(func(m map[string]any) { delete(m, "summary") }),
		genItem(func(m map[string]any) { m["quotes"] = []any{} }),
		genItem(func(m map[string]any) { m["quotes"] = []any{"a", "b", "c", "d"} }),
		genItem(func(m map[string]any) { m["tags"] = "hook" }),
		genItem(func(m map[string]any) { m["type"] = "decision" }),
		genItem(func(m map[string]any) { m["tags"] = []any{"nope"} }),
		genItem(func(m map[string]any) { m["scope"] = "nowhere" }),
		genItem(nil),
	}
	raw := `{"candidates":[` + strings.Join(items, ",") + `],"nothing":false}`
	cands, _, dropped, err := ParseCandidates(raw, testVocab, "", 3)
	if err != nil || len(cands) != 1 {
		t.Fatalf("멀쩡한 하나만 남아야 한다 : %v %d", err, len(cands))
	}
	want := map[string]int{DropParse: 4, DropType: 1, DropTags: 1, DropScope: 1}
	if fmt.Sprint(dropped) != fmt.Sprint(want) {
		t.Fatalf("버린 셈이 틀렸다 : %v (기대 %v)", dropped, want)
	}
}

func TestParseCandidatesMovesSupersedesAndCaps(t *testing.T) {
	items := []string{}
	for at := 0; at < 5; at++ {
		items = append(items, genItem(func(m map[string]any) {
			m["title"] = fmt.Sprintf("후보 %d", at)
			m["supersedes"] = "20261001-aaaaaaaa"
		}))
	}
	raw := `{"candidates":[` + strings.Join(items, ",") + `],"nothing":false}`
	cands, _, dropped, _ := ParseCandidates(raw, testVocab, "", 3)
	if len(cands) != 3 || dropped[DropCap] != 2 || cands[2].Title != "후보 2" {
		t.Fatalf("앞에서 셋만 받고 둘을 상한으로 센다 : %d %v", len(cands), dropped)
	}
	if cands[0].Supersedes != "" || !strings.HasSuffix(cands[0].Body, "\n덮을 후보 : mem:20261001-aaaaaaaa") {
		t.Fatalf("supersedes 는 본문 끝 줄로 옮기고 비운다 : %+v", cands[0])
	}
}

func TestParseCandidatesScopeNormalizeAndFallback(t *testing.T) {
	items := []string{
		genItem(func(m map[string]any) { m["scope"] = "AIMemoryTool" }),
		genItem(func(m map[string]any) { m["scope"] = "mem" }),
		genItem(func(m map[string]any) { m["scope"] = "nowhere" }),
		genItem(func(m map[string]any) { m["scope"] = "" }),
	}
	raw := `{"candidates":[` + strings.Join(items, ",") + `],"nothing":false}`
	cands, _, dropped, _ := ParseCandidates(raw, testVocab, "officina", 5)
	if len(cands) != 4 || len(dropped) != 0 {
		t.Fatalf("넷 다 남아야 한다 : %+v %v", cands, dropped)
	}
	if cands[0].Scope != "aimemorytool" || cands[0].ScopeFrom != nil || cands[1].Scope != "aimemorytool" || cands[1].ScopeFrom != nil {
		t.Fatalf("대소문자·별칭은 맞추고 추정으로 덮지 않는다 : %+v %+v", cands[0], cands[1])
	}
	if cands[2].Scope != "officina" || *cands[2].ScopeFrom != "nowhere" || cands[3].Scope != "officina" {
		t.Fatalf("vocab 밖·빈 scope 는 추정 scope 로 바꾼다 : %+v %+v", cands[2], cands[3])
	}
	if _, _, dropped, _ := ParseCandidates(raw, testVocab, "", 5); dropped[DropScope] != 1 || dropped[DropParse] != 1 {
		t.Fatalf("추정이 비면 vocab 밖은 scope 로, 빈 scope 는 parse 로 버린다 : %v", dropped)
	}
}
