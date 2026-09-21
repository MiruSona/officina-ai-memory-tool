package quality

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 규격 길이. model 의 Validate 와 같은 값이다 — 관문은 규칙 이름을 붙여 말해야
// 해서 검사를 다시 쓴다.
const (
	summaryMinRunes = 30
	summaryMaxRunes = 120
	titleMinRunes   = 6
	titleMaxRunes   = 40
	titleEchoRunes  = 20
)

// genericTitles 는 주제어가 없는 제목이다 (규칙 F07). 이런 제목은 head 열이
// 16배로 쳐도 새 신호를 안 준다.
var genericTitles = []string{
	"정리", "메모", "결정", "기록", "요약", "노트", "내용", "확인", "작업",
	"할 일", "진행", "정한 것", "잡담", "임시", "테스트", "시험", "todo", "note", "memo",
}

// checkField 는 머리말 규격(F 계열)을 본다. 태그 별칭 치환은 Gate 가 먼저
// 해 두고 여기는 치환된 값을 본다.
func checkField(m *model.Memory, opt Options) []Finding {
	found := []Finding{}
	add := func(rule, reason string, next ...string) {
		found = append(found, opt.finding(rule, m, reason, next...))
	}
	for _, name := range missingFields(m, opt.spec(m)) {
		// id 는 `add` 가 스스로 붙이는 칸이라 `--id` 라는 옵션이 없다. 없는
		// 옵션을 시키면 안 된다 (실데이터 시험 A2).
		if name == "id" {
			add(RuleRequiredField, "필수 칸 `id` 가 비었다. 파일 머리말에 id 줄이 없다",
				"파일 머리말에 `id: YYYYMMDD-8자리hex` 를 넣고 mem index 를 돌린다")
			continue
		}
		add(RuleRequiredField, fmt.Sprintf("필수 칸 `%s` 가 비었다", name),
			fmt.Sprintf("mem add … --%s <값>", strings.ReplaceAll(name, "_", "-")))
	}
	if m.ID != "" && !model.IsID(m.ID) {
		add(RuleIDShape, fmt.Sprintf("id `%s` 가 `YYYYMMDD-8자리hex` 꼴이 아니다", m.ID))
	}
	if names := opt.types().Names(); m.Type != "" && !hasString(names, m.Type) {
		add(RuleTypeValue, fmt.Sprintf("종류 `%s` 는 %d가지(%s) 밖이다",
			m.Type, len(names), strings.Join(names, " ")))
	}
	if count := utf8.RuneCountInString(m.Summary); m.Summary != "" && (count < summaryMinRunes || count > summaryMaxRunes) {
		add(RuleSummaryLen, fmt.Sprintf("요약이 %d자다. %d~%d자 한 문장으로 쓴다", count, summaryMinRunes, summaryMaxRunes))
	}
	found = append(found, checkTitle(m, opt)...)
	found = append(found, checkTags(m, opt)...)
	found = append(found, checkScope(m, opt)...)
	found = append(found, checkPerType(m, opt)...)
	found = append(found, checkSources(m, opt)...)
	if opt.spec(m) >= model.SpecV2 && m.Author != "" && !model.IsAuthor(m.Author) {
		add(RuleAuthorShape, fmt.Sprintf("author `%s` 가 `human:<아이디>` · `<도구>/<버전>` · `hook:<이름>` 셋 중 하나가 아니다", m.Author))
	}
	found = append(found, checkDates(m, opt)...)
	return found
}

// missingFields 는 비어 있는 필수 칸 이름이다 (규칙 F02).
func missingFields(m *model.Memory, spec int) []string {
	pairs := [][2]string{{"id", m.ID}, {"type", m.Type}, {"summary", m.Summary},
		{"tags", strings.Join(m.Tags, "")}, {"scope", m.Scope}, {"date", m.Date}}
	if spec >= model.SpecV2 {
		pairs = append(pairs, [2]string{"author", m.Author})
		// 이전분은 제목이 비어 있는 것이 정상이다 — migrate 가 요약을 베낀
		// 제목을 지어내지 않는다. 제목은 `mem review --kind title` 이 부른다.
		if !m.Migrated {
			pairs = append(pairs, [2]string{"title", m.Title})
		}
	}
	missing := []string{}
	for _, pair := range pairs {
		if strings.TrimSpace(pair[1]) == "" {
			missing = append(missing, pair[0])
		}
	}
	return missing
}

func checkTitle(m *model.Memory, opt Options) []Finding {
	if opt.spec(m) < model.SpecV2 || m.Title == "" {
		return nil
	}
	found := []Finding{}
	count := utf8.RuneCountInString(m.Title)
	if count < titleMinRunes || count > titleMaxRunes {
		found = append(found, opt.finding(RuleTitleShape, m,
			fmt.Sprintf("제목이 %d자다. %d~%d자로 쓴다", count, titleMinRunes, titleMaxRunes)))
	} else if titleEchoesSummary(m.Title, m.Summary) {
		found = append(found, opt.finding(RuleTitleShape, m,
			"제목이 요약 앞머리를 그대로 베꼈다. 제목에는 요약에 없는 말을 쓴다"))
	}
	if generic := genericTitle(m.Title); generic != "" {
		found = append(found, opt.finding(RuleTitleNotGeneric, m,
			fmt.Sprintf("제목 `%s` 에 주제어가 없다 (`%s` 만으로는 뭘 정했는지 모른다)", m.Title, generic)))
	}
	return found
}

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

// genericTitle 은 제목이 빈 말뿐인지다. 금지어를 다 떼고 남는 글자가 두 자
// 미만이면 주제어가 없는 것이다.
func genericTitle(title string) string {
	rest := strings.ToLower(strings.TrimSpace(title))
	hit := ""
	for _, word := range genericTitles {
		if strings.Contains(rest, word) {
			hit = word
			rest = strings.ReplaceAll(rest, word, "")
		}
	}
	if hit == "" {
		return ""
	}
	if utf8.RuneCountInString(strings.TrimSpace(strings.Trim(rest, "-·, .:"))) >= 2 {
		return ""
	}
	return hit
}

func checkTags(m *model.Memory, opt Options) []Finding {
	found := []Finding{}
	low, high := opt.Config.Quality.TagMin, opt.Config.Quality.TagMax
	if opt.spec(m) < model.SpecV2 {
		low = 1
	}
	if len(m.Tags) > 0 && (len(m.Tags) < low || len(m.Tags) > high) {
		found = append(found, opt.finding(RuleTagCount, m,
			fmt.Sprintf("태그가 %d개다. %d~%d개로 단다", len(m.Tags), low, high)))
	}
	for _, tag := range m.Tags {
		if opt.Vocab.TagDenied(tag) {
			found = append(found, opt.finding(RuleTagNotSource, m,
				fmt.Sprintf("`%s` 는 주제가 아니라 출처다. 출처는 author·sources 칸이 맡는다", tag)))
			continue
		}
		// 규격 밖 글자는 **거절**이다. `index` 승격이 model.Validate 로 무조건
		// 막는 자라, 여기서 경고로 받으면 사람은 「들어갔다」고 듣고 기억은
		// inbox/bad 로 간다 (W3). 표준 목록을 아직 안 정했어도 안 봐준다 —
		// 이 자는 목록이 아니라 글자 규격이다.
		if !model.IsTag(tag) {
			found = append(found, opt.finding(RuleTagShape, m,
				fmt.Sprintf("`%s` 는 태그 규격 밖이다. 태그는 영어 소문자·숫자·하이픈만 쓴다", tag),
				"mem tags --suggest — 이 저장소가 쓰는 태그 후보를 본다",
				"mem tags --add <영문태그> — 쓸 태그를 표준으로 삼는다"))
			continue
		}
		if _, standard, _ := opt.Vocab.NormalizeTag(tag); !standard {
			found = append(found, opt.finding(RuleTagStandard, m,
				fmt.Sprintf("`%s` 는 표준 태그 목록 밖이다%s", tag, learningNote(opt.Vocab.TagLearning())),
				standardTagSteps(opt.Vocab, tag)...))
		}
	}
	return found
}

// nearTagLimit 은 가까운 후보를 몇 개까지 찍을지다. 넷을 넘으면 고르는 일이
// 다시 사람 몫으로 돌아간다.
const nearTagLimit = 3

// standardTagSteps 는 표준 밖 태그에 붙는 「다음에 할 것」이다. 가까운 후보를
// 먼저 보여줘야 낱말 표 파일을 직접 열 일이 없다 (사용 피드백 2026-09-20).
func standardTagSteps(vocab config.Vocab, tag string) []string {
	steps := []string{}
	if near := vocab.NearTags(tag, nearTagLimit); len(near) > 0 {
		steps = append(steps,
			fmt.Sprintf("가까운 표준 태그 : %s — 뜻이 같으면 이것을 쓴다", strings.Join(near, " · ")))
	}
	return append(steps,
		fmt.Sprintf("mem tags --add %s — 이 태그를 표준으로 삼는다", tag),
		"mem tags --list — 표준 태그·scope 를 다 본다")
}

// learningNote 는 「아직 표준을 안 정해서 경고만 한다」 는 꼬리말이다.
// 거절이 아닌 이유를 그 자리에서 말해 줘야 사람이 무시해도 되는지 안다.
func learningNote(learning bool) string {
	if !learning {
		return ""
	}
	return " (표준 목록을 아직 안 정해서 경고만 한다)"
}

func checkScope(m *model.Memory, opt Options) []Finding {
	if m.Scope == "" {
		return nil
	}
	if _, standard, _ := opt.Vocab.NormalizeScope(m.Scope); standard {
		return nil
	}
	// `--new-scope` 라는 옵션은 없다. 없는 옵션을 시키면 거절당한 사람이
	// 빠져나갈 길이 없다 (실데이터 시험 A2). 표준 목록을 늘리는 명령을 준다.
	return []Finding{opt.finding(RuleScopeStandard, m,
		fmt.Sprintf("scope `%s` 는 표준 목록 밖이다. scope 는 툴·부품 이름 통째 하나만 쓴다%s",
			m.Scope, learningNote(opt.Vocab.ScopeLearning())),
		fmt.Sprintf("mem tags --add-scope %s — 이 이름을 이 저장소 표준으로 삼는다", m.Scope))}
}

func checkPerType(m *model.Memory, opt Options) []Finding {
	found := []Finding{}
	kind := opt.typeSpec(m)
	add := func(rule, reason string) { found = append(found, opt.finding(rule, m, reason)) }
	switch {
	case kind.TodoStatus && m.TodoStatus == "":
		add(RuleTodoStatus, "todo 는 todo_status(open·doing·done)가 있어야 한다")
	case m.TodoStatus != "" && !kind.TodoStatus:
		add(RuleTodoStatus, fmt.Sprintf("todo_status 는 todo 에만 붙는다 (지금 종류는 %s)", m.Type))
	case m.TodoStatus != "" && !hasString(model.Statuses, m.TodoStatus):
		add(RuleTodoStatus, fmt.Sprintf("todo_status `%s` 는 open·doing·done 밖이다", m.TodoStatus))
	}
	issueLike := kind.Severity
	switch {
	case issueLike && m.Severity == "":
		add(RuleSeverity, fmt.Sprintf("%s 는 severity(high·mid·low)가 있어야 한다", m.Type))
	case m.Severity != "" && !issueLike:
		add(RuleSeverity, fmt.Sprintf("severity 는 issue·caution 에만 붙는다 (지금 종류는 %s)", m.Type))
	case m.Severity != "" && !hasString(model.Severities, m.Severity):
		add(RuleSeverity, fmt.Sprintf("severity `%s` 는 high·mid·low 밖이다%s", m.Severity, severityHint(m.Severity)))
	}
	return found
}

// severityHint 는 `medium` 처럼 흔히 쓰는 다른 말에 표준 이름을 권한다.
// add 는 관문 앞에서 바꿔 주지만 파일에 손으로 적은 값은 여기로 온다.
func severityHint(value string) string {
	fixed, changed := model.NormalizeSeverity(value)
	if !changed {
		return ""
	}
	return fmt.Sprintf(" (`%s` 로 적는다)", fixed)
}

func checkSources(m *model.Memory, opt Options) []Finding {
	if opt.spec(m) < model.SpecV2 {
		return nil
	}
	found := []Finding{}
	if len(m.Sources) == 0 {
		// 종류 표의 sources 칸이 세기를 정한다 — required 면 거절, warn 이면 경고다
		// (기본표에서 required 는 decision·issue·caution·fact, warn 은 history 다).
		// 조사 표가 수치를 담는데 근거가 없으면 반년 뒤에 아무도 못 믿는다
		// (골든셋 SRC 15건 중 3건이 근거 없는 history 다).
		one := Finding{}
		switch opt.typeSpec(m).Sources {
		case model.SourcesRequired:
			one = opt.finding(RuleSourcesRequired, m,
				fmt.Sprintf("%s 는 근거(sources)가 하나 이상 있어야 한다", m.Type),
				"mem add … --sources file:<경로> 또는 --sources commit:<hex>")
		case model.SourcesWarn:
			one = opt.finding(RuleSourcesRequired, m, "기록에 근거(sources)가 없다. 어디서 온 수인지 적는다",
				"mem add … --sources file:<경로>")
			one.Level = GradeWarn
		default:
			return found
		}
		return append(found, one)
	}
	for _, source := range m.Sources {
		if !model.IsSource(source) {
			found = append(found, opt.finding(RuleSourcesShape, m,
				fmt.Sprintf("근거 `%s` 가 file:·commit:·url:·mem:·note: 다섯 접두 밖이다", source)))
		}
		if spacedSources(source) {
			found = append(found, opt.finding(RuleSourcesShape, m,
				"근거 여럿을 공백으로 이은 것 같다. 쉼표로 나눈다"))
		}
	}
	if model.SourcesNoteOnly(m.Sources) {
		found = append(found, opt.finding(RuleSourcesNoteOnly, m,
			"근거가 note: 뿐이다. 파일·커밋·주소 중 하나를 대면 낡았는지 기계가 볼 수 있다"))
	}
	return found
}

// spacedSources 는 `file:a note:b` 처럼 한 값에 근거 여럿을 공백으로 이은 꼴이다.
// 쉼표로 안 나누면 앞 하나만 먹고 뒤가 통째로 사라진다 (사용 피드백 2026-09-20).
// `note:` 는 사람이 쓰는 글이라 안 본다 — 그 안의 `file:내용` 은 다음 근거가
// 아니라 글의 일부다 (리뷰 2026-09-21).
func spacedSources(source string) bool {
	if strings.HasPrefix(source, model.SourceNote) {
		return false
	}
	fields := strings.Fields(source)
	if len(fields) < 2 {
		return false
	}
	for _, field := range fields[1:] {
		for _, prefix := range model.SourcePrefixes {
			if strings.HasPrefix(field, prefix) {
				return true
			}
		}
	}
	return false
}

func checkDates(m *model.Memory, opt Options) []Finding {
	found := []Finding{}
	for _, pair := range [][2]string{{"date", m.Date}, {"invalid_at", m.InvalidAt}, {"stale_after", m.StaleAfter}} {
		if pair[1] == "" {
			continue
		}
		if !model.IsDate(pair[1]) {
			found = append(found, opt.finding(RuleDateShape, m,
				fmt.Sprintf("%s `%s` 는 달력에 있는 YYYY-MM-DD 가 아니다", pair[0], pair[1])))
		}
	}
	// 미래 날짜는 date 만 본다. stale_after 는 원래 미래를 가리키는 칸이다.
	if model.IsFutureDate(m.Date, opt.Now) {
		found = append(found, opt.finding(RuleDateShape, m,
			fmt.Sprintf("date `%s` 가 오늘(%s)보다 뒤다", m.Date, opt.Now.Format(model.DayLayout))))
	}
	return found
}

func hasString(list []string, value string) bool {
	for _, one := range list {
		if one == value {
			return true
		}
	}
	return false
}
