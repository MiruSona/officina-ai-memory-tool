package gc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
	"github.com/mirusona/officina-ai-memory-tool/internal/token"
)

// OpFold 는 아카이브 줄의 op 값이다. amend 와 같은 칸 이름을 쓴다 — 읽는 쪽이
// 하나뿐이라 칸 이름이 다르면 아무도 다시 못 찾는다.
const OpFold = "gc"

// codeFoldLines 는 코드 블록을 몇 줄부터 접는지다.
const codeFoldLines = 8

// mover 는 한 번 적용하는 동안의 상태다.
type mover struct {
	db    *index.DB
	store *store.Store
	now   time.Time
}

// applyItem 은 기억 하나를 옮긴다. 따뜻함은 본문을 규칙으로 줄이고, 차가움은
// 요약만 남긴다. 둘 다 파일은 그 자리에 그대로 두고 원본을 아카이브에 남긴다.
func (m *mover) applyItem(item Item) error {
	file, err := m.store.ReadMemory(item.Path)
	if err != nil {
		return err
	}
	memory := file.Memory
	// 원본을 아카이브에 먼저 넣는다. 옛 본문을 잃는 것만은 되돌릴 수 없다.
	archived, err := m.archive(item, memory)
	if err != nil {
		return err
	}
	if memory.Archived == "" {
		memory.Archived = archived
	}
	if item.Step == StepCold {
		memory.State = index.StateCold
		memory.Body = i18n.T(i18n.GCColdNotice, memory.ID)
	} else {
		memory.State = index.StateWarm
		memory.Body = compress(memory.Body, memory.ID)
	}
	if err := m.store.WriteMemory(memory); err != nil {
		return err
	}
	return m.refreshRow(item, memory)
}

// archive 는 파일의 바이트를 그대로 넣는다. 다시 인코딩하면 규격이 모르는
// 머리말 칸이 사라지는데, 아카이브가 마지막 사본이다.
func (m *mover) archive(item Item, memory *model.Memory) (string, error) {
	line, err := json.Marshal(store.ArchiveRecord{Op: OpFold, ID: item.ID, Path: item.Path,
		ArchivedAt: m.now.Format(time.RFC3339), Markdown: m.verbatim(item, memory)})
	if err != nil {
		return "", err
	}
	written, _, err := store.AppendRecord(m.store.ArchiveDir(), m.now, line)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(m.store.Dir, written)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(relative), nil
}

func (m *mover) verbatim(item Item, memory *model.Memory) string {
	abs, err := m.store.Abs(item.Path)
	if err != nil {
		return string(model.Encode(memory))
	}
	raw, err := os.ReadFile(abs)
	if err != nil || !utf8.Valid(raw) {
		return string(model.Encode(memory))
	}
	return string(raw)
}

// ftsRewrite 는 FTS 표 하나를 다시 쓰는 데 필요한 것이다.
// refreshRow 는 색인 행을 새 본문에 맞춘다. 안 맞추면 검색이 파일에 없는 말을
// 계속 답하고, 다음 gc 가 같은 기억을 또 집는다.
func (m *mover) refreshRow(item Item, memory *model.Memory) error {
	docid := int64(0)
	if err := m.db.SQL().QueryRow("SELECT docid FROM memories WHERE id = ?", item.ID).Scan(&docid); err != nil {
		return err
	}
	// 본문은 색인에 안 담는다 (md 가 정본). 대신 재순위가 쓰는 낱말 수는
	// 접은 본문에 맞춰 고쳐 준다.
	if _, err := m.db.SQL().Exec(`UPDATE memories SET state = ?, body_hash = ?, n_body = ?
		WHERE docid = ?`, memory.State, hashOf([]byte(memory.Body)),
		len(strings.Fields(token.ForIndex(memory.Body))), docid); err != nil {
		return err
	}
	// FTS 표는 원문을 안 담는 꼴(content='')이라 열 하나만 고칠 수 없다 —
	// 지우고 다시 넣는다. 표가 셋(fts_ko·fts_en·**fts_norm**)이 되면서 여기서
	// 손으로 둘만 다시 넣으면 warm 접기 뒤 fts_norm 이 옛 본문을 계속 답한다.
	// 색인이 쓰는 것과 **같은 함수**를 쓴다 (결정 58).
	if err := m.db.RewriteFTS(docid, memory); err != nil {
		return err
	}
	return m.refreshFile(item, memory)
}

// refreshFile 은 증분 색인이 보는 자리를 맞춘다. 안 맞추면 다음 index 가 이
// 파일을 바뀐 것으로 보고 다시 읽는다 (틀리진 않지만 헛일이다).
func (m *mover) refreshFile(item Item, memory *model.Memory) error {
	abs, err := m.store.Abs(item.Path)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	hash := hashOf(model.Encode(memory))
	if _, err := m.db.SQL().Exec(`UPDATE files SET mtime = ?, size = ?, hash = ?, indexed_at = ?
		WHERE path = ?`, info.ModTime().UnixNano(), info.Size(), hash, m.now.Unix(), item.Path); err != nil {
		return err
	}
	_, err = m.db.SQL().Exec("UPDATE memories SET updated_at = ?, mtime = ?, size = ?, sha = ? WHERE id = ?",
		info.ModTime().Unix(), info.ModTime().UnixNano(), info.Size(), hash, item.ID)
	return err
}

// compress 는 본문을 규칙으로 줄인다 — 빈 줄 눌러 쓰기 · 바로 되풀이되는 줄
// 지우기 · 긴 코드 블록 접기. LLM 은 안 쓴다 (설계 9-4).
func compress(body, id string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	out := []string{}
	at := 0
	for at < len(lines) {
		line := lines[at]
		if isFence(line) {
			folded, next := foldCode(lines, at)
			out = append(out, folded...)
			at = next
			continue
		}
		if skipLine(out, line) {
			at++
			continue
		}
		out = append(out, line)
		at++
	}
	shrunk := strings.TrimRight(strings.Join(out, "\n"), "\n")
	if shrunk == strings.TrimRight(body, "\n") {
		return shrunk
	}
	return shrunk + "\n\n" + i18n.T(i18n.GCFoldNotice, id)
}

// skipLine 은 빈 줄이 이어지거나 바로 앞과 똑같은 줄인지 본다.
func skipLine(out []string, line string) bool {
	if len(out) == 0 {
		return strings.TrimSpace(line) == ""
	}
	last := out[len(out)-1]
	if strings.TrimSpace(line) == "" && strings.TrimSpace(last) == "" {
		return true
	}
	return line == last && strings.TrimSpace(line) != ""
}

// foldCode 는 코드 블록 하나를 접는다. 짧으면 그대로 둔다.
func foldCode(lines []string, at int) ([]string, int) {
	end := at + 1
	for end < len(lines) && !isFence(lines[end]) {
		end++
	}
	if end >= len(lines) {
		return lines[at:], len(lines)
	}
	inside := end - at - 1
	if inside < codeFoldLines {
		return lines[at : end+1], end + 1
	}
	return []string{lines[at], i18n.T(i18n.GCCodeFolded, inside), lines[end]}, end + 1
}

func isFence(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "```")
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
