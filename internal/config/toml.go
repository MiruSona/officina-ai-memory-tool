package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

type tomlKind int

const (
	kindString tomlKind = iota
	kindInt
	kindBool
	kindList
	kindFloat
)

// Warn is where a line we could not read is reported. The hook swaps it for a
// silent one: a bad mem.toml must never write to a live session's stderr
// (review #7).
var Warn = func(line string) { fmt.Fprintln(os.Stderr, line) }

// Silence makes config say nothing at all, and gives back what was there.
func Silence() func() {
	before := Warn
	Warn = func(string) {}
	return func() { Warn = before }
}

// tomlValue is one right-hand side. We only support the four shapes mem.toml uses.
type tomlValue struct {
	kind tomlKind
	str  string
	num  int
	real float64
	flag bool
	list []string
}

// tomlFile keeps sections by name; the keys before any [section] live under "".
type tomlFile struct {
	sections map[string]map[string]tomlValue
	// order is the section names in the order they were written. [type.*] is
	// merged in that order, and a map would shuffle it every run.
	order []string
}

func (f *tomlFile) value(section, key string) (tomlValue, bool) {
	keys, ok := f.sections[section]
	if !ok {
		return tomlValue{}, false
	}
	found, ok := keys[key]
	return found, ok
}

func (f *tomlFile) stringOr(section, key, fallback string) string {
	found, ok := f.value(section, key)
	if !ok || found.kind != kindString {
		return fallback
	}
	return found.str
}

func (f *tomlFile) intOr(section, key string, fallback int) int {
	found, ok := f.value(section, key)
	if !ok || found.kind != kindInt {
		return fallback
	}
	return found.num
}

func (f *tomlFile) boolOr(section, key string, fallback bool) bool {
	found, ok := f.value(section, key)
	if !ok || found.kind != kindBool {
		return fallback
	}
	return found.flag
}

func (f *tomlFile) listOr(section, key string, fallback []string) []string {
	found, ok := f.value(section, key)
	if !ok || found.kind != kindList {
		return fallback
	}
	return found.list
}

// floatOr reads a number that may be written with a decimal point. A whole
// number is a fine float too, so 1 reads the same as 1.0.
func (f *tomlFile) floatOr(section, key string, fallback float64) float64 {
	found, ok := f.value(section, key)
	if !ok {
		return fallback
	}
	if found.kind == kindFloat {
		return found.real
	}
	if found.kind == kindInt {
		return float64(found.num)
	}
	return fallback
}

// mapOfLists returns every list key of one section, which is how [synonym]
// writes 색인 = ["인덱스", "index"]. A section that is not there gives an
// empty table, never nil.
func (f *tomlFile) mapOfLists(section string) map[string][]string {
	table := map[string][]string{}
	for key, value := range f.sections[section] {
		if value.kind == kindList {
			table[key] = value.list
		}
	}
	return table
}

// mapOf returns every string key of one section as a plain table. A section
// that is not there gives an empty table, never nil.
func (f *tomlFile) mapOf(section string) map[string]string {
	table := map[string]string{}
	for key, value := range f.sections[section] {
		if value.kind == kindString {
			table[key] = value.str
		}
	}
	return table
}

// parseTOML reads what it understands and skips the rest with a warning. One
// line it cannot read used to throw the whole repository away, and the hook
// then started a session with nothing (review #7).
func parseTOML(text string) (*tomlFile, error) {
	file := tomlFile{sections: map[string]map[string]tomlValue{"": {}}}
	section := ""
	pending, from := "", 0
	for number, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(stripComment(raw))
		if pending != "" {
			line, pending = strings.TrimSpace(pending+" "+line), ""
		} else {
			from = number + 1
		}
		if line == "" {
			continue
		}
		// A list may run over several lines; hold it until the brackets close.
		if unclosedList(line) {
			pending = line
			continue
		}
		if strings.HasPrefix(line, "[") && !strings.Contains(line, "=") {
			name, ok := parseSectionName(line)
			if !ok {
				Warn(i18n.T(i18n.SkippedConfigLine, from, line))
				continue
			}
			section = name
			if _, seen := file.sections[section]; !seen {
				file.sections[section] = map[string]tomlValue{}
				file.order = append(file.order, section)
			}
			continue
		}
		key, value, ok := parseAssignment(line)
		if !ok {
			Warn(i18n.T(i18n.SkippedConfigLine, from, line))
			continue
		}
		file.sections[section][key] = value
	}
	if pending != "" {
		Warn(i18n.T(i18n.SkippedConfigLine, from, pending))
	}
	return &file, nil
}

// unclosedList says the line opens a list that has not been closed yet.
func unclosedList(line string) bool {
	cut := strings.Index(line, "=")
	if cut < 0 {
		return false
	}
	value := strings.TrimSpace(line[cut+1:])
	if !strings.HasPrefix(value, "[") {
		return false
	}
	depth := 0
	for _, letter := range scanOutsideQuotes(value) {
		if letter == '[' {
			depth++
		}
		if letter == ']' {
			depth--
		}
	}
	return depth > 0
}

// scanOutsideQuotes returns the characters that sit outside any quoted string.
func scanOutsideQuotes(text string) []rune {
	out := []rune{}
	inDouble, inSingle, escaped := false, false, false
	for _, letter := range text {
		switch {
		case escaped:
			escaped = false
		case letter == '\\' && inDouble:
			escaped = true
		case letter == '"' && !inSingle:
			inDouble = !inDouble
		case letter == '\'' && !inDouble:
			inSingle = !inSingle
		case !inDouble && !inSingle:
			out = append(out, letter)
		}
	}
	return out
}

func parseSectionName(line string) (string, bool) {
	if !strings.HasSuffix(line, "]") {
		return "", false
	}
	name := strings.TrimSpace(line[1 : len(line)-1])
	return name, name != ""
}

func parseAssignment(line string) (string, tomlValue, bool) {
	cut := strings.Index(line, "=")
	if cut < 0 {
		return "", tomlValue{}, false
	}
	key := strings.TrimSpace(line[:cut])
	if key == "" {
		return "", tomlValue{}, false
	}
	// A key may be quoted, which is how the [scope] table writes folder names
	// that are not bare TOML keys.
	if unquoted, ok := unquote(key); ok {
		key = unquoted
	}
	value, ok := parseValue(strings.TrimSpace(line[cut+1:]))
	return key, value, ok
}

func parseValue(text string) (tomlValue, bool) {
	if strings.HasPrefix(text, `"`) || strings.HasPrefix(text, "'") {
		unquoted, ok := unquote(text)
		if !ok {
			return tomlValue{}, false
		}
		return tomlValue{kind: kindString, str: unquoted}, true
	}
	if strings.HasPrefix(text, "[") {
		return parseList(text)
	}
	if text == "true" || text == "false" {
		return tomlValue{kind: kindBool, flag: text == "true"}, true
	}
	number, err := strconv.Atoi(text)
	if err == nil {
		return tomlValue{kind: kindInt, num: number}, true
	}
	real, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return tomlValue{}, false
	}
	return tomlValue{kind: kindFloat, real: real}, true
}

func parseList(text string) (tomlValue, bool) {
	if !strings.HasSuffix(text, "]") {
		return tomlValue{}, false
	}
	items := []string{}
	for _, part := range splitList(text[1 : len(text)-1]) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		unquoted, ok := unquote(part)
		if !ok {
			// 따옴표 없는 숫자도 받는다 — `field_weights = [16, 6, 1]` 이
			// 그 꼴이다 (설계 6-2). 그 밖의 맨 낱말은 여전히 안 받는다.
			if _, err := strconv.ParseFloat(part, 64); err != nil {
				return tomlValue{}, false
			}
			unquoted = part
		}
		items = append(items, unquoted)
	}
	return tomlValue{kind: kindList, list: items}, true
}

// unquote reads one quoted string. A '…' literal is taken exactly as written,
// and an escape we do not know inside "…" is kept as it stands, so a pattern
// such as "\d{6}" no longer kills the whole file (review #7).
func unquote(text string) (string, bool) {
	if len(text) < 2 {
		return "", false
	}
	if text[0] == '\'' {
		if text[len(text)-1] != '\'' {
			return "", false
		}
		return text[1 : len(text)-1], true
	}
	if text[0] != '"' || text[len(text)-1] != '"' {
		return "", false
	}
	return unescape(text[1 : len(text)-1]), true
}

func unescape(text string) string {
	out := strings.Builder{}
	for i := 0; i < len(text); i++ {
		if text[i] != '\\' || i+1 >= len(text) {
			out.WriteByte(text[i])
			continue
		}
		i++
		switch text[i] {
		case 'n':
			out.WriteByte('\n')
		case 't':
			out.WriteByte('\t')
		case 'r':
			out.WriteByte('\r')
		case '"', '\\', '\'':
			out.WriteByte(text[i])
		default:
			out.WriteByte('\\')
			out.WriteByte(text[i])
		}
	}
	return out.String()
}

// splitList splits on commas that sit outside quotes, because our patterns contain commas.
func splitList(text string) []string {
	parts := []string{}
	start := 0
	inDouble, inSingle, escaped := false, false, false
	for i := 0; i < len(text); i++ {
		switch {
		case escaped:
			escaped = false
		case text[i] == '\\' && inDouble:
			escaped = true
		case text[i] == '"' && !inSingle:
			inDouble = !inDouble
		case text[i] == '\'' && !inDouble:
			inSingle = !inSingle
		case text[i] == ',' && !inDouble && !inSingle:
			parts = append(parts, text[start:i])
			start = i + 1
		}
	}
	return append(parts, text[start:])
}

// stripComment drops a trailing # comment, ignoring # inside quotes.
func stripComment(line string) string {
	inDouble, inSingle, escaped := false, false, false
	for i := 0; i < len(line); i++ {
		switch {
		case escaped:
			escaped = false
		case line[i] == '\\' && inDouble:
			escaped = true
		case line[i] == '"' && !inSingle:
			inDouble = !inDouble
		case line[i] == '\'' && !inDouble:
			inSingle = !inSingle
		case line[i] == '#' && !inDouble && !inSingle:
			return line[:i]
		}
	}
	return line
}
