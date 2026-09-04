package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// W3 — `add` 관문과 `index` 승격이 같은 태그 자를 써야 한다.
//
// 예전에는 한글 태그가 관문에서 **경고**로 지나 큐에 들어가고 id 까지 찍힌 뒤,
// `index` 가 `model.Validate` 로 막아 inbox/bad 로 버렸다. 사람은 「들어갔다」고
// 듣고 기억은 조용히 사라졌다.
func TestAddRejectsNonASCIITag(t *testing.T) {
	memory := newRepo(t)
	args := addArgs("본문 한 줄")
	for at, one := range args {
		if one == "index,korean" {
			args[at] = "색인,품질"
		}
	}
	out, code := capture(t, func() int { return run(args) })
	if code != exitCheck {
		t.Fatalf("한글 태그는 품질 관문 거절(2)이라야 한다 : %d %s", code, out)
	}
	if !strings.Contains(out, "tag-shape") || !strings.Contains(out, "소문자") {
		t.Fatalf("무엇이 잘못됐는지 안 알려 준다 : %s", out)
	}
	if !strings.Contains(out, "mem tags --suggest") {
		t.Fatalf("다음에 할 것을 안 알려 준다 : %s", out)
	}
	// 큐 폴더가 아직 없을 수도 있다 (거절이 폴더를 안 만든다). 있으면 비어야 한다.
	queued, err := os.ReadDir(filepath.Join(memory, "inbox", "new"))
	if err == nil && len(queued) != 0 {
		t.Fatalf("거절된 기억이 큐에 들어갔다 : %v", queued)
	}
}
