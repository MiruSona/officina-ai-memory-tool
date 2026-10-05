package quality

import (
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// keys 칸(C1) — 꼴이 틀리면 F21 거절, 글에 이미 있는 말이면 F22 경고다.
func TestCheckKeys(t *testing.T) {
	memory := &model.Memory{Title: "색인 다시 만들기", Summary: "판이 바뀌면 index.db 를 지운다",
		Body: "본문", Keys: []string{"reindex", "색인", "ㅁ"}}
	found := checkKeys(memory, Options{})
	rules := map[string]Grade{}
	for _, one := range found {
		rules[one.Rule] = one.Level
	}
	if rules[RuleKeysShape] != GradeReject {
		t.Fatalf("한 글자 key 가 F21 거절이 아니다 : %+v", found)
	}
	if rules[RuleKeysEcho] != GradeWarn {
		t.Fatalf("제목에 있는 key 가 F22 경고가 아니다 : %+v", found)
	}
	if len(found) != 2 {
		t.Fatalf("reindex 는 글에 없으니 아무 말도 없어야 한다 : %+v", found)
	}
	if len(checkKeys(&model.Memory{Title: "제목"}, Options{})) != 0 {
		t.Fatal("keys 가 없는 기억에 말이 붙었다")
	}
}
