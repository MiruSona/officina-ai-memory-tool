package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func day(t *testing.T) time.Time {
	t.Helper()
	at, err := time.Parse(DayLayout, "2026-08-22")
	if err != nil {
		t.Fatal(err)
	}
	return at
}

// TestAppendRecordWritesOnePlainDayFile is design 2-3 #9: the new archive is
// text git can merge, one file per day, nothing compressed.
func TestAppendRecordWritesOnePlainDayFile(t *testing.T) {
	dir := t.TempDir()
	first, _ := mustJSON(t, map[string]string{"op": "gc", "id": "20260822-aaaa1111"})
	second, _ := mustJSON(t, map[string]string{"op": "gc", "id": "20260822-bbbb2222"})
	path, offset, err := AppendRecord(dir, day(t), first)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "2026-08-22"+PlainSuffix {
		t.Fatalf("the day file is named %q", filepath.Base(path))
	}
	if offset != 0 {
		t.Fatalf("the first line starts at %d", offset)
	}
	_, second_at, err := AppendRecord(dir, day(t), second)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ReadRecordAt(path, second_at)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != string(second) {
		t.Fatalf("read back %q", back)
	}
	lines, err := ReadArchiveFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("the day file holds %d lines", len(lines))
	}
}

// TestPlainReadDropsOnlyTheTornLine is #9b: an uncompressed file loses the half
// written last line and nothing else, and it is not an error.
func TestPlainReadDropsOnlyTheTornLine(t *testing.T) {
	dir := t.TempDir()
	first, _ := mustJSON(t, map[string]string{"op": "gc", "id": "20260822-aaaa1111"})
	path, _, err := AppendRecord(dir, day(t), first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appendLine(path, []byte(`{"op":"gc","id":"20260822-bb`)); err != nil {
		t.Fatal(err)
	}
	lines, err := ReadArchiveFile(path)
	if err != nil {
		t.Fatalf("a torn plain line must not be an error: %v", err)
	}
	if len(lines) != 1 || string(lines[0]) != string(first) {
		t.Fatalf("read %d lines: %q", len(lines), lines)
	}
}

// TestArchivePartsTellsDayAndMonthApart makes sure a folder holding both the
// old gzip months and the new day files never mixes the two up.
func TestArchivePartsTellsDayAndMonthApart(t *testing.T) {
	dir := t.TempDir()
	line, _ := mustJSON(t, map[string]string{"op": "gc", "id": "20260822-aaaa1111"})
	if _, _, err := AppendArchiveLines(dir, "2026-08", [][]byte{line}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2026-08.1"+ArchiveSuffix), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := AppendRecord(dir, day(t), line); err != nil {
		t.Fatal(err)
	}
	month := ArchiveParts(dir, "2026-08")
	if len(month) != 2 {
		t.Fatalf("the month has %d parts: %v", len(month), month)
	}
	for _, path := range month {
		if filepath.Ext(path) != ".gz" {
			t.Fatalf("a day file was counted as a month part: %s", path)
		}
	}
	if parts := ArchiveParts(dir, "2026-08-22"); len(parts) != 1 {
		t.Fatalf("the day has %d parts: %v", len(parts), parts)
	}
}
