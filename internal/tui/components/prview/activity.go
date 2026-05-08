package prview

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

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
		rendered, err := m.renderReviewThread(thread.Path, thread.Line, thread.IsResolved, focused, visible)
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

// renderReviewThread renders one inline-review thread as a grouped
// block: a faint location header (path:line, plus a [resolved] tag when
// applicable), the diff hunk above the conversation, then the root
// comment followed by replies indented by a leading bar. Replies share
// a single header style so the visual grouping reads as one unit even
// when authors differ.
func (m *Model) renderReviewThread(
	path string,
	line int,
	resolved bool,
	focused bool,
	comments []data.ReviewComment,
) (string, error) {
	width := m.getIndentedContentWidth()
	faint := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText)

	// Header: ▶ path:line  [resolved]   (▶ marks the focused thread)
	prefix := "╭─ "
	if focused {
		prefix = "▶ "
	}
	loc := fmt.Sprintf("%s%s:%d", prefix, path, line)
	if resolved {
		loc += "  [resolved]"
	}
	headerStyle := faint
	if focused {
		headerStyle = lipgloss.NewStyle().Foreground(m.ctx.Theme.PrimaryText).Bold(true)
	}
	header := headerStyle.Width(width).Render(loc)

	// Diff hunk lifted from the *root* comment — every comment in the
	// thread carries the same hunk; we render it once, faintly, as
	// quoted-style code context above the conversation.
	var hunk string
	if h := strings.TrimRight(comments[0].DiffHunk, "\n"); h != "" {
		var lines []string
		for _, ln := range strings.Split(h, "\n") {
			lines = append(lines, "│ "+ln)
		}
		hunk = faint.Render(strings.Join(lines, "\n"))
	}

	// Root comment uses the existing renderComment shape (with header
	// trimmed because we already drew the location above). Replies use a
	// lighter ├─ rule so the indentation reads as continuation.
	var blocks []string
	if hunk != "" {
		blocks = append(blocks, hunk)
	}
	for i, c := range comments {
		prefix := "├─ "
		if i == 0 {
			prefix = "└─ "
		}
		who := lipgloss.JoinHorizontal(lipgloss.Top,
			faint.Render(prefix),
			m.ctx.Styles.Common.MainTextStyle.Render(c.Author.Login),
			" ",
			faint.Render(utils.TimeElapsed(c.UpdatedAt)),
		)
		body := lineCleanupRegex.ReplaceAllString(c.Body, "")
		body = m.injectHints(body)
		rendered, err := markdown.Render(width, body)
		if err != nil {
			return "", err
		}
		blocks = append(blocks, who, rendered)
	}

	all := append([]string{header}, blocks...)
	if focused {
		// Reply input renders inline below the focused thread's
		// conversation when active — so the user can see the thread
		// they're typing a reply to. Outside reply mode, show the
		// action hint instead.
		if reply := m.EditorReplyView(); reply != "" {
			all = append(all, reply)
		} else {
			action := "x resolve"
			if resolved {
				action = "x unresolve"
			}
			hint := faint.Render("  n/N next/prev  r reply  " + action)
			all = append(all, hint)
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, all...), nil
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
