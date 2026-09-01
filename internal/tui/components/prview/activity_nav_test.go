package prview

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/markdown"
)

// buildActivity puts the model on the Activity tab, gives it a width and a
// viewport height, and runs SyncActivity so activityItems (and the two
// panes) are populated the way syncSidebar would populate them in the app.
func buildActivity(t *testing.T, m *Model) {
	t.Helper()
	markdown.InitializeMarkdownStyle(true) // detail bodies render markdown
	m.GoToActivityTab()
	m.SetWidth(120)
	m.SetViewportHeight(40)
	m.SyncActivity()
}

// The activity list is one unified, oldest-first timeline of review
// threads, PR comments, and reviews. n (next) moves DOWN the list, so the
// items must be ordered by UpdatedAt ascending regardless of how the
// GraphQL arrays came back. This pins that ordering.
func TestActivityListOrderMatchesTimeline(t *testing.T) {
	m := newTestModelForAction(t)

	mkThread := func(id, path string, updated time.Time) data.ReviewThread {
		return data.ReviewThread{
			Id:   id,
			Path: path,
			Line: 10,
			Comments: data.ReviewComments{Nodes: []data.ReviewComment{{
				Author:    struct{ Login string }{Login: "octocat"},
				Body:      "comment on " + path,
				UpdatedAt: updated,
			}}},
		}
	}

	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)

	enriched := data.EnrichedPullRequestData{}
	// Append out of chronological order to prove ordering comes from time.
	enriched.ReviewThreads.Nodes = append(enriched.ReviewThreads.Nodes,
		mkThread("T3", "ccc_newest.go", t3),
		mkThread("T1", "aaa_oldest.go", t1),
		mkThread("T2", "bbb_mid.go", t2),
	)
	m.pr.Data.Enriched = enriched
	m.pr.Data.IsEnriched = true

	buildActivity(t, &m)

	require.Len(t, m.activityItems, 3)
	require.Equal(t, []string{"T1", "T2", "T3"},
		[]string{m.activityItems[0].threadId, m.activityItems[1].threadId, m.activityItems[2].threadId},
		"activity items should be oldest-first to match the list layout")

	// Default cursor is the topmost (oldest) item.
	require.Equal(t, 0, m.activityCursor)
	id, _, ok := m.FocusedThread()
	require.True(t, ok)
	require.Equal(t, "T1", id)

	// n (next) moves DOWN the list to the newer thread.
	m.MoveThreadCursor(1)
	id, _, ok = m.FocusedThread()
	require.True(t, ok)
	require.Equal(t, "T2", id)
}

// The list mixes threads with plain PR comments. x/r/R only act on review
// threads, so FocusedThread must report a thread when a thread row is
// selected and ok=false when a comment row is selected.
func TestActivitySelectionMapsToThread(t *testing.T) {
	m := newTestModelForAction(t)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	enriched := data.EnrichedPullRequestData{}
	enriched.ReviewThreads.Nodes = append(enriched.ReviewThreads.Nodes,
		data.ReviewThread{
			Id:   "T1",
			Path: "file.go",
			Line: 1,
			Comments: data.ReviewComments{Nodes: []data.ReviewComment{{
				Author:    struct{ Login string }{Login: "octocat"},
				Body:      "thread comment",
				UpdatedAt: base,
			}}},
		},
	)
	// A plain PR conversation comment, newer so it sorts below the thread.
	enriched.Comments.Nodes = append(enriched.Comments.Nodes, data.Comment{
		Author:    struct{ Login string }{Login: "octocat"},
		Body:      "just a comment",
		UpdatedAt: base.Add(time.Hour),
	})
	m.pr.Data.Enriched = enriched
	m.pr.Data.IsEnriched = true

	buildActivity(t, &m)

	require.Len(t, m.activityItems, 2)
	require.Equal(t, kindThread, m.activityItems[0].kind)
	require.Equal(t, kindComment, m.activityItems[1].kind)

	// Thread row selected → FocusedThread reports the thread.
	m.activityCursor = 0
	id, _, ok := m.FocusedThread()
	require.True(t, ok)
	require.Equal(t, "T1", id)

	// Comment row selected → not a thread, so x/r/R no-op.
	m.activityCursor = 1
	_, _, ok = m.FocusedThread()
	require.False(t, ok)
}

// j/k scroll the detail pane, and that scroll must survive the syncSidebar
// cycle that runs on every keystroke. syncSidebar calls SetRow (same PR) +
// SetWidth + SetViewportHeight + SyncActivity; none of those may reset the
// detail scroll, or j/k would appear to do nothing.
func TestScrollDetailSurvivesSyncCycle(t *testing.T) {
	m := newTestModelForAction(t)

	body := strings.Repeat("a line in the comment body\n\n", 60) // tall enough to overflow
	enriched := data.EnrichedPullRequestData{}
	enriched.ReviewThreads.Nodes = append(enriched.ReviewThreads.Nodes, data.ReviewThread{
		Id: "T1", Path: "file.go", Line: 1,
		Comments: data.ReviewComments{Nodes: []data.ReviewComment{{
			Author: struct{ Login string }{Login: "octocat"}, Body: body, UpdatedAt: time.Now(),
		}}},
	})
	m.pr.Data.Enriched = enriched
	m.pr.Data.IsEnriched = true

	buildActivity(t, &m)
	require.Greater(t, m.activityDetail.TotalLineCount(), m.activityDetail.Height(),
		"detail must overflow for this test to be meaningful")

	m.ScrollDetail(5)
	scrolled := m.activityDetail.YOffset()
	require.Greater(t, scrolled, 0)

	// Simulate one syncSidebar pass for the same PR.
	m.SetRow(m.pr.Data)
	m.SetWidth(120)
	m.SetViewportHeight(40)
	m.SyncActivity()

	require.Equal(t, scrolled, m.activityDetail.YOffset(),
		"a same-PR sync cycle must not reset the detail scroll")
}

// A refresh (auto-tick / enrichment) rebuilds the activity list. When the
// same item is still selected, the detail scroll must be preserved, not
// yanked back to the top. This pins that behavior.
func TestScrollDetailSurvivesRefresh(t *testing.T) {
	m := newTestModelForAction(t)

	body := strings.Repeat("a line in the comment body\n\n", 60)
	enriched := data.EnrichedPullRequestData{}
	enriched.ReviewThreads.Nodes = append(enriched.ReviewThreads.Nodes, data.ReviewThread{
		Id: "T1", Path: "file.go", Line: 1,
		Comments: data.ReviewComments{Nodes: []data.ReviewComment{{
			Author: struct{ Login string }{Login: "octocat"}, Body: body, UpdatedAt: time.Now(),
		}}},
	})
	m.pr.Data.Enriched = enriched
	m.pr.Data.IsEnriched = true

	buildActivity(t, &m)
	m.ScrollDetail(5)
	scrolled := m.activityDetail.YOffset()
	require.Greater(t, scrolled, 0)

	// A refresh marks the list dirty (as SetEnrichedPR does) and re-syncs.
	m.activityDirty = true
	m.SyncActivity()

	require.Equal(t, scrolled, m.activityDetail.YOffset(),
		"a refresh with the same item selected must preserve the detail scroll")
}

// Every list row is exactly one line whether or not it is selected. This is
// the core of the fix: selection must never change a row's height, so
// scrolling can't reflow the list.
func TestActivityRowHeightStable(t *testing.T) {
	m := newTestModelForAction(t)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	enriched := data.EnrichedPullRequestData{}
	enriched.ReviewThreads.Nodes = append(enriched.ReviewThreads.Nodes,
		data.ReviewThread{
			Id: "T1", Path: "file.go", Line: 1, IsResolved: true,
			Comments: data.ReviewComments{Nodes: []data.ReviewComment{{
				Author: struct{ Login string }{Login: "octocat"}, Body: "x", UpdatedAt: base,
			}}},
		},
	)
	m.pr.Data.Enriched = enriched
	m.pr.Data.IsEnriched = true

	buildActivity(t, &m)
	require.Len(t, m.activityItems, 1)

	unselected := m.styleActivityRow(m.activityItems[0], false, 80)
	selected := m.styleActivityRow(m.activityItems[0], true, 80)
	require.Equal(t, 1, len(splitLines(stripANSI(unselected))))
	require.Equal(t, 1, len(splitLines(stripANSI(selected))))
}
