package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/embed"
)

// v0.4 갈래 D — 작은 것 다섯. 설계 2절 ⑤ 표.

// ① index --json — 칸이 다 있고 사람 글이 안 섞인다.
func TestIndexJSONHasFields(t *testing.T) {
	memory := newRepo(t)
	// 색인이 승격할 큐 한 건을 남기려고 락을 쥔 채 add 한다.
	release := holdLock(t, memory)
	if _, code := capture(t, func() int { return run(addArgs("본문 한 줄")) }); code != 0 {
		t.Fatal("add 가 실패했다")
	}
	release()
	out, code := capture(t, func() int { return run([]string{"index", "--json"}) })
	if code != exitOK {
		t.Fatalf("index --json 이 실패했다 : %d %s", code, out)
	}
	got := map[string]any{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		t.Fatalf("한 줄 JSON 이 아니다 : %v %q", err, out)
	}
	for _, key := range []string{"locked", "rebuilt", "added", "appended", "duplicated",
		"patched", "bad", "left", "indexed", "skipped", "removed", "total", "secret",
		"auto_linked", "link_blocked", "link_biggest", "unindexed", "elapsed_ms"} {
		if _, found := got[key]; !found {
			t.Fatalf("칸 %s 가 없다 : %s", key, out)
		}
	}
	if got["added"].(float64) != 1 {
		t.Fatalf("승격 건수가 안 맞는다 : %s", out)
	}
	// 사람이 읽는 한글 줄이 JSON 과 같이 나오면 파서가 죽는다.
	if strings.Contains(out, "승격") || strings.Contains(out, "색인") {
		t.Fatalf("사람 글이 섞였다 : %s", out)
	}
}

// ① index --json — 비밀정보 값은 안 찍고 종료 코드 4 는 그대로다.
func TestIndexJSONHidesSecretValue(t *testing.T) {
	memory := newRepo(t)
	putRaw(t, memory, "20260105-cccc1111", "",
		"열쇠를 AKIAIOSFODNN7EXAMPLE 로 적어 뒀다.\n둘째 줄이다.\n셋째 줄이다.\n")
	out, code := capture(t, func() int { return run([]string{"index", "--full", "--json"}) })
	if code != exitSecurity {
		t.Fatalf("비밀정보가 있으면 4 라야 한다 : %d %s", code, out)
	}
	if strings.Contains(out, "AKIA") {
		t.Fatalf("값을 찍었다 : %s", out)
	}
	got := map[string]any{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		t.Fatalf("한 줄 JSON 이 아니다 : %v %q", err, out)
	}
	if got["secret"].(float64) < 1 {
		t.Fatalf("막힌 건수가 0 이다 : %s", out)
	}
}

// ② mem.toml [embed] model — 비면 기본 모델, 적으면 그것, 환경변수가 제일 세다.
func TestEmbedModelFromConfig(t *testing.T) {
	memory := newRepo(t)
	if modelName() != embed.DefaultModel {
		t.Fatalf("빈 칸이면 기본 모델이라야 한다 : %s", modelName())
	}
	toml := "schema = 1\nname = \"시험\"\n\n[embed]\nmodel = \"시험모델\"\n"
	if err := os.WriteFile(filepath.Join(memory, "mem.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code := capture(t, func() int { return run([]string{"status"}) }); code == exitNoStore {
		t.Fatal("저장소를 못 찾았다")
	}
	if modelName() != "시험모델" {
		t.Fatalf("mem.toml 값을 안 읽었다 : %s", modelName())
	}
	t.Setenv("MEM_EMBED_MODEL", "뒷문모델")
	if modelName() != "뒷문모델" {
		t.Fatalf("환경변수가 제일 세야 한다 : %s", modelName())
	}
}

// ② init 은 빠진 키만 채운다 — model 칸이 생겨도 그 규칙은 그대로다.
func TestInitAddsEmbedModelKeyOnly(t *testing.T) {
	memory := newRepo(t)
	toml := "schema = 1\nname = \"내가 정한 이름\"\n\n[embed]\nfloor = 0.77\n"
	if err := os.WriteFile(filepath.Join(memory, "mem.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code := capture(t, func() int { return run([]string{"init"}) }); code != exitOK {
		t.Fatal("init 이 실패했다")
	}
	text := readFile(t, filepath.Join(memory, "mem.toml"))
	if !strings.Contains(text, "model = ") {
		t.Fatalf("[embed] model 칸이 안 생겼다 : %s", text)
	}
	if !strings.Contains(text, "내가 정한 이름") || !strings.Contains(text, "0.77") {
		t.Fatalf("이미 있던 값을 덮었다 : %s", text)
	}
}

// ④ eval — 골든셋이 없으면 exit 3 이고 「잴 것이 없다」를 말한다.
func TestEvalMissingGoldenExitsThree(t *testing.T) {
	newRepo(t)
	if _, code := capture(t, func() int { return run(addArgs("본문 한 줄")) }); code != 0 {
		t.Fatal("add 가 실패했다")
	}
	if _, code := capture(t, func() int { return run([]string{"index"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	code := run([]string{"eval"})
	if code != exitNoStore {
		t.Fatalf("골든셋이 없으면 3 이라야 한다 : %d", code)
	}
}

// ⑤ set --link — 한 개를 더한다. --links 는 통째 교체 그대로다.
func TestSetLinkAddsOne(t *testing.T) {
	memory := newRepo(t)
	first := putLinked(t, memory, "20260105-aaaa1111", nil)
	putLinked(t, memory, "20260105-aaaa2222", nil)
	putLinked(t, memory, "20260105-aaaa3333", nil)
	if _, code := capture(t, func() int { return run([]string{"index", "--full"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	if _, code := capture(t, func() int {
		return run([]string{"set", first, "--links", "20260105-aaaa2222"})
	}); code != exitOK {
		t.Fatal("set --links 가 실패했다")
	}
	if _, code := capture(t, func() int { return run([]string{"index"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	if _, code := capture(t, func() int {
		return run([]string{"set", first, "--link", "20260105-aaaa3333"})
	}); code != exitOK {
		t.Fatal("set --link 가 실패했다")
	}
	if _, code := capture(t, func() int { return run([]string{"index"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	text := readFile(t, filepath.Join(memory, "store", "2026", "01", first+".md"))
	if !strings.Contains(text, "20260105-aaaa2222") || !strings.Contains(text, "20260105-aaaa3333") {
		t.Fatalf("--link 가 더하지 않고 갈아 끼웠다 : %s", text)
	}
	// 같은 것을 또 더해도 두 벌이 되면 안 된다.
	if _, code := capture(t, func() int {
		return run([]string{"set", first, "--link", "20260105-aaaa3333"})
	}); code != exitOK {
		t.Fatal("set --link 가 실패했다")
	}
	if _, code := capture(t, func() int { return run([]string{"index"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	text = readFile(t, filepath.Join(memory, "store", "2026", "01", first+".md"))
	if strings.Count(text, "20260105-aaaa3333") != 1 {
		t.Fatalf("같은 링크가 두 벌이다 : %s", text)
	}
}

// ⑤ --links 는 통째 교체다. 이 뜻이 바뀌면 안 된다.
func TestSetLinksStillReplaces(t *testing.T) {
	memory := newRepo(t)
	first := putLinked(t, memory, "20260105-bbbb1111", []string{"20260105-bbbb2222"})
	putLinked(t, memory, "20260105-bbbb2222", nil)
	putLinked(t, memory, "20260105-bbbb3333", nil)
	if _, code := capture(t, func() int { return run([]string{"index", "--full"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	if _, code := capture(t, func() int {
		return run([]string{"set", first, "--links", "20260105-bbbb3333"})
	}); code != exitOK {
		t.Fatal("set --links 가 실패했다")
	}
	if _, code := capture(t, func() int { return run([]string{"index"}) }); code != exitOK {
		t.Fatal("index 가 실패했다")
	}
	text := readFile(t, filepath.Join(memory, "store", "2026", "01", first+".md"))
	if strings.Contains(text, "20260105-bbbb2222") {
		t.Fatalf("--links 가 통째 교체를 안 했다 : %s", text)
	}
}

// 도움말과 모르는 옵션 — 새 옵션이 도움말에 뜨고, 오타는 즉시 실패다.
func TestSmallFiveHelpAndUnknownOption(t *testing.T) {
	newRepo(t)
	out, code := capture(t, func() int { return run([]string{"help", "index"}) })
	if code != exitOK || !strings.Contains(out, "--json") {
		t.Fatalf("index 도움말에 --json 이 없다 : %d %s", code, out)
	}
	out, code = capture(t, func() int { return run([]string{"help", "set"}) })
	if code != exitOK || !strings.Contains(out, "--link ") {
		t.Fatalf("set 도움말에 --link 가 없다 : %d %s", code, out)
	}
	if code := run([]string{"index", "--jsonn"}); code != exitUsage {
		t.Fatalf("모르는 옵션은 1 이라야 한다 : %d", code)
	}
	if code := run([]string{"set", "20260105-aaaa1111", "--linkk", "x"}); code != exitUsage {
		t.Fatalf("모르는 옵션은 1 이라야 한다 : %d", code)
	}
}

// putLinked 는 links 칸을 정해 둔 기억 하나를 store/ 에 놓는다.
func putLinked(t *testing.T, memory, id string, links []string) string {
	t.Helper()
	head := ""
	if len(links) > 0 {
		head = "links: [" + strings.Join(links, ", ") + "]\n"
	}
	putRaw(t, memory, id, head, "링크를 다루는 시험 기억이다.\n둘째 줄이다.\n셋째 줄이다.\n")
	return id
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
