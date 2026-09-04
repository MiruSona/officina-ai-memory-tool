package lint

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/transform"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

var byteOrderMark = []byte{0xEF, 0xBB, 0xBF}

// scanned 는 lint 가 보는 파일 하나다.
type scanned struct {
	Path    string
	Abs     string
	ModTime time.Time
	Text    string
	CP949   bool
	BOM     bool
	CRLF    bool
	Memory  *model.Memory
	Err     error
}

// read 는 store 와 같은 세 단계로 푼다. store.ReadMemory 를 안 쓰는 이유는
// 그쪽이 cp949 파일을 그 자리에서 다시 쓰기 때문이다 — lint 는 고치기 전에
// 먼저 말해야 한다.
func read(file store.FileInfo) *scanned {
	item := scanned{Path: file.Path, Abs: file.Abs, ModTime: file.ModTime}
	raw, err := os.ReadFile(file.Abs)
	if err != nil {
		item.Err = err
		return &item
	}
	item.Text, item.CP949, item.BOM = decode(raw)
	item.CRLF = strings.Contains(item.Text, "\r\n")
	memory, err := model.Parse([]byte(item.Text))
	if err != nil {
		item.Err = err
		return &item
	}
	item.Memory = memory
	return &item
}

func decode(raw []byte) (string, bool, bool) {
	if bytes.HasPrefix(raw, byteOrderMark) {
		return string(raw[len(byteOrderMark):]), false, true
	}
	if utf8.Valid(raw) {
		return string(raw), false, false
	}
	decoded, err := io.ReadAll(transform.NewReader(bytes.NewReader(raw), korean.EUCKR.NewDecoder()))
	if err != nil {
		return string(raw), true, false
	}
	return string(decoded), true, false
}

// checkFile 은 이 파일 하나만 보면 아는 규칙 전부다 — 바이트 규칙은 lint 가,
// 나머지는 quality 관문이 본다.
func checkFile(item *scanned, gate quality.Options) []Problem {
	problems := checkBytes(item)
	if item.Memory == nil {
		return append(problems, fail(quality.RuleFrontMatter, item.Path, 0,
			fmt.Sprintf("머리말을 못 읽었다 : %s", item.Err.Error())))
	}
	return appendFindings(problems, quality.Check(item.Memory, gate), item.Path)
}

// checkBytes 는 파싱하기 전에 아는 것이다 (X01~X03). 셋 다 --fix 가 고친다 —
// 한국 회사 기계에서 메모장·엑셀이 cp949 를 만든다. 거절하면 기억이 통째로
// 안 들어온다 (품질규칙표 1-7).
func checkBytes(item *scanned) []Problem {
	problems := []Problem{}
	if item.CP949 {
		problems = append(problems, warn(quality.RuleEncoding, item.Path, 0,
			"cp949 로 저장돼 있다. `mem lint --fix` 가 UTF-8 로 다시 써 준다"))
	}
	if item.BOM {
		problems = append(problems, warn(quality.RuleBOM, item.Path, 0,
			"파일 앞에 BOM 이 붙어 있다. `mem lint --fix` 가 지운다"))
	}
	if item.CRLF {
		problems = append(problems, warn(quality.RuleCRLF, item.Path, 0,
			"줄끝이 CRLF 다. `mem lint --fix` 가 LF 로 바꾼다"))
	}
	return problems
}
