// Package model 은 기억 한 건이다 — 머리말 칸, 종류 표, 그리고 저장되는
// 파일이 다 지켜야 하는 규칙.
package model

import "strings"

// 기억 종류 일곱. 저장소마다 vocab.toml 로 늘릴 수 있고, 취급은
// typespec.go 의 표가 정한다.
const (
	TypeTodo     = "todo"
	TypeHistory  = "history"
	TypeIssue    = "issue"
	TypeCaution  = "caution"
	TypeDecision = "decision"
	TypeHowto    = "howto"
	TypeFact     = "fact"
)

// Types 는 기본표의 차례 그대로다. 저장소 표를 든 자리는 이것 대신
// TypeTable.Names() 를 본다.
var Types = DefaultTypes().Names()

// SourcesRequiredTypes 는 `sources` 를 반드시 달아야 하는 기본 종류다 (규칙 F16).
var SourcesRequiredTypes = DefaultTypes().Where(func(spec TypeSpec) bool {
	return spec.Sources == SourcesRequired
})

// 규격 판. Spec 이 SpecV2 인 기억만 v0.2 필수 칸을 다 본다.
// 옛 파일(SpecV1)까지 v0.2 자로 재면 `Validate` 를 통과하는 기억이 하나도 없어
// 불변조건 8(통과한 것만 색인) 때문에 저장소가 통째로 안 보인다 (설계 2-5).
const (
	SpecUnknown = 0
	SpecV1      = 1
	SpecV2      = 2
)

// 옛 규격(v0.1)의 `source` 값. 읽기와 migrate 만 쓴다. 새로 쓰는 자리는 Author 다.
const (
	LegacySourceUser   = "user"
	LegacySourceAI     = "ai"
	LegacySourceHook   = "hook"
	LegacySourceImport = "import"
)

// LegacySources 는 옛 `source` 칸이 가질 수 있던 값 전부다.
var LegacySources = []string{LegacySourceUser, LegacySourceAI, LegacySourceHook, LegacySourceImport}

// todo_status 값 셋 (규칙 F14).
const (
	StatusOpen  = "open"
	StatusDoing = "doing"
	StatusDone  = "done"
)

var Statuses = []string{StatusOpen, StatusDoing, StatusDone}

// severity 값 셋 (규칙 F15).
const (
	SeverityLow  = "low"
	SeverityMid  = "mid"
	SeverityHigh = "high"
)

var Severities = []string{SeverityLow, SeverityMid, SeverityHigh}

// severityAlias 는 사람이 흔히 치는 다른 말이다. `medium` 하나에 거절당하는
// 왕복이 잦아서 받아 주기로 했다 (사용 피드백 2026-09-20).
var severityAlias = map[string]string{"medium": SeverityMid, "middle": SeverityMid}

// NormalizeSeverity 는 별칭을 표준 값으로 바꾼다. 두 번째 값은 바꿨는지다.
func NormalizeSeverity(value string) (string, bool) {
	fixed, found := severityAlias[strings.ToLower(strings.TrimSpace(value))]
	if !found {
		return value, false
	}
	return fixed, true
}

// `sources` 항목이 쓸 수 있는 접두 다섯 (규칙 F17).
const (
	SourceFile   = "file:"
	SourceCommit = "commit:"
	SourceURL    = "url:"
	SourceMem    = "mem:"
	SourceNote   = "note:"
)

// NoteMigrated 는 `migrate` 가 **근거를 못 찾은 기억에 일괄로 넣는 자리표시**다.
// 뜻은 「근거가 없다」지 「이런 근거가 있다」가 아니다. 그래서 이웃 링크는 이
// 값을 근거로 안 센다 (v0.4 리뷰 A R5 — 20k 이전 저장소에서 15,600건이 이 한
// 줄을 똑같이 갖고 한 통에 들어갔다). 글을 고치면 이미 이전된 저장소가 이
// 값을 못 알아본다 — 고치지 않는다.
const NoteMigrated = SourceNote + "이전됨 — v0.1 규격이라 근거가 안 적혀 있다"

// SourcePrefixes 는 좁은 것부터가 아니라 설계 2-2 가 적은 차례다.
var SourcePrefixes = []string{SourceFile, SourceCommit, SourceURL, SourceMem, SourceNote}

// `author` 가 쓸 수 있는 접두 (규칙 F19). 접두가 없으면 `<도구>/<버전>` 꼴이다.
const (
	AuthorHuman  = "human:"
	AuthorHook   = "hook:"
	AuthorImport = "import:"
)

// Memory 는 기억 파일 한 건이다 — 머리말과 본문.
// 여기 칸 차례가 곧 파일에 쓰이는 차례다 (설계 2-1).
// json 칸 이름은 다른 명령의 `--json` 과 같은 소문자 snake 다.
type Memory struct {
	ID      string   `yaml:"id" json:"id"`
	Type    string   `yaml:"type" json:"type"`
	Title   string   `yaml:"title,omitempty" json:"title,omitempty"`
	Summary string   `yaml:"summary" json:"summary"`
	Tags    []string `yaml:"tags" json:"tags"`
	Scope   string   `yaml:"scope" json:"scope"`
	Date    string   `yaml:"date" json:"date"`
	// Author 는 누가 썼나다. `human:<아이디>` / `<도구>/<버전>` / `hook:<이름>` /
	// `import:<저장소>`. AI 가 쓴 기억과 사람이 확인한 기억을 가르는 것이
	// 오염 사고의 유일한 방어선이다 (설계 결정 3).
	Author string `yaml:"author,omitempty" json:"author,omitempty"`
	// Sources 는 근거다. 접두 다섯 중 하나로 시작한다 (설계 결정 4).
	Sources []string `yaml:"sources,omitempty" json:"sources,omitempty"`
	Links   []string `yaml:"links,omitempty" json:"links,omitempty"`
	// SupersededBy 는 이 결정을 덮은 새 기억이다. invalid_at 은 언제부터 틀렸는지만
	// 알려주고 지금 맞는 것이 무엇인지는 못 알려준다.
	SupersededBy string `yaml:"superseded_by,omitempty" json:"superseded_by,omitempty"`
	InvalidAt    string `yaml:"invalid_at,omitempty" json:"invalid_at,omitempty"`
	// StaleAfter 는 "이 날 지나면 다시 보자" 는 예약이다 (설계 결정 6).
	StaleAfter string `yaml:"stale_after,omitempty" json:"stale_after,omitempty"`
	Importance int    `yaml:"importance,omitempty" json:"importance,omitempty"`
	Pinned     bool   `yaml:"pinned,omitempty" json:"pinned,omitempty"`
	// TodoStatus 는 todo 의 진행이다. OKF 의 `status` 와 뜻이 달라 이름을 비켰다
	// (설계 결정 5).
	TodoStatus string `yaml:"todo_status,omitempty" json:"todo_status,omitempty"`
	Severity   string `yaml:"severity,omitempty" json:"severity,omitempty"`
	// Migrated 는 migrate 가 옛 규격에서 옮긴 기억이라는 표시다. 첫 판에서
	// F06·F08·C04 를 경고로 보고, G6 결함률에서 뺀다 (설계 2-5).
	Migrated bool `yaml:"migrated,omitempty" json:"migrated,omitempty"`
	// Review 는 자동으로 만들어져 아직 사람이 승격 안 한 기억이다 (결정 6).
	// 검색·훅에 안 뜨고, `mem review --promote` 로 사람이 뗀다.
	Review bool `yaml:"review,omitempty" json:"review,omitempty"`
	// State 와 Archived 는 gc 가 본문을 접을 때 쓴다. 사람이 쓰면 안 된다.
	State    string `yaml:"state,omitempty" json:"state,omitempty"`
	Archived string `yaml:"archived,omitempty" json:"archived,omitempty"`

	Body string `yaml:"-" json:"body"`

	// Spec 은 이 기억이 어느 규격으로 읽혔나다. 파일에 안 쓴다.
	Spec int `yaml:"-" json:"-"`
	// LegacySource·LegacyStatus 는 옛 파일에서 읽은 원래 값이다. 그대로 다시
	// 써야 migrate 전에 도구가 남의 파일을 조용히 고치지 않는다.
	LegacySource string `yaml:"-" json:"-"`
	LegacyStatus string `yaml:"-" json:"-"`
}

// DisplayTitle 은 제목이 없을 때 요약 앞 40자를 쓴다. v0.2 규격에서는 title 이
// 필수라 이 폴백은 옛 기억에만 걸린다.
func (m *Memory) DisplayTitle() string {
	if m.Title != "" {
		return m.Title
	}
	runes := []rune(m.Summary)
	if len(runes) <= 40 {
		return m.Summary
	}
	return string(runes[:40])
}

// IsLegacy 는 옛 규격으로 읽힌 기억인지다.
func (m *Memory) IsLegacy() bool { return m.Spec < SpecV2 }

// LegacyAuthor 는 옛 `source` 값을 `author` 꼴로 옮긴다 (설계 2-5).
// `user` 의 사람 아이디는 여기서 못 안다 — migrate 가 git user.name 으로 덮는다.
func LegacyAuthor(source string) string {
	switch source {
	case LegacySourceUser:
		return AuthorHuman + "unknown"
	case LegacySourceHook:
		return AuthorHook + "session-start"
	case LegacySourceImport:
		return AuthorImport + "unknown"
	case LegacySourceAI:
		return "claude-code/unknown"
	case "":
		return ""
	}
	return "claude-code/unknown"
}

// SourceKind 는 근거 한 줄의 접두를 돌려준다. 접두가 없으면 빈 글이다.
func SourceKind(source string) string {
	for _, prefix := range SourcePrefixes {
		if strings.HasPrefix(source, prefix) {
			return strings.TrimSuffix(prefix, ":")
		}
	}
	return ""
}

// SourcesNoteOnly 는 근거가 `note:` 뿐인지다 (규칙 F18 — 경고를 내는 것은 lint 다).
func SourcesNoteOnly(sources []string) bool {
	if len(sources) == 0 {
		return false
	}
	for _, source := range sources {
		if SourceKind(source) != "note" {
			return false
		}
	}
	return true
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
