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
