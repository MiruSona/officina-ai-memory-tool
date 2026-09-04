package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// The shapes an inbox file can have (design 2-2). OpAmend replaces the body of
// a memory that already exists; the original is kept in the archive first.
const (
	OpAdd   = "add"
	OpPatch = "patch"
	// OpBody 는 본문을 통째로 갈아 끼운다. v0.0 은 이걸 amend 라 불렀는데
	// 설계 5-2 가 body 로 정해서 옛 이름도 같이 받는다.
	OpBody  = "body"
	OpAmend = "amend"
)

// PatchableFields are the only front matter fields a patch may touch. Nothing
// that decides the id or the path is in here: date and type stay put.
var PatchableFields = []string{"pinned", "status", "invalid_at", "links",
	"summary", "tags", "scope", "severity", "title", "importance", "superseded_by",
	// v0.2 가 늘린 칸. todo_status 는 status 의 새 이름이고, migrate 가
	// author·sources·migrated 를 채워 옛 기억을 새 규격으로 올린다 (설계 2-5).
	"todo_status", "stale_after", "author", "sources", "migrated",
	// v0.3 이 늘린 칸. `review --promote` 가 이 칸을 떼는 유일한 길이다 (결정 6).
	"review"}

// AddRequest is a new memory waiting to be promoted to Markdown.
type AddRequest struct {
	Op         string   `json:"op"`
	Type       string   `json:"type"`
	Date       string   `json:"date,omitempty"`
	Summary    string   `json:"summary"`
	Tags       []string `json:"tags"`
	Source     string   `json:"source"`
	Scope      string   `json:"scope"`
	Title      string   `json:"title,omitempty"`
	Status     string   `json:"status,omitempty"`
	Severity   string   `json:"severity,omitempty"`
	Pinned     bool     `json:"pinned,omitempty"`
	Importance int      `json:"importance,omitempty"`
	InvalidAt  string   `json:"invalid_at,omitempty"`
	Links      []string `json:"links,omitempty"`
	// Author·Sources·StaleAfter 는 v0.2 규격이 새로 받는 칸이다 (설계 2-2).
	// Source 는 옛 규격을 읽을 때만 쓴다.
	Author     string   `json:"author,omitempty"`
	Sources    []string `json:"sources,omitempty"`
	StaleAfter string   `json:"stale_after,omitempty"`
	// Review 는 `add --hold` 다. 승격되면 머리말에 `review: true` 로 남아
	// 검색·훅에서 빠지고, 사람이 `review --promote` 로 뗀다 (결정 6).
	Review bool   `json:"review,omitempty"`
	Body   string `json:"body"`
}

// PatchRequest changes a few front matter fields of a memory that already exists.
type PatchRequest struct {
	Op  string         `json:"op"`
	ID  string         `json:"id"`
	Set map[string]any `json:"set"`
}

// AmendRequest replaces the body of one memory. The id and the date do not
// move, so links and file names made earlier keep pointing at the same memory.
type AmendRequest struct {
	Op   string `json:"op"`
	ID   string `json:"id"`
	Body string `json:"body"`
}

// opEnvelope reads just enough to tell the two shapes apart.
type opEnvelope struct {
	Op string `json:"op"`
}

// InboxItem is one decoded file in inbox/new.
type InboxItem struct {
	Name  string
	Path  string
	Op    string
	Add   *AddRequest
	Patch *PatchRequest
	Amend *AmendRequest
	Raw   []byte
}

var writeSequence atomic.Uint64

// queueName is the name of one queued file: nanosecond, PID and a counter can
// never collide, so a rename into inbox/new never overwrites. A var so the test
// can force the rename to fail.
var queueName = func() string {
	return fmt.Sprintf("%d.%d.%d.json", time.Now().UnixNano(), os.Getpid(), writeSequence.Add(1))
}

// WriteAdd queues a new memory. It writes to inbox/tmp and renames into
// inbox/new, so a reader never sees a half written file (design 7-1).
func (s *Store) WriteAdd(request AddRequest) (string, error) {
	request.Op = OpAdd
	if err := checkAdd(&request); err != nil {
		return "", err
	}
	return s.writeInbox(request)
}

// WritePatch queues a change to an existing memory.
func (s *Store) WritePatch(id string, set map[string]any) (string, error) {
	if id == "" {
		return "", errors.New(i18n.T(i18n.MissingField, "id"))
	}
	if len(set) == 0 {
		return "", errors.New(i18n.T(i18n.MissingField, "set"))
	}
	for key := range set {
		if !containsString(PatchableFields, key) {
			return "", errors.New(i18n.T(i18n.NotPatchable, key))
		}
	}
	return s.writeInbox(PatchRequest{Op: OpPatch, ID: id, Set: set})
}

// WriteAmend queues a new body for a memory that already exists.
func (s *Store) WriteAmend(id, body string) (string, error) {
	if id == "" {
		return "", errors.New(i18n.T(i18n.MissingField, "id"))
	}
	if strings.TrimSpace(body) == "" {
		return "", errors.New(i18n.T(i18n.MissingField, "body"))
	}
	return s.writeInbox(AmendRequest{Op: OpBody, ID: id, Body: strings.TrimRight(body, "\r\n")})
}

func (s *Store) writeInbox(payload any) (string, error) {
	if err := s.writable(); err != nil {
		return "", err
	}
	data, err := encodeJSON(payload)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.InboxTmpDir(), 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.InboxNewDir(), 0o755); err != nil {
		return "", err
	}
	name := queueName()
	tmp := filepath.Join(s.InboxTmpDir(), name)
	if err := writeFileRetry(tmp, data); err != nil {
		return "", err
	}
	if err := renameRetry(tmp, filepath.Join(s.InboxNewDir(), name)); err != nil {
		// The half queued file must not stay in inbox/tmp forever.
		os.Remove(tmp)
		return "", err
	}
	return name, nil
}

// encodeJSON keeps Korean as Korean; escaping it costs 2.6x the tokens.
func encodeJSON(payload any) ([]byte, error) {
	buffer := bytes.Buffer{}
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func writeFileRetry(path string, data []byte) error {
	return withRetry(func() error {
		return os.WriteFile(path, data, 0o644)
	})
}

// ListInbox returns the queued file names in name order, which is write order.
func (s *Store) ListInbox() ([]string, error) {
	entries, err := os.ReadDir(s.InboxNewDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// ReadInbox decodes one queued file. A broken file is an error, never a silent skip.
func (s *Store) ReadInbox(name string) (*InboxItem, error) {
	path := filepath.Join(s.InboxNewDir(), name)
	raw, err := readFileRetry(path)
	if err != nil {
		return nil, err
	}
	text, _, err := decode(raw, path)
	if err != nil {
		return nil, err
	}
	envelope := opEnvelope{}
	if err := json.Unmarshal(text, &envelope); err != nil {
		return nil, err
	}
	item := InboxItem{Name: name, Path: path, Op: envelope.Op, Raw: text}
	if envelope.Op == OpAdd {
		return decodeAdd(text, &item)
	}
	if envelope.Op == OpPatch {
		return decodePatch(text, &item)
	}
	if envelope.Op == OpBody || envelope.Op == OpAmend {
		return decodeAmend(text, &item)
	}
	return nil, errors.New(i18n.T(i18n.UnknownOp, envelope.Op))
}

func decodeAdd(text []byte, item *InboxItem) (*InboxItem, error) {
	request := AddRequest{}
	if err := json.Unmarshal(text, &request); err != nil {
		return nil, err
	}
	if err := checkAdd(&request); err != nil {
		return nil, err
	}
	item.Add = &request
	return item, nil
}

func decodePatch(text []byte, item *InboxItem) (*InboxItem, error) {
	request := PatchRequest{}
	if err := json.Unmarshal(text, &request); err != nil {
		return nil, err
	}
	if request.ID == "" {
		return nil, errors.New(i18n.T(i18n.MissingField, "id"))
	}
	for key := range request.Set {
		if !containsString(PatchableFields, key) {
			return nil, errors.New(i18n.T(i18n.NotPatchable, key))
		}
	}
	item.Patch = &request
	return item, nil
}

func decodeAmend(text []byte, item *InboxItem) (*InboxItem, error) {
	request := AmendRequest{}
	if err := json.Unmarshal(text, &request); err != nil {
		return nil, err
	}
	if request.ID == "" {
		return nil, errors.New(i18n.T(i18n.MissingField, "id"))
	}
	if strings.TrimSpace(request.Body) == "" {
		return nil, errors.New(i18n.T(i18n.MissingField, "body"))
	}
	item.Amend = &request
	return item, nil
}

func checkAdd(request *AddRequest) error {
	// v0.2 는 `author` 를, v0.1 큐 파일은 `source` 를 쓴다. 둘 중 하나만 있으면
	// 된다 — 판을 올리는 중에 큐에 남아 있던 것이 죽으면 안 된다 (설계 2-5).
	pairs := []string{"type", request.Type, "summary", request.Summary,
		"author", request.Author + request.Source, "scope", request.Scope}
	for i := 0; i < len(pairs); i += 2 {
		if pairs[i+1] == "" {
			return errors.New(i18n.T(i18n.MissingField, pairs[i]))
		}
	}
	if len(request.Tags) == 0 {
		return errors.New(i18n.T(i18n.MissingField, "tags"))
	}
	return nil
}

// badReasonsDir 는 실패 이유 파일이 사는 자리다. inbox/bad 옆이 아니라
// local 이다 — lint·status·hook 이 InboxBadDir() 를 그대로 훑어 「bad 파일
// 수」를 세는데, 이유 파일까지 거기 섞이면 그 수가 흔들린다.
func (s *Store) badReasonsDir() string {
	return filepath.Join(s.LocalDir(), "bad-reasons")
}

// WriteBadReason 은 name 이 왜 bad 로 갔는지 한 줄 남긴다. 부르는 쪽이 이미
// 중화한 글만 넘겨야 한다 — 여기서는 다시 안 훑는다.
func (s *Store) WriteBadReason(name, reason string) error {
	if err := s.writable(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.badReasonsDir(), 0o755); err != nil {
		return err
	}
	return writeFileRetry(filepath.Join(s.badReasonsDir(), name+".err"), []byte(reason))
}

// BadReason 은 남겨 둔 이유를 돌려준다. 없으면 빈 글이다 — 이유 파일 없이
// bad 로 간 옛 항목도 있을 수 있다.
func (s *Store) BadReason(name string) string {
	data, err := os.ReadFile(filepath.Join(s.badReasonsDir(), name+".err"))
	if err != nil {
		return ""
	}
	return string(data)
}

// ListBad 는 inbox/bad 에 남은 파일 이름이다. 무엇을 지우는지 먼저 보여줄 수
// 있어야 지우는 명령을 만들 수 있다 (리뷰 C #14).
func (s *Store) ListBad() ([]string, error) {
	entries, err := os.ReadDir(s.InboxBadDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// ClearBad 는 이름을 준 것만 지운다. 사람이 본 목록과 지운 것이 어긋나지 않게
// 폴더를 통째로 비우지 않는다.
func (s *Store) ClearBad(names []string) (int, error) {
	if err := s.writable(); err != nil {
		return 0, err
	}
	gone := 0
	for _, name := range names {
		if strings.ContainsAny(name, `/\:`) {
			continue
		}
		if err := os.Remove(filepath.Join(s.InboxBadDir(), name)); err != nil {
			return gone, err
		}
		gone++
		// 이유 파일은 곁다리다. 옛 항목처럼 없을 수 있으니 못 지워도 넘어간다.
		os.Remove(filepath.Join(s.badReasonsDir(), name+".err"))
	}
	return gone, nil
}

// MoveToBad parks a file we could not use. Nothing is ever deleted quietly.
func (s *Store) MoveToBad(name string) error {
	if err := s.writable(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.InboxBadDir(), 0o755); err != nil {
		return err
	}
	return renameRetry(filepath.Join(s.InboxNewDir(), name), filepath.Join(s.InboxBadDir(), name))
}

// Archive appends the raw JSON to local/received/YYYY-MM-DD.jsonl and drops the
// queued file. It is a receipt of what this machine was asked to store, not a
// memory, so it stays out of git (design 2-3 #9).
func (s *Store) Archive(name string) error {
	if err := s.writable(); err != nil {
		return err
	}
	path := filepath.Join(s.InboxNewDir(), name)
	raw, err := readFileRetry(path)
	if err != nil {
		return err
	}
	if err := s.appendArchive(time.Now(), raw); err != nil {
		return err
	}
	return os.Remove(path)
}

func (s *Store) appendArchive(at time.Time, raw []byte) error {
	_, _, err := AppendRecord(s.ReceivedDir(), at, raw)
	return err
}

func containsString(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
