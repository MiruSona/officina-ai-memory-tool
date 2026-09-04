package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestArchiveReadIsForgivingOfATornTail is bug 2 at the source: one shared
// reader, and it hands back the whole lines that sit in front of the tear.
func TestArchiveReadIsForgivingOfATornTail(t *testing.T) {
	dir := t.TempDir()
	first, _ := mustJSON(t, map[string]string{"op": "gc", "id": "20260822-aaaa1111"})
	second, _ := mustJSON(t, map[string]string{"op": "gc", "id": "20260822-bbbb2222"})
	path, _, err := AppendArchiveLines(dir, "2026-08", [][]byte{first, second})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, info.Size()-6); err != nil {
		t.Fatal(err)
	}
	lines, err := ReadArchiveFile(path)
	if err == nil {
		t.Fatal("a torn tail must be reported")
	}
	if len(lines) == 0 {
		t.Fatal("the lines in front of the tear must still come back")
	}
	for _, line := range lines {
		if !json.Valid(line) {
			t.Fatalf("a half written line came back: %q", line)
		}
	}
}

// TestAppendMovesOnFromATornFile is bug 1 at the source.
func TestAppendMovesOnFromATornFile(t *testing.T) {
	dir := t.TempDir()
	line, _ := mustJSON(t, map[string]string{"op": "gc", "id": "20260822-aaaa1111"})
	first, _, err := AppendArchiveLines(dir, "2026-08", [][]byte{line})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(first, info.Size()-6); err != nil {
		t.Fatal(err)
	}
	tailKnown.Delete(first)

	second, at, err := AppendArchiveLines(dir, "2026-08", [][]byte{line})
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("the append went back into the torn file")
	}
	if filepath.Base(second) != "2026-08.1"+ArchiveSuffix {
		t.Fatalf("unexpected spillover name: %s", second)
	}
	back, err := ReadArchiveMemberAt(second, at)
	if err != nil || len(back) != 1 {
		t.Fatalf("the member we just wrote must read back: %d %v", len(back), err)
	}
	parts := ArchiveParts(dir, "2026-08")
	if len(parts) != 2 {
		t.Fatalf("both files belong to the month: %+v", parts)
	}
}

func TestArchiveStampReadsSpilloverNames(t *testing.T) {
	cases := map[string]string{
		"2026-08" + ArchiveSuffix:    "2026-08",
		"2026-08.7" + ArchiveSuffix:  "2026-08",
		"notes.txt":                  "",
		"2026-08.ab" + ArchiveSuffix: "2026-08.ab",
	}
	for name, want := range cases {
		if got := ArchiveStamp(name); got != want {
			t.Errorf("%s gave %q, wanted %q", name, got, want)
		}
	}
}

func mustJSON(t *testing.T, value any) ([]byte, error) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw, nil
}
