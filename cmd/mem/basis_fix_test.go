package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// 코드 리뷰에서 나온 자리들 — 자기-덮기 거절 · 알림 셈을 큐와 맞추기 ·
// 「0건」과 「못 셌다」 가르기 · 역참조 제목 살균.

// captureBoth 는 stdout 과 stderr 를 같이 담는다. `add --by` 의 알림은 stderr 로
// 나가서 capture 하나로는 안 잡힌다.
func captureBoth(t *testing.T, action func() int) (string, int) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outBefore, errBefore := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = writer, writer
	code := action()
	os.Stdout, os.Stderr = outBefore, errBefore
	writer.Close()
	text, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(text), code
}

// 리뷰 #7 — 자기가 자기를 덮으면 그 기억이 검색에서 통째로 사라진다.
func TestSetRefusesSelfSupersede(t *testing.T) {
	memory := newRepo(t)
	one := basisMemory("20260822-eeee0001", model.TypeDecision, "공유기를 모델 M 으로 바꾼다")
	putMemory(t, memory, one)
	out, code := captureBoth(t, func() int { return run([]string{"set", one.ID, "--by", one.ID}) })
	if code == 0 {
		t.Fatalf("자기-덮기가 통과했다 : %s", out)
	}
	if !strings.Contains(out, "자기 자신") {
		t.Fatalf("왜 거절했는지 안 말했다 : %s", out)
	}
	// 남을 덮는 것은 그대로 된다.
	mustRun(t, "set", one.ID, "--by", "20260830-eeee0002")
}

// 리뷰 #9 — `--no-index` 는 사람이 일부러 준 것이라 「색인이 없다」고 하면 안 된다.
// set·add 는 아직 그 깃발을 안 받으니 알림 함수를 바로 부른다.
func TestNoteUsedByTellsWhyOnNoIndex(t *testing.T) {
	memory := newRepo(t)
	one := basisMemory("20260822-eeee0011", model.TypeDecision, "공유기를 모델 M 으로 바꾼다")
	putMemory(t, memory, one)
	mustRun(t, "index")
	parsed, err := parseOptions([]string{"--no-index"}, []string{"no-index"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	said := strings.Builder{}
	noteUsedBy(parsed, store.Open(memory, false), one.ID, &said)
	if !strings.Contains(said.String(), "--no-index") {
		t.Fatalf("`--no-index` 때문이라고 안 말했다 : %s", said.String())
	}
	if strings.Contains(said.String(), "색인이 없다") {
		t.Fatalf("있는 색인을 없다고 했다 : %s", said.String())
	}
}

// 리뷰 #6 — 알림 셈은 검토 큐와 같은 규칙이다. 이미 죽은 참조자만 있으면
// review 로 보내지 않는다. 「N건 있다」 해 놓고 큐가 비면 안 된다.
func TestSetSkipsDeadOnlyReferrers(t *testing.T) {
	memory := newRepo(t)
	base := basisMemory("20260822-eeee0021", model.TypeDecision, "공유기를 모델 M 으로 바꾼다")
	deadUser := basisMemory("20260822-eeee0022", model.TypeFact, "옛 회선 속도")
	deadUser.Sources = []string{model.SourceMem + base.ID}
	deadUser.InvalidAt = "2026-08-23"
	deadUser.SupersededBy = "20260830-eeee0023"
	putMemory(t, memory, base)
	putMemory(t, memory, deadUser)
	mustRun(t, "index")
	out := mustRun(t, "set", base.ID, "--by", "20260830-eeee0024")
	if strings.Contains(out, "mem review --kind basis") {
		t.Fatalf("큐가 빌 텐데 review 로 보냈다 : %s", out)
	}
	if !strings.Contains(out, "검토 큐에 안 오른다") {
		t.Fatalf("왜 안 보내는지 안 말했다 : %s", out)
	}
	// 큐도 실제로 비어 있어야 앞줄이 맞는 말이다.
	mustRun(t, "index")
	if queue := mustRun(t, "review", "--kind", "basis"); strings.Contains(queue, deadUser.ID) {
		t.Fatalf("죽은 참조자가 큐에 떴다 : %s", queue)
	}
}

// 리뷰 #4 — used_by 는 「0건」과 「못 셌다」가 다르다. JSON 에서 빈 목록과 키
// 없음으로 가른다.
func TestShowUsedByZeroIsNotUnknown(t *testing.T) {
	memory := newRepo(t)
	one := basisMemory("20260822-eeee0031", model.TypeDecision, "공유기를 모델 M 으로 바꾼다")
	putMemory(t, memory, one)
	mustRun(t, "index")

	shown := showJSONOf(t, one.ID)
	rows, found := shown["used_by"]
	if !found {
		t.Fatalf("센 결과가 0건인데 키를 뺐다 : %v", shown)
	}
	if list, ok := rows.([]any); !ok || len(list) != 0 {
		t.Fatalf("0건이 빈 목록이 아니다 : %v", rows)
	}

	// 색인을 안 보면 못 센 것이라 키가 아예 없다.
	if _, found := showJSONOf(t, one.ID, "--no-index")["used_by"]; found {
		t.Fatal("못 셌는데 0건이라고 했다")
	}
}

func showJSONOf(t *testing.T, id string, extra ...string) map[string]any {
	t.Helper()
	out := mustRun(t, append([]string{"show", id, "--json"}, extra...)...)
	shown := map[string]any{}
	if err := json.Unmarshal([]byte(out), &shown); err != nil {
		t.Fatalf("JSON 이 아니다 : %v (%s)", err, out)
	}
	return shown
}

// `mem add … --by <옛id>` 도 같은 알림을 낸다. 그쪽은 stderr 로 나간다.
func TestAddByTellsUsedByOnStderr(t *testing.T) {
	memory := newRepo(t)
	old := basisMemory("20260822-eeee0041", model.TypeDecision, "공유기를 모델 M 으로 바꾼다")
	user := basisMemory("20260822-eeee0042", model.TypeFact, "옛 회선 속도")
	user.Sources = []string{model.SourceMem + old.ID}
	putMemory(t, memory, old)
	putMemory(t, memory, user)
	mustRun(t, "index")
	out, code := captureBoth(t, func() int { return run(append(addArgs("새 결정을 넣는다"), "--by", old.ID)) })
	if code != 0 {
		t.Fatalf("add --by 가 실패했다 (%d) : %s", code, out)
	}
	if !strings.Contains(out, user.ID) {
		t.Fatalf("옛 기억을 근거로 삼은 기억을 안 알려줬다 : %s", out)
	}
}

// 역참조 줄의 제목은 남이 쓴 글이다. `--raw` 가 아니면 살균을 지난다 (불변조건 I3).
func TestShowSanitizesUsedByTitle(t *testing.T) {
	memory := newRepo(t)
	base := basisMemory("20260822-eeee0051", model.TypeDecision, "공유기를 모델 M 으로 바꾼다")
	hostile := basisMemory("20260822-eeee0052", model.TypeFact, "제목 <b> 굵게")
	hostile.Sources = []string{model.SourceMem + base.ID}
	putMemory(t, memory, base)
	putMemory(t, memory, hostile)
	mustRun(t, "index")
	out := mustRun(t, "show", base.ID)
	if strings.Contains(out, "<b>") {
		t.Fatalf("역참조 제목이 살균을 안 탔다 : %s", out)
	}
	if !strings.Contains(out, hostile.ID) {
		t.Fatalf("역참조 줄 자체가 사라졌다 : %s", out)
	}
}
