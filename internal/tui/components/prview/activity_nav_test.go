package prview

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/markdown"
)

// The Activity tab renders review threads oldest-first, top to bottom
// (renderActivity sorts every entry by UpdatedAt ascending). The n/N
// thread cursor has to walk threads in that same order so "next" (n)
// moves DOWN the page and "previous" (N) moves UP. This regression test
// pins both halves together: allThreads() ordering AND the rendered line
// offsets the cursor scrolls to. It guards against the old bug where
// allThreads() reversed the array (newest-first) while the body rendered
// oldest-first, so n jumped up and N jumped down.
func TestThreadCursorDirectionFollowsRenderOrder(t *testing.T) {
	markdown.InitializeMarkdownStyle(true) // renderActivity renders comment bodies
	m := newTestModelForAction(t)
	m.width = 120 // getIndentedContentWidth needs a positive width to render

	mkThread := func(id, path string, updated time.Time) data.ReviewThread {
		return data.ReviewThread{
			Id:   id,
			Path: path,
			Line: 10,
			Comments: data.ReviewComments{Nodes: []data.ReviewComment{
				{
					Author:    struct{ Login string }{Login: "octocat"},
					Body:      "comment on " + path,
					UpdatedAt: updated,
				},
			}},
		}
	}

	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)

	// Distinct, sortable paths so we can locate each thread in the output.
	const (
		pathOldest = "aaa_oldest.go"
		pathMid    = "bbb_mid.go"
		pathNewest = "ccc_newest.go"
	)

	enriched := data.EnrichedPullRequestData{}
	// Append out of chronological order to prove ordering comes from
	// UpdatedAt, not from the GraphQL array position.
	enriched.ReviewThreads.Nodes = append(enriched.ReviewThreads.Nodes,
		mkThread("T3", pathNewest, t3),
		mkThread("T1", pathOldest, t1),
		mkThread("T2", pathMid, t2),
	)
	m.pr.Data.Enriched = enriched
	m.pr.Data.IsEnriched = true

	// allThreads() must be oldest-first (the rendered top-to-bottom order).
	threads := m.allThreads()
	require.Len(t, threads, 3)
	require.Equal(t, []string{"T1", "T2", "T3"},
		[]string{threads[0].Id, threads[1].Id, threads[2].Id},
		"allThreads() should be oldest-first to match the rendered layout")

	// The rendered body must place the threads in that same order.
	body := m.renderActivity()
	posOldest := strings.Index(body, pathOldest)
	posMid := strings.Index(body, pathMid)
	posNewest := strings.Index(body, pathNewest)
	require.NotEqual(t, -1, posOldest)
	require.NotEqual(t, -1, posMid)
	require.NotEqual(t, -1, posNewest)
	require.Less(t, posOldest, posMid, "oldest thread should render above mid")
	require.Less(t, posMid, posNewest, "mid thread should render above newest")

	// Default cursor is the topmost (oldest) thread — visible on entry.
	require.Equal(t, 0, m.threadCursorIdx)
	id, _, ok := m.FocusedThread()
	require.True(t, ok)
	require.Equal(t, "T1", id)

	// Walking the cursor with "next" (n / +1) must move DOWN the page:
	// the focused thread's line offset strictly increases each step.
	var offsets []int
	for i := 0; i < len(threads); i++ {
		m.renderActivity() // re-render so offsets reflect the current focus
		offsets = append(offsets, m.FocusedThreadLineOffset())
		m.MoveThreadCursor(1)
	}
	for i := 1; i < len(offsets); i++ {
		require.Greater(t, offsets[i], offsets[i-1],
			"pressing n (next review thread) should move the cursor DOWN the page")
	}
}
