package prview

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
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

// With the bottom scroll-padding in place, the LAST review thread can be
// scrolled to the same top-anchor row as every other thread. This pins the
// fix for the old behavior where the viewport clamped YOffset at the
// document end (maxYOffset = totalLines - height), so the final threads
// landed progressively lower — "n/N jumps to top, then middle, then bottom".
func TestLastThreadReachesTopAnchor(t *testing.T) {
	markdown.InitializeMarkdownStyle(true)
	m := newTestModelForAction(t)
	m.width = 120

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

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	enriched := data.EnrichedPullRequestData{}
	for i := 0; i < 6; i++ {
		enriched.ReviewThreads.Nodes = append(enriched.ReviewThreads.Nodes,
			mkThread(
				"T"+string(rune('1'+i)),
				string(rune('a'+i))+"_thread.go",
				base.Add(time.Duration(i)*time.Hour),
			),
		)
	}
	m.pr.Data.Enriched = enriched
	m.pr.Data.IsEnriched = true

	threads := m.allThreads()
	require.NotEmpty(t, threads)
	m.threadCursorIdx = len(threads) - 1 // focus the bottom-most thread

	// A viewport shorter than the whole conversation, so the last thread
	// starts below maxYOffset unless we pad the bottom.
	const vpH = 12
	m.viewportHeight = vpH

	body := m.renderActivity()
	total := lipgloss.Height(m.viewHeader()) + lipgloss.Height(body)
	maxYOffset := total - vpH

	// n/N scrolls to the thread's start offset; with the bottom padding it
	// must be reachable (<= maxYOffset) so the header lands at the top row
	// rather than the viewport clamping it mid-screen.
	require.LessOrEqual(t, m.FocusedThreadLineOffset(), maxYOffset,
		"last thread's header must reach the top row (no clamp) thanks to bottom scroll-padding")

	// Precondition: without the padding the same target WOULD clamp — the
	// last thread's start really is past the unpadded body's maxYOffset.
	m.viewportHeight = 0 // disables both paddings in renderActivity
	unpadded := m.renderActivity()
	unpaddedMax := lipgloss.Height(m.viewHeader()) + lipgloss.Height(unpadded) - vpH
	require.Greater(t, m.FocusedThreadLineOffset(), unpaddedMax,
		"precondition: last thread would clamp without bottom scroll-padding")
}
