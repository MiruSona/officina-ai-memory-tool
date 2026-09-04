package quality

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// DemotedSection 은 강등표가 사는 mem.toml 절 이름이다 (설계 6-2).
//
//	[quality.demoted]
//	multi-table = "warn"    # 정밀도 0.62 < 0.80 라 경고로 내렸다
//
// 왜 파일에 적나 : 강등은 `mem eval --quality` 가 정하지만 그것을 쓰는 것은
// 다음번 `mem add` 다. 코드 안에만 두면 잰 것이 안 쓰인다.
const DemotedSection = "quality.demoted"

var demotedLine = regexp.MustCompile(`(?m)^\s*([A-Za-z0-9_-]+)\s*=\s*"([a-z]+)"`)

// ParseDemoted 는 mem.toml 글에서 강등표를 읽는다. 절이 없으면 빈 표다.
// config 패키지의 파서를 안 쓰는 이유는 이 절이 규칙 이름을 열쇠로 쓰는 자유
// 표라서다 — 설정 구조체에 칸으로 못 박을 수 없다.
func ParseDemoted(text string) map[string]Grade {
	table := map[string]Grade{}
	body := sectionBody(text, DemotedSection)
	for _, match := range demotedLine.FindAllStringSubmatch(body, -1) {
		grade := Grade(match[2])
		if grade != GradeWarn && grade != GradeOff && grade != GradeCandidate {
			continue
		}
		if _, ok := Lookup(match[1]); !ok {
			continue
		}
		table[match[1]] = grade
	}
	return table
}

// sectionBody 는 `[이름]` 절의 본문이다. 다음 절 머리에서 끊는다.
func sectionBody(text, name string) string {
	head := "[" + name + "]"
	at := strings.Index(text, head)
	if at < 0 {
		return ""
	}
	rest := text[at+len(head):]
	if end := strings.Index(rest, "\n["); end >= 0 {
		return rest[:end]
	}
	return rest
}

// WriteDemoted 는 mem.toml 글의 강등표를 이 표로 갈아 끼운다. 다른 절은 한 글자도
// 안 건드린다 — 사람이 손으로 적은 설정을 도구가 다시 쓰면 주석과 차례를 잃는다.
func WriteDemoted(text string, table map[string]Grade, notes map[string]string) string {
	block := encodeDemoted(table, notes)
	head := "[" + DemotedSection + "]"
	at := strings.Index(text, head)
	if at < 0 {
		if block == "" {
			return text
		}
		if !strings.HasSuffix(text, "\n") && text != "" {
			text += "\n"
		}
		return text + "\n" + block
	}
	tail := ""
	rest := text[at+len(head):]
	if end := strings.Index(rest, "\n["); end >= 0 {
		tail = rest[end+1:]
	}
	if block == "" {
		return strings.TrimRight(text[:at], " \t\n") + "\n" + tail
	}
	return text[:at] + strings.TrimPrefix(block, "") + tail
}

func encodeDemoted(table map[string]Grade, notes map[string]string) string {
	if len(table) == 0 {
		return ""
	}
	names := make([]string, 0, len(table))
	for name := range table {
		names = append(names, name)
	}
	sort.Strings(names)
	out := strings.Builder{}
	out.WriteString("[" + DemotedSection + "]\n")
	out.WriteString("# mem eval --quality 가 잰 정밀도로 스스로 내린 등급이다. 손으로 고쳐도 다음 측정이 덮는다.\n")
	for _, name := range names {
		fmt.Fprintf(&out, "%s = %q", name, string(table[name]))
		if note := notes[name]; note != "" {
			fmt.Fprintf(&out, "   # %s", note)
		}
		out.WriteString("\n")
	}
	return out.String()
}

// DemoteNotes 는 강등 이유 한 줄씩이다.
func (r GoldenReport) DemoteNotes() map[string]string {
	notes := map[string]string{}
	for _, one := range r.Rules {
		if one.Note != "" {
			notes[one.Rule] = one.Note
		}
	}
	return notes
}
