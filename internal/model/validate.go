package model

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

const (
	summaryMinRunes = 30
	summaryMaxRunes = 120
	titleMinRunes   = 6
	titleMaxRunes   = 40
	// titleEchoRunes 는 제목이 요약 앞머리를 그대로 베낀 것인지 보는 길이다
	// (규칙 F06). v0.1 이 쓰던 폴백(요약 앞 40자)이 그대로 파일에 남는 것을 막는다.
	titleEchoRunes = 20
	tagMin         = 2
	tagMax         = 5
	// tagMinLegacy 는 옛 규격의 하한이다. v0.1 은 상한만 봤다.
	tagMinLegacy = 1
	// bodyErrorLines 는 설계 2-3 이 거절로 정한 본문 줄 수다. 120줄 경고는
	// 등급을 나눌 수 있는 lint 쪽에 둔다.
	bodyErrorLines = 300
)

// BodyMaxLines 는 승격·색인이 거절하는 본문 줄 수다. `[quality] body_max` 가
// 이 값을 넘으면 add 가 통과시킨 것을 승격이 버린다 — 두 검사가 같은 자를
// 쓰는지 config 가 여기 물어본다 (스트레스시험 D3).
const BodyMaxLines = bodyErrorLines

var (
	scopePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,29}$`)
	tagPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,29}$`)
	datePattern  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	idPattern    = regexp.MustCompile(`^\d{8}-[0-9a-f]{8}$`)

	// author 세 꼴 (규칙 F19). `import:` 은 migrate 가 쓰는 넷째 꼴이다.
	authorHumanPattern  = regexp.MustCompile(`^human:[A-Za-z0-9._-]{1,64}$`)
	authorHookPattern   = regexp.MustCompile(`^hook:[a-z0-9][a-z0-9-]{0,39}$`)
	authorImportPattern = regexp.MustCompile(`^import:[A-Za-z0-9._/-]{1,80}$`)
	authorToolPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*$`)

	// sources 접두별 꼴 (규칙 F17).
	sourceCommitPattern = regexp.MustCompile(`^commit:[0-9a-f]{7,40}$`)
	sourceURLPattern    = regexp.MustCompile(`^url:https?://\S+$`)
)

// IsDate 는 달력에 있는 YYYY-MM-DD 인지다. 모양만 보면 `2026-13-45` 가 통과하고,
// 색인이 그걸 오늘로 바꿔 감쇠와 정렬이 틀어진다.
func IsDate(value string) bool {
	if !datePattern.MatchString(value) {
		return false
	}
	parsed, err := time.ParseInLocation(DayLayout, value, time.Local)
	return err == nil && parsed.Format(DayLayout) == value
}

// IsAuthor 는 `author` 가 네 꼴 중 하나인지다 (규칙 F19).
func IsAuthor(value string) bool {
	switch {
	case strings.HasPrefix(value, AuthorHuman):
		return authorHumanPattern.MatchString(value)
	case strings.HasPrefix(value, AuthorHook):
		return authorHookPattern.MatchString(value)
	case strings.HasPrefix(value, AuthorImport):
		return authorImportPattern.MatchString(value)
	}
	return authorToolPattern.MatchString(value)
}

// IsTag 는 태그 한 낱말이 꼴에 맞는지다 (규칙 F09 앞단계). tags 명령이 쓴다.
func IsTag(value string) bool { return tagPattern.MatchString(value) }

// IsSource 는 근거 한 줄이 접두 다섯 중 하나로 제대로 적혔는지다 (규칙 F17).
func IsSource(value string) bool {
	switch SourceKind(value) {
	case "file":
		return strings.TrimPrefix(value, SourceFile) != ""
	case "commit":
		return sourceCommitPattern.MatchString(value)
	case "url":
		return sourceURLPattern.MatchString(value)
	case "mem":
		return idPattern.MatchString(strings.TrimPrefix(value, SourceMem))
	case "note":
		return strings.TrimSpace(strings.TrimPrefix(value, SourceNote)) != ""
	}
	return false
}

// Validate 는 기억이 어기는 규칙을 전부 돌려준다 — 부르는 쪽이 한 번에 다 알려줄
// 수 있게. 규격 판은 m.Spec 을 따른다.
func Validate(m *Memory) []error {
	return ValidateAs(m, m.Spec)
}

// ValidateAs 는 규격 판을 지정해서 본다.
// SpecV2 아래는 v0.1 규격이다 — `title`·`author`·`sources` 를 안 따지고 태그
// 하한이 1이다. 옛 파일까지 v0.2 자로 재면 통과하는 기억이 하나도 없어
// 불변조건 8 때문에 저장소가 통째로 안 보인다 (설계 2-5).
func ValidateAs(m *Memory, spec int) []error {
	return ValidateWith(m, spec, DefaultTypes())
}

// ValidateWith 는 저장소의 종류 표로 잰다. 표는 vocab.toml 이 늘릴 수 있어,
// 저장소 설정을 든 자리(승격·색인)만 이것을 부른다. 표를 모르는 자리는
// Validate·ValidateAs 가 기본 7종으로 부른다.
func ValidateWith(m *Memory, spec int, table TypeTable) []error {
	if len(table) == 0 {
		table = DefaultTypes()
	}
	kind := table.Spec(m.Type)
	problems := requiredProblems(m, spec, kind)
	problems = append(problems, valueProblems(m, spec, table)...)
	return append(problems, perTypeProblems(m, spec, kind)...)
}

// requiredProblems 는 빈 필수 칸을 센다 (규칙 F02).
func requiredProblems(m *Memory, spec int, kind TypeSpec) []error {
	pairs := []string{"id", m.ID, "type", m.Type, "date", m.Date, "summary", m.Summary, "scope", m.Scope}
	if spec >= SpecV2 {
		pairs = append(pairs, "author", m.Author)
		// 이전된 기억은 제목을 안 따진다. migrate 가 요약을 베낀 제목을 지어내는
		// 대신 비워 두고 사람에게 넘기기 때문이다 (리뷰 B · 설계 2-5).
		if !m.Migrated {
			pairs = append(pairs, "title", m.Title)
		}
	} else {
		// 옛 규격은 `source` 를 봤다. 도구가 Go 에서 바로 만든 기억은 `author`
		// 만 채워 오므로 둘 중 하나만 있으면 된다.
		pairs = append(pairs, "source", m.LegacySource+m.Author)
	}
	problems := []error{}
	for i := 0; i < len(pairs); i += 2 {
		if pairs[i+1] == "" {
			problems = append(problems, errors.New(i18n.T(i18n.MissingField, pairs[i])))
		}
	}
	if len(m.Tags) == 0 {
		problems = append(problems, errors.New(i18n.T(i18n.MissingField, "tags")))
	}
	if spec >= SpecV2 && len(m.Sources) == 0 && kind.Sources == SourcesRequired {
		problems = append(problems, errors.New(i18n.T(i18n.MissingSources, m.Type)))
	}
	return problems
}

func valueProblems(m *Memory, spec int, table TypeTable) []error {
	problems := []error{}
	if m.Type != "" && !table.Has(m.Type) {
		problems = append(problems, errors.New(i18n.T(i18n.BadType, strings.Join(table.Names(), " "), m.Type)))
	}
	if m.LegacySource != "" && !contains(LegacySources, m.LegacySource) {
		problems = append(problems, errors.New(i18n.T(i18n.BadSource, m.LegacySource)))
	}
	if m.ID != "" && !idPattern.MatchString(m.ID) {
		problems = append(problems, errors.New(i18n.T(i18n.BadID, m.ID)))
	}
	for _, day := range []string{m.Date, m.InvalidAt, m.StaleAfter} {
		if day != "" && !IsDate(day) {
			problems = append(problems, errors.New(i18n.T(i18n.BadDate, day)))
		}
	}
	if m.SupersededBy != "" && !idPattern.MatchString(m.SupersededBy) {
		problems = append(problems, errors.New(i18n.T(i18n.BadSupersededBy, m.SupersededBy)))
	}
	if m.Scope != "" && !scopePattern.MatchString(m.Scope) {
		problems = append(problems, errors.New(i18n.T(i18n.BadScope, m.Scope)))
	}
	if count := utf8.RuneCountInString(m.Summary); m.Summary != "" && (count < summaryMinRunes || count > summaryMaxRunes) {
		problems = append(problems, errors.New(i18n.T(i18n.BadSummaryLength, count)))
	}
	if m.Importance != 0 && (m.Importance < 1 || m.Importance > 5) {
		problems = append(problems, errors.New(i18n.T(i18n.BadImportance, m.Importance)))
	}
	if lines := bodyLines(m.Body); lines > bodyErrorLines {
		problems = append(problems, errors.New(i18n.T(i18n.BadBodyLines, lines, bodyErrorLines)))
	}
	problems = append(problems, tagProblems(m.Tags, spec)...)
	if spec < SpecV2 {
		return problems
	}
	return append(problems, v2Problems(m)...)
}

// v2Problems 는 v0.2 에서 새로 생긴 칸들만 본다.
func v2Problems(m *Memory) []error {
	problems := []error{}
	if m.Author != "" && !IsAuthor(m.Author) {
		problems = append(problems, errors.New(i18n.T(i18n.BadAuthor, m.Author)))
	}
	if title := utf8.RuneCountInString(m.Title); m.Title != "" && (title < titleMinRunes || title > titleMaxRunes) {
		problems = append(problems, errors.New(i18n.T(i18n.BadTitleLength, title)))
	}
	// 이전분(migrated)은 제목이 요약 앞머리 폴백이라 이 자에 다 걸린다. 첫 판은
	// 경고로 보고 사람이 `mem review` 에서 다듬는다 — 여기서 막으면 불변조건
	// I7 때문에 이전한 기억이 통째로 색인에서 빠진다 (설계 2-5).
	if !m.Migrated && titleEchoesSummary(m.Title, m.Summary) {
		problems = append(problems, errors.New(i18n.T(i18n.TitleEchoesSummary)))
	}
	for _, source := range m.Sources {
		if !IsSource(source) {
			problems = append(problems, errors.New(i18n.T(i18n.BadSourceEntry, source)))
		}
	}
	// 덮임은 둘이 한 짝이다. 하나만 있으면 "언제부터 틀렸나" 나 "지금 맞는 것이
	// 무엇인가" 중 하나를 잃는다 (설계 2-2).
	if (m.SupersededBy == "") != (m.InvalidAt == "") {
		problems = append(problems, errors.New(i18n.T(i18n.SupersedePairBroken)))
	}
	return problems
}

// titleEchoesSummary 는 제목이 요약 앞 20자를 그대로 베꼈는지다 (규칙 F06).
func titleEchoesSummary(title, summary string) bool {
	if title == "" || summary == "" {
		return false
	}
	head := []rune(summary)
	if len(head) > titleEchoRunes {
		head = head[:titleEchoRunes]
	}
	return strings.HasPrefix(title, string(head))
}

// bodyLines 는 본문 줄 수다. 빈 본문은 0줄이다.
func bodyLines(body string) int {
	trimmed := strings.TrimRight(body, "\n")
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "\n") + 1
}

func tagProblems(tags []string, spec int) []error {
	problems := []error{}
	low := tagMinLegacy
	if spec >= SpecV2 {
		low = tagMin
	}
	if len(tags) > tagMax || (len(tags) > 0 && len(tags) < low) {
		problems = append(problems, errors.New(i18n.T(i18n.BadTagCount, low, tagMax, len(tags))))
	}
	for _, tag := range tags {
		if !tagPattern.MatchString(tag) {
			problems = append(problems, errors.New(i18n.T(i18n.BadTag, tag)))
		}
	}
	return problems
}

func perTypeProblems(m *Memory, spec int, kind TypeSpec) []error {
	problems := []error{}
	if kind.TodoStatus && m.TodoStatus == "" {
		problems = append(problems, errors.New(i18n.T(i18n.MissingField, todoStatusKey(spec))))
	}
	if kind.Severity && m.Severity == "" {
		problems = append(problems, errors.New(i18n.T(i18n.MissingField, "severity")))
	}
	if m.TodoStatus != "" && !contains(Statuses, m.TodoStatus) {
		problems = append(problems, errors.New(i18n.T(i18n.BadStatus, m.TodoStatus)))
	}
	// todo_status 는 그 칸을 쓰는 종류에만 붙는다 (규칙 F14).
	if m.TodoStatus != "" && m.Type != "" && !kind.TodoStatus {
		problems = append(problems, errors.New(i18n.T(i18n.StatusOnlyTodo, m.Type)))
	}
	if m.Severity != "" && !contains(Severities, m.Severity) {
		problems = append(problems, errors.New(i18n.T(i18n.BadSeverity, m.Severity)))
	}
	// severity 는 그 칸을 쓰는 종류에만 붙는다 (규칙 F15).
	if m.Severity != "" && m.Type != "" && !kind.Severity {
		problems = append(problems, errors.New(i18n.T(i18n.SeverityOnlyIssue, m.Type)))
	}
	return problems
}

// todoStatusKey 는 사람에게 보여줄 칸 이름이다. 옛 파일에는 `status` 로 적혀 있다.
func todoStatusKey(spec int) string {
	if spec >= SpecV2 {
		return "todo_status"
	}
	return "status"
}

// IsScope 는 scope 로 쓸 수 있는 이름인지다. 기억 머리말 검사와 vocab.toml 의
// `[scope]` 키 검사가 같은 자를 쓴다 — 규격 밖 이름이 목록에 앉으면 훅이 그것을
// 그대로 프롬프트에 싣는다.
func IsScope(value string) bool {
	return scopePattern.MatchString(value)
}

// IsFutureDate 는 날짜가 오늘보다 뒤인지다 (규칙 F20 의 뒷절반).
// Validate 에 안 넣었다 — 시각에 기대는 판정은 되돌려 볼 수 없어 자로 못 쓴다.
// 거절은 `add` 관문(quality)이 지금 시각을 받아서 한다.
func IsFutureDate(value string, now time.Time) bool {
	if !IsDate(value) {
		return false
	}
	parsed, err := time.ParseInLocation(DayLayout, value, time.Local)
	if err != nil {
		return false
	}
	return parsed.After(now)
}
