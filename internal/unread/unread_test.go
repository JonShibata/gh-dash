package unread

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func writeEvents(t *testing.T, root, repo string, number int, lines ...string) {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(repo))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	name := filepath.Join(dir, strconv.Itoa(number)+eventsExt)
	if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ts(s string) time.Time {
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return v
}

func TestLoadCountsEventsAfterReadThrough(t *testing.T) {
	root := t.TempDir()
	writeEvents(t, root, "o/r", 7,
		"2026-11-20T10:00:00Z\tissue_comment\t1",
		"2026-11-20T11:00:00Z\treview_comment\t2",
		"2026-11-20T12:00:00Z\treview\t3",
	)
	writeEvents(t, root, "o/r", 8, "2026-11-20T10:00:00Z\tissue_comment\t9")

	s := NewAt(root)
	if !s.Load() {
		t.Fatal("first Load should report a change")
	}
	if got := s.Count("o/r", 7); got != 3 {
		t.Fatalf("Count(7) = %d, want 3", got)
	}
	if got := s.Total(); got != 4 {
		t.Fatalf("Total = %d, want 4", got)
	}
	if s.Load() {
		t.Fatal("second Load with no changes should report none")
	}

	if _, err := s.MarkRead("o/r", 7, ts("2026-11-20T11:00:00Z")); err != nil {
		t.Fatal(err)
	}
	if got := s.Count("o/r", 7); got != 1 {
		t.Fatalf("Count(7) after MarkRead = %d, want 1", got)
	}
	if got := s.Count("o/r", 8); got != 1 {
		t.Fatalf("Count(8) = %d, want 1", got)
	}
	if got := s.Total(); got != 2 {
		t.Fatalf("Total after MarkRead = %d, want 2", got)
	}

	// A fresh Store, like another gh-dash or the daemon's --count, sees it too.
	other := NewAt(root)
	other.Load()
	if got := other.Count("o/r", 7); got != 1 {
		t.Fatalf("other store Count(7) = %d, want 1", got)
	}
}

func TestMarkReadOnlyMovesForward(t *testing.T) {
	root := t.TempDir()
	writeEvents(t, root, "o/r", 1,
		"2026-11-20T10:00:00Z\tissue_comment\t1",
		"2026-11-20T12:00:00Z\tissue_comment\t2",
	)
	s := NewAt(root)
	s.Load()

	if _, err := s.MarkRead("o/r", 1, ts("2026-11-20T12:00:00Z")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkRead("o/r", 1, ts("2026-11-20T10:00:00Z")); err != nil {
		t.Fatal(err)
	}
	if got := readThrough(filepath.Join(root, "o", "r", "1"+readExt)); !got.Equal(ts("2026-11-20T12:00:00Z")) {
		t.Fatalf("read-through moved backwards: %v", got)
	}
	if got := s.Count("o/r", 1); got != 0 {
		t.Fatalf("Count = %d, want 0", got)
	}
	if changed, _ := s.MarkRead("o/r", 1, ts("2026-11-20T12:00:00Z")); changed {
		t.Fatal("repeat MarkRead should report no change")
	}

	// A new Store must not move it back either.
	s2 := NewAt(root)
	if _, err := s2.MarkRead("o/r", 1, ts("2026-11-20T11:00:00Z")); err != nil {
		t.Fatal(err)
	}
	if got := readThrough(filepath.Join(root, "o", "r", "1"+readExt)); !got.Equal(ts("2026-11-20T12:00:00Z")) {
		t.Fatalf("read-through moved backwards via second store: %v", got)
	}
}

func TestNewEventAfterReadStaysUnread(t *testing.T) {
	root := t.TempDir()
	writeEvents(t, root, "o/r", 1, "2026-11-20T10:00:00Z\tissue_comment\t1")
	s := NewAt(root)
	s.Load()
	if _, err := s.MarkRead("o/r", 1, ts("2026-11-20T10:00:00Z")); err != nil {
		t.Fatal(err)
	}

	writeEvents(t, root, "o/r", 1,
		"2026-11-20T10:00:00Z\tissue_comment\t1",
		"2026-11-20T10:05:00Z\tissue_comment\t2",
	)
	s.Load()
	if got := s.Count("o/r", 1); got != 1 {
		t.Fatalf("Count = %d, want 1", got)
	}
}

func TestDuplicateAndMalformedEvents(t *testing.T) {
	root := t.TempDir()
	writeEvents(t, root, "o/r", 1,
		"2026-11-20T10:00:00Z\tissue_comment\t1",
		"2026-11-20T10:00:00Z\tissue_comment\t1",
		"garbage",
		"not-a-time\tissue_comment\t5",
	)
	s := NewAt(root)
	s.Load()
	if got := s.Count("o/r", 1); got != 1 {
		t.Fatalf("Count = %d, want 1", got)
	}
}

func TestNilAndEmptyStore(t *testing.T) {
	var s *Store
	if s.Count("o/r", 1) != 0 || s.Total() != 0 || s.Load() {
		t.Fatal("nil store should report nothing")
	}
	if _, err := s.MarkRead("o/r", 1, time.Now()); err != nil {
		t.Fatal(err)
	}

	missing := NewAt(filepath.Join(t.TempDir(), "nope"))
	missing.Load()
	if missing.Total() != 0 {
		t.Fatal("missing root should report nothing")
	}
}
