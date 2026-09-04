package model

import (
	"strings"
	"testing"
)

// #19 A broken front matter must say so in Korean; the library's line number is
// kept because it is the only useful part of the English.
func TestYamlErrorIsKorean(t *testing.T) {
	broken := "---\nid: [열린 대괄호\n---\n\n본문\n"
	_, err := Parse([]byte(broken))
	if err == nil {
		t.Fatal("a broken front matter must be an error")
	}
	if !strings.Contains(err.Error(), "머리말") {
		t.Fatalf("the message must be Korean: %s", err)
	}
	if !strings.Contains(err.Error(), "line") {
		t.Fatalf("the line number must survive: %s", err)
	}
}

// #4 The id of a promoted memory comes from the queue file, so promoting the
// same file twice lands on the same path.
func TestQueueIDIsDeterministic(t *testing.T) {
	first := QueueID("1787.1.1.json", "본문", "2026-08-22")
	if second := QueueID("1787.1.1.json", "본문", "2026-08-22"); second != first {
		t.Fatalf("the same queued file must give the same id: %s %s", first, second)
	}
	if !IsID(first) {
		t.Fatalf("the id must be well formed: %s", first)
	}
	if !strings.HasPrefix(first, "20260822-") {
		t.Fatalf("the date must lead the id: %s", first)
	}
	if other := QueueID("1787.1.2.json", "본문", "2026-08-22"); other == first {
		t.Fatal("another queued file must give another id")
	}
	if other := QueueID("1787.1.1.json", "다른 본문", "2026-08-22"); other == first {
		t.Fatal("another body must give another id")
	}
}

// #3 The one date check both the command line and Validate use.
func TestIsDate(t *testing.T) {
	if !IsDate("2026-08-22") {
		t.Fatal("a plain day must pass")
	}
	for _, bad := range []string{"", "내일", "2026-8-2", "2026-08-22T00:00", "언젠가"} {
		if IsDate(bad) {
			t.Fatalf("%q must not pass", bad)
		}
	}
}
