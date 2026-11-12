package stacks

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// Shaped like the real response for stack 65129 (four merged, one open).
const sample = `[{"number":65129,"base":{"ref":"master"},"open":true,"pull_requests":[
 {"number":64859,"state":"closed","draft":false,"merged_at":"2026-11-20T20:00:29Z","head":{"ref":"a"}},
 {"number":64913,"state":"closed","draft":false,"merged_at":"2026-11-20T20:00:29Z","head":{"ref":"b"}},
 {"number":64945,"state":"open","draft":false,"merged_at":null,"head":{"ref":"c"}}]},
 {"number":70000,"base":{"ref":"master"},"open":true,"pull_requests":[
 {"number":1,"state":"open","draft":true,"merged_at":null,"head":{"ref":"x"}},
 {"number":2,"state":"open","draft":false,"merged_at":null,"head":{"ref":"y"}},
 {"number":3,"state":"open","draft":false,"merged_at":null,"head":{"ref":"z"}}]},
 {"number":70001,"base":{"ref":"master"},"open":false,"pull_requests":[
 {"number":9,"state":"closed","draft":false,"merged_at":"2026-11-20T20:00:29Z","head":{"ref":"q"}}]}]`

func TestParseAndIndex(t *testing.T) {
	all, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || len(all[0].PRs) != 3 || !all[0].PRs[0].Merged || all[0].PRs[2].Merged {
		t.Fatalf("bad parse: %+v", all)
	}
	idx := Index(all)

	if _, ok := idx[9]; ok {
		t.Error("PR in a closed stack should not be indexed")
	}
	top := idx[64945]
	if top.Stack != 65129 || top.Position != 3 || top.Size != 3 || top.Merged != 2 || len(top.Below) != 0 {
		t.Errorf("top of partly merged stack = %+v, want nothing below", top)
	}
	if got := idx[3].Below; !reflect.DeepEqual(got, []int{1, 2}) {
		t.Errorf("Below of #3 = %v, want [1 2] (drafts count)", got)
	}
	if got := idx[1].Below; len(got) != 0 {
		t.Errorf("bottom PR Below = %v, want none", got)
	}
}

func TestParseSlurpedPages(t *testing.T) {
	all, err := Parse([]byte("[" + sample + "," + sample + "]"))
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 6 {
		t.Fatalf("got %d stacks from two pages, want 6", len(all))
	}
	if _, err := Parse([]byte(`{"message":"Not Found"}`)); err == nil {
		t.Error("an error object should not parse as stacks")
	}
}

func TestLabels(t *testing.T) {
	i := Info{Stack: 65129, Position: 5, Size: 5, Merged: 4}
	if got := i.Short(); got != "stack 5/5" {
		t.Errorf("Short = %q", got)
	}
	if got := i.Long(); got != "stack #65129 · 5/5 · 4 merged" {
		t.Errorf("Long = %q", got)
	}
	if got := (Info{}).MergePrompt(7); got != "" {
		t.Errorf("MergePrompt with nothing below = %q, want empty", got)
	}
	want := "#3 is in stack #70000. Merge #1, #2 and #3 together, all or nothing? (Y/n) "
	if got := (Info{Stack: 70000, Below: []int{1, 2}}).MergePrompt(3); got != want {
		t.Errorf("MergePrompt = %q, want %q", got, want)
	}
	if got := (Info{Stack: 7, Below: []int{1}}).MergePrompt(2); got != "#2 is in stack #7. Merge #1 and #2 together, all or nothing? (Y/n) " {
		t.Errorf("MergePrompt (two) = %q", got)
	}
}

func TestStoreFetchLifecycle(t *testing.T) {
	s := NewStore()
	clock := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	all, _ := Parse([]byte(sample))

	if !s.Begin("o/r") {
		t.Fatal("first Begin should fetch")
	}
	if s.Begin("o/r") {
		t.Fatal("second Begin while running should not fetch")
	}
	if !s.Done("o/r", all, nil) {
		t.Fatal("first Done should report a change")
	}
	if got := s.Below("o/r", 3); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("Below = %v", got)
	}
	if s.Begin("o/r") {
		t.Fatal("fresh cache should not refetch")
	}

	clock = clock.Add(refreshAfter + time.Second)
	if !s.Begin("o/r") {
		t.Fatal("stale cache should refetch")
	}
	if s.Done("o/r", all, nil) {
		t.Fatal("identical data should report no change")
	}

	s.Invalidate("o/r")
	if !s.Begin("o/r") {
		t.Fatal("Invalidate should allow an immediate refetch")
	}
	s.Done("o/r", nil, errors.New("boom"))
	if got := s.Below("o/r", 3); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("failed fetch must keep the old cache, got %v", got)
	}
	clock = clock.Add(time.Minute)
	if s.Begin("o/r") {
		t.Fatal("should back off after a failure")
	}
	clock = clock.Add(backoff)
	if !s.Begin("o/r") {
		t.Fatal("should retry after the backoff")
	}
}

func TestNilStore(t *testing.T) {
	var s *Store
	if _, ok := s.Info("o/r", 1); ok || s.Below("o/r", 1) != nil || s.Begin("o/r") || s.Done("o/r", nil, nil) {
		t.Fatal("nil store should report nothing")
	}
	s.Invalidate("o/r")
}
