package config

import (
	"strings"
	"testing"
)

// 코드리뷰 10-05 높음 1 · 중간 2 : init 이 빠진 키를 채우다 사람이 고친 값을
// 기본값으로 덮으면 안 된다. 절 머리는 파서와 같은 함수로 읽고, 끼운 뒤에는
// 다시 Parse 해서 원래 값이 그대로인지 대조한다.

// searchHandTuned 는 rrf_k 를 99 로 고치고 mix_keep 을 지운 기본 파일에서
// [search] 머리만 header 로 바꾼 글이다.
func searchHandTuned(header string) string {
	base := string(Encode(Default("시험")))
	base = strings.Replace(base, "rrf_k = 10\n", "rrf_k = 99\n", 1)
	base = dropKey(base, "mix_keep")
	return strings.Replace(base, "[search]\n", header+"\n", 1)
}

// countHeaders 는 파서 눈으로 본 그 절 머리 수다.
func countHeaders(text, section string) int {
	count := 0
	for _, line := range strings.Split(text, "\n") {
		if name, ok := SectionHeader(line); ok && name == section {
			count++
		}
	}
	return count
}

func TestFillMissingReadsHeaderLikeParser(t *testing.T) {
	for _, header := range []string{"[search] # 검색 손잡이", "[ search ]", "  [search]  "} {
		text := searchHandTuned(header)
		if strings.Contains(strings.Join(MissingKeys(text), " "), "search.rrf_k") {
			t.Fatalf("%q : 있는 search.rrf_k 를 빠졌다고 본다", header)
		}
		result, err := FillMissing(text)
		if err != nil {
			t.Fatalf("%q : %v", header, err)
		}
		loaded, err := Parse(result)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Search.RRFK != 99 {
			t.Fatalf("%q : 사람이 고친 rrf_k 99 가 %v 로 덮였다", header, loaded.Search.RRFK)
		}
		if count := countHeaders(result, "search"); count != 1 {
			t.Fatalf("%q : [search] 머리가 %d 개다\n%s", header, count, result)
		}
		if loaded.Embed.MixKeep != Default("").Embed.MixKeep {
			t.Fatalf("%q : 빠진 mix_keep 을 못 채웠다", header)
		}
	}
}

// 옛 이름 `[repo] pin_max` 만 있는 파일. 빠진 `[pin] max` 는 기본값 10 이 아니라
// Parse 가 옮겨 읽은 33 으로 채워야 한다.
const oldPinOnly = "schema = 2\nname = \"x\"\n[repo]\npin_max = 33\n"

func TestFillMissingKeepsOldPinName(t *testing.T) {
	result, err := FillMissing(oldPinOnly)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Parse(result)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Repo.PinMax != 33 || loaded.Pin.Max != 33 {
		t.Fatalf("pin_max 33 이 바뀌었다 : repo %d · pin %d", loaded.Repo.PinMax, loaded.Pin.Max)
	}
}

// 대조 장치는 값이 바뀌는 끼우기를 키 이름과 함께 잡는다. 빠졌던 키가 새로
// 생기는 것은 바뀐 것으로 안 친다.
func TestCheckConfigKeptCatchesChange(t *testing.T) {
	// 기본값 줄로 채우던 옛 길 : [pin] max = 10 이 옛 이름 33 을 이긴다.
	fromDefaults := InsertMissing(oldPinOnly, MissingKeys(oldPinOnly), Default(""))
	err := CheckConfigKept(oldPinOnly, fromDefaults)
	if err == nil || !strings.Contains(err.Error(), "repo.pin_max") {
		t.Fatalf("pin_max 가 바뀐 것을 못 잡았다 : %v", err)
	}
	// 머리를 잘못 읽어 절을 하나 더 붙이던 옛 길 : 뒤 [search] 가 rrf_k 를 덮는다.
	text := searchHandTuned("[search] # 검색 손잡이")
	doubled := text + "\n[search]\nrrf_k = 10\n"
	err = CheckConfigKept(text, doubled)
	if err == nil || !strings.Contains(err.Error(), "search.rrf_k") {
		t.Fatalf("rrf_k 가 바뀐 것을 못 잡았다 : %v", err)
	}
	// 빠진 키만 새로 생기면 통과다.
	filled, err := FillMissing(text)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckConfigKept(text, filled); err != nil {
		t.Fatalf("빠진 키만 채웠는데 거절했다 : %v", err)
	}
}

// 코드리뷰 10-05 낮음 7 — 끼우는 키에 바로 위 설명 주석이 따라간다. 주석이 이미
// 파일에 남아 있으면(키 줄만 지운 경우) 두 벌로 붙이지 않는다.
func TestFillMissingBringsComment(t *testing.T) {
	base := string(Encode(Default("시험")))
	keysComment := "# keys_strict 를 켜면"
	mixComment := "# mix 는 뜻 후보 섞기다"
	// 주석째 지운 keys_strict : 주석이 키 바로 위로 돌아온다.
	noKeys := strings.Replace(base, keysComment, "# 지운 자리", 1)
	noKeys = dropKey(noKeys, "keys_strict")
	filled := fillOnce(noKeys)
	at := strings.Index(filled, keysComment)
	if at < 0 || !strings.HasPrefix(filled[at:], keysComment) ||
		!strings.Contains(filled[at:], "\nkeys_strict = false\n") ||
		strings.Index(filled[at:], "\nkeys_strict = ") > strings.Index(filled[at:], "\n")+1 {
		t.Fatalf("keys_strict 주석이 키 바로 위로 안 따라왔다 :\n%s", filled)
	}
	// 여러 키에 걸친 mix 주석 : mix_keep 만 빠지면 주석은 그대로 하나다.
	for _, drop := range [][]string{{"mix_keep"}, {"mix", "mix_keep"}} {
		text := base
		for _, key := range drop {
			text = dropKey(text, key)
		}
		got := fillOnce(text)
		if count := strings.Count(got, mixComment); count != 1 {
			t.Fatalf("%v 를 지웠을 때 mix 주석이 %d 벌이다", drop, count)
		}
	}
}

func TestMissingVocabKeysReadsHeaderLikeParser(t *testing.T) {
	text := "[tag] # 표준 태그\n\"search\" = [\"ranking\"]\n[ tag.alias ]\n[tag.deny]\nwords = []\n" +
		"[scope]  # 폴더\n[scope.alias]\n"
	if missing := MissingVocabKeys(text); len(missing) != 0 {
		t.Fatalf("있는 절을 빠졌다고 본다 : %v", missing)
	}
}

func TestCheckVocabKeptCatchesChange(t *testing.T) {
	before := "[tag]\n\"search\" = [\"ranking\"]\n[tag.alias]\n[tag.deny]\nwords = [\"ai\"]\n[scope]\n[scope.alias]\n"
	after := before + "\n[tag.deny]\nwords = [\"misc\"]\n"
	err := CheckVocabKept(before, after)
	if err == nil || !strings.Contains(err.Error(), "tag.deny.words") {
		t.Fatalf("tag.deny.words 가 바뀐 것을 못 잡았다 : %v", err)
	}
	appended := AppendSections("[tag]\n\"search\" = [\"ranking\"]\n", []string{"tag.deny"},
		map[string][]string{"tag.deny": {"words = [\"ai\"]"}})
	if err := CheckVocabKept("[tag]\n\"search\" = [\"ranking\"]\n", appended); err != nil {
		t.Fatalf("빠진 절만 붙였는데 거절했다 : %v", err)
	}
}
