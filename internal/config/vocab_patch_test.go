package config

import (
	"strings"
	"testing"
)

// mem tags 가 vocab.toml 을 고칠 때 바뀐 줄만 갈아 끼우는지 본다. 사람이 단 주석 ·
// 모르는 키 · 모르는 절이 남아야 하고, 결과는 다시 읽혀 v 와 같아야 한다.

// patchedVocab 은 before 에 v 를 고쳐 넣고, 결과가 다시 읽혀 v 와 같은지 본다.
func patchedVocab(t *testing.T, before string, v Vocab) string {
	t.Helper()
	data, err := PatchVocab([]byte(before), v)
	if err != nil {
		t.Fatalf("PatchVocab 실패 : %v", err)
	}
	again, err := ParseVocab(strings.TrimPrefix(string(data), bomMark))
	if err != nil {
		t.Fatalf("고친 결과를 다시 못 읽는다 : %v\n%s", err, data)
	}
	if got, want := string(EncodeVocab(again)), string(EncodeVocab(v)); got != want {
		t.Fatalf("다시 읽은 표가 v 와 다르다 :\n%s\n--- 기대 ---\n%s\n--- 결과 파일 ---\n%s", got, want, data)
	}
	return string(data)
}

// mustVocab 은 시험 글을 읽는다.
func mustVocab(t *testing.T, text string) Vocab {
	t.Helper()
	v, err := ParseVocab(strings.TrimPrefix(text, bomMark))
	if err != nil {
		t.Fatalf("시험 vocab 을 못 읽는다 : %v", err)
	}
	return v
}

const patchBase = `# 사람이 단 머리 주석
[tag]
# 부모 태그 설명
"art" = ["sprite"]
"code" = [
  "go",
  "test",
] # 여러 줄 꼬리 주석
"sound" = ["sfx"] # 한 줄 꼬리 주석

[tag.alias]
"img" = "art"

[scope]
"mytool" = []

[extra]
note = "모르는 절"
`

// 주석 · 모르는 절이 남고, 여러 줄 목록이 한 줄로 바뀌며 꼬리 주석이 붙어 남는다.
func TestPatchVocabKeepsCommentsAndUnknown(t *testing.T) {
	v := mustVocab(t, patchBase)
	v.Tags["code"] = append(v.Tags["code"], "lint")
	v.Tags["sound"] = append(v.Tags["sound"], "bgm")
	v.TagAlias["pic"] = "art"
	got := patchedVocab(t, patchBase, v)
	for _, want := range []string{
		"# 사람이 단 머리 주석\n", "# 부모 태그 설명\n", "\"art\" = [\"sprite\"]\n",
		"\"code\" = [\"go\", \"test\", \"lint\"] # 여러 줄 꼬리 주석\n",
		"\"sound\" = [\"sfx\", \"bgm\"] # 한 줄 꼬리 주석\n",
		"\"img\" = \"art\"\n\"pic\" = \"art\"\n",
		"[extra]\nnote = \"모르는 절\"\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q 가 없다 :\n%s", want, got)
		}
	}
	if strings.Contains(got, "  \"go\",") {
		t.Fatalf("여러 줄 목록의 옛 줄이 남았다 :\n%s", got)
	}
}

// 차이가 없으면 바이트가 그대로다 (CRLF · BOM 포함).
func TestPatchVocabSameIsIdentical(t *testing.T) {
	before := bomMark + strings.ReplaceAll(patchBase, "\n", "\r\n")
	data, err := PatchVocab([]byte(before), mustVocab(t, before))
	if err != nil || string(data) != before {
		t.Fatalf("같은 내용인데 바이트가 바뀌었다 (%v) :\n%q", err, data)
	}
}

// CRLF · BOM 파일을 고쳐도 BOM 과 줄 끝이 지켜진다. 끼운 줄도 CRLF 다.
func TestPatchVocabKeepsCRLFAndBOM(t *testing.T) {
	before := bomMark + strings.ReplaceAll(patchBase, "\n", "\r\n")
	v := mustVocab(t, before)
	v.Tags["code"] = append(v.Tags["code"], "lint")
	v.Tags["data"] = nil
	v.ScopeAlias["mt"] = "mytool"
	got := patchedVocab(t, before, v)
	if !strings.HasPrefix(got, bomMark) {
		t.Fatalf("BOM 이 사라졌다")
	}
	if strings.Count(got, "\n") != strings.Count(got, "\r\n") {
		t.Fatalf("LF 만 있는 줄이 생겼다 :\n%q", got)
	}
	if !strings.Contains(got, "# 여러 줄 꼬리 주석\r\n") {
		t.Fatalf("꼬리 주석 뒤 CRLF 가 없다 :\n%q", got)
	}
}

// 없는 키는 그 절 마지막 키 뒤에, 없는 절은 파일 끝에 붙는다.
func TestPatchVocabInsertsKeyAndSection(t *testing.T) {
	v := mustVocab(t, patchBase)
	v.Tags["data"] = []string{"csv"}
	v.ScopeAlias["mt"] = "mytool"
	got := patchedVocab(t, patchBase, v)
	if !strings.Contains(got, "\"sound\" = [\"sfx\"] # 한 줄 꼬리 주석\n\"data\" = [\"csv\"]\n\n[tag.alias]") {
		t.Fatalf("새 키가 [tag] 끝에 안 붙었다 :\n%s", got)
	}
	if !strings.HasSuffix(got, "note = \"모르는 절\"\n\n[scope.alias]\n\"mt\" = \"mytool\"\n") {
		t.Fatalf("없는 절이 파일 끝에 안 붙었다 :\n%s", got)
	}
}

// 대소문자가 다른 키 "Foo" 는 같은 키로 보고 그 줄을 갈아 끼운다.
func TestPatchVocabFoldsKeyCase(t *testing.T) {
	before := "[tag]\n\"Foo\" = [\"x\"]\n"
	v := mustVocab(t, before)
	v.Tags["foo"] = append(v.Tags["foo"], "y")
	got := patchedVocab(t, before, v)
	if got != "[tag]\n\"foo\" = [\"x\", \"y\"]\n" {
		t.Fatalf("대소문자 다른 키를 갈아 끼우지 못했다 :\n%s", got)
	}
}

// 키가 하나도 없는 [tag] 에 더하면 씨앗 태그까지 다 써서 사라지지 않게 한다.
func TestPatchVocabEmptySectionWritesAll(t *testing.T) {
	before := "# 빈 표\n[tag]\n\n[scope]\n"
	v := mustVocab(t, before)
	seeds := len(v.Tags)
	v.Tags["zz"] = nil
	got := patchedVocab(t, before, v)
	if strings.Count(got, " = [") != seeds+1 {
		t.Fatalf("씨앗 %d 개 + 새 키가 다 안 써졌다 :\n%s", seeds, got)
	}
	if !strings.HasPrefix(got, "# 빈 표\n[tag]\n\"") || !strings.HasSuffix(got, "\n\n[scope]\n") {
		t.Fatalf("키가 [tag] 머리 뒤에 안 들어갔다 :\n%s", got)
	}
}

// 같은 키가 두 줄이면 파서가 쓰는 마지막 줄을 갈아 끼운다.
func TestPatchVocabDuplicateKeyLastLine(t *testing.T) {
	before := "[tag]\n\"foo\" = [\"x\"]\n\"foo\" = [\"z\"]\n"
	v := mustVocab(t, before)
	v.Tags["foo"] = append(v.Tags["foo"], "y")
	got := patchedVocab(t, before, v)
	if got != "[tag]\n\"foo\" = [\"x\"]\n\"foo\" = [\"z\", \"y\"]\n" {
		t.Fatalf("마지막 줄을 안 갈아 끼웠다 :\n%s", got)
	}
}

// 목록 아닌 값만 있어 기본값으로 읽힌 [tag] 에 더하면 씨앗 태그까지 다 쓴다.
// 씨앗에 없는 키의 맞지 않는 줄은 그대로 남는다.
func TestPatchVocabUnreadSectionWritesAll(t *testing.T) {
	before := "[tag]\nbug = \"oops\"\nmemo = \"keep\"\n"
	v := mustVocab(t, before)
	seeds := len(DefaultVocab().Tags)
	if len(v.Tags) != seeds {
		t.Fatalf("시험 전제가 틀렸다 : 기본 태그 %d 개가 아니라 %d 개", seeds, len(v.Tags))
	}
	v.Tags["zz"] = nil
	got := patchedVocab(t, before, v)
	if !strings.Contains(got, "memo = \"keep\"\n") {
		t.Fatalf("씨앗에 없는 줄이 사라졌다 :\n%s", got)
	}
	if again := mustVocab(t, got); len(again.Tags) != seeds+1 {
		t.Fatalf("기본 태그가 다 안 남았다 : %d 개\n%s", len(again.Tags), got)
	}
}

// 빈 파일이면 EncodeVocab 과 같다.
func TestPatchVocabEmptyBefore(t *testing.T) {
	v := DefaultVocab()
	data, err := PatchVocab(nil, v)
	if err != nil || string(data) != string(EncodeVocab(v)) {
		t.Fatalf("빈 파일인데 EncodeVocab 과 다르다 (%v)", err)
	}
}
