package prrow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/stacks"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/table"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
	"github.com/dlvhdr/gh-dash/v4/internal/unread"
)

func unreadStore(t *testing.T, events int) *unread.Store {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "o", "r")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := 0; i < events; i++ {
		b.WriteString("2026-11-20T10:00:00Z\tissue_comment\t")
		b.WriteString(strings.Repeat("1", i+1))
		b.WriteString("\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "5.events"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	s := unread.NewAt(root)
	s.Load()
	return s
}

func numCommentsCell(t *testing.T, store *unread.Store) string {
	t.Helper()
	primary := &data.PullRequestData{Number: 5}
	primary.Repository.NameWithOwner = "o/r"
	primary.Comments.TotalCount = 7
	pr := &PullRequest{
		Ctx:  &context.ProgramContext{Theme: *theme.DefaultTheme, Unread: store},
		Data: &Data{Primary: primary},
	}
	return ansi.Strip(pr.renderNumComments())
}

func TestRenderNumCommentsShowsUnread(t *testing.T) {
	if got := numCommentsCell(t, unreadStore(t, 3)); got != "●3" {
		t.Errorf("cell = %q, want %q", got, "●3")
	}
}

func TestRenderNumCommentsCapsUnread(t *testing.T) {
	if got := numCommentsCell(t, unreadStore(t, 120)); got != "●99+" {
		t.Errorf("cell = %q, want %q", got, "●99+")
	}
}

func TestRenderNumCommentsFallsBackToTotal(t *testing.T) {
	for name, store := range map[string]*unread.Store{
		"nil store":     nil,
		"empty store":   unread.NewAt(t.TempDir()),
		"all read":      readThroughEnd(t),
		"other PR only": otherPR(t),
	} {
		if got := numCommentsCell(t, store); got != "7" {
			t.Errorf("%s: cell = %q, want %q", name, got, "7")
		}
	}
}

func readThroughEnd(t *testing.T) *unread.Store {
	t.Helper()
	s := unreadStore(t, 2)
	if _, err := s.MarkRead("o/r", 5, mustTime("2026-11-20T10:00:00Z")); err != nil {
		t.Fatal(err)
	}
	return s
}

func otherPR(t *testing.T) *unread.Store {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "o", "r")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "6.events"), []byte("2026-11-20T10:00:00Z\tissue_comment\t1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := unread.NewAt(root)
	s.Load()
	return s
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func threadsPR(resolved ...bool) *PullRequest {
	primary := &data.PullRequestData{Number: 5}
	primary.Repository.NameWithOwner = "o/r"
	primary.Comments.TotalCount = 7
	for _, r := range resolved {
		primary.ReviewThreads.Nodes = append(primary.ReviewThreads.Nodes, data.ReviewThreadState{IsResolved: r})
	}
	primary.ReviewThreads.TotalCount = len(resolved)
	return &PullRequest{
		Ctx:  &context.ProgramContext{Theme: *theme.DefaultTheme},
		Data: &Data{Primary: primary},
	}
}

func TestUnresolvedThreads(t *testing.T) {
	if got := threadsPR(true, false, false).Data.UnresolvedThreads(); got != 2 {
		t.Errorf("list data: UnresolvedThreads = %d, want 2", got)
	}
	if got := threadsPR().Data.UnresolvedThreads(); got != 0 {
		t.Errorf("no threads: UnresolvedThreads = %d, want 0", got)
	}

	// Enriched data wins, so a thread resolved in the sidebar clears the flag
	// before the next list refresh.
	pr := threadsPR(false)
	pr.Data.IsEnriched = true
	pr.Data.Enriched.ReviewThreads.Nodes = []data.ReviewThread{{IsResolved: true}}
	if got := pr.Data.UnresolvedThreads(); got != 0 {
		t.Errorf("enriched, resolved: UnresolvedThreads = %d, want 0", got)
	}
}

func TestRenderNumCommentsRedWhenUnresolved(t *testing.T) {
	for name, tc := range map[string]struct {
		pr       *PullRequest
		wantText string
		wantRed  bool
	}{
		"unresolved":   {threadsPR(true, false), "9", true},
		"all resolved": {threadsPR(true, true), "9", false},
		"no threads":   {threadsPR(), "7", false},
	} {
		out := tc.pr.renderNumComments()
		if got := ansi.Strip(out); got != tc.wantText {
			t.Errorf("%s: text = %q, want %q", name, got, tc.wantText)
		}
		// SGR 48 sets a background colour.
		if hasBg := strings.Contains(out, "48;"); hasBg != tc.wantRed {
			t.Errorf("%s: has background = %v, want %v (%q)", name, hasBg, tc.wantRed, out)
		}
	}
}

func TestExtendedTitleShowsStackBadge(t *testing.T) {
	store := stacks.NewStore()
	store.Begin("o/r")
	store.Done("o/r", []stacks.Stack{{Number: 9, Open: true, PRs: []stacks.PR{{Number: 4}, {Number: 5}}}}, nil)

	grow := true
	pr := threadsPR()
	pr.Ctx.Stacks = store
	pr.Ctx.Config = &config.Config{}
	pr.Columns = []table.Column{{Grow: &grow, ComputedWidth: 100}}
	pr.Data.Primary.Title = "Add thing"

	got := ansi.Strip(pr.renderExtendedTitle(false))
	if !strings.Contains(got, "stack 2/2") {
		t.Errorf("stacked PR title line = %q, want a stack 2/2 badge", got)
	}

	pr.Data.Primary.Number = 99 // not in any stack
	if got := ansi.Strip(pr.renderExtendedTitle(false)); strings.Contains(got, "stack") {
		t.Errorf("unstacked PR title line = %q, want no badge", got)
	}
}
