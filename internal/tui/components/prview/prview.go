package prview

import (
	"fmt"
	"image/color"
	"os"
	"regexp"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	log "charm.land/log/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/carousel"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/cmp"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/cmpcontroller"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/inputbox"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prssection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tasks"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/markdown"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

var (
	htmlCommentRegex = regexp.MustCompile("(?U)<!--(.|[[:space:]])*-->")
	lineCleanupRegex = regexp.MustCompile(`((\n)+|^)([^\r\n]*\|[^\r\n]*(\n)?)+`)
	foldBodyHeight   = 8
)

type Model struct {
	ctx             *context.ProgramContext
	sectionId       int
	pr              *prrow.PullRequest
	width           int
	carousel        carousel.Model
	editor          cmpcontroller.Controller
	summaryViewMore bool
	// replyTargetCommentId is the REST databaseId of the root comment of
	// the thread the user is currently replying to. Set by
	// SetIsReplyingToReview when the editor opens; consumed by Update on
	// submit. Zero when no reply is in flight.
	replyTargetCommentId int
	// replyThenResolveThreadId is the GraphQL node id of the thread to
	// resolve after the reply posts, set by SetIsReplyingAndResolving (the
	// R combo). Empty for a plain reply (r). Consumed and cleared by Update
	// on submit alongside replyTargetCommentId.
	replyThenResolveThreadId string
	// activityCursor is the index of the selected row in the Activity tab's
	// unified list (activityItems), which is ordered oldest-first to match
	// the list pane's top-to-bottom layout. n/N move it; x/r/R act on it
	// when the selected row is a review thread. Reset to 0 on PR change,
	// clamped on move.
	activityCursor int
	// activityList is the top pane: a pinned, scrollable list of one-line
	// activity rows. activityDetail is the bottom pane: the full render of
	// the selected row. Both are populated by SyncActivity (a pointer
	// method) so their scroll state persists across the value-receiver
	// View(). Splitting the tab into these two viewports is what removes the
	// old reflow-on-scroll bug: list rows are fixed height and the detail
	// scrolls on its own.
	activityList   viewport.Model
	activityDetail viewport.Model
	// activityItems is the pre-rendered unified activity list, rebuilt only
	// when the data or width changes (activityDirty). Each item holds its
	// list row and its detail, so scrolling never re-renders.
	activityItems []activityItem
	// activityDirty marks activityItems as needing a rebuild (set on PR
	// data change, enrichment swap, width change, and theme change).
	activityDirty bool
	// loadedDetailKey identifies which item's detail is currently loaded in
	// activityDetail, so SyncActivity reloads (and resets scroll) only when
	// the selection actually changes. Empty forces a reload.
	loadedDetailKey string
	// viewportHeight is the current sidebar viewport content height (in
	// lines), refreshed by the parent on every syncSidebar. SyncActivity
	// uses it to size the list and detail panes so the tab exactly fills
	// the viewport (no outer scroll). Zero before the first sync.
	viewportHeight int
	// imageHints is the ordered list of (label, url) pairs for every
	// image embedded in the current PR's bodies. Rebuilt on PR change
	// and on every enrich payload swap so auto-refresh ticks pick up
	// new attachments.
	imageHints []imageHint
	// imageURLToHint is the lookup the body rewriter uses to inject
	// **[label]** markers next to image references. Same data as
	// imageHints, indexed by URL.
	imageURLToHint map[string]string
	// pendingImageHint is the first character of a 2-char hint while
	// we wait for the second. Cleared on completion, mismatch, or PR
	// change.
	pendingImageHint string
}

// Exported tab names so other packages can compare SelectedTab() without
// duplicating the literal strings (which include leading icon glyphs).
var (
	OverviewTab     = " Overview"
	ActivityTab     = " Activity"
	CommitsTab      = " Commits"
	ChecksTab       = " Checks"
	FilesChangedTab = " Files Changed"
)

var tabs = []string{OverviewTab, ActivityTab, CommitsTab, ChecksTab, FilesChangedTab}

func NewModel(ctx *context.ProgramContext) Model {
	c := carousel.New(
		carousel.WithItems(tabs),
		carousel.WithWidth(ctx.MainContentWidth),
	)

	ta := inputbox.DefaultTextArea(ctx)
	return Model{
		pr:       nil,
		carousel: c,
		editor:   cmpcontroller.New(ctx, inputbox.ModelOpts{TextArea: &ta}),
		// The two Activity-tab panes. Dimensions and content are set by
		// SyncActivity on every sidebar sync; they start empty.
		activityList:   viewport.New(viewport.WithWidth(0), viewport.WithHeight(0)),
		activityDetail: viewport.New(viewport.WithWidth(0), viewport.WithHeight(0)),
		activityDirty:  true,
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	cmd, handled := m.editor.Update(msg)

	if msg, ok := msg.(tea.KeyMsg); ok && msg.String() == "ctrl+d" {
		value := m.editor.Value()
		mode := m.editor.Mode()
		m.editor.Exit()
		if m.pr == nil {
			return m, nil
		}

		sid := tasks.SectionIdentifier{Id: m.sectionId, Type: prssection.SectionType}

		switch mode {
		case cmpcontroller.ModeComment:
			if len(strings.TrimSpace(value)) != 0 {
				return m, tasks.CommentOnPR(m.ctx, sid, m.pr.Data.Primary, value)
			}
			return m, nil

		case cmpcontroller.ModeApprove:
			comment := ""
			if len(strings.TrimSpace(value)) != 0 {
				comment = value
			}
			return m, tasks.ApprovePR(m.ctx, sid, m.pr.Data.Primary, comment)

		case cmpcontroller.ModeAssign:
			usernames := cmp.AllWords(value)
			if len(usernames) > 0 {
				return m, tasks.AssignPR(m.ctx, sid, m.pr.Data.Primary, usernames)
			}
			return m, nil

		case cmpcontroller.ModeUnassign:
			usernames := cmp.AllWords(value)
			if len(usernames) > 0 {
				return m, tasks.UnassignPR(m.ctx, sid, m.pr.Data.Primary, usernames)
			}
			return m, nil

		case cmpcontroller.ModeLabel:
			labels := cmp.CurrentLabels(value)
			if len(labels) > 0 || len(m.pr.Data.Primary.Labels.Nodes) > 0 {
				return m, m.label(labels)
			}
			return m, nil

		case cmpcontroller.ModeReplyReview:
			target := m.replyTargetCommentId
			resolveThreadId := m.replyThenResolveThreadId
			m.replyTargetCommentId = 0
			m.replyThenResolveThreadId = ""
			if target == 0 || len(strings.TrimSpace(value)) == 0 {
				return m, nil
			}
			replyCmd := tasks.ReplyToReviewComment(m.ctx, sid, m.pr.Data.Primary, target, value)
			if resolveThreadId == "" {
				return m, replyCmd
			}
			// R combo: post the reply AND resolve the thread, flipping
			// the resolved state optimistically so the sidebar updates
			// immediately. The two GitHub calls are independent (resolve
			// keys on the thread id, reply on the root comment id), so
			// order doesn't matter server-side.
			return m, tea.Batch(
				replyCmd,
				tasks.ResolveReviewThread(m.ctx, sid, m.pr.Data.Primary, resolveThreadId),
				tasks.EmitOptimisticThreadResolve(resolveThreadId, true),
			)

		case cmpcontroller.ModeRequestReview:
			usernames := cmp.AllWords(value)
			if len(usernames) > 0 {
				return m, tasks.RequestReviewers(m.ctx, sid, m.pr.Data.Primary, usernames)
			}
			return m, nil
		}
	}

	if handled {
		return m, cmd
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(keyMsg, keys.PRKeys.PrevSidebarTab):
			m.carousel.MoveLeft()
		case key.Matches(keyMsg, keys.PRKeys.NextSidebarTab):
			m.carousel.MoveRight()
		}
	}

	return m, cmd
}

func (m Model) View() string {
	if !m.hasData() {
		return ""
	}

	// The Activity tab owns its own two-pane layout (list + detail) and is
	// sized to fill the viewport itself, so it composes viewHeader on its
	// own rather than sharing the padded-body path below.
	if m.carousel.SelectedItem() == ActivityTab {
		return m.viewActivity()
	}

	body := strings.Builder{}
	switch m.carousel.SelectedItem() {
	case tabs[0]:
		body.WriteString(m.viewOverviewTab())
	case tabs[2]:
		body.WriteString(m.renderCommits())
	case tabs[3]:
		body.WriteString(m.renderChecksOverview())
		body.WriteString("\n\n")
		body.WriteString(m.renderChecks())
	case tabs[4]:
		body.WriteString(m.renderChangedFiles())
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		m.viewHeader(),
		lipgloss.NewStyle().Padding(0, m.ctx.Styles.Sidebar.ContentPadding).Render(body.String()),
	)
}

func (m *Model) viewHeader() string {
	header := strings.Builder{}

	header.WriteString(m.renderFullNameAndNumber())
	header.WriteString("\n")

	header.WriteString(m.renderTitle())
	header.WriteString("\n\n")
	header.WriteString(m.renderBranches())
	header.WriteString("\n\n")
	header.WriteString(m.renderAuthor())
	header.WriteString("\n\n")
	header.WriteString(lipgloss.NewStyle().Width(m.width).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(m.ctx.Theme.FaintBorder).
		Render(m.carousel.View()),
	)

	header.WriteString("\n")
	return header.String()
}

func (m *Model) viewOverviewTab() string {
	body := strings.Builder{}
	reviewers := m.renderRequestedReviewers()
	if reviewers != "" {
		body.WriteString(reviewers)
		body.WriteString("\n\n")
	}

	labels := m.renderLabels()
	if labels != "" {
		body.WriteString(labels)
		body.WriteString("\n\n")
	}

	body.WriteString(m.renderSummary())
	body.WriteString("\n\n")
	body.WriteString(
		m.ctx.Styles.Common.MainTextStyle.MarginBottom(1).Underline(true).Render(" Changes"),
	)
	body.WriteString("\n")
	body.WriteString(m.renderChangesOverview())
	body.WriteString("\n\n")
	body.WriteString(
		m.ctx.Styles.Common.MainTextStyle.MarginBottom(1).Underline(true).Render(" Checks"),
	)
	body.WriteString("\n")
	body.WriteString(m.renderChecksOverview())

	// Reply-review input is rendered inline on the Activity tab beneath
	// the focused thread (so the user sees the thread they're replying
	// to). All other input modes render here at the bottom of Overview.
	if mode := m.editor.Mode(); mode != cmpcontroller.ModeNone && mode != cmpcontroller.ModeReplyReview {
		body.WriteString(m.ctx.Styles.Sidebar.InputBox.Render(m.editor.View()))
	}

	return body.String()
}

// EditorReplyView returns the rendered reply-mode input box, or "" when
// the editor is not in reply mode. Called from the Activity tab to
// render the input inline below the focused thread.
func (m *Model) EditorReplyView() string {
	if m.editor.Mode() != cmpcontroller.ModeReplyReview {
		return ""
	}
	return m.ctx.Styles.Sidebar.InputBox.Render(m.editor.View())
}

func (m *Model) ViewCompletions() string {
	if !m.hasData() {
		return ""
	}

	return m.editor.ViewCompletions()
}

func (m *Model) InputBoxLineFromBottom() int {
	return m.editor.LineFromBottom()
}

func (m *Model) renderFullNameAndNumber() string {
	if !m.hasData() {
		return ""
	}

	return common.RenderPreviewHeader(
		m.ctx.Theme,
		m.width,
		fmt.Sprintf(
			"%s · #%d",
			m.pr.Data.Primary.GetRepoNameWithOwner(),
			m.pr.Data.Primary.GetNumber(),
		),
	)
}

func (m *Model) renderTitle() string {
	if !m.hasData() {
		return ""
	}

	return common.RenderPreviewTitle(
		m.ctx.Theme,
		m.ctx.Styles.Common,
		m.width,
		m.pr.Data.Primary.Title,
	)
}

func (m *Model) renderBranches() string {
	return lipgloss.JoinHorizontal(lipgloss.Left,
		" ",
		m.renderStatusPill(),
		" ",
		lipgloss.NewStyle().
			Foreground(m.ctx.Theme.SecondaryText).
			Render(m.pr.Data.Primary.BaseRefName+"  "+m.pr.Data.Primary.HeadRefName))
}

func (m *Model) renderStatusPill() string {
	var bgColor color.Color
	switch m.pr.Data.Primary.State {
	case "OPEN":
		if m.pr.Data.Primary.IsDraft {
			bgColor = m.ctx.Theme.FaintText.Dark
		} else {
			bgColor = m.ctx.Styles.Colors.OpenPR.Dark
		}
	case "CLOSED":
		bgColor = m.ctx.Styles.Colors.ClosedPR.Dark
	case "MERGED":
		bgColor = m.ctx.Styles.Colors.MergedPR.Dark
	}

	return m.ctx.Styles.PrView.PillStyle.
		BorderForeground(bgColor).
		Background(bgColor).
		Render(m.pr.RenderState())
}

func (m *Model) renderLabels() string {
	width := m.getIndentedContentWidth()
	labels := m.pr.Data.Primary.Labels.Nodes
	style := m.ctx.Styles.PrView.PillStyle
	if len(labels) == 0 {
		return ""
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.ctx.Styles.Common.MainTextStyle.Underline(true).Bold(true).Render(
			fmt.Sprintf("%s Labels", constants.LabelsIcon)),
		"",
		common.RenderLabels(labels, common.LabelOpts{
			Width:     width,
			PillStyle: style,
		}),
	)
}

type reviewerItem struct {
	text string
}

// reviewerSources returns the four datasets the Reviewers sidebar renders.
// The shallow reviewer fields (requested reviewers + one deduped review per
// author) now ride in the per-tick list query, so the section paints
// immediately from Primary, with no "Loading..." round-trip, for every PR
// including the first one opened at startup. Once the heavier per-PR
// enrichment lands it takes over: a larger page plus suggestedReviewers (a
// git-blame-based computation kept out of the per-tick list query). Mirrors
// changedFiles()'s Primary->Enriched fallback.
func (m *Model) reviewerSources() (reviewRequests []data.ReviewRequestNode, reviews, opinionatedReviews []data.Review, suggested []data.SuggestedReviewer) {
	if m.pr.Data.IsEnriched {
		e := m.pr.Data.Enriched
		return e.ReviewRequests.Nodes, e.LatestReviews.Nodes, e.LatestOpinionatedReviews.Nodes, e.SuggestedReviewers
	}
	if p := m.pr.Data.Primary; p != nil {
		// suggestedReviewers is enrichment-only; nil here is fine.
		return p.ReviewRequests.Nodes, p.LatestReviews.Nodes, p.LatestOpinionatedReviews.Nodes, nil
	}
	return nil, nil, nil, nil
}

func (m *Model) renderRequestedReviewers() string {
	// latestReviews is one node per author (so no reviewer is truncated out
	// of the fetch window by a chatty peer); latestOpinionatedReviews is the
	// latest APPROVED/CHANGES_REQUESTED per author. See reviewerSources.
	reviewRequests, reviews, opinionatedReviews, suggestedReviewers := m.reviewerSources()

	if len(reviewRequests) == 0 && len(reviews) == 0 && len(suggestedReviewers) == 0 {
		return ""
	}

	reviewStates := make(map[string]string)
	for _, review := range reviews {
		reviewStates[review.Author.Login] = review.State
	}
	// Opinionated reviews win: an APPROVED/CHANGES_REQUESTED must not be
	// masked by a later COMMENTED review from the same author.
	for _, review := range opinionatedReviews {
		reviewStates[review.Author.Login] = review.State
	}

	reviewerItems := make([]reviewerItem, 0)
	faintStyle := m.ctx.Styles.Common.FaintTextStyle
	reviewerStyle := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText)
	successStyle := lipgloss.NewStyle().Foreground(m.ctx.Theme.SuccessText)
	errorStyle := lipgloss.NewStyle().Foreground(m.ctx.Theme.ErrorText)

	shownReviewers := make(map[string]bool)

	for _, req := range reviewRequests {
		displayName := req.GetReviewerDisplayName()
		if displayName == "" {
			continue
		}
		shownReviewers[displayName] = true

		var reviewerStr string
		stateIcon := ""
		if state, hasReview := reviewStates[displayName]; hasReview && state == "COMMENTED" {
			stateIcon = m.ctx.Styles.Common.CommentGlyph
		} else {
			stateIcon = m.ctx.Styles.Common.WaitingDotGlyph
		}

		if req.IsTeam() {
			reviewerStr += reviewerStyle.Render(displayName)
		} else {
			reviewerStr += reviewerStyle.Render("@" + displayName)
		}

		if req.AsCodeOwner {
			reviewerStr = lipgloss.JoinHorizontal(lipgloss.Top,
				faintStyle.Render(constants.OwnerIcon), " ", reviewerStr)
		}
		reviewerStr = lipgloss.JoinHorizontal(lipgloss.Top, stateIcon, " ", reviewerStr)

		reviewerItems = append(reviewerItems, reviewerItem{text: reviewerStr})
	}

	// Iterate reviewStates in sorted login order. Go randomizes map
	// iteration, so without this the reviewers sourced from here (people
	// who reviewed but weren't explicitly requested) reshuffle on every
	// render — the section visibly re-orders on any repaint, e.g. pressing
	// left while already on the overview tab.
	reviewedLogins := make([]string, 0, len(reviewStates))
	for login := range reviewStates {
		reviewedLogins = append(reviewedLogins, login)
	}
	sort.Strings(reviewedLogins)

	for _, login := range reviewedLogins {
		state := reviewStates[login]
		if shownReviewers[login] {
			continue
		}
		// Only skip states that aren't a submitted review: "" (none) and
		// PENDING (an unsubmitted draft). Everything else is real
		// participation and must be listed — notably DISMISSED, a review
		// later dismissed (e.g. by a new push), whose author still reviewed
		// and often left comments. Filtering it out was why reviewers who
		// commented went missing.
		if state == "" || state == "PENDING" {
			continue
		}
		shownReviewers[login] = true

		var stateIcon string
		switch state {
		case "APPROVED":
			stateIcon = successStyle.Render(constants.ApprovedIcon)
		case "CHANGES_REQUESTED":
			stateIcon = errorStyle.Render(constants.ChangesRequestedIcon)
		default: // COMMENTED, DISMISSED, or any other submitted state
			stateIcon = m.ctx.Styles.Common.CommentGlyph
		}
		reviewerStr := stateIcon + " " + reviewerStyle.Render("@"+login)

		reviewerItems = append(reviewerItems, reviewerItem{text: reviewerStr})
	}

	// Show suggested reviewers (= code owners) who haven't been requested or reviewed yet
	for _, suggested := range suggestedReviewers {
		login := suggested.Reviewer.Login
		if shownReviewers[login] {
			continue
		}
		if suggested.IsAuthor {
			continue
		}
		shownReviewers[login] = true

		reviewerStr := lipgloss.JoinHorizontal(lipgloss.Top,
			faintStyle.Render(constants.OwnerIcon), " ",
			faintStyle.Render("@"+login),
		)

		reviewerItems = append(reviewerItems, reviewerItem{text: reviewerStr})
	}

	if len(reviewerItems) == 0 {
		return ""
	}

	width := m.getIndentedContentWidth()
	var rows []string
	var currentRow strings.Builder
	currentRowWidth := 0

	for i, item := range reviewerItems {
		itemWidth := lipgloss.Width(item.text)
		separator := ", "
		separatorWidth := lipgloss.Width(separator)

		// Check if adding this item would exceed the width
		needsSeparator := i < len(reviewerItems)-1
		totalItemWidth := itemWidth
		if needsSeparator {
			totalItemWidth += separatorWidth
		}

		if currentRowWidth > 0 && currentRowWidth+totalItemWidth > width {
			// Start a new row
			rows = append(rows, currentRow.String())
			currentRow.Reset()
			currentRowWidth = 0
		}

		currentRow.WriteString(item.text)
		currentRowWidth += itemWidth

		if needsSeparator {
			currentRow.WriteString(separator)
			currentRowWidth += separatorWidth
		}
	}

	// Add the last row
	if currentRow.Len() > 0 {
		rows = append(rows, currentRow.String())
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.ctx.Styles.Common.MainTextStyle.Underline(true).Bold(true).Render(
			fmt.Sprintf("%s Reviewers", constants.CodeReviewIcon)),
		"",
		strings.Join(rows, "\n"),
	)
}

func (m *Model) renderAuthor() string {
	authorAssociation := m.pr.Data.Primary.AuthorAssociation
	if authorAssociation == "" {
		authorAssociation = "unknown role"
	}
	time := lipgloss.NewStyle().Render(utils.TimeElapsed(m.pr.Data.Primary.CreatedAt))
	return lipgloss.JoinHorizontal(lipgloss.Top,
		" by ",
		lipgloss.NewStyle().Foreground(m.ctx.Theme.PrimaryText).Render(
			lipgloss.NewStyle().Bold(true).Render("@"+m.pr.Data.Primary.Author.Login)),
		lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Render(
			lipgloss.JoinHorizontal(lipgloss.Top, " ⋅ ", time, " ago", " ⋅ ")),
		lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Render(
			lipgloss.JoinHorizontal(lipgloss.Top, data.GetAuthorRoleIcon(m.pr.Data.Primary.AuthorAssociation,
				m.ctx.Theme),
				" ", lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Render(strings.ToLower(authorAssociation))),
		),
	)
}

func (m *Model) renderSummary() string {
	width := m.getIndentedContentWidth()
	// Strip HTML comments from body and cleanup body.
	body := htmlCommentRegex.ReplaceAllString(m.pr.Data.Primary.Body, "")
	body = lineCleanupRegex.ReplaceAllString(body, "")
	body = m.injectHints(body)

	desc := m.ctx.Styles.Common.MainTextStyle.Bold(true).Underline(true).Render(" Summary")
	title := lipgloss.JoinVertical(
		lipgloss.Left,
		desc,
		"",
	)
	sbody := lipgloss.NewStyle().Width(m.getIndentedContentWidth())
	body = strings.TrimSpace(body)
	if body == "" {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			title,
			sbody.Italic(true).Foreground(m.ctx.Theme.FaintText).Render("No description provided."),
		)
	}

	rendered, err := markdown.Render(width, body)
	if err != nil {
		return ""
	}

	// DEBUG: when GHD_DUMP_RENDER=1, write the post-markdown.Render
	// output (with the bg-padded code-block lines as built by
	// padCodeBlockLines) to /tmp/gh-dash-render-summary.dump on every
	// render, with ANSI escapes shown literally so we can inspect
	// what bytes actually leave the renderer.
	if os.Getenv("GHD_DUMP_RENDER") == "1" {
		dump := strings.Builder{}
		dump.WriteString(fmt.Sprintf("=== width=%d  prNumber=%d ===\n", width, m.pr.Data.GetNumber()))
		for i, line := range strings.Split(rendered, "\n") {
			visW := lipgloss.Width(line)
			bg := strings.Contains(line, markdown.CodeBgMarker)
			literal := strings.ReplaceAll(line, "\x1b", "\\x1b")
			dump.WriteString(fmt.Sprintf("%3d w=%d bg=%v : %s\n", i, visW, bg, literal))
		}
		_ = os.WriteFile("/tmp/gh-dash-render-summary.dump", []byte(dump.String()), 0o644)
	}

	bodyHeight := lipgloss.Height(rendered)
	if !m.summaryViewMore && bodyHeight > foldBodyHeight {
		rendered = lipgloss.NewStyle().MaxHeight(foldBodyHeight).Render(rendered)
		rendered = lipgloss.JoinVertical(lipgloss.Left,
			rendered,
			"",
			lipgloss.PlaceHorizontal(m.getIndentedContentWidth(), lipgloss.Center,
				lipgloss.JoinHorizontal(
					lipgloss.Top,
					lipgloss.NewStyle().Bold(true).Italic(true).Render("Press "),
					lipgloss.NewStyle().
						Background(m.ctx.Theme.SelectedBackground).
						Foreground(m.ctx.Theme.PrimaryText).
						Render("e"),
					lipgloss.NewStyle().Bold(true).Italic(true).Render(" to read more..."),
				),
			),
		)
	}

	return lipgloss.JoinVertical(lipgloss.Left, title,
		lipgloss.NewStyle().
			Width(width).
			MaxWidth(width).
			Align(lipgloss.Left).
			Render(rendered),
	)
}

func (m *Model) SetSectionId(id int) {
	m.sectionId = id
}

func (m *Model) SetRow(d *prrow.Data) {
	prevNumber := 0
	if m.pr != nil && m.pr.Data != nil {
		prevNumber = m.pr.Data.GetNumber()
	}
	if d == nil {
		m.pr = nil
	} else {
		m.pr = &prrow.PullRequest{Ctx: m.ctx, Data: d}
	}
	newNumber := 0
	if d != nil {
		newNumber = d.GetNumber()
	}
	if newNumber != prevNumber {
		// Switched to a different PR: reset the selection and rebuild the
		// activity list. Do NOT mark dirty on same-PR SetRow calls:
		// syncSidebar calls SetRow on every keystroke, and a rebuild there
		// would reset the detail pane to the top and wipe j/k scrolling.
		// Content changes within a PR are marked dirty by their own setters
		// (SetEnrichedPR, SetThreadResolvedOptimistic, SetWidth,
		// UpdateProgramContext).
		m.activityCursor = 0
		m.loadedDetailKey = ""
		m.activityDirty = true
	}
	m.RebuildImageHints()
}

type EnrichedPrMsg struct {
	Id   int
	Type string
	Data data.EnrichedPullRequestData
	Err  error
}

func (m *Model) EnrichCurrRow() tea.Cmd {
	if m == nil || m.pr == nil || m.pr.Data.IsEnriched {
		log.Info("EnrichCurrRow skip", "nilModel", m == nil, "nilPr", m == nil || m.pr == nil, "alreadyEnriched", m != nil && m.pr != nil && m.pr.Data.IsEnriched)
		return nil
	}
	url := m.pr.Data.Primary.Url
	log.Info("EnrichCurrRow firing", "url", url, "sectionId", m.sectionId)
	return func() tea.Msg {
		d, err := data.FetchPullRequest(url)
		return EnrichedPrMsg{
			Id:   m.sectionId,
			Type: prssection.SectionType,
			Data: d,
			Err:  err,
		}
	}
}

// RefreshEnrichedCurrRow re-fetches the enriched payload (checks,
// reviews, comments, etc.) for the currently-viewed PR even when it's
// already enriched. Used by the auto-tick so the sidebar stays current
// while the user watches a build. Crucially, IsEnriched is NOT flipped
// to false beforehand — that would blank the sidebar to "Loading..."
// every tick. Instead the EnrichedPrMsg handler swaps the payload in
// place when the fetch returns; the user sees old data → new data with
// no "Loading..." intermediate state.
func (m *Model) RefreshEnrichedCurrRow() tea.Cmd {
	if m == nil || m.pr == nil {
		return nil
	}
	url := m.pr.Data.Primary.Url
	return func() tea.Msg {
		d, err := data.FetchPullRequest(url)
		return EnrichedPrMsg{
			Id:   m.sectionId,
			Type: prssection.SectionType,
			Data: d,
			Err:  err,
		}
	}
}

func (m *Model) SetWidth(width int) {
	if width != m.width {
		// Width drives the pre-rendered activity rows/details, so a change
		// invalidates them.
		m.activityDirty = true
	}
	m.width = width
	m.carousel.SetWidth(width) // header carousel is NOT padded — keep full width
	// The editor renders inside the body's content padding (both sides) AND
	// inside the bordered InputBox, so its textarea must be narrowed by both
	// or its right edge is clipped by the sidebar's MaxWidth (invisible text).
	editorWidth := width -
		m.ctx.Styles.Sidebar.ContentPadding*2 -
		m.ctx.Styles.Sidebar.InputBox.GetHorizontalFrameSize()
	m.editor.SetWidth(max(1, editorWidth))
}

func (m *Model) IsTextInputBoxFocused() bool {
	return m.editor.Active()
}

func (m *Model) GetIsCommenting() bool {
	return m.editor.Mode() == cmpcontroller.ModeComment
}

func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) {
	m.ctx = ctx
	// Theme/context change affects the pre-rendered activity colors.
	m.activityDirty = true
	m.editor.UpdateProgramContext(ctx)
	m.carousel.SetStyles(
		carousel.Styles{
			Item:     lipgloss.NewStyle().Padding(0, 1).Foreground(m.ctx.Theme.FaintText),
			Selected: lipgloss.NewStyle().Padding(0, 1).Bold(true),
		},
	)
}

func (m *Model) SetIsCommenting(isCommenting bool) tea.Cmd {
	if m.pr == nil {
		return nil
	}

	if !isCommenting {
		if m.editor.Mode() == cmpcontroller.ModeComment {
			m.editor.Exit()
		}
		return nil
	}

	cmd := m.editor.Enter(cmpcontroller.EnterOptions{
		Mode:                             cmpcontroller.ModeComment,
		Prompt:                           constants.CommentPrompt,
		Source:                           cmp.UserMentionSource{},
		Repo:                             m.repoRef(),
		SuggestionKind:                   cmpcontroller.SuggestionUsers,
		EnterFetch:                       cmpcontroller.FetchSilent,
		ConfirmDiscardOnCancel:           true,
		HideAutocompleteWhenContextEmpty: true,
	})
	return cmd
}

func (m *Model) getIndentedContentWidth() int {
	// View() wraps body content in `lipgloss.Padding(0, ContentPadding)`,
	// which consumes 2*ContentPadding columns total (left + right). The
	// historical `4*ContentPadding` here under-counted the available
	// width by 2*ContentPadding, leaving a visible gap on the right of
	// every body element — most obvious on code blocks where the
	// padCodeBlockLines bg fills only up to wrap width and stops short
	// of the actual visible right edge. Use the correct factor so
	// content (and the code-block bg) extends fully to the visible
	// right edge of the sidebar.
	return m.width - 2*m.ctx.Styles.Sidebar.ContentPadding
}

func (m *Model) GetIsApproving() bool {
	return m.editor.Mode() == cmpcontroller.ModeApprove
}

func (m *Model) SetIsApproving(isApproving bool) tea.Cmd {
	if m.pr == nil {
		return nil
	}

	if !isApproving {
		if m.editor.Mode() == cmpcontroller.ModeApprove {
			m.editor.Exit()
		}
		return nil
	}

	cmd := m.editor.Enter(cmpcontroller.EnterOptions{
		Mode:                             cmpcontroller.ModeApprove,
		Prompt:                           constants.ApprovalPrompt,
		InitialValue:                     m.ctx.Config.Defaults.PrApproveComment,
		Source:                           cmp.WhitespaceSource{},
		Repo:                             m.repoRef(),
		SuggestionKind:                   cmpcontroller.SuggestionUsers,
		EnterFetch:                       cmpcontroller.FetchSilent,
		ConfirmDiscardOnCancel:           true,
		HideAutocompleteWhenContextEmpty: false,
	})
	return cmd
}

func (m *Model) GetIsAssigning() bool {
	return m.editor.Mode() == cmpcontroller.ModeAssign
}

func (m *Model) SetIsAssigning(isAssigning bool) tea.Cmd {
	if m.pr == nil {
		return nil
	}

	if !isAssigning {
		if m.editor.Mode() == cmpcontroller.ModeAssign {
			m.editor.Exit()
		}
		return nil
	}

	initialValue := ""
	if !m.userAssignedToPr(m.ctx.User) {
		initialValue = m.ctx.User
	}

	cmd := m.editor.Enter(cmpcontroller.EnterOptions{
		Mode:                             cmpcontroller.ModeAssign,
		Prompt:                           constants.AssignPrompt,
		InitialValue:                     initialValue,
		Source:                           cmp.WhitespaceSource{},
		Repo:                             m.repoRef(),
		SuggestionKind:                   cmpcontroller.SuggestionUsers,
		EnterFetch:                       cmpcontroller.FetchSilent,
		HideAutocompleteWhenContextEmpty: false,
	})
	return cmd
}

func (m *Model) userAssignedToPr(login string) bool {
	for _, a := range m.pr.Data.Primary.Assignees.Nodes {
		if login == a.Login {
			return true
		}
	}
	return false
}

// GetIsRequestingReview / SetIsRequestingReview mirror the Assign
// setters: open the editor in ModeRequestReview and let the user type
// whitespace-separated usernames. Submit (Ctrl+D) dispatches
// tasks.RequestReviewers which calls `gh pr edit --add-reviewer`.
func (m *Model) GetIsRequestingReview() bool {
	return m.editor.Mode() == cmpcontroller.ModeRequestReview
}

func (m *Model) SetIsRequestingReview(isRequesting bool) tea.Cmd {
	if m.pr == nil {
		return nil
	}

	if !isRequesting {
		if m.editor.Mode() == cmpcontroller.ModeRequestReview {
			m.editor.Exit()
		}
		return nil
	}

	cmd := m.editor.Enter(cmpcontroller.EnterOptions{
		Mode:                             cmpcontroller.ModeRequestReview,
		Prompt:                           constants.RequestReviewPrompt,
		Source:                           cmp.WhitespaceSource{},
		Repo:                             m.repoRef(),
		SuggestionKind:                   cmpcontroller.SuggestionUsersAndTeams,
		EnterFetch:                       cmpcontroller.FetchSilent,
		HideAutocompleteWhenContextEmpty: false,
	})
	return cmd
}

func (m *Model) GetIsUnassigning() bool {
	return m.editor.Mode() == cmpcontroller.ModeUnassign
}

func (m *Model) SetIsUnassigning(isUnassigning bool) tea.Cmd {
	if m.pr == nil {
		return nil
	}

	if !isUnassigning {
		if m.editor.Mode() == cmpcontroller.ModeUnassign {
			m.editor.Exit()
		}
		return nil
	}

	cmd := m.editor.Enter(cmpcontroller.EnterOptions{
		Mode:         cmpcontroller.ModeUnassign,
		Prompt:       constants.UnassignPrompt,
		InitialValue: strings.Join(m.prAssignees(), "\n"),
		Repo:         m.repoRef(),
	})
	return cmd
}

func (m *Model) prAssignees() []string {
	var assignees []string
	for _, n := range m.pr.Data.Primary.Assignees.Nodes {
		assignees = append(assignees, n.Login)
	}
	return assignees
}

func (m *Model) GoToFirstTab() {
	m.carousel.SetCursor(0)
}

// PrevTab / NextTab move the prview tab cursor by one. Exposed so the
// parent can wire h/l (or arrow keys) to the same carousel that [/]
// drives, without synthesizing a keypress.
func (m *Model) PrevTab() {
	m.carousel.MoveLeft()
}

func (m *Model) NextTab() {
	m.carousel.MoveRight()
}

func (m *Model) GoToActivityTab() {
	m.carousel.SetCursor(1) // Activity is the second tab (index 1)
}

func (m Model) SelectedTab() string {
	return m.carousel.SelectedItem()
}

func (m *Model) SetSummaryViewMore() {
	m.summaryViewMore = true
}

func (m *Model) SetSummaryViewLess() {
	m.summaryViewMore = false
}

// SetThreadResolvedOptimistic flips a review thread's IsResolved flag in
// the enriched data immediately, so the UI reflects a resolve/unresolve
// the instant the user confirms it instead of waiting for the next
// refetch. Mutates the SOURCE slice in place by index. The next
// EnrichedPrMsg overwrites Enriched wholesale (SetEnrichedPR), reconciling
// to server truth, so a failed mutation simply reverts on the next tick.
// The caller also marks the activity list dirty so the row/detail re-render
// with the new state.
func (m *Model) SetThreadResolvedOptimistic(id string, resolved bool) {
	if m.pr == nil || m.pr.Data == nil || !m.pr.Data.IsEnriched {
		return
	}
	nodes := m.pr.Data.Enriched.ReviewThreads.Nodes
	for i := range nodes {
		if nodes[i].Id == id {
			nodes[i].IsResolved = resolved
			// The pre-rendered row + detail must rebuild so the resolved
			// pill and gutter reflect the flip immediately.
			m.activityDirty = true
			return
		}
	}
}

func (m *Model) SetEnrichedPR(data data.EnrichedPullRequestData) {
	if m.pr.Data.Primary.Url == data.Url {
		m.pr.Data.Enriched = data
		m.pr.Data.IsEnriched = true
	}
	// New enrichment payload: rebuild the activity list from it.
	m.activityDirty = true
	m.RebuildImageHints()
}

// RebuildImageHints walks the current PR's summary and activity bodies
// to collect embedded images and assign hint labels. Idempotent —
// safe to call after every PR-data mutation. Clears any pending hint
// (the user's in-flight 2-char keystroke is meaningless against a new
// hint set).
func (m *Model) RebuildImageHints() {
	m.pendingImageHint = ""
	if !m.hasData() {
		m.imageHints = nil
		m.imageURLToHint = nil
		return
	}
	bodies := []string{m.pr.Data.Primary.Body}
	if m.pr.Data.IsEnriched {
		for _, c := range m.pr.Data.Enriched.Comments.Nodes {
			bodies = append(bodies, c.Body)
		}
		for _, t := range m.pr.Data.Enriched.ReviewThreads.Nodes {
			for _, c := range t.Comments.Nodes {
				bodies = append(bodies, c.Body)
			}
		}
	}
	for _, r := range m.pr.Data.Primary.Reviews.Nodes {
		bodies = append(bodies, r.Body)
	}
	m.imageHints = extractImages(bodies)
	if len(m.imageHints) == 0 {
		m.imageURLToHint = nil
		return
	}
	m.imageURLToHint = make(map[string]string, len(m.imageHints))
	for _, h := range m.imageHints {
		m.imageURLToHint[h.URL] = h.Label
	}
}

// HandleImageHintKey is the dispatcher's entry point for image-hint
// keys. It owns the pendingImageHint state machine.
//
// Returns:
//   - consumed=true, target!=nil → key completed a hint; caller fires
//     DownloadImageCmd(target.URL).
//   - consumed=true, target==nil → key matched a 2-char prefix; caller
//     swallows the key and waits for the next one.
//   - consumed=false             → key isn't a hint; caller falls
//     through to existing handlers.
func (m *Model) HandleImageHintKey(s string) (bool, *imageHint) {
	if len(m.imageHints) == 0 || s == "" {
		return false, nil
	}
	candidate := m.pendingImageHint + s
	for i := range m.imageHints {
		if m.imageHints[i].Label == candidate {
			m.pendingImageHint = ""
			return true, &m.imageHints[i]
		}
	}
	if m.pendingImageHint == "" {
		for i := range m.imageHints {
			if len(m.imageHints[i].Label) > 1 && strings.HasPrefix(m.imageHints[i].Label, candidate) {
				m.pendingImageHint = candidate
				return true, nil
			}
		}
		return false, nil
	}
	// Pending was set but no hint matched — abandon the prefix and let
	// the original key fall through to normal handlers.
	m.pendingImageHint = ""
	return false, nil
}

// injectHints rewrites a markdown body so that recognised images get a
// visible **[label]** prefix. Pure pass-through when the PR has no
// images.
func (m *Model) injectHints(body string) string {
	return rewriteBodyWithHints(body, m.imageURLToHint)
}

func (m *Model) GetIsLabeling() bool {
	return m.editor.Mode() == cmpcontroller.ModeLabel
}

// SetIsLabeling enters or exits labeling mode
func (m *Model) SetIsLabeling(isLabeling bool) tea.Cmd {
	if m.pr == nil {
		return nil
	}

	if !isLabeling {
		if m.editor.Mode() == cmpcontroller.ModeLabel {
			m.editor.Exit()
		}
		return nil
	}

	labels := make([]string, 0, len(m.pr.Data.Primary.Labels.Nodes)+1)
	for _, label := range m.pr.Data.Primary.Labels.Nodes {
		labels = append(labels, label.Name)
	}
	labels = append(labels, "")

	cmd := m.editor.Enter(cmpcontroller.EnterOptions{
		Mode:                             cmpcontroller.ModeLabel,
		Prompt:                           constants.LabelPrompt,
		InitialValue:                     strings.Join(labels, ", "),
		Source:                           cmp.LabelSource{},
		Repo:                             m.repoRef(),
		SuggestionKind:                   cmpcontroller.SuggestionLabels,
		EnterFetch:                       cmpcontroller.FetchSilent,
		HideAutocompleteWhenContextEmpty: false,
		ConfirmDiscardOnCancel:           false,
	})
	return cmd
}

// GetIsReplyingToReview reports whether the editor is currently open in
// review-thread reply mode. Mirrors the GetIsCommenting/GetIsLabeling
// shape so the UI layer can probe focus state uniformly.
func (m *Model) GetIsReplyingToReview() bool {
	return m.editor.Mode() == cmpcontroller.ModeReplyReview
}

// SetIsReplyingToReview opens (or closes) the inline editor for replying
// to the most recent unresolved review thread on this PR. v1 picks the
// target automatically — there is no thread cursor yet — so the keypress
// becomes a no-op when there are no unresolved threads.
//
// The target comment id is stashed on the Model and consumed by Update
// on Ctrl+D submit. We don't pass it through EnterOptions because the
// cmpcontroller layer is shared with the issue view and doesn't carry
// per-feature payloads.
func (m *Model) SetIsReplyingToReview(isReplying bool) tea.Cmd {
	return m.enterReplyEditor(isReplying, false)
}

// SetIsReplyingAndResolving opens the inline reply editor like
// SetIsReplyingToReview, but also records the focused thread so that on
// submit the reply is posted AND the thread is resolved in one motion
// (the R combo). Closing (isReplying=false) clears the pending resolve.
func (m *Model) SetIsReplyingAndResolving(isReplying bool) tea.Cmd {
	return m.enterReplyEditor(isReplying, true)
}

// enterReplyEditor is the shared open/close path for the r (reply) and R
// (reply+resolve) actions. When alsoResolve is true it stashes the
// focused thread's GraphQL id so Update can batch a resolve after the
// reply on submit.
func (m *Model) enterReplyEditor(isReplying, alsoResolve bool) tea.Cmd {
	if m.pr == nil {
		return nil
	}

	if !isReplying {
		if m.editor.Mode() == cmpcontroller.ModeReplyReview {
			m.editor.Exit()
		}
		m.replyTargetCommentId = 0
		m.replyThenResolveThreadId = ""
		return nil
	}

	target := m.pickReplyTarget()
	if target == 0 {
		return nil
	}
	m.replyTargetCommentId = target
	m.replyThenResolveThreadId = ""
	if alsoResolve {
		if t, ok := m.focusedThread(); ok {
			m.replyThenResolveThreadId = t.Id
		}
	}

	prompt := constants.ReplyPrompt
	if alsoResolve {
		prompt = constants.ReplyResolvePrompt
	}
	cmd := m.editor.Enter(cmpcontroller.EnterOptions{
		Mode:                             cmpcontroller.ModeReplyReview,
		Prompt:                           prompt,
		Source:                           cmp.UserMentionSource{},
		Repo:                             m.repoRef(),
		SuggestionKind:                   cmpcontroller.SuggestionUsers,
		EnterFetch:                       cmpcontroller.FetchSilent,
		ConfirmDiscardOnCancel:           true,
		HideAutocompleteWhenContextEmpty: true,
	})
	return cmd
}

// selectedActivity returns the activity row under the list cursor, or
// ok=false when the list is empty or the cursor is out of range.
func (m *Model) selectedActivity() (activityItem, bool) {
	if m.activityCursor < 0 || m.activityCursor >= len(m.activityItems) {
		return activityItem{}, false
	}
	return m.activityItems[m.activityCursor], true
}

// threadById looks up an enriched review thread by its GraphQL node id.
func (m *Model) threadById(id string) (data.ReviewThread, bool) {
	if m.pr == nil || m.pr.Data == nil || !m.pr.Data.IsEnriched {
		return data.ReviewThread{}, false
	}
	for _, t := range m.pr.Data.Enriched.ReviewThreads.Nodes {
		if t.Id == id {
			return t, true
		}
	}
	return data.ReviewThread{}, false
}

// focusedThread returns the review thread for the selected list row, or
// ok=false when the selected row is a comment/review (so x/r/R no-op) or
// when there is no selection.
func (m *Model) focusedThread() (data.ReviewThread, bool) {
	sel, ok := m.selectedActivity()
	if !ok || sel.kind != kindThread {
		return data.ReviewThread{}, false
	}
	return m.threadById(sel.threadId)
}

// pickReplyTarget returns the root-comment databaseId of the focused
// thread (the one R/X act on). 0 when there's nothing to reply to.
func (m *Model) pickReplyTarget() int {
	t, ok := m.focusedThread()
	if !ok || len(t.Comments.Nodes) == 0 {
		return 0
	}
	return t.Comments.Nodes[0].DatabaseId
}

// SetViewportHeight records the sidebar viewport's content height so
// SyncActivity can size the list and detail panes to exactly fill it.
// Called by the parent from syncSidebar before every render.
func (m *Model) SetViewportHeight(h int) {
	m.viewportHeight = h
}

// FocusedThread returns the GraphQL node id and resolved state of the
// review thread under the activity-tab list cursor. ok=false when the
// selected row is not a review thread (no PR, not enriched, or the row is
// a comment/review). Used to plumb the id through the prompt-confirmation
// pipeline and to decide between resolve and unresolve mutations.
func (m *Model) FocusedThread() (id string, isResolved bool, ok bool) {
	t, found := m.focusedThread()
	if !found {
		return "", false, false
	}
	return t.Id, t.IsResolved, true
}

// MoveThreadCursor advances the activity-tab list selection by delta and
// clamps to the bounds of activityItems. No-op when the list is empty.
func (m *Model) MoveThreadCursor(delta int) {
	n := len(m.activityItems)
	if n == 0 {
		m.activityCursor = 0
		return
	}
	idx := m.activityCursor + delta
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	m.activityCursor = idx
}

// ScrollDetail scrolls the detail pane by delta lines (positive = down).
// Bound to j/k and PageUp/PageDown on the Activity tab.
func (m *Model) ScrollDetail(delta int) {
	switch {
	case delta > 0:
		m.activityDetail.ScrollDown(delta)
	case delta < 0:
		m.activityDetail.ScrollUp(-delta)
	}
}

// ScrollDetailToTop / ScrollDetailToBottom jump the detail pane to its top
// or bottom. Bound to g / G on the Activity tab; ToBottom is also used when
// the inline reply editor opens so the input is visible.
func (m *Model) ScrollDetailToTop() {
	m.activityDetail.GotoTop()
}

func (m *Model) ScrollDetailToBottom() {
	m.activityDetail.GotoBottom()
}

func (m *Model) repoRef() cmpcontroller.RepoRef {
	owner, repo := m.pr.Data.Primary.GetRepoNameAndOwner()
	return cmpcontroller.RepoRef{
		NameWithOwner: m.pr.Data.Primary.GetRepoNameWithOwner(),
		Owner:         owner,
		Name:          repo,
	}
}

func (m *Model) hasData() bool {
	return m.pr != nil && m.pr.Data != nil
}

// RepoNameWithOwner returns the "owner/name" of the previewed PR, or ""
// when no PR is loaded. Used by the parent to give the image-hint
// downloader the repo context needed to resolve GitHub attachment URLs to
// signed CDN URLs.
func (m *Model) RepoNameWithOwner() string {
	if !m.hasData() || m.pr.Data.Primary == nil {
		return ""
	}
	return m.pr.Data.Primary.GetRepoNameWithOwner()
}
