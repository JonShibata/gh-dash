package prview

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/markdown"
)

func init() {
	// renderReviewThread/renderComment call markdown.Render, which needs
	// the package-global markdown style initialized (done at app startup
	// in production). Idempotent, so safe to call from tests.
	markdown.InitializeMarkdownStyle(true)
}

// thread builds a minimal review thread with a single root comment so it
// survives allThreads() (which drops zero-comment threads and sorts by the
// root comment's UpdatedAt).
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

func TestFocusedThreadPosition(t *testing.T) {
	now := time.Now()

	// No threads → not ok.
	m0 := newTestModelForAction(t)
	_, _, ok := m0.focusedThreadPosition()
	require.False(t, ok)

	m := modelWithThreads(t,
		thread("t1", 11, false, now.Add(-2*time.Minute)),
		thread("t2", 22, false, now.Add(-1*time.Minute)),
	)
	idx, count, ok := m.focusedThreadPosition()
	require.True(t, ok)
	require.Equal(t, 0, idx)
	require.Equal(t, 2, count)

	m.MoveThreadCursor(1)
	idx, count, _ = m.focusedThreadPosition()
	require.Equal(t, 1, idx)
	require.Equal(t, 2, count)

	// Clamps past the end.
	m.MoveThreadCursor(5)
	idx, _, _ = m.focusedThreadPosition()
	require.Equal(t, 1, idx)
}

// Smoke test the full activity render with a focused resolved thread (it
// must expand) and an unresolved one, exercising the gutter/width math,
// collapse logic, and the action bar without panicking.
func TestRenderActivitySmoke(t *testing.T) {
	now := time.Now()
	m := modelWithThreads(t,
		thread("t1", 11, false, now.Add(-2*time.Minute)),
		thread("t2", 22, true, now.Add(-1*time.Minute)),
	)
	m.SetWidth(80)
	m.MoveThreadCursor(1) // focus the resolved thread → it should expand

	out := m.renderActivity()
	visible := stripANSI(out)

	require.NotEmpty(t, out)
	require.Contains(t, visible, "file.go:1")   // header / location
	require.Contains(t, visible, "reply")       // action bar legend
	require.Contains(t, visible, "x unresolve") // resolved+focused state
	require.Contains(t, visible, "2/2")         // action bar position
}

// A resolved, NON-focused thread collapses to a single summary line.
func TestResolvedThreadCollapsesWhenNotFocused(t *testing.T) {
	now := time.Now()
	m := modelWithThreads(t,
		thread("t1", 11, true, now.Add(-2*time.Minute)),  // resolved, not focused
		thread("t2", 22, false, now.Add(-1*time.Minute)), // focused (cursor 0 → t1; move to t2)
	)
	m.SetWidth(80)
	m.MoveThreadCursor(1) // focus t2 so t1 stays collapsed

	rendered, err := m.renderReviewThread("file.go", 1, true /*resolved*/, false /*outdated*/, false /*focused*/, m.allThreads()[0].Comments.Nodes)
	require.NoError(t, err)
	// Collapsed form is one line with the ✓ resolved pill + location.
	require.Equal(t, 1, len(splitNonEmpty(stripANSI(rendered))))
	visible := stripANSI(rendered)
	require.Contains(t, visible, "✓ resolved")
	require.Contains(t, visible, "file.go:1")
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
	m.MoveThreadCursor(1) // focus t2

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
