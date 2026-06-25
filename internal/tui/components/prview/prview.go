package prview

import (
	"fmt"
	"image/color"
	"os"
	"regexp"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
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
	// threadCursorIdx is the index into allThreads() that the user has
	// focused on the Activity tab. allThreads() is ordered oldest-first to
	// match the rendered body, so idx 0 is the topmost thread. R/X act on
	// this thread. Reset to 0 on PR change. Clamped on cursor move.
	threadCursorIdx int
	// threadLineOffsets maps a thread's GraphQL Id to its starting line
	// offset within the rendered Activity body (including viewHeader so
	// these are absolute viewport coordinates). Populated by
	// renderActivity; consumed by the parent on n/N and the reply-mode
	// scroll-to-bottom logic.
	threadLineOffsets map[string]int
	// threadLineEnds is the line just past each thread block's last
	// visible line. Equals threadLineOffsets[id] + height of the
	// rendered block (including the inline reply input when reply
	// mode is on). Used to bottom-align the input in the viewport.
	threadLineEnds map[string]int
	// replyViewportHeight is the height (in lines) of the sidebar
	// viewport at the moment reply mode opened. Used by renderActivity
	// to pad above the focused thread so its end lands at the viewport
	// bottom even when the thread is near the document top (otherwise
	// the input would render in the upper portion of the screen with
	// blank space below it, since YOffset can't go negative).
	// Set by SetReplyViewportHeight before syncSidebar in
	// openSidebarForReply; ignored when reply mode is off.
	replyViewportHeight int
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
		// Allocate the offset maps here so renderActivity can mutate
		// them in place (delete + assign). View() is a VALUE receiver,
		// so re-assigning these fields inside it would be lost — but
		// mutations to the underlying map persist because maps are
		// reference types.
		threadLineOffsets: map[string]int{},
		threadLineEnds:    map[string]int{},
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

	body := strings.Builder{}
	switch m.carousel.SelectedItem() {
	case tabs[0]:
		body.WriteString(m.viewOverviewTab())
	case tabs[1]:
		body.WriteString(m.renderActivity())
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

func (m *Model) renderRequestedReviewers() string {
	if !m.pr.Data.IsEnriched {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			m.ctx.Styles.Common.MainTextStyle.Underline(true).Bold(true).Render(
				fmt.Sprintf("%s Reviewers", constants.CodeReviewIcon)),
			"",
			lipgloss.JoinHorizontal(
				lipgloss.Top,
				m.ctx.Styles.Common.WaitingGlyph,
				" ",
				m.ctx.Styles.Common.FaintTextStyle.Render("Loading..."),
			),
		)
	}

	reviewRequests := m.pr.Data.Enriched.ReviewRequests.Nodes
	reviews := m.pr.Data.Enriched.Reviews.Nodes
	suggestedReviewers := m.pr.Data.Enriched.SuggestedReviewers

	if len(reviewRequests) == 0 && len(reviews) == 0 && len(suggestedReviewers) == 0 {
		return ""
	}

	reviewStates := make(map[string]string)
	for _, review := range reviews {
		login := review.Author.Login
		existingState := reviewStates[login]
		// Don't override APPROVED or CHANGES_REQUESTED with COMMENTED
		if review.State == "COMMENTED" &&
			(existingState == "APPROVED" || existingState == "CHANGES_REQUESTED") {
			continue
		}
		reviewStates[login] = review.State
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

	for login, state := range reviewStates {
		if shownReviewers[login] {
			continue
		}
		if state != "APPROVED" && state != "CHANGES_REQUESTED" && state != "COMMENTED" {
			continue
		}
		shownReviewers[login] = true

		var stateIcon string
		switch state {
		case "APPROVED":
			stateIcon = successStyle.Render(constants.ApprovedIcon)
		case "CHANGES_REQUESTED":
			stateIcon = errorStyle.Render(constants.ChangesRequestedIcon)
		case "COMMENTED":
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
			bg := strings.Contains(line, "\x1b[48;2;234;238;242")
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
		m.threadCursorIdx = 0
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
	m.width = width
	m.carousel.SetWidth(width)
	m.editor.SetWidth(width)
}

func (m *Model) IsTextInputBoxFocused() bool {
	return m.editor.Active()
}

func (m *Model) GetIsCommenting() bool {
	return m.editor.Mode() == cmpcontroller.ModeComment
}

func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) {
	m.ctx = ctx
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
// refetch. Mutates the SOURCE slice in place by index (allThreads()
// returns a copy, so mutating that would be lost). The next EnrichedPrMsg
// overwrites Enriched wholesale (SetEnrichedPR), reconciling to server
// truth — so a failed mutation simply reverts on the next tick.
func (m *Model) SetThreadResolvedOptimistic(id string, resolved bool) {
	if m.pr == nil || m.pr.Data == nil || !m.pr.Data.IsEnriched {
		return
	}
	nodes := m.pr.Data.Enriched.ReviewThreads.Nodes
	for i := range nodes {
		if nodes[i].Id == id {
			nodes[i].IsResolved = resolved
			return
		}
	}
}

func (m *Model) SetEnrichedPR(data data.EnrichedPullRequestData) {
	if m.pr.Data.Primary.Url == data.Url {
		m.pr.Data.Enriched = data
		m.pr.Data.IsEnriched = true
	}
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

// allThreads returns the visible review threads in the same top-to-bottom
// order renderActivity lays them out: oldest root comment first. The
// activity body sorts every entry by UpdatedAt ascending, so the cursor
// MUST walk threads in that same ascending order — otherwise n/N (and the
// scroll-follow in SetThreadCursorAtLine) move the wrong way: "next" would
// jump UP the page and "previous" DOWN. Keying on the root comment's
// UpdatedAt mirrors renderActivity's per-thread sort key exactly, so
// cursor order == visual order regardless of how GraphQL returned the
// nodes. Includes resolved threads — the cursor walks them too so x can
// toggle resolve/unresolve. Threads with zero comments are dropped
// (nothing to act on).
func (m *Model) allThreads() []data.ReviewThread {
	if m.pr == nil || m.pr.Data == nil || !m.pr.Data.IsEnriched {
		return nil
	}
	src := m.pr.Data.Enriched.ReviewThreads.Nodes
	out := make([]data.ReviewThread, 0, len(src))
	for _, t := range src {
		if len(t.Comments.Nodes) == 0 {
			continue
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Comments.Nodes[0].UpdatedAt.Before(out[j].Comments.Nodes[0].UpdatedAt)
	})
	return out
}

// focusedThread returns the thread under the cursor on the Activity
// tab, or zero-value when no threads exist.
func (m *Model) focusedThread() (data.ReviewThread, bool) {
	threads := m.allThreads()
	if len(threads) == 0 {
		return data.ReviewThread{}, false
	}
	idx := m.threadCursorIdx
	if idx < 0 {
		idx = 0
	}
	if idx >= len(threads) {
		idx = len(threads) - 1
	}
	return threads[idx], true
}

// focusedThreadPosition returns the 0-based index of the focused thread
// and the total thread count, clamped to match focusedThread(). ok=false
// when there are no threads. Used by the activity-tab action bar to show
// "n/m" so the user can see which thread x/r/R will act on.
func (m *Model) focusedThreadPosition() (idx, count int, ok bool) {
	threads := m.allThreads()
	count = len(threads)
	if count == 0 {
		return 0, 0, false
	}
	idx = m.threadCursorIdx
	if idx < 0 {
		idx = 0
	}
	if idx >= count {
		idx = count - 1
	}
	return idx, count, true
}

// pickReplyTarget returns the root-comment databaseId of the focused
// thread (the one R/X act on). 0 when there's nothing to reply to.
func (m *Model) pickReplyTarget() int {
	t, ok := m.focusedThread()
	if !ok {
		return 0
	}
	return t.Comments.Nodes[0].DatabaseId
}

// SetThreadCursorAtLine snaps the activity-tab thread cursor to the
// thread that contains the given absolute line of the rendered Activity
// body. Used for "cursor follows scroll" behavior — the parent passes
// in the line at the vertical midpoint of the viewport (YOffset +
// Height/2) and the cursor lands on whichever thread the user is
// looking at. Returns true when the cursor index actually changed.
//
// Walks allThreads() in cursor order (oldest first → array index
// ascending, matching the rendered top-to-bottom layout). A line is
// considered "in" thread T if T's start offset
// is the largest one ≤ targetLine — i.e., the line falls between T's
// start and the next thread's start. Falls back to index 0 (top
// thread) when nothing matches; that covers the case where the user
// is scrolled above the first thread (looking at the activity title).
//
// Using the viewport's MIDPOINT (rather than its top) means that when
// two threads fit on the screen at once, both can be focused as the
// user scrolls — at top of viewport you're in the first, scroll a few
// lines and the midpoint crosses into the second.
func (m *Model) SetThreadCursorAtLine(targetLine int) bool {
	threads := m.allThreads()
	if len(threads) == 0 || m.threadLineOffsets == nil {
		return false
	}
	best := 0
	bestOffset := -1
	for i, t := range threads {
		off, ok := m.threadLineOffsets[t.Id]
		if !ok {
			continue
		}
		if off <= targetLine && off > bestOffset {
			best = i
			bestOffset = off
		}
	}
	if best == m.threadCursorIdx {
		return false
	}
	m.threadCursorIdx = best
	return true
}

// FocusedThreadLineOffset returns the line offset of the focused
// thread within the most-recently-rendered Activity body. Returns 0
// when there is no focused thread or when renderActivity hasn't run
// yet (no offsets recorded). Used by the parent's n/N handler to
// scroll the sidebar viewport to the focused thread.
func (m *Model) FocusedThreadLineOffset() int {
	t, ok := m.focusedThread()
	if !ok {
		return 0
	}
	if m.threadLineOffsets == nil {
		return 0
	}
	return m.threadLineOffsets[t.Id]
}

// SetReplyViewportHeight stashes the sidebar viewport's content height
// so renderActivity can pad above the focused thread to bottom-align
// the reply input. Called by the parent in openSidebarForReply just
// before syncSidebar.
func (m *Model) SetReplyViewportHeight(h int) {
	m.replyViewportHeight = h
}

// FocusedThreadEndLine returns the line just past the focused thread
// block's last rendered line — including the inline reply input when
// reply mode is on. Used to bottom-align the input in the viewport so
// the input sits at the screen's bottom and the tail of the thread
// fills the space above it.
func (m *Model) FocusedThreadEndLine() int {
	t, ok := m.focusedThread()
	if !ok {
		return 0
	}
	if m.threadLineEnds == nil {
		return 0
	}
	return m.threadLineEnds[t.Id]
}

// FocusedThread returns the GraphQL node id and resolved state of the
// thread under the activity-tab cursor. ok=false when there's no
// eligible thread (no PR, not enriched, no threads with comments).
// Used to plumb the id through the prompt-confirmation pipeline and
// to decide between resolve and unresolve mutations.
func (m *Model) FocusedThread() (id string, isResolved bool, ok bool) {
	t, found := m.focusedThread()
	if !found {
		return "", false, false
	}
	return t.Id, t.IsResolved, true
}

// MoveThreadCursor advances the activity-tab thread cursor by delta and
// clamps to the bounds of allThreads(). No-op when there are no
// threads.
func (m *Model) MoveThreadCursor(delta int) {
	threads := m.allThreads()
	if len(threads) == 0 {
		m.threadCursorIdx = 0
		return
	}
	idx := m.threadCursorIdx + delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(threads) {
		idx = len(threads) - 1
	}
	m.threadCursorIdx = idx
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
