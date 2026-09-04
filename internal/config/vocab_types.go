package config

// vocab.toml 의 `[type.<이름>]` 절을 읽어 기억 종류 표를 덮어쓴다.
//
// **이름별·칸별 덮어쓰기다** — toml 에 적힌 종류의, 적힌 칸만 바뀐다. 안 적은
// 종류·안 적은 칸은 기본표 그대로다. 기본 7종은 toml 로 못 지운다. 지우면 이미
// 저장된 기억이 통째로 규격 밖이 되어 색인에서 사라진다 (불변조건 8).

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// typeSection 은 종류 절의 앞머리다.
const typeSection = "type."

// typeNamePattern 은 종류 이름 규격이다. scope 와 같은 자를 쓴다.
var typeNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,19}$`)

// typeFieldNames 는 절이 가질 수 있는 칸 이름이다. 쓰는 차례도 이것이다.
var typeFieldNames = []string{"label", "sources", "severity", "todo_status", "body",
	"evidence", "conclusion", "one_thing", "gate", "stale_days", "half_life",
	"gc_keep", "search_bonus", "hook"}

// 칸마다 고를 수 있는 값. 모르는 값이면 그 칸만 기본값을 쓴다.
var (
	sourcesValues  = []string{model.SourcesNone, model.SourcesWarn, model.SourcesRequired}
	bodyValues     = []string{model.BodyFree, model.BodyIssueSections, model.BodyNumbered}
	halfLifeValues = []string{model.HalfLifeNone, model.HalfLifeHistory, model.HalfLifeDecision}
)

// mergeTypes 는 파일에 적힌 `[type.*]` 절을 기본표 위에 얹는다.
func mergeTypes(base model.TypeTable, file *tomlFile) model.TypeTable {
	table := append(model.TypeTable{}, base...)
	for _, section := range file.order {
		if !strings.HasPrefix(section, typeSection) {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(section, typeSection))
		if !typeNamePattern.MatchString(name) {
			Warn(i18n.T(i18n.VocabTypeName, name))
			continue
		}
		at := indexOfType(table, name)
		spec := model.TypeSpec{Name: name, Label: name, Sources: model.SourcesNone,
			Body: model.BodyFree, HalfLife: model.HalfLifeNone}
		if at >= 0 {
			spec = table[at]
		}
		spec = applyTypeFields(spec, file, section)
		if at >= 0 {
			table[at] = spec
			continue
		}
		table = append(table, spec)
	}
	return table
}

func indexOfType(table model.TypeTable, name string) int {
	for at, spec := range table {
		if spec.Name == name {
			return at
		}
	}
	return -1
}

// applyTypeFields 는 절에 적힌 칸만 바꾼다. 모르는 칸은 경고만 하고 넘어간다 —
// 옛 mem 이 새 toml 을 읽어도 저장소는 그대로 돌아야 한다.
func applyTypeFields(spec model.TypeSpec, file *tomlFile, section string) model.TypeSpec {
	for key, value := range file.sections[section] {
		switch key {
		case "label":
			spec.Label = pickString(spec.Label, spec.Name, key, value, nil)
		case "sources":
			spec.Sources = pickString(spec.Sources, spec.Name, key, value, sourcesValues)
		case "body":
			spec.Body = pickString(spec.Body, spec.Name, key, value, bodyValues)
		case "half_life":
			spec.HalfLife = pickString(spec.HalfLife, spec.Name, key, value, halfLifeValues)
		case "hook":
			spec.Hook = pickString(spec.Hook, spec.Name, key, value, nil)
		case "severity":
			spec.Severity = pickBool(spec.Severity, spec.Name, key, value)
		case "todo_status":
			spec.TodoStatus = pickBool(spec.TodoStatus, spec.Name, key, value)
		case "evidence":
			spec.Evidence = pickBool(spec.Evidence, spec.Name, key, value)
		case "conclusion":
			spec.Conclusion = pickBool(spec.Conclusion, spec.Name, key, value)
		case "one_thing":
			spec.OneThing = pickBool(spec.OneThing, spec.Name, key, value)
		case "gate":
			spec.Gate = pickBool(spec.Gate, spec.Name, key, value)
		case "gc_keep":
			spec.GCKeep = pickBool(spec.GCKeep, spec.Name, key, value)
		case "search_bonus":
			spec.SearchBonus = pickBool(spec.SearchBonus, spec.Name, key, value)
		case "stale_days":
			spec.StaleDays = pickInt(spec.StaleDays, spec.Name, key, value)
		default:
			Warn(i18n.T(i18n.VocabTypeKey, spec.Name, key))
		}
	}
	return spec
}

// pickString 은 글 한 칸을 읽는다. allowed 가 있으면 그 안의 값만 받는다.
func pickString(now, name, key string, value tomlValue, allowed []string) string {
	if value.kind != kindString {
		Warn(i18n.T(i18n.VocabTypeValue, name, key, showValue(value)))
		return now
	}
	text := strings.TrimSpace(value.str)
	if allowed == nil {
		if key == "label" && text == "" {
			Warn(i18n.T(i18n.VocabTypeValue, name, key, text))
			return now
		}
		return text
	}
	for _, one := range allowed {
		if text == one {
			return text
		}
	}
	Warn(i18n.T(i18n.VocabTypeValue, name, key, text))
	return now
}

func pickBool(now bool, name, key string, value tomlValue) bool {
	if value.kind != kindBool {
		Warn(i18n.T(i18n.VocabTypeValue, name, key, showValue(value)))
		return now
	}
	return value.flag
}

func pickInt(now int, name, key string, value tomlValue) int {
	if value.kind != kindInt || value.num < 0 {
		Warn(i18n.T(i18n.VocabTypeValue, name, key, showValue(value)))
		return now
	}
	return value.num
}

// showValue 는 경고에 적을 값이다. 무엇을 적었길래 안 먹었는지 보여준다.
func showValue(value tomlValue) string {
	switch value.kind {
	case kindString:
		return value.str
	case kindInt:
		return strconv.Itoa(value.num)
	case kindFloat:
		return strconv.FormatFloat(value.real, 'g', -1, 64)
	case kindBool:
		return strconv.FormatBool(value.flag)
	}
	return "[" + strings.Join(value.list, ", ") + "]"
}

// writeTypes 는 `[type.*]` 절을 쓴다. 칸을 다 적어 두는 편이 낫다 — 무엇을
// 고칠 수 있는지 파일만 보고 알 수 있다.
func writeTypes(out *strings.Builder, table model.TypeTable) {
	out.WriteString("\n# 기억 종류 표다. 여기 없으면 코드 안 기본 7종으로 돈다.\n")
	out.WriteString("# 적은 종류의 적은 칸만 바뀐다. 기본 7종은 지울 수 없다.\n")
	out.WriteString("#   sources    none | warn | required        근거를 얼마나 세게 묻나\n")
	out.WriteString("#   body       free | issue-sections | numbered   본문 꼴 검사\n")
	out.WriteString("#   half_life  none | history(90일) | decision(720일)   검색 감쇠\n")
	out.WriteString("#   stale_days 낡음 후보 일수 (0 이면 안 묻는다)\n")
	out.WriteString("#   hook       세션 훅 절 이름 (비우면 안 실린다)\n")
	for _, spec := range table {
		out.WriteString("\n[" + typeSection + spec.Name + "]\n")
		for _, key := range typeFieldNames {
			out.WriteString(key + " = " + typeFieldText(spec, key) + "\n")
		}
	}
}

func typeFieldText(spec model.TypeSpec, key string) string {
	switch key {
	case "label":
		return quote(spec.Label)
	case "sources":
		return quote(spec.Sources)
	case "body":
		return quote(spec.Body)
	case "half_life":
		return quote(spec.HalfLife)
	case "hook":
		return quote(spec.Hook)
	case "severity":
		return strconv.FormatBool(spec.Severity)
	case "todo_status":
		return strconv.FormatBool(spec.TodoStatus)
	case "evidence":
		return strconv.FormatBool(spec.Evidence)
	case "conclusion":
		return strconv.FormatBool(spec.Conclusion)
	case "one_thing":
		return strconv.FormatBool(spec.OneThing)
	case "gate":
		return strconv.FormatBool(spec.Gate)
	case "gc_keep":
		return strconv.FormatBool(spec.GCKeep)
	case "search_bonus":
		return strconv.FormatBool(spec.SearchBonus)
	}
	return strconv.Itoa(spec.StaleDays)
}
