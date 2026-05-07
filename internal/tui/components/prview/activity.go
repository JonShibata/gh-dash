package prview

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/markdown"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

type RenderedActivity struct {
	UpdatedAt      time.Time
	RenderedString string
}

func (m *Model) renderActivity() string {
	width := m.getIndentedContentWidth()
	markdownRenderer := markdown.GetMarkdownRenderer(width)
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
		rendered, err := m.renderReviewThread(thread.Path, thread.Line, thread.IsResolved, visible, markdownRenderer)
		if err != nil {
			continue
		}
		activities = append(activities, RenderedActivity{
			UpdatedAt:      visible[0].UpdatedAt,
			RenderedString: rendered,
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
		renderedComment, err := m.renderComment(comment, markdownRenderer)
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
		renderedReview, err := m.renderReview(review, markdownRenderer)
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
	if len(activities) == 0 {
		body = renderEmptyState()
	} else {
		var renderedActivities []string
		for _, activity := range activities {
			renderedActivities = append(renderedActivities, activity.RenderedString)
		}
		title := m.ctx.Styles.Common.MainTextStyle.MarginBottom(1).Underline(true).Render(
			fmt.Sprintf("%s  %d comments", constants.CommentsIcon, len(activities)))
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
	markdownRenderer glamour.TermRenderer,
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
	body, err := markdownRenderer.Render(body)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		body,
	), err
}

func (m *Model) renderReview(
	review data.Review,
	markdownRenderer glamour.TermRenderer,
) (string, error) {
	header := m.renderReviewHeader(review)
	body, err := markdownRenderer.Render(review.Body)
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
	comments []data.ReviewComment,
	markdownRenderer glamour.TermRenderer,
) (string, error) {
	width := m.getIndentedContentWidth()
	faint := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText)

	// Header: ╭─ path:line  [resolved]
	loc := fmt.Sprintf("╭─ %s:%d", path, line)
	if resolved {
		loc += "  [resolved]"
	}
	header := faint.Width(width).Render(loc)

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
		rendered, err := markdownRenderer.Render(body)
		if err != nil {
			return "", err
		}
		blocks = append(blocks, who, rendered)
	}

	return lipgloss.JoinVertical(lipgloss.Left, append([]string{header}, blocks...)...), nil
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
