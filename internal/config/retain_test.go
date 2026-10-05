package config

import (
	"strings"
	"testing"
)

// [retain] 은 기본값이면 Encode 에 안 나오고, 바꾼 값은 쓰고 읽어도 같다.
func TestRetainParseAndEncode(t *testing.T) {
	if strings.Contains(string(Encode(Default("시험"))), "[retain]") {
		t.Fatal("기본값인데 [retain] 절을 썼다")
	}
	parsed, err := Parse("schema = 1\nname = \"시험\"\n\n" + RetainBlock(Default("").Retain))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Retain != Default("").Retain {
		t.Fatalf("기본 블록을 읽은 값이 기본값과 다르다 : %+v", parsed.Retain)
	}
	changed, err := Parse("schema = 1\n[retain]\nnudge = false\nnudge_edits = 9\nper_day = 3\n")
	if err != nil {
		t.Fatal(err)
	}
	if changed.Retain.Nudge || changed.Retain.NudgeEdits != 9 || changed.Retain.PerDay != 3 || changed.Retain.NudgeTurns != 8 {
		t.Fatalf("바꾼 값이 틀렸다 : %+v", changed.Retain)
	}
	again, err := Parse(string(Encode(changed)))
	if err != nil || again.Retain != changed.Retain {
		t.Fatalf("다시 읽은 값이 다르다 : %+v %v", again.Retain, err)
	}
	if len(MissingKeys(string(Encode(Default("시험"))))) != 0 {
		t.Fatal("기본 파일이 키가 모자란다고 한다")
	}
}
