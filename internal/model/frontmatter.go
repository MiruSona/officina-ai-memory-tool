package model

import (
	"bytes"
	"errors"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

const fence = "---"

// rawHead 는 머리말을 읽을 때만 쓰는 꼴이다. v0.2 칸과 옛(v0.1) 칸을 같이
// 받는다 — migrate 가 옛 파일을 읽어 새 규격으로 써야 하고, 그 전까지도
// 저장소가 보여야 한다 (설계 2-5).
type rawHead struct {
	ID           string   `yaml:"id"`
	Type         string   `yaml:"type"`
	Title        string   `yaml:"title"`
	Summary      string   `yaml:"summary"`
	Tags         []string `yaml:"tags"`
	Scope        string   `yaml:"scope"`
	Date         string   `yaml:"date"`
	Author       string   `yaml:"author"`
	Sources      []string `yaml:"sources"`
	Links        []string `yaml:"links"`
	SupersededBy string   `yaml:"superseded_by"`
	InvalidAt    string   `yaml:"invalid_at"`
	StaleAfter   string   `yaml:"stale_after"`
	Importance   int      `yaml:"importance"`
	Pinned       bool     `yaml:"pinned"`
	TodoStatus   string   `yaml:"todo_status"`
	Severity     string   `yaml:"severity"`
	Migrated     bool     `yaml:"migrated"`
	Review       bool     `yaml:"review"`
	State        string   `yaml:"state"`
	Archived     string   `yaml:"archived"`

	// 옛 칸 둘. 새 파일에는 없다.
	Source string `yaml:"source"`
	Status string `yaml:"status"`
}

// Parse 는 기억 파일을 머리말과 본문으로 가른다.
// 다른 도구도 우리 파일을 쓰므로 CRLF 와 앞머리 BOM 을 받아 준다.
// 옛 칸(`source`·`status`)만 있는 파일은 새 칸으로 옮겨 읽고 Spec 을 SpecV1 로
// 표시한다 — 원래 값은 LegacySource·LegacyStatus 에 그대로 남는다.
func Parse(data []byte) (*Memory, error) {
	text := strings.ReplaceAll(strings.TrimPrefix(string(data), "\ufeff"), "\r\n", "\n")
	if !strings.HasPrefix(text, fence+"\n") {
		return nil, errors.New(i18n.T(i18n.BadFrontMatter))
	}
	rest := text[len(fence)+1:]
	end := strings.Index(rest, "\n"+fence)
	if end < 0 {
		return nil, errors.New(i18n.T(i18n.UnclosedFrontMatter))
	}
	head := rest[:end]
	body := rest[end+len(fence)+1:]
	if cut := strings.IndexByte(body, '\n'); cut >= 0 {
		body = body[cut+1:]
	} else {
		body = ""
	}

	raw := rawHead{}
	if err := yaml.Unmarshal([]byte(head), &raw); err != nil {
		// 라이브러리는 영어로 말한다. 그 안의 줄 번호만 살려서 쓴다.
		return nil, errors.New(i18n.T(i18n.BadYAML, err.Error()))
	}
	memory := raw.toMemory()
	memory.Body = strings.Trim(body, "\n")
	return memory, nil
}

func (r rawHead) toMemory() *Memory {
	memory := Memory{
		ID: r.ID, Type: r.Type, Title: r.Title, Summary: r.Summary, Tags: r.Tags,
		Scope: r.Scope, Date: r.Date, Author: r.Author, Sources: r.Sources, Links: r.Links,
		SupersededBy: r.SupersededBy, InvalidAt: r.InvalidAt, StaleAfter: r.StaleAfter,
		Importance: r.Importance, Pinned: r.Pinned, TodoStatus: r.TodoStatus,
		Severity: r.Severity, Migrated: r.Migrated, Review: r.Review,
		State: r.State, Archived: r.Archived,
		Spec: SpecV2,
	}
	if r.Source != "" {
		memory.LegacySource = r.Source
		memory.Spec = SpecV1
		if memory.Author == "" {
			memory.Author = LegacyAuthor(r.Source)
		}
	}
	if r.Status != "" {
		memory.LegacyStatus = r.Status
		memory.Spec = SpecV1
		if memory.TodoStatus == "" {
			memory.TodoStatus = r.Status
		}
	}
	// 옛 파일은 author 도 sources 도 없는 것이 보통이다. 둘 다 비었으면
	// 새 규격으로 볼 수 없다.
	if memory.Author == "" {
		memory.Spec = SpecV1
	}
	return &memory
}

// Encode 는 머리말을 정해진 칸 차례로, LF 로, BOM 없이 쓴다.
// 따옴표는 yaml.v3 에 맡긴다. 손으로 감싸다가 `\` 가 든 요약이 조용히 깨졌다.
// 옛 규격으로 읽은 기억(SpecV1)은 옛 칸 이름 그대로 다시 쓴다 — 규격을 옮기는
// 것은 `mem migrate` 하나뿐이어야 한다.
func Encode(m *Memory) []byte {
	head := yaml.Node{Kind: yaml.MappingNode}
	putText(&head, "id", m.ID)
	putText(&head, "type", m.Type)
	putText(&head, "title", m.Title)
	putText(&head, "summary", m.Summary)
	putList(&head, "tags", m.Tags)
	putText(&head, "scope", m.Scope)
	putDate(&head, "date", m.Date)
	if m.IsLegacy() && m.LegacySource != "" {
		putText(&head, "source", m.LegacySource)
	} else {
		putText(&head, "author", m.Author)
	}
	putList(&head, "sources", m.Sources)
	putList(&head, "links", m.Links)
	putText(&head, "superseded_by", m.SupersededBy)
	putDate(&head, "invalid_at", m.InvalidAt)
	putDate(&head, "stale_after", m.StaleAfter)
	// importance 는 선택 칸이라 안 적으면 3 이다. 0 이면 줄 자체를 안 쓴다.
	if m.Importance != 0 {
		putRaw(&head, "importance", "!!int", strconv.Itoa(m.Importance))
	}
	if m.Pinned {
		putRaw(&head, "pinned", "!!bool", "true")
	}
	if m.IsLegacy() && m.LegacyStatus != "" {
		putText(&head, "status", m.LegacyStatus)
	} else {
		putText(&head, "todo_status", m.TodoStatus)
	}
	putText(&head, "severity", m.Severity)
	if m.Migrated {
		putRaw(&head, "migrated", "!!bool", "true")
	}
	// 승격을 기다리는 자동 생성 기억이다 (결정 6). 꺼지면 줄 자체가 사라진다 —
	// `review --promote` 가 「칸을 뗀다」는 것이 이 뜻이다.
	if m.Review {
		putRaw(&head, "review", "!!bool", "true")
	}
	putText(&head, "state", m.State)
	putText(&head, "archived", m.Archived)

	out := strings.Builder{}
	out.WriteString(fence + "\n")
	out.WriteString(marshalHead(&head))
	out.WriteString(fence + "\n")
	if m.Body != "" {
		out.WriteString("\n" + strings.Trim(m.Body, "\n") + "\n")
	}
	return []byte(out.String())
}

func marshalHead(head *yaml.Node) string {
	if len(head.Content) == 0 {
		return ""
	}
	buffer := bytes.Buffer{}
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(head); err != nil {
		return ""
	}
	encoder.Close()
	return buffer.String()
}

func putText(head *yaml.Node, key, value string) {
	if value == "" {
		return
	}
	putRaw(head, key, "!!str", value)
}

// putDate 는 날짜를 따옴표 없이 적는다. `!!str` 을 달면 yaml 이 날짜로 읽힐까 봐
// 감싸는데, 우리 날짜는 Validate 가 이미 꼴을 못 박아 둬서 그럴 일이 없다.
func putDate(head *yaml.Node, key, value string) {
	if value == "" {
		return
	}
	putRaw(head, key, "", value)
}

func putRaw(head *yaml.Node, key, tag, value string) {
	name := yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	item := yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value}
	head.Content = append(head.Content, &name, &item)
}

// putList 는 목록을 한 줄로 쓴다: 머리말은 사람이 눈으로 훑는 자리다.
// 다만 `sources` 는 줄이 길어 한 줄에 넣으면 안 읽히므로 여러 줄로 쓴다.
func putList(head *yaml.Node, key string, values []string) {
	if len(values) == 0 {
		return
	}
	name := yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	list := yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	if key == "sources" {
		list.Style = 0
	}
	for _, value := range values {
		item := yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
		list.Content = append(list.Content, &item)
	}
	head.Content = append(head.Content, &name, &list)
}
