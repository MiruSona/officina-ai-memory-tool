package config

import (
	"strings"
	"testing"
)

// init 이 mem.toml 의 빠진 키를 채울 때 사람이 단 주석·줄을 안 지운다
// (mem issue 20261005-ebe02fad). 판정 넷 : ① 원래 줄이 순서대로 다 남는다
// ② 결과에 빠진 키가 없다 ③ 다시 Parse 된다 ④ 두 번 돌려도 같다.

// dropKey 는 기본 파일에서 「키 =」 로 시작하는 줄 하나를 뺀다.
func dropKey(text, key string) string {
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, key+" =") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// dropSection 은 절 머리부터 다음 빈 줄까지를 뺀다.
func dropSection(text, header string) string {
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	skipping := false
	for _, line := range lines {
		if line == header {
			skipping = true
			continue
		}
		if skipping && strings.TrimSpace(line) == "" {
			skipping = false
		}
		if skipping {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func fillOnce(text string) string {
	filled, err := FillMissing(text)
	if err != nil {
		panic(err)
	}
	return filled
}

// keepsOrder 는 original 의 줄이 모두 result 에 같은 차례로 남았는지다.
// 줄 끝 \r 까지 그대로 견준다.
func keepsOrder(original, result string) bool {
	want := strings.Split(original, "\n")
	have := strings.Split(result, "\n")
	at := 0
	for _, line := range have {
		if at < len(want) && line == want[at] {
			at++
		}
	}
	return at == len(want)
}

func TestInsertMissingKeepsLines(t *testing.T) {
	base := string(Encode(Default("시험")))
	hookCommented := strings.Replace(dropKey(base, "max_bytes"), "[hook]\n",
		"[hook]\n# 손으로 단 주석 — 훅은 조용히\n", 1)
	retainShort := base + "\n# 자동 쌓기 손잡이\n[retain]\nnudge = false\nnudge_edits = 9\n"
	multiLine := dropKey(base, "subagent_max_bytes") + "\n[extra]\nlist = [\n  \"가\",\n  \"나\",\n]\n"
	multiLine = strings.Replace(multiLine, "subagent_skip = []",
		"subagent_skip = [\n  \"Explore\", # 뒤 주석\n  \"Plan\",\n]", 1)
	textBlock := strings.Replace(dropKey(base, "k1"), "rrf_k = ",
		"note = \"\"\"\n여러 줄 = 글\n\"\"\"\nrrf_k = ", 1)
	cases := []struct {
		name string
		text string
	}{
		{"주석 있는 [hook] 에서 키 하나 빠짐", hookCommented},
		{"키가 빠진 [retain] (기본값에 없는 절)", dropKey(retainShort, "nudge_turns")},
		{"절이 통째로 없음", dropSection(base, "[quality]")},
		{"CRLF 파일", strings.ReplaceAll(hookCommented, "\n", "\r\n")},
		{"절 끝에 여러 줄 배열", multiLine},
		{"여러 줄 문자열", textBlock},
		{"끝 줄바꿈 없음", strings.TrimRight(dropSection(base, "[quality]"), "\n")},
	}
	for _, test := range cases {
		if len(MissingKeys(test.text)) == 0 && test.name != "키가 빠진 [retain] (기본값에 없는 절)" {
			t.Fatalf("%s : 시험 자료에 빠진 키가 없다", test.name)
		}
		result := fillOnce(test.text)
		if !keepsOrder(test.text, result) {
			t.Fatalf("%s : 원래 줄이 사라지거나 바뀌었다\n%s", test.name, result)
		}
		if left := MissingKeys(result); len(left) != 0 {
			t.Fatalf("%s : 아직 빠진 키가 있다 : %v", test.name, left)
		}
		if _, err := Parse(result); err != nil {
			t.Fatalf("%s : 다시 못 읽는다 : %v", test.name, err)
		}
		if again := fillOnce(result); again != result {
			t.Fatalf("%s : 두 번 돌리니 또 바뀌었다", test.name)
		}
		if strings.HasPrefix(test.name, "CRLF") && strings.Count(result, "\n") != strings.Count(result, "\r\n") {
			t.Fatalf("%s : CRLF 파일에 LF 줄을 끼웠다", test.name)
		}
	}
}

// 끼운 키는 제 절 안에 들어가 값이 그 절로 읽힌다.
func TestInsertMissingLandsInSection(t *testing.T) {
	base := string(Encode(Default("시험")))
	text := strings.Replace(dropKey(base, "max_bytes"), "[hook]\n", "[hook]\n# 주석\n", 1)
	loaded, err := Parse(fillOnce(text))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Hook.MaxBytes != Default("").Hook.MaxBytes {
		t.Fatalf("끼운 max_bytes 가 [hook] 으로 안 읽힌다 : %d", loaded.Hook.MaxBytes)
	}
	missingQuality := fillOnce(dropSection(base, "[quality]"))
	if !strings.Contains(missingQuality, "\n[quality]\ndup_reject = ") {
		t.Fatalf("없던 절을 절째로 안 붙였다 :\n%s", missingQuality)
	}
}

// vocab.toml 은 절 단위로 빠짐을 본다. 빠진 절만 끝에 붙이고 있던 줄은 그대로다.
func TestAppendSectionsKeepsVocab(t *testing.T) {
	text := "# 손 주석\r\n[tag]\r\n\"search\" = [\"ranking\"]\r\n"
	result := AppendSections(text, []string{"tag.deny"}, map[string][]string{"tag.deny": {"words = [\"ai\"]"}})
	if !strings.HasPrefix(result, text) {
		t.Fatalf("있던 줄이 바뀌었다 : %q", result)
	}
	if !strings.HasSuffix(result, "\r\n[tag.deny]\r\nwords = [\"ai\"]\r\n") {
		t.Fatalf("CRLF 로 절을 안 붙였다 : %q", result)
	}
}

// cutSection 은 기본 파일에서 절 하나(머리와 그 아래 줄)를 뺀다. withComment 면
// 머리 위 설명 주석도 같이 뺀다.
func cutSection(text, name string, withComment bool) string {
	lines := strings.Split(text, "\n")
	drop := map[int]bool{}
	for at, one := range scanTOMLLines(lines) {
		if one.section != name {
			continue
		}
		drop[at] = true
		if one.header && withComment {
			for from := at - len(commentAbove(lines, at)); from < at; from++ {
				drop[from] = true
			}
		}
	}
	kept := []string{}
	for at, line := range lines {
		if !drop[at] {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

const canonComment = "# 낱말 -> 대표말. 색인과 질의가 이 표를 똑같이 타서 같은 글자가 된다."

// 키가 없는 절이 빠지면 MissingKeys 로는 안 잡히고 MissingSections 로 잡힌다.
// 채우면 머리와 그 위 설명 주석이 따라오고, 두 번 돌려도 같다.
func TestFillMissingAddsEmptySection(t *testing.T) {
	base := string(Encode(Default("x")))
	for _, crlf := range []bool{false, true} {
		text := cutSection(base, "canon", true)
		if crlf {
			text = strings.ReplaceAll(text, "\n", "\r\n")
		}
		if got := strings.Join(MissingSections(text), " "); got != "canon" {
			t.Fatalf("crlf=%v : 빠진 절을 %q 로 셌다", crlf, got)
		}
		if len(MissingKeys(text)) != 0 {
			t.Fatalf("crlf=%v : 키는 안 빠졌는데 %v", crlf, MissingKeys(text))
		}
		result := fillOnce(text)
		if left := MissingSections(result); len(left) != 0 {
			t.Fatalf("crlf=%v : 채운 뒤에도 빠진 절 %v", crlf, left)
		}
		if strings.Count(result, canonComment) != 1 || !strings.Contains(result, "[canon]") {
			t.Fatalf("crlf=%v : 절 머리 주석이 안 따라왔다 :\n%s", crlf, result)
		}
		if !keepsOrder(text, result) {
			t.Fatalf("crlf=%v : 원래 줄이 바뀌었다", crlf)
		}
		if crlf && strings.Count(result, "\n") != strings.Count(result, "\r\n") {
			t.Fatalf("CRLF 파일에 LF 줄을 끼웠다")
		}
		if again := fillOnce(result); again != result {
			t.Fatalf("crlf=%v : 두 번 돌리니 달라졌다", crlf)
		}
	}
}

// 절 머리 주석이 이미 파일에 있으면(머리만 지운 경우) 두 벌이 되지 않는다.
func TestFillMissingKeepsSingleHeaderComment(t *testing.T) {
	text := cutSection(string(Encode(Default("x"))), "canon", false)
	if !strings.Contains(text, canonComment) {
		t.Fatalf("시험 준비가 틀렸다 : 주석까지 지워졌다")
	}
	result := fillOnce(text)
	if strings.Count(result, canonComment) != 1 || !strings.Contains(result, "\n[canon]") {
		t.Fatalf("주석이 두 벌이거나 머리가 없다 :\n%s", result)
	}
}

// 빠진 키가 있는 절을 절째 붙일 때도 머리 주석이 따라온다 ([quality]).
func TestInsertMissingSectionCarriesHeaderComment(t *testing.T) {
	text := cutSection(string(Encode(Default("x"))), "quality", true)
	result := fillOnce(text)
	comment := "# 문서 품질 문턱. mem eval --quality 가 규칙을 강등하면 여기 적는다."
	if strings.Count(result, comment+"\n[quality]") != 1 {
		t.Fatalf("[quality] 머리 주석이 안 따라왔다 :\n%s", result)
	}
	if fillOnce(result) != result || len(MissingKeys(result))+len(MissingSections(result)) != 0 {
		t.Fatalf("멱등이 아니거나 아직 빠진 것이 있다")
	}
}

// 다 있는 파일은 바이트까지 그대로다.
func TestFillMissingCompleteFileUntouched(t *testing.T) {
	base := string(Encode(Default("x")))
	for _, text := range []string{base, strings.ReplaceAll(base, "\n", "\r\n")} {
		if len(MissingSections(text)) != 0 || fillOnce(text) != text {
			t.Fatalf("다 있는 파일을 바꿨다")
		}
	}
}

// vocab.toml 에 절을 붙일 때도 기본 글의 머리 주석이 따라오고, 있으면 한 벌만 둔다.
func TestAppendSectionsCarriesHeaderComment(t *testing.T) {
	comment := "# 태그로 못 쓰는 낱말. 주제가 아니라 출처라서다 (author·sources 칸이 맡는다)."
	bodies := map[string][]string{"tag.deny": {"words = [\"ai\"]"}}
	result := AppendSections("[tag]\n", []string{"tag.deny"}, bodies)
	if !strings.HasSuffix(result, comment+"\n[tag.deny]\nwords = [\"ai\"]\n") {
		t.Fatalf("머리 주석이 안 따라왔다 : %q", result)
	}
	already := AppendSections("[tag]\n"+comment+"\n", []string{"tag.deny"}, bodies)
	if strings.Count(already, comment) != 1 {
		t.Fatalf("주석이 두 벌이 됐다 : %q", already)
	}
}
