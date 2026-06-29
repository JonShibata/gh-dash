package prview

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/cmpcontroller"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/markdown"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

type RenderedActivity struct {
	UpdatedAt      time.Time
	RenderedString string
	// ThreadId is non-empty when this activity is a review thread.
	// Used to record the thread's line offset in the final rendered
	// output so n/N can scroll the focused thread to the top.
	ThreadId string
}

func (m *Model) renderActivity() string {
	bodyStyle := lipgloss.NewStyle()

	var activities []RenderedActivity
	var comments []comment

	if !m.pr.Data.IsEnriched {
		return bodyStyle.Render("Loading...")
	}

	// Build a hidden-author lookup once. Empty when not configured, in
	// which case isHidden is a constant-false closure and the filter is a
	// no-op — no per-comment map allocation or lookup overhead.
	hidden := make(map[string]struct{}, len(m.ctx.Config.Defaults.HideAuthors))
	for _, login := range m.ctx.Config.Defaults.HideAuthors {
		hidden[login] = struct{}{}
	}
	isHidden := func(login string) bool {
		if len(hidden) == 0 {
			return false
		}
		_, ok := hidden[login]
		return ok
	}

	// Render review threads as grouped blocks (root + indented replies +
	// diff-hunk header) rather than as flat per-comment entries. Sorting
	// is by the *root* comment's UpdatedAt so a late reply doesn't reorder
	// the whole thread.
	focusedId := ""
	if t, ok := m.focusedThread(); ok {
		focusedId = t.Id
	}
	for _, thread := range m.pr.Data.Enriched.ReviewThreads.Nodes {
		visible := make([]data.ReviewComment, 0, len(thread.Comments.Nodes))
		for _, c := range thread.Comments.Nodes {
			if isHidden(c.Author.Login) {
				continue
			}
			visible = append(visible, c)
		}
		if len(visible) == 0 {
			continue
		}
		focused := focusedId != "" && thread.Id == focusedId
		rendered, err := m.renderReviewThread(thread.Path, thread.Line, thread.IsResolved, thread.IsOutdated, focused, visible)
		if err != nil {
			continue
		}
		activities = append(activities, RenderedActivity{
			UpdatedAt:      visible[0].UpdatedAt,
			RenderedString: rendered,
			ThreadId:       thread.Id,
		})
	}

	for _, c := range m.pr.Data.Enriched.Comments.Nodes {
		if isHidden(c.Author.Login) {
			continue
		}
		comments = append(comments, comment{
			Author:    c.Author.Login,
			Body:      c.Body,
			UpdatedAt: c.UpdatedAt,
		})
	}

	for _, comment := range comments {
		renderedComment, err := m.renderComment(comment)
		if err != nil {
			continue
		}
		activities = append(activities, RenderedActivity{
			UpdatedAt:      comment.UpdatedAt,
			RenderedString: renderedComment,
		})
	}

	for _, review := range m.pr.Data.Primary.Reviews.Nodes {
		if isHidden(review.Author.Login) {
			continue
		}
		renderedReview, err := m.renderReview(review)
		if err != nil {
			continue
		}
		activities = append(activities, RenderedActivity{
			UpdatedAt:      review.UpdatedAt,
			RenderedString: renderedReview,
		})
	}

	sort.Slice(activities, func(i, j int) bool {
		return activities[i].UpdatedAt.Before(activities[j].UpdatedAt)
	})

	body := ""
	// Reset the offset maps every render IN PLACE. View() is a value
	// receiver; assigning a new map (`m.threadLineOffsets = map{}`)
	// would only update the local copy and the parent's offsets would
	// stay empty. Maps are reference types, so deletions persist.
	for k := range m.threadLineOffsets {
		delete(m.threadLineOffsets, k)
	}
	for k := range m.threadLineEnds {
		delete(m.threadLineEnds, k)
	}
	if len(activities) == 0 {
		body = renderEmptyState()
	} else {
		title := m.ctx.Styles.Common.MainTextStyle.MarginBottom(1).Underline(true).Render(
			fmt.Sprintf("%s  %d comments", constants.CommentsIcon, len(activities)))
		// Fold the thread action bar into `title` so the existing
		// `cum += lipgloss.Height(title)` below already accounts for its
		// height — the n/N scroll-follow line offsets stay correct
		// without touching the offset loop.
		if bar := m.renderThreadActionBar(); bar != "" {
			title = lipgloss.JoinVertical(lipgloss.Left, title, bar, "")
		}
		// Offsets must be in *viewport* coordinates: View() prepends
		// viewHeader() before the tab body, and YOffset() measures the
		// full output. Without this adjustment the cursor-follow logic
		// would compare a viewport offset (with header) against a
		// body-relative offset (without) and never match anything past
		// the first thread.
		cum := lipgloss.Height(m.viewHeader()) + lipgloss.Height(title)
		// In reply mode, pad above the focused thread block so its end
		// lands at (or below) the viewport's bottom edge. Without this,
		// when the focused thread is near the document top, the viewport
		// can't scroll high enough to put the input at the bottom (X
		// would have to be negative) and the input renders in the upper
		// half of the screen with blank space below.
		inReplyMode := m.editor.Mode() == cmpcontroller.ModeReplyReview
		focusedId := ""
		if inReplyMode {
			if t, ok := m.focusedThread(); ok {
				focusedId = t.Id
			}
		}
		var renderedActivities []string
		for _, activity := range activities {
			rendered := activity.RenderedString
			padBefore := 0
			if focusedId != "" && activity.ThreadId == focusedId && m.replyViewportHeight > 0 {
				naturalEnd := cum + lipgloss.Height(rendered)
				if naturalEnd < m.replyViewportHeight {
					padBefore = m.replyViewportHeight - naturalEnd
					rendered = strings.Repeat("\n", padBefore) + rendered
				}
			}
			cum += padBefore
			if activity.ThreadId != "" {
				m.threadLineOffsets[activity.ThreadId] = cum
			}
			renderedActivities = append(renderedActivities, rendered)
			cum += lipgloss.Height(activity.RenderedString)
			if activity.ThreadId != "" {
				m.threadLineEnds[activity.ThreadId] = cum
			}
		}
		body = lipgloss.JoinVertical(lipgloss.Left, renderedActivities...)
		body = lipgloss.JoinVertical(lipgloss.Left, title, body)
	}

	return bodyStyle.Render(body)
}

func renderEmptyState() string {
	return lipgloss.NewStyle().Italic(true).Render("No comments...")
}

// renderThreadActionBar renders a single, fixed-height line that names the
// currently-selected review thread (index/count, path:line, resolved
// state) and the actions available on it. It removes the ambiguity of
// "which thread does x/r/R act on?" by stating it explicitly above the
// conversation. Empty when there are no review threads.
func (m *Model) renderThreadActionBar() string {
	idx, count, ok := m.focusedThreadPosition()
	if !ok {
		return ""
	}
	t, ok := m.focusedThread()
	if !ok {
		return ""
	}

	state := "unresolved"
	if t.IsResolved {
		state = "resolved"
	}

	accent := lipgloss.NewStyle().Foreground(m.ctx.Theme.PrimaryText).Bold(true)
	faint := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText)

	// Identity only: which thread is selected and its state. The key
	// legend lives inline beneath the focused thread (renderThreadHints)
	// so it stays visible when the user has scrolled past this bar.
	bar := lipgloss.JoinHorizontal(
		lipgloss.Top,
		accent.Render(fmt.Sprintf("▶ %d/%d", idx+1, count)),
		faint.Render(fmt.Sprintf(" · %s:%d · %s", t.Path, t.Line, state)),
	)
	return lipgloss.NewStyle().Width(m.getIndentedContentWidth()).MaxHeight(1).Render(bar)
}

// renderThreadHints renders the always-visible key legend shown beneath
// the focused thread, so the available actions stay reachable without
// scrolling back to the top-of-tab action bar.
func (m *Model) renderThreadHints(resolved bool, width int) string {
	action := "x resolve"
	if resolved {
		action = "x unresolve"
	}
	// Sit the legend on the same active-bg band as the active header so
	// the two bracket the active comment. SecondaryText (not FaintText) so
	// it stays legible against the band.
	return lipgloss.NewStyle().
		Foreground(m.ctx.Theme.SecondaryText).
		Background(m.ctx.Theme.ActiveBackground).
		Width(width).
		MaxHeight(1).
		Render("  n/N prev/next · r reply · R reply+resolve · " + action)
}

type comment struct {
	Author    string
	UpdatedAt time.Time
	Body      string
	Path      *string
	Line      *int
}

func (m *Model) renderComment(
	comment comment,
) (string, error) {
	width := m.getIndentedContentWidth()
	authorAndTime := lipgloss.NewStyle().
		Width(width).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(m.ctx.Theme.FaintBorder).Render(
		lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.ctx.Styles.Common.MainTextStyle.Render(comment.Author),
			" ",
			lipgloss.NewStyle().
				Foreground(m.ctx.Theme.FaintText).
				Render(utils.TimeElapsed(comment.UpdatedAt)),
		))

	var header string
	if comment.Path != nil && comment.Line != nil {
		filePath := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Width(width).Render(
			fmt.Sprintf(
				"%s#l%d",
				*comment.Path,
				*comment.Line,
			),
		)
		header = lipgloss.JoinVertical(lipgloss.Left, authorAndTime, filePath, "")
	} else {
		header = authorAndTime
	}

	body := lineCleanupRegex.ReplaceAllString(comment.Body, "")
	body = m.injectHints(body)
	body, err := markdown.Render(width, body)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		body,
	), err
}

func (m *Model) renderReview(
	review data.Review,
) (string, error) {
	header := m.renderReviewHeader(review)
	body, err := markdown.Render(m.getIndentedContentWidth(), m.injectHints(review.Body))
	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		body,
	), err
}

func (m *Model) renderReviewHeader(review data.Review) string {
	return lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderReviewDecision(review.State),
		" ",
		m.ctx.Styles.Common.MainTextStyle.Render(review.Author.Login),
		" ",
		lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Render(
			"reviewed "+utils.TimeElapsed(review.UpdatedAt)),
	)
}

// renderThreadHeader builds the one-line location header shared by a
// thread's collapsed and expanded views, keeping the two consistent: the
// only thing that changes on focus/expand is the disclosure triangle and
// whether the body follows — the header itself stays put.
//
// State pills are LEFT-anchored. That fixes two things at once: they no
// longer teleport from the right edge (expanded) to the left (collapsed),
// and a long path can never push them off-screen — the path is the only
// element that gives way. Both resolved and outdated render in both
// states, so an outdated thread still reads as outdated while collapsed.
//
// The path:line is left-truncated to whatever width the pills (and, for
// the collapsed view, the caller-reserved count suffix) leave, dropping
// leading directories so the filename + line number — what you navigate
// by — always survive.
func (m *Model) renderThreadHeader(path string, line int, resolved, outdated, focused, expanded bool, width int) string {
	// The active (focused) thread sits on a full-width band painted with
	// Theme.ActiveBackground — a color dedicated to "active comment",
	// deliberately distinct from SelectedBackground (used for selected
	// line numbers / list rows) so the two don't read as the same thing.
	// Each text segment carries the background explicitly so a nested ANSI
	// reset can't punch a hole between the pills; the outer Width fill then
	// extends the band to the right edge.
	base := lipgloss.NewStyle()
	sep := " "
	if focused {
		base = base.Background(m.ctx.Theme.ActiveBackground)
		sep = base.Render(" ")
	}

	// State pills reuse the same themed badge treatment as the check
	// badges (checks.go): a semantic background + inverted text. Resolved
	// is the "pass" green; outdated is the "pending" warning amber.
	resolvedPill := lipgloss.NewStyle().
		Foreground(m.ctx.Theme.InvertedText).
		Background(m.ctx.Theme.SuccessText).
		Padding(0, 1)
	outdatedPill := lipgloss.NewStyle().
		Foreground(m.ctx.Theme.InvertedText).
		Background(m.ctx.Theme.WarningText).
		Padding(0, 1)

	var parts []string
	if resolved {
		parts = append(parts, resolvedPill.Render("✓ resolved"), sep)
	}
	if outdated {
		parts = append(parts, outdatedPill.Render("outdated"), sep)
	}
	pills := lipgloss.JoinHorizontal(lipgloss.Top, parts...)

	// Disclosure triangle encodes body state; focus is carried by the
	// gutter accent + bold path (+ the active band), so the marker is
	// free to mean expanded.
	marker := "▸ "
	if expanded {
		marker = "▾ "
	}
	headerStyle := base.Foreground(m.ctx.Theme.FaintText)
	if focused {
		headerStyle = base.Foreground(m.ctx.Theme.PrimaryText).Bold(true)
	}

	pathLine := fmt.Sprintf("%s:%d", path, line)
	budget := width - lipgloss.Width(pills) - lipgloss.Width(marker)
	if budget < 1 {
		budget = 1
	}
	if lipgloss.Width(pathLine) > budget {
		// Keep the tail (…dir/file.go:NN); drop leading directories. The
		// ellipsis takes one column, so leave room for it in the budget.
		pathLine = constants.Ellipsis + ansi.TruncateLeft(pathLine, lipgloss.Width(pathLine)-budget+1, "")
	}

	header := lipgloss.JoinHorizontal(lipgloss.Top, pills, headerStyle.Render(marker+pathLine))
	if focused {
		// Pad the band to the full width so the fill reaches the right edge.
		header = base.Width(width).MaxHeight(1).Render(header)
	}
	return header
}

// renderReviewThread renders one inline-review thread as a grouped block
// with a state-colored left gutter: a location header (path:line, plus
// [resolved]/[outdated] tags), the colorized diff hunk above the
// conversation, then the root comment followed by replies indented by a
// leading bar. Replies share a single header style so the visual
// grouping reads as one unit even when authors differ.
//
// The left gutter encodes state at a glance — focused (accent), outdated
// (warning), resolved (faint), or default — and makes the *selected*
// thread (the one x/r/R act on) unmistakable. Inner content is rendered
// 2 columns narrower so the gutter+padding leaves the outer width equal
// to a non-focused block; keeping outer width invariant is what lets the
// n/N scroll-follow line offsets stay stable across focus changes.
//
// Resolved threads collapse to a single summary line unless focused, so
// closed-out conversations stop eating vertical space. Focusing one (via
// n/N) expands it again so x can unresolve.
func (m *Model) renderReviewThread(
	path string,
	line int,
	resolved bool,
	outdated bool,
	focused bool,
	comments []data.ReviewComment,
) (string, error) {
	width := m.getIndentedContentWidth()
	innerWidth := width - 2 // left gutter border (1) + padding (1)
	if innerWidth < 1 {
		innerWidth = width
	}
	faint := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText)

	// Left gutter color encodes thread state; focused wins so the
	// selected thread reads as a brighter version of the same gutter.
	gutterColor := m.ctx.Theme.FaintBorder
	switch {
	case focused:
		gutterColor = m.ctx.Theme.PrimaryText
	case outdated:
		gutterColor = m.ctx.Theme.WarningText
	case resolved:
		gutterColor = m.ctx.Theme.FaintText
	}
	gutter := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(gutterColor).
		PaddingLeft(1)

	// Collapsed summary for resolved, non-focused threads. One line →
	// the caller's height/offset bookkeeping is unaffected. Shares the
	// header builder with the expanded view so the two stay consistent;
	// only the trailing comment count is collapsed-specific. The count's
	// width is reserved up front so the path truncates around it.
	if resolved && !focused {
		n := len(comments)
		noun := "comments"
		if n == 1 {
			noun = "comment"
		}
		countSuffix := fmt.Sprintf(" · %d %s", n, noun)
		header := m.renderThreadHeader(path, line, resolved, outdated, focused, false, innerWidth-lipgloss.Width(countSuffix))
		summary := lipgloss.JoinHorizontal(lipgloss.Top, header, faint.Render(countSuffix))
		return gutter.Render(summary), nil
	}

	header := m.renderThreadHeader(path, line, resolved, outdated, focused, true, innerWidth)

	// Diff hunk lifted from the *root* comment — every comment in the
	// thread carries the same hunk; render it once, colorized like a
	// real diff (background-filled rows), above the conversation.
	hunk := colorizeDiffHunk(comments[0].DiffHunk, innerWidth, &m.ctx.Theme)

	// Root comment uses the existing renderComment shape (with header
	// trimmed because we already drew the location above). Replies use a
	// lighter ├─ rule so the indentation reads as continuation.
	var blocks []string
	if hunk != "" {
		blocks = append(blocks, hunk)
	}
	for i, c := range comments {
		p := "├─ "
		if i == 0 {
			p = "└─ "
		}
		who := lipgloss.JoinHorizontal(lipgloss.Top,
			faint.Render(p),
			m.ctx.Styles.Common.MainTextStyle.Render(c.Author.Login),
			" ",
			faint.Render(utils.TimeElapsed(c.UpdatedAt)),
		)
		body := lineCleanupRegex.ReplaceAllString(c.Body, "")
		body = m.injectHints(body)
		rendered, err := markdown.Render(innerWidth, body)
		if err != nil {
			return "", err
		}
		blocks = append(blocks, who, rendered)
	}

	all := append([]string{header}, blocks...)
	// Inline key legend beneath the focused thread — always visible next
	// to the conversation x/r/R act on, even when scrolled past the
	// top-of-tab action bar. Suppressed while the reply editor is open
	// (the editor takes its place).
	if focused && m.EditorReplyView() == "" {
		all = append(all, m.renderThreadHints(resolved, innerWidth))
	}
	conv := gutter.Render(lipgloss.JoinVertical(lipgloss.Left, all...))

	// The inline reply input renders below the conversation when active.
	// It's kept OUTSIDE the gutter wrapper because the editor is styled
	// at the full sidebar width; wrapping it would overflow the gutter
	// padding and reflow.
	if focused {
		if reply := m.EditorReplyView(); reply != "" {
			return lipgloss.JoinVertical(lipgloss.Left, conv, reply), nil
		}
	}
	return conv, nil
}

func (m *Model) renderReviewDecision(decision string) string {
	switch decision {
	case "PENDING":
		return m.ctx.Styles.Common.WaitingGlyph
	case "COMMENTED":
		return lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Render("󰈈")
	case "APPROVED":
		return m.ctx.Styles.Common.SuccessGlyph
	case "CHANGES_REQUESTED":
		return m.ctx.Styles.Common.FailureGlyph
	}

	return ""
}
