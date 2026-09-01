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

// activityKind tags each row in the unified activity list so the detail
// pane knows how to render it and so x/r/R can tell whether the selected
// row is an actionable review thread.
type activityKind int

const (
	kindThread activityKind = iota
	kindComment
	kindReview
)

// activityItem is one row in the Activity tab's list pane, plus the fully
// rendered detail shown when it is selected. Both `row` and `detail` are
// pre-rendered at the current width so scrolling never re-renders and never
// changes any block's height (the old focus-reflow bug). Selection styling
// is applied later in renderActivityList, so `row` here is the base line
// without the cursor marker.
type activityItem struct {
	kind      activityKind
	updatedAt time.Time
	// threadId is the GraphQL thread node id, non-empty only for
	// kindThread rows. x/r/R act on it.
	threadId string
	resolved bool
	outdated bool
	row      string
	// detailHeader is the one-line bar pinned to the top of the detail
	// pane while the body scrolls: for a thread it carries the resolved /
	// outdated pills and path:line, so the thread's status stays visible no
	// matter how far you scroll. For comments/reviews it carries the author
	// and time.
	detailHeader string
	// detail is the scrollable body shown below the pinned header.
	detail string
}

// key returns a stable identity for an item, used to tell "the selection
// changed" (reset the detail scroll to top) apart from "the same item was
// re-rendered by a refresh" (keep the scroll where it was).
func (i activityItem) key() string {
	switch i.kind {
	case kindThread:
		return "t:" + i.threadId
	default:
		return string(rune('0'+int(i.kind))) + ":" + i.updatedAt.String()
	}
}

// buildActivityItems flattens every activity entry (review threads, PR
// conversation comments, review summaries) into one time-ordered list.
// Each item carries its one-line list row and its full detail render.
// Mirrors the old renderActivity aggregation, but produces stable,
// focus-independent output.
func (m *Model) buildActivityItems() []activityItem {
	if m.pr == nil || m.pr.Data == nil || !m.pr.Data.IsEnriched {
		return nil
	}
	width := m.getIndentedContentWidth()

	// Hidden-author lookup (empty = no-op filter).
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

	var items []activityItem

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
		body, err := m.renderThreadBody(thread.IsResolved, thread.IsOutdated, visible)
		if err != nil {
			continue
		}
		header := m.renderThreadHeader(thread.Path, thread.Line, thread.IsResolved, thread.IsOutdated, width)
		items = append(items, activityItem{
			kind:         kindThread,
			updatedAt:    visible[0].UpdatedAt,
			threadId:     thread.Id,
			resolved:     thread.IsResolved,
			outdated:     thread.IsOutdated,
			row:          m.renderThreadRow(thread.Path, thread.Line, thread.IsResolved, thread.IsOutdated, visible, width),
			detailHeader: m.pinBar(header, width),
			detail:       body,
		})
	}

	for _, c := range m.pr.Data.Enriched.Comments.Nodes {
		if isHidden(c.Author.Login) {
			continue
		}
		body, err := m.renderCommentBody(c.Body)
		if err != nil {
			continue
		}
		items = append(items, activityItem{
			kind:         kindComment,
			updatedAt:    c.UpdatedAt,
			row:          m.renderCommentRow(c.Author.Login, c.Body, c.UpdatedAt, width),
			detailHeader: m.pinBar(m.commentHeaderLine(c.Author.Login, c.UpdatedAt), width),
			detail:       body,
		})
	}

	for _, review := range m.pr.Data.Primary.Reviews.Nodes {
		if isHidden(review.Author.Login) {
			continue
		}
		body, err := m.renderReviewBody(review)
		if err != nil {
			continue
		}
		items = append(items, activityItem{
			kind:         kindReview,
			updatedAt:    review.UpdatedAt,
			row:          m.renderReviewRow(review, width),
			detailHeader: m.pinBar(m.renderReviewHeader(review), width),
			detail:       body,
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].updatedAt.Before(items[j].updatedAt)
	})
	return items
}

// SyncActivity rebuilds the activity items (when dirty), sizes the two
// panes to exactly fill the sidebar viewport, and loads content into them.
// It is a pointer method called from the parent's syncSidebar before
// View(), because View() is a value receiver and cannot persist the inner
// viewports' state.
func (m *Model) SyncActivity() {
	if !m.hasData() || !m.pr.Data.IsEnriched {
		return
	}
	if m.carousel.SelectedItem() != ActivityTab {
		return
	}
	rebuilt := false
	if m.activityDirty {
		m.activityItems = m.buildActivityItems()
		m.activityDirty = false
		rebuilt = true
	}
	if m.activityCursor >= len(m.activityItems) {
		m.activityCursor = max(0, len(m.activityItems)-1)
	}
	if m.activityCursor < 0 {
		m.activityCursor = 0
	}
	if m.viewportHeight <= 0 || len(m.activityItems) == 0 {
		return
	}

	sel, ok := m.selectedActivity()
	if !ok {
		m.activityDetail.SetContent("")
		m.loadedDetailKey = ""
		return
	}

	w := m.getIndentedContentWidth()
	hHeader := lipgloss.Height(m.viewHeader())
	hAhead := lipgloss.Height(m.renderActivityHeader(w))
	hPinned := lipgloss.Height(sel.detailHeader)
	const hDivider = 1
	// Budget shared by the list pane and the (scrollable) detail body,
	// after the fixed chrome: PR header, legend, divider, pinned status bar.
	avail := m.viewportHeight - hHeader - hAhead - hDivider - hPinned
	if avail < 2 {
		avail = 2
	}

	// List pane gets up to half the space, capped at 8 rows, at least 2.
	maxList := avail / 2
	if maxList > 8 {
		maxList = 8
	}
	if maxList < 2 {
		maxList = 2
	}
	listH := len(m.activityItems)
	if listH > maxList {
		listH = maxList
	}
	if listH < 1 {
		listH = 1
	}
	detailH := avail - listH
	if detailH < 1 {
		detailH = 1
	}

	m.activityList.SetWidth(w)
	m.activityList.SetHeight(listH)
	m.activityList.SetContent(m.renderActivityList(w))
	m.activityList.EnsureVisible(m.activityCursor, 0, 0)

	m.activityDetail.SetWidth(w)
	m.activityDetail.SetHeight(detailH)

	// In reply mode the inline editor rides at the bottom of the detail
	// pane; keep it pinned to the bottom and always refresh so it tracks
	// what the user types.
	if m.editor.Mode() == cmpcontroller.ModeReplyReview {
		m.activityDetail.SetContent(sel.detail + "\n" + m.EditorReplyView())
		m.activityDetail.GotoBottom()
		m.loadedDetailKey = "reply"
		return
	}

	key := sel.key()
	switch {
	case key != m.loadedDetailKey:
		// Selection changed (or first load / just left reply mode): show
		// the new item from the top.
		m.activityDetail.SetContent(sel.detail)
		m.activityDetail.GotoTop()
		m.loadedDetailKey = key
	case rebuilt:
		// Same item still selected but its data was refreshed (auto-tick,
		// enrichment, resolve). Update the body but keep the scroll where
		// the user left it, clamped to the new content length, so a refresh
		// doesn't yank the pane back to the top.
		off := m.activityDetail.YOffset()
		m.activityDetail.SetContent(sel.detail)
		maxOff := max(0, m.activityDetail.TotalLineCount()-detailH)
		if off > maxOff {
			off = maxOff
		}
		m.activityDetail.SetYOffset(off)
	}
	// else: same item, no rebuild — leave content and scroll untouched.
}

// viewActivity composes the Activity tab: the PR header, a fixed legend
// line, the pinned list pane, a divider, and the scrollable detail pane.
// Sized to exactly the sidebar viewport height so the outer viewport does
// not scroll (all scrolling lives in the two inner panes). Value receiver:
// it only reads the panes that SyncActivity populated.
func (m Model) viewActivity() string {
	pad := lipgloss.NewStyle().Padding(0, m.ctx.Styles.Sidebar.ContentPadding)

	if !m.pr.Data.IsEnriched {
		return lipgloss.JoinVertical(lipgloss.Left, m.viewHeader(), pad.Render("Loading..."))
	}

	w := m.getIndentedContentWidth()
	if len(m.activityItems) == 0 {
		body := lipgloss.JoinVertical(lipgloss.Left, m.renderActivityHeader(w), "", renderEmptyState())
		return lipgloss.JoinVertical(lipgloss.Left, m.viewHeader(), pad.Render(body))
	}

	divider := lipgloss.NewStyle().
		Foreground(m.ctx.Theme.FaintBorder).
		Render(strings.Repeat("─", max(1, w)))

	// The selected item's status bar (thread pills + path:line, or the
	// author/time for comments/reviews) is pinned above the scrolling body
	// so it stays visible no matter how far the detail is scrolled.
	pinned := ""
	if sel, ok := m.selectedActivity(); ok {
		pinned = sel.detailHeader
	}

	body := lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderActivityHeader(w),
		m.activityList.View(),
		divider,
		pinned,
		m.activityDetail.View(),
	)
	out := lipgloss.JoinVertical(lipgloss.Left, m.viewHeader(), pad.Render(body))
	if m.viewportHeight > 0 {
		// Insurance against an off-by-one that would make the outer
		// viewport scrollable and reintroduce a stray shift.
		out = lipgloss.NewStyle().MaxHeight(m.viewportHeight).Render(out)
	}
	return out
}

func renderEmptyState() string {
	return lipgloss.NewStyle().Italic(true).Render("No comments...")
}

// renderActivityHeader is the fixed legend line above the list pane. It
// shows the selected item's position and the always-available key legend.
// Fixed height and content, so it never reflows.
func (m *Model) renderActivityHeader(width int) string {
	accent := lipgloss.NewStyle().Foreground(m.ctx.Theme.PrimaryText).Bold(true)
	faint := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText)

	count := len(m.activityItems)
	pos := 0
	if count > 0 {
		pos = m.activityCursor + 1
	}
	legend := " · n/N select · j/k/g/G scroll · x resolve · r reply · R reply+resolve"
	line := lipgloss.JoinHorizontal(
		lipgloss.Top,
		accent.Render(fmt.Sprintf("%s %d/%d", constants.CommentsIcon, pos, count)),
		faint.Render(legend),
	)
	return lipgloss.NewStyle().Width(width).MaxHeight(1).Render(
		ansi.Truncate(line, width, constants.Ellipsis),
	)
}

// renderActivityList joins the item rows for the list viewport, applying
// the selection marker + emphasis to the cursor row. Every row is exactly
// one line, selected or not, so selection never changes layout.
func (m *Model) renderActivityList(width int) string {
	lines := make([]string, 0, len(m.activityItems))
	for i, item := range m.activityItems {
		lines = append(lines, m.styleActivityRow(item, i == m.activityCursor, width))
	}
	return strings.Join(lines, "\n")
}

// styleActivityRow prepends the cursor marker and applies emphasis. The
// selected row is bold/primary; resolved and outdated rows are dimmed. No
// background band is used, so a colored state glyph inside the row can't
// punch a hole in a fill (and the row height stays 1).
func (m *Model) styleActivityRow(item activityItem, selected bool, width int) string {
	marker := "  "
	base := lipgloss.NewStyle()
	switch {
	case selected:
		marker = "▸ "
		base = base.Bold(true).Foreground(m.ctx.Theme.PrimaryText)
	case item.resolved || item.outdated:
		base = base.Foreground(m.ctx.Theme.FaintText)
	}
	line := ansi.Truncate(marker+item.row, width, constants.Ellipsis)
	return base.MaxHeight(1).Render(line)
}

// stateGlyph is the compact leading glyph for a thread row.
func (m *Model) stateGlyph(resolved, outdated bool) string {
	switch {
	case resolved:
		return lipgloss.NewStyle().Foreground(m.ctx.Theme.SuccessText).Render("✓")
	case outdated:
		return lipgloss.NewStyle().Foreground(m.ctx.Theme.WarningText).Render("⚠")
	default:
		return lipgloss.NewStyle().Foreground(m.ctx.Theme.SecondaryText).Render("○")
	}
}

func (m *Model) renderThreadRow(path string, line int, resolved, outdated bool, comments []data.ReviewComment, width int) string {
	author := ""
	if len(comments) > 0 {
		author = comments[0].Author.Login
	}
	pathLine := fmt.Sprintf("%s:%d", path, line)
	faint := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText)
	row := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.stateGlyph(resolved, outdated),
		" ",
		pathLine,
		faint.Render(fmt.Sprintf("  @%s · %d", author, len(comments))),
	)
	return row
}

func (m *Model) renderCommentRow(author, body string, updated time.Time, width int) string {
	faint := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText)
	snippet := firstLine(body)
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Foreground(m.ctx.Theme.SecondaryText).Render(constants.CommentsIcon),
		" @",
		author,
		faint.Render(" · "+utils.TimeElapsed(updated)+" · "+snippet),
	)
}

func (m *Model) renderReviewRow(review data.Review, width int) string {
	faint := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText)
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.renderReviewDecision(review.State),
		" @",
		review.Author.Login,
		faint.Render(" reviewed · "+utils.TimeElapsed(review.UpdatedAt)),
	)
}

// firstLine returns the first non-empty, cleaned-up line of a body for use
// as a one-line list snippet.
func firstLine(body string) string {
	body = lineCleanupRegex.ReplaceAllString(body, "")
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			return l
		}
	}
	return ""
}

// pinBar formats a one-line status bar for the pinned top of the detail
// pane: filled to the pane width and clamped to a single line.
func (m *Model) pinBar(content string, width int) string {
	return lipgloss.NewStyle().Width(width).MaxHeight(1).Render(
		ansi.Truncate(content, width, constants.Ellipsis),
	)
}

// commentHeaderLine is the pinned bar for a PR conversation comment.
func (m *Model) commentHeaderLine(author string, updated time.Time) string {
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Foreground(m.ctx.Theme.SecondaryText).Render(constants.CommentsIcon),
		" ",
		m.ctx.Styles.Common.MainTextStyle.Render(author),
		lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Render(" · "+utils.TimeElapsed(updated)),
	)
}

// renderCommentBody is the scrollable body for a PR conversation comment.
func (m *Model) renderCommentBody(body string) (string, error) {
	body = lineCleanupRegex.ReplaceAllString(body, "")
	body = m.injectHints(body)
	return markdown.Render(m.getIndentedContentWidth(), body)
}

// renderReviewBody is the scrollable body for a review summary.
func (m *Model) renderReviewBody(review data.Review) (string, error) {
	return markdown.Render(m.getIndentedContentWidth(), m.injectHints(review.Body))
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

// renderThreadHeader builds the one-line location header shown at the top
// of a thread's detail: state pills (left-anchored) followed by the
// path:line. The path is left-truncated to whatever the pills leave, so the
// filename + line number (what you navigate by) always survive. Selection
// is indicated in the list pane, so this header carries no focus state and
// never changes height.
func (m *Model) renderThreadHeader(path string, line int, resolved, outdated bool, width int) string {
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
		parts = append(parts, resolvedPill.Render("✓ resolved"), " ")
	}
	if outdated {
		parts = append(parts, outdatedPill.Render("outdated"), " ")
	}
	pills := lipgloss.JoinHorizontal(lipgloss.Top, parts...)

	headerStyle := lipgloss.NewStyle().Foreground(m.ctx.Theme.PrimaryText).Bold(true)

	pathLine := fmt.Sprintf("%s:%d", path, line)
	budget := width - lipgloss.Width(pills)
	if budget < 1 {
		budget = 1
	}
	if lipgloss.Width(pathLine) > budget {
		pathLine = constants.Ellipsis + ansi.TruncateLeft(pathLine, lipgloss.Width(pathLine)-budget+1, "")
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, pills, headerStyle.Render(pathLine))
}

// renderThreadBody renders the scrollable part of a review thread's detail:
// a state-colored left gutter wrapping the colorized diff hunk followed by
// the root comment and its replies (indented by a leading bar). The
// resolved/outdated pills and location live in the pinned header
// (renderThreadHeader), not here, so they stay visible while this scrolls.
func (m *Model) renderThreadBody(
	resolved bool,
	outdated bool,
	comments []data.ReviewComment,
) (string, error) {
	width := m.getIndentedContentWidth()
	innerWidth := width - 2 // left gutter border (1) + padding (1)
	if innerWidth < 1 {
		innerWidth = width
	}
	faint := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText)

	// Left gutter color encodes thread state.
	gutterColor := m.ctx.Theme.FaintBorder
	switch {
	case outdated:
		gutterColor = m.ctx.Theme.WarningText
	case resolved:
		gutterColor = m.ctx.Theme.SuccessText
	}
	gutter := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(gutterColor).
		PaddingLeft(1)

	// Diff hunk lifted from the root comment (every comment in the thread
	// carries the same hunk); render it once, colorized, above the
	// conversation.
	hunk := colorizeDiffHunk(comments[0].DiffHunk, innerWidth, &m.ctx.Theme)

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

	return gutter.Render(lipgloss.JoinVertical(lipgloss.Left, blocks...)), nil
}

// renderReviewThread is the pinned header stacked on the scrollable body,
// i.e. the whole thread detail as one string. Retained for tests; the app
// renders the header (pinned) and body (scrollable) separately.
func (m *Model) renderReviewThread(
	path string,
	line int,
	resolved bool,
	outdated bool,
	comments []data.ReviewComment,
) (string, error) {
	body, err := m.renderThreadBody(resolved, outdated, comments)
	if err != nil {
		return "", err
	}
	header := m.renderThreadHeader(path, line, resolved, outdated, m.getIndentedContentWidth())
	return lipgloss.JoinVertical(lipgloss.Left, header, body), nil
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
