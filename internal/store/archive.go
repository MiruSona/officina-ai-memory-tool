package store

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// The archive keeps one gzip file per month. Every append is its own gzip
// member, which readers join back together, so nothing is ever rewritten.
// A month whose tail was torn (a crash mid write) gets a numbered spillover
// file next to it -- 2026-08.1.jsonl.gz -- because appending after a torn
// member makes gzip refuse the file from that point on.
const ArchiveSuffix = ".jsonl.gz"

// PlainSuffix is what new archive files use: one uncompressed file per day.
// Plain text is what git can merge line by line, and a day file stops one
// month growing into a blob every commit rewrites (design 2-3 #9).
const PlainSuffix = ".jsonl"

// DayLayout is the stamp of a plain archive file: archive/2026-08-22.jsonl.
const DayLayout = "2006-01-02"

// maxArchiveParts stops a runaway loop if every spillover is torn too.
const maxArchiveParts = 1000

// MaxArchiveBytes caps how much one archive file may expand to, so a crafted
// gzip file cannot fill memory. A var so tests can lower it.
var MaxArchiveBytes int64 = 1024 << 20

// ErrArchiveTail says the last gzip member of a file is torn. Whatever was
// read before the tear still comes back with it: a torn tail must never hide
// the whole records in front of it.
var ErrArchiveTail = errors.New("archive tail is torn")

// ReadArchiveFile reads every line of one archive file across all appended
// gzip members. A torn tail returns the whole lines read so far together with
// ErrArchiveTail; a file past the size cap is a plain error with no lines.
func ReadArchiveFile(path string) ([][]byte, error) {
	if !strings.HasSuffix(path, ArchiveSuffix) {
		return readPlainFile(path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, ErrArchiveTail
	}
	defer reader.Close()
	limited := io.LimitedReader{R: reader, N: MaxArchiveBytes + 1}
	lines, scanErr := scanLines(&limited)
	if limited.N == 0 {
		return nil, errors.New(i18n.T(i18n.ArchiveTooBig, path))
	}
	if scanErr != nil {
		return dropTornLine(lines), ErrArchiveTail
	}
	return lines, nil
}

func scanLines(from io.Reader) ([][]byte, error) {
	lines := [][]byte{}
	scanner := bufio.NewScanner(from)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		lines = append(lines, append([]byte(nil), scanner.Bytes()...))
	}
	return lines, scanner.Err()
}

// dropTornLine throws away the half written last line a tear leaves behind.
func dropTornLine(lines [][]byte) [][]byte {
	if len(lines) > 0 && !json.Valid(lines[len(lines)-1]) {
		return lines[:len(lines)-1]
	}
	return lines
}

// tailKnown remembers the size a file had when it last read cleanly to its
// end, so a run that appends many records reads the month through only once.
// Nothing but our own append changes an archive file, so a size that still
// matches means the tail is still whole.
var tailKnown sync.Map

// ArchiveTailWhole says the file reads to its end, so a new gzip member can be
// appended without cutting off what is already there. A missing or empty file
// counts as whole. There is no size cap here: the bytes go straight to Discard,
// so a huge file costs time, not memory.
func ArchiveTailWhole(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return true
	}
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if info.Size() == 0 {
		return true
	}
	if size, ok := tailKnown.Load(path); ok && size.(int64) == info.Size() {
		return true
	}
	if !readsToEnd(path) {
		return false
	}
	tailKnown.Store(path, info.Size())
	return true
}

func readsToEnd(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return false
	}
	defer reader.Close()
	_, err = io.Copy(io.Discard, reader)
	return err == nil
}

// ArchiveStamp is the stamp a file name belongs to: "2026-08" for both
// "2026-08.jsonl.gz" and its spillover "2026-08.3.jsonl.gz", "2026-08-22" for
// the plain day file "2026-08-22.jsonl". Anything else gives "".
func ArchiveStamp(name string) string {
	base := filepath.Base(name)
	if !strings.HasSuffix(base, ArchiveSuffix) {
		if strings.HasSuffix(base, PlainSuffix) {
			return strings.TrimSuffix(base, PlainSuffix)
		}
		return ""
	}
	stamp := strings.TrimSuffix(base, ArchiveSuffix)
	if dot := strings.LastIndexByte(stamp, '.'); dot >= 0 {
		if _, err := strconv.Atoi(stamp[dot+1:]); err == nil {
			return stamp[:dot]
		}
	}
	return stamp
}

// ArchivePartPath is the file of one month and part number. Part 0 is the
// plain month file every repository already has.
func ArchivePartPath(dir, stamp string, part int) string {
	if part <= 0 {
		return filepath.Join(dir, stamp+ArchiveSuffix)
	}
	return filepath.Join(dir, stamp+"."+strconv.Itoa(part)+ArchiveSuffix)
}

func archivePartNumber(path string) int {
	stamp := strings.TrimSuffix(filepath.Base(path), ArchiveSuffix)
	dot := strings.LastIndexByte(stamp, '.')
	if dot < 0 {
		return 0
	}
	part, err := strconv.Atoi(stamp[dot+1:])
	if err != nil {
		return 0
	}
	return part
}

// ArchiveParts lists one month's files in write order: the plain file first,
// then the numbered spillovers a torn tail forced.
func ArchiveParts(dir, stamp string) []string {
	names, err := filepath.Glob(filepath.Join(dir, stamp+"*"))
	if err != nil {
		return nil
	}
	found := []string{}
	for _, name := range names {
		if ArchiveStamp(name) != stamp {
			continue
		}
		if info, err := os.Stat(name); err != nil || !info.Mode().IsRegular() {
			continue
		}
		found = append(found, name)
	}
	sort.Slice(found, func(a, b int) bool {
		return archivePartNumber(found[a]) < archivePartNumber(found[b])
	})
	return found
}

// ArchiveAppendPath picks the file the next append goes to: the month's own
// file while its tail is whole, otherwise the next free numbered one.
func ArchiveAppendPath(dir, stamp string) (string, error) {
	for part := 0; part < maxArchiveParts; part++ {
		path := ArchivePartPath(dir, stamp, part)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path, nil
		}
		if ArchiveTailWhole(path) {
			return path, nil
		}
	}
	return "", errors.New(i18n.T(i18n.ArchiveNoRoom, stamp))
}

// AppendArchiveLines adds the lines as one new gzip member. It returns the file
// it wrote to and the offset that member starts at, so the caller can read back
// just what it wrote instead of the whole month.
func AppendArchiveLines(dir, stamp string, lines [][]byte) (string, int64, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, err
	}
	path, err := ArchiveAppendPath(dir, stamp)
	if err != nil {
		return "", 0, err
	}
	at := int64(0)
	if info, err := os.Stat(path); err == nil {
		at = info.Size()
	}
	if err := withRetry(func() error { return writeArchiveMember(path, lines) }); err != nil {
		return "", 0, err
	}
	if info, err := os.Stat(path); err == nil {
		tailKnown.Store(path, info.Size())
	}
	return path, at, nil
}

// ReadArchiveMemberAt reads the lines of the gzip member that starts at the
// given offset, which is how an append checks its own write landed.
func ReadArchiveMemberAt(path string, at int64) ([][]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if _, err := file.Seek(at, io.SeekStart); err != nil {
		return nil, err
	}
	reader, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	reader.Multistream(false)
	lines, err := scanLines(reader)
	if err != nil {
		return nil, err
	}
	return lines, nil
}

func writeArchiveMember(path string, lines [][]byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := gzip.NewWriter(file)
	for _, line := range lines {
		if _, err := writer.Write(bytes.TrimRight(line, "\r\n")); err != nil {
			return err
		}
		if _, err := writer.Write([]byte{'\n'}); err != nil {
			return err
		}
	}
	return writer.Close()
}

// --- plain day files (design 2-3 #9) ---

// dirReceived is where the raw add requests land, under the git ignored
// local/ folder that store/hits.go names.
const dirReceived = "received"

// ReceivedDir keeps the raw add requests as they were queued. They are a
// machine local receipt, not a memory, so they stay out of git.
func (s *Store) ReceivedDir() string { return filepath.Join(s.LocalDir(), dirReceived) }

// DayPath is the plain archive file of one day.
func DayPath(dir string, at time.Time) string {
	return filepath.Join(dir, at.Format(DayLayout)+PlainSuffix)
}

// AppendRecord adds one JSON line to the day file of dir, creating the folder
// when needed. It returns the file it wrote to and the offset the line starts
// at, so the caller can read back exactly what it wrote. Plain files need no
// tail check and no spillover: a torn write can only hurt its own last line,
// and readers drop that line (design 2-3 #9b).
func AppendRecord(dir string, at time.Time, record []byte) (string, int64, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, err
	}
	path := DayPath(dir, at)
	offset := int64(0)
	if info, err := os.Stat(path); err == nil {
		offset = info.Size()
	}
	line := append(bytes.TrimRight(record, "\r\n"), '\n')
	if err := withRetry(func() error {
		at, err := appendLine(path, line)
		offset = at
		return err
	}); err != nil {
		return "", 0, err
	}
	return path, offset, nil
}

// appendLine adds one line and says where it starts. A file whose last line was
// torn by a crash gets its newline back first, so the half written line stays a
// line of its own for the reader to drop instead of swallowing this record too.
func appendLine(path string, line []byte) (int64, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	at := info.Size()
	if at > 0 && !endsWithNewline(file, at) {
		if _, err := file.Write([]byte{'\n'}); err != nil {
			return 0, err
		}
		at++
	}
	if _, err := file.Write(line); err != nil {
		return 0, err
	}
	return at, nil
}

func endsWithNewline(file *os.File, size int64) bool {
	last := make([]byte, 1)
	if _, err := file.ReadAt(last, size-1); err != nil {
		return false
	}
	return last[0] == '\n'
}

// ReadRecordAt reads the one line that starts at the given offset, which is how
// an append checks its own write landed.
func ReadRecordAt(path string, offset int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(io.LimitReader(file, MaxArchiveBytes))
	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil, err
	}
	return bytes.TrimRight(line, "\r\n"), nil
}

// readPlainFile reads every whole line of an uncompressed archive file. A line
// torn by a crash is the last one and is simply dropped: unlike gzip, the
// records in front of it are not affected at all.
func readPlainFile(path string) ([][]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	whole := info.Size() == 0 || endsWithNewline(file, info.Size())
	limited := io.LimitedReader{R: file, N: MaxArchiveBytes + 1}
	lines, scanErr := scanLines(&limited)
	if limited.N == 0 {
		return nil, errors.New(i18n.T(i18n.ArchiveTooBig, path))
	}
	// A file that does not end in a newline was cut off mid line by a crash.
	// That last line is the only thing lost, and it is not an error.
	if whole && scanErr == nil {
		return lines, nil
	}
	return dropTornLine(lines), nil
}

// ReadReceived returns every line of one day's received file, which is where
// mem add's own request lands once it has been promoted.
func (s *Store) ReadReceived(at time.Time) ([]string, error) {
	read, err := ReadArchiveFile(DayPath(s.ReceivedDir(), at))
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(read))
	for _, line := range read {
		lines = append(lines, string(line))
	}
	return lines, nil
}

// ArchiveFiles lists every archive file of one folder, whatever its format:
// the old gzip months first (they were written first) and the plain day files
// after them. A folder that is not there gives nothing.
func ArchiveFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	found := []string{}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || ArchiveStamp(entry.Name()) == "" {
			continue
		}
		found = append(found, filepath.Join(dir, entry.Name()))
	}
	sort.Slice(found, func(a, b int) bool {
		first := strings.HasSuffix(found[a], ArchiveSuffix)
		second := strings.HasSuffix(found[b], ArchiveSuffix)
		if first != second {
			return first
		}
		return found[a] < found[b]
	})
	return found
}

// ArchiveRecord is one line of the git tracked archive. gc (op "gc") and amend
// (op "amend") write the very same keys, because one reader reads both: a line
// with different key names is a memory nobody can find again (설계검토 #9·#14c).
type ArchiveRecord struct {
	Op         string `json:"op"`
	ID         string `json:"id"`
	Path       string `json:"path"`
	ArchivedAt string `json:"archived_at"`
	Markdown   string `json:"markdown"`
}

// ArchivedMemory 는 아카이브에 남은 그 기억의 마지막 판을 되살린다. 본문을
// 갈아 끼우기 전 판이 여기 말고는 없다 (mem show --from-archive).
func (s *Store) ArchivedMemory(id string) (*model.Memory, error) {
	newest := ""
	when := ""
	for _, path := range ArchiveFiles(s.ArchiveDir()) {
		lines, err := ReadArchiveFile(path)
		if err != nil && len(lines) == 0 {
			continue
		}
		for _, line := range lines {
			record := ArchiveRecord{}
			if err := json.Unmarshal(line, &record); err != nil || record.ID != id {
				continue
			}
			if record.Markdown == "" || record.ArchivedAt < when {
				continue
			}
			newest, when = record.Markdown, record.ArchivedAt
		}
	}
	if newest == "" {
		return nil, errors.New(i18n.T(i18n.ArchiveNoMemory, id))
	}
	return model.Parse([]byte(newest))
}

// AppendAmendRecord keeps the memory as it stands right now, so mem amend can
// replace the body and still let mem show --from-archive read the old one. It
// goes to the git tracked archive, not the local receipts: this is the only
// copy of what the memory used to say. It returns the file it landed in.
func (s *Store) AppendAmendRecord(id, path, markdown string, at time.Time) (string, error) {
	if err := s.writable(); err != nil {
		return "", err
	}
	line, err := json.Marshal(ArchiveRecord{Op: OpAmend, ID: id, Path: path,
		ArchivedAt: at.Format(time.RFC3339), Markdown: markdown})
	if err != nil {
		return "", err
	}
	file, _, err := AppendRecord(s.ArchiveDir(), at, line)
	return file, err
}
