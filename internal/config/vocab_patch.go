package config

import (
	"sort"
	"strings"
)

// mem tags 가 vocab.toml 을 고칠 때 쓰는 줄 갈아 끼우기다. EncodeVocab 으로 파일
// 전체를 다시 만들면 사람이 단 주석 · 모르는 키 · 모르는 절이 사라진다 — 그래서
// 바뀐 키 줄만 갈아 끼우고, 없는 키·절만 끼운다. 나머지 줄은 한 글자도 안 바꾼다.

// bomMark 는 메모장이 붙이는 BOM 이다.
var bomMark = string(rune(0xFEFF))

// patchSections 는 PatchVocab 이 견주는 절이다. 없는 절을 붙일 때도 이 순서다.
// `[type.*]` 은 tags 가 안 바꾸므로 안 본다.
var patchSections = []string{"tag", "tag.alias", "tag.deny", "scope", "scope.alias"}

// PatchVocab 은 before(읽어 둔 vocab.toml) 에 v 와 다른 칸만 고쳐 넣은 바이트를
// 준다. 지우기는 안 한다. 차이가 없으면 before 를 그대로 준다. BOM · CRLF 는 지킨다.
func PatchVocab(before []byte, v Vocab) ([]byte, error) {
	if len(before) == 0 {
		return EncodeVocab(v), nil
	}
	bom := ""
	text := string(before)
	if strings.HasPrefix(text, bomMark) {
		bom, text = bomMark, strings.TrimPrefix(text, bomMark)
	}
	old, err := ParseVocab(text)
	if err != nil {
		return nil, err
	}
	file, err := parseTOML(text)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(text, "\n")
	scanned := scanTOMLLines(lines)
	// 절 → 고칠 키(소문자). 파서가 그 절을 안 읽고 기본값을 쓴 절(키가 없거나
	// `bug = "oops"` 처럼 맞지 않는 값만 있는 절)은 그 절의 키를 전부 쓴다 —
	// 고친 키만 쓰면 씨앗 키가 파일에서 사라진다.
	changed := map[string][]string{}
	for _, section := range patchSections {
		keys := changedVocabKeys(section, old, v)
		if len(keys) > 0 && !sectionRead(section, file) {
			keys = vocabKeys(section, v)
		}
		if len(keys) > 0 {
			changed[section] = keys
		}
	}
	if len(changed) == 0 {
		return before, nil
	}
	crlf := mostlyCRLF(text)
	lines, missing := replaceVocabLines(lines, scanned, changed, v)
	return []byte(bom + insertVocabLines(lines, missing, v, crlf)), nil
}

// replaceVocabLines 는 파일에 이미 있는 키를 아래에서 위로 한 줄로 갈아 끼우고,
// 파일에 없는 키를 절별로 돌려준다. 한계: 여러 줄 배열을 갈아 끼우면 여는 줄 ·
// 안쪽 줄의 주석은 없어지고 닫는 줄의 꼬리 주석만 남는다.
func replaceVocabLines(lines []string, scanned []tomlLine, changed map[string][]string, v Vocab) ([]string, map[string][]string) {
	type span struct{ start, end int }
	where := map[string]span{}
	for at, one := range scanned {
		if one.key == "" {
			continue
		}
		// 같은 키가 여러 줄이면 파서는 마지막 줄을 쓰니 늘 덮어써 마지막 줄을 잡는다.
		where[one.section+"\x00"+strings.ToLower(one.key)] = span{at, one.end}
	}
	type swap struct {
		span
		text string
	}
	swaps := []swap{}
	missing := map[string][]string{}
	for section, keys := range changed {
		for _, key := range keys {
			found, ok := where[section+"\x00"+key]
			if !ok {
				missing[section] = append(missing[section], key)
				continue
			}
			swaps = append(swaps, swap{found, vocabLine(section, key, v)})
		}
	}
	sort.Slice(swaps, func(i, j int) bool { return swaps[i].start > swaps[j].start })
	for _, one := range swaps {
		first := lines[one.start]
		indent := first[:len(first)-len(strings.TrimLeft(first, " \t"))]
		last := lines[one.end]
		end := ""
		if strings.HasSuffix(last, "\r") {
			end, last = "\r", strings.TrimSuffix(last, "\r")
		}
		// 닫는 줄 끝의 `# 꼬리 주석` 은 사람이 단 것이라 다시 붙인다.
		tail := ""
		if code := stripComment(last); len(code) < len(last) {
			tail = " " + strings.TrimSpace(last[len(code):])
		}
		line := indent + one.text + tail + end
		lines = append(lines[:one.start], append([]string{line}, lines[one.end+1:]...)...)
	}
	return lines, missing
}

// insertVocabLines 는 없는 키를 그 절 마지막 키 뒤(키가 없으면 머리 뒤)에,
// 없는 절은 파일 끝에 붙인다. AppendSections 와 같은 길로 한 번에 합친다.
func insertVocabLines(lines []string, missing map[string][]string, v Vocab, crlf bool) string {
	lastOf, headerOf := sectionAnchors(scanTOMLLines(lines))
	after := map[int][]string{}
	tailSections := []string{}
	bodies := map[string][]string{}
	for _, section := range patchSections {
		keys := missing[section]
		if len(keys) == 0 {
			continue
		}
		sort.Strings(keys)
		body := make([]string, 0, len(keys))
		for _, key := range keys {
			body = append(body, vocabLine(section, key, v))
		}
		if at, ok := lastOf[section]; ok {
			after[at] = append(after[at], body...)
		} else if at, ok := headerOf[section]; ok {
			after[at] = append(after[at], body...)
		} else {
			tailSections = append(tailSections, section)
			bodies[section] = body
		}
	}
	tail := []string{}
	if len(tailSections) > 0 {
		comments := headerComments(string(EncodeVocab(DefaultVocab())))
		tail = sectionLines(tailSections, bodies, comments, trimmedSet(lines))
	}
	return joinInserted(lines, after, tail, crlf)
}

// changedVocabKeys 는 한 절에서 old 와 다른(또는 old 에 없는) v 의 키다.
func changedVocabKeys(section string, old, v Vocab) []string {
	keys := []string{}
	switch section {
	case "tag", "scope":
		have, want := old.Tags, v.Tags
		if section == "scope" {
			have, want = old.Scopes, v.Scopes
		}
		for _, key := range sortedListKeys(want) {
			if words, ok := have[key]; !ok || !sameWords(words, want[key]) {
				keys = append(keys, key)
			}
		}
	case "tag.alias", "scope.alias":
		have, want := old.TagAlias, v.TagAlias
		if section == "scope.alias" {
			have, want = old.ScopeAlias, v.ScopeAlias
		}
		for _, key := range sortedKeys(want) {
			if to, ok := have[key]; !ok || to != want[key] {
				keys = append(keys, key)
			}
		}
	case "tag.deny":
		if !sameWords(old.TagDeny, v.TagDeny) {
			keys = append(keys, "words")
		}
	}
	return keys
}

// sectionRead 는 ParseVocab 이 그 절을 기본값 대신 파일 값으로 읽었나다.
// ParseVocab 과 같은 조건으로 본다.
func sectionRead(section string, file *tomlFile) bool {
	switch section {
	case "tag", "scope":
		return len(file.mapOfLists(section)) > 0
	case "tag.alias", "scope.alias":
		return len(file.mapOf(section)) > 0
	}
	found, ok := file.value(section, "words")
	return ok && found.kind == kindList
}

// vocabKeys 는 v 가 한 절에 가진 키 전부다.
func vocabKeys(section string, v Vocab) []string {
	switch section {
	case "tag":
		return sortedListKeys(v.Tags)
	case "scope":
		return sortedListKeys(v.Scopes)
	case "tag.alias":
		return sortedKeys(v.TagAlias)
	case "scope.alias":
		return sortedKeys(v.ScopeAlias)
	}
	return []string{"words"}
}

// vocabLine 은 키 한 줄이다. EncodeVocab 이 쓰는 줄과 글자까지 같다.
func vocabLine(section, key string, v Vocab) string {
	out := strings.Builder{}
	switch section {
	case "tag":
		writeList(&out, quote(key), v.Tags[key])
	case "scope":
		writeList(&out, quote(key), v.Scopes[key])
	case "tag.alias":
		out.WriteString(quote(key) + " = " + quote(v.TagAlias[key]) + "\n")
	case "scope.alias":
		out.WriteString(quote(key) + " = " + quote(v.ScopeAlias[key]) + "\n")
	case "tag.deny":
		writeList(&out, "words", v.TagDeny)
	}
	return strings.TrimSuffix(out.String(), "\n")
}
