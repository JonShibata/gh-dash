package prview

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/markdown"
)

func init() {
	// renderReviewThread/renderComment call markdown.Render, which needs
	// the package-global markdown style initialized (done at app startup
	// in production). Idempotent, so safe to call from tests.
	markdown.InitializeMarkdownStyle(true)
}

// thread builds a minimal review thread with a single root comment so it
// survives buildActivityItems (which drops zero-comment threads and sorts
// by the root comment's UpdatedAt).
func thread(id string, dbID int, resolved bool, updated time.Time) data.ReviewThread {
	return data.ReviewThread{
		Id:         id,
		IsResolved: resolved,
		Path:       "file.go",
		Line:       1,
		Comments: data.ReviewComments{
			Nodes: []data.ReviewComment{{DatabaseId: dbID, UpdatedAt: updated}},
		},
	}
}

func modelWithThreads(t *testing.T, threads ...data.ReviewThread) Model {
	m := newTestModelForAction(t)
	m.pr.Data.Enriched = data.EnrichedPullRequestData{
		ReviewThreads: data.ReviewThreadsWithComments{Nodes: threads},
	}
	m.pr.Data.IsEnriched = true
	return m
}

func TestSetThreadResolvedOptimisticFlipsSourceInPlace(t *testing.T) {
	now := time.Now()
	m := modelWithThreads(t,
		thread("t1", 11, false, now.Add(-2*time.Minute)),
		thread("t2", 22, false, now.Add(-1*time.Minute)),
	)

	m.SetThreadResolvedOptimistic("t1", true)

	// Assert against the SOURCE slice (allThreads() returns a copy, so a
	// mutation that hit the copy would not show here).
	require.True(t, m.pr.Data.Enriched.ReviewThreads.Nodes[0].IsResolved)
	require.False(t, m.pr.Data.Enriched.ReviewThreads.Nodes[1].IsResolved)

	// Unknown id is a no-op.
	m.SetThreadResolvedOptimistic("nope", true)
	require.False(t, m.pr.Data.Enriched.ReviewThreads.Nodes[1].IsResolved)

	// Unresolve flips back.
	m.SetThreadResolvedOptimistic("t1", false)
	require.False(t, m.pr.Data.Enriched.ReviewThreads.Nodes[0].IsResolved)
}

// Smoke test the full Activity view with a selected resolved thread and an
// unresolved one, exercising the pane sizing, list rows, and detail render
// without panicking. The detail of the selected thread is always fully
// expanded (no focus-based collapse anymore).
func TestViewActivitySmoke(t *testing.T) {
	now := time.Now()
	m := modelWithThreads(t,
		thread("t1", 11, false, now.Add(-2*time.Minute)),
		thread("t2", 22, true, now.Add(-1*time.Minute)),
	)
	buildActivity(t, &m)
	m.MoveThreadCursor(1) // select the resolved thread
	m.SyncActivity()

	out := m.viewActivity()
	require.NotEmpty(t, out)
	visible := stripANSI(out)
	require.Contains(t, visible, "file.go:1") // header / location in a row
	require.Contains(t, visible, "resolve")   // legend line
}

// The selected item's status (resolved pill + location) is pinned in the
// detail header, and the scrollable body carries the conversation. Resolved
// threads no longer collapse based on focus, so scrolling can't balloon
// them.
func TestResolvedThreadDetailIsExpanded(t *testing.T) {
	now := time.Now()
	m := modelWithThreads(t, thread("t1", 11, true, now))
	buildActivity(t, &m)

	require.Len(t, m.activityItems, 1)
	// Status pill + location live in the pinned header.
	header := stripANSI(m.activityItems[0].detailHeader)
	require.Contains(t, header, "✓ resolved")
	require.Contains(t, header, "file.go:1")
	// The scrollable body carries the conversation (author bar at least).
	require.NotEmpty(t, stripANSI(m.activityItems[0].detail))
}

// The detail header shows the outdated pill for an outdated thread.
func TestOutdatedPillVisibleInDetail(t *testing.T) {
	m := modelWithThreads(t)
	m.SetWidth(80)
	rendered, err := m.renderReviewThread("file.go", 1, true /*resolved*/, true /*outdated*/, []data.ReviewComment{{
		Author: struct{ Login string }{Login: "octocat"}, UpdatedAt: time.Now(),
	}})
	require.NoError(t, err)
	visible := stripANSI(rendered)
	require.Contains(t, visible, "outdated")
	require.Contains(t, visible, "✓ resolved")
}

// A very long path in the detail header is left-truncated (…/tail kept) so
// the state pills are never pushed off-screen and the filename + line
// number survive.
func TestLongPathTruncatesButKeepsPills(t *testing.T) {
	m := modelWithThreads(t)
	m.SetWidth(60)
	longPath := "internal/tui/components/prview/very/deep/nested/activity.go"

	header := stripANSI(m.renderThreadHeader(longPath, 401, true /*resolved*/, true /*outdated*/, 50))
	require.Contains(t, header, "✓ resolved")
	require.Contains(t, header, "outdated")
	require.Contains(t, header, constants.Ellipsis)
	require.Contains(t, header, "activity.go:401")
	require.NotContains(t, header, "internal/tui")
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, l := range splitLines(s) {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	out = append(out, cur)
	return out
}

// The R combo must stash BOTH the reply target (root comment DatabaseId)
// and the thread id to resolve; plain reply (r) must leave the resolve id
// empty so Update doesn't resolve on a normal reply.
func TestReplyAndResolveStashesBothTargets(t *testing.T) {
	now := time.Now()
	m := modelWithThreads(t,
		thread("t1", 11, false, now.Add(-2*time.Minute)),
		thread("t2", 22, false, now.Add(-1*time.Minute)),
	)
	buildActivity(t, &m)
	m.MoveThreadCursor(1) // select t2

	m.SetIsReplyingAndResolving(true)
	require.Equal(t, 22, m.replyTargetCommentId)
	require.Equal(t, "t2", m.replyThenResolveThreadId)

	// Closing clears both.
	m.SetIsReplyingAndResolving(false)
	require.Equal(t, 0, m.replyTargetCommentId)
	require.Equal(t, "", m.replyThenResolveThreadId)

	// Plain reply leaves the resolve id empty.
	m.SetIsReplyingToReview(true)
	require.Equal(t, 22, m.replyTargetCommentId)
	require.Equal(t, "", m.replyThenResolveThreadId)
}
