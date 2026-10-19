package lint

import (
	"strings"
	"testing"
)

// 파일 이름이 id 와 다르면 --fix 가 아무 말 없이 건너뛰지 않고 Held 에 남긴다
// (피드백 2026-10-10 「CRLF 경고가 --fix 로 안 사라진다」).
func TestFixHoldsMisplacedFile(t *testing.T) {
	opened := newRepo(t)
	raw := strings.ReplaceAll(string(goodMemory("20260820-aaaa0002", "", "")), "\n", "\r\n")
	writeMemoryFile(t, opened, "20260820-aaaa0001", []byte(raw))
	report := runOn(t, opened, true)
	if len(report.Fixes) != 0 {
		t.Fatalf("자리가 다른 파일을 고쳤다 : %+v", report.Fixes)
	}
	if len(report.Held) != 1 || !report.Held[0].Note {
		t.Fatalf("Held 에 1건이 남아야 한다 : %+v", report.Held)
	}
}
