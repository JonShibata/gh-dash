package footer

import (
	"fmt"
	"path"
	"strings"

	bbHelp "charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/git"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

const viewSeparator = " │ "

type Model struct {
	ctx             *context.ProgramContext
	leftSection     *string
	help            bbHelp.Model
	ShowAll         bool
	// ShowConfirmQuit is a state flag consumed by the main view to render
	// the quit-confirmation modal overlay; the footer no longer draws it
	// in the bar.
	ShowConfirmQuit bool
}

func NewModel(ctx *context.ProgramContext) Model {
	help := bbHelp.New()
	help.ShowAll = true
	help.Styles = ctx.Styles.Help.BubbleStyles
	l := ""
	return Model{
		ctx:         ctx,
		help:        help,
		leftSection: &l,
	}
}

func (m Model) View() string {
	var footer string

	// Confirmations (quit, section actions, notification actions) and
	// transient status messages are no longer drawn in the bar; they render
	// as centered-modal / toast overlays in the main view. The footer only
	// carries the view switcher, pager, and help/donate chrome.
	{
		helpIndicator := lipgloss.NewStyle().
			Background(m.ctx.Theme.FaintText).
			Foreground(m.ctx.Theme.SelectedBackground).
			Padding(0, 1).
			Render("? help")
		donationIndicator := zone.Mark("donate", lipgloss.NewStyle().
			Background(m.ctx.Theme.SelectedBackground).
			Foreground(m.ctx.Theme.WarningText).
			Padding(0, 1).
			Underline(true).
			Render(fmt.Sprintf("%s donate", constants.DonateIcon)))
		viewSwitcher := m.renderViewSwitcher(m.ctx)
		leftSection := ""
		if m.leftSection != nil {
			leftSection = *m.leftSection
		}
		spacing := lipgloss.NewStyle().
			Background(m.ctx.Theme.SelectedBackground).
			Render(
				strings.Repeat(
					" ",
					utils.Max(0,
						m.ctx.ScreenWidth-lipgloss.Width(
							viewSwitcher,
						)-lipgloss.Width(leftSection)-
							lipgloss.Width(
								helpIndicator,
							)-lipgloss.Width(donationIndicator),
					)))

		footer = m.ctx.Styles.Common.FooterStyle.
			Render(lipgloss.JoinHorizontal(lipgloss.Top, viewSwitcher, leftSection, spacing,
				donationIndicator, helpIndicator))
	}

	if m.ShowAll {
		return lipgloss.JoinVertical(lipgloss.Top, footer, m.renderFullHelp())
	}

	return footer
}

// renderFullHelp lays out the full help as a flex-wrapping grid. The bubbles
// help renderer (help.View) drops any column that doesn't fit the viewport
// width, silently hiding bindings on narrow windows. Instead we render each
// functional group as a column (matching the bubbles column styling) and wrap
// overflow columns onto new rows, so every binding is always visible.
func (m Model) renderFullHelp() string {
	groups := keys.CreateKeyMapForView(m.ctx.View).FullHelp()
	styles := m.help.Styles
	width := m.help.Width()
	separator := styles.FullSeparator.Inline(true).Render(m.help.FullSeparator)
	sepWidth := lipgloss.Width(separator)

	// Render each non-empty group into a column, skipping disabled bindings.
	var cols []string
	for _, group := range groups {
		var keyList, descList []string
		for _, kb := range group {
			if !kb.Enabled() {
				continue
			}
			keyList = append(keyList, kb.Help().Key)
			descList = append(descList, kb.Help().Desc)
		}
		if len(keyList) == 0 {
			continue
		}
		cols = append(cols, lipgloss.JoinHorizontal(lipgloss.Top,
			styles.FullKey.Render(strings.Join(keyList, "\n")),
			" ",
			styles.FullDesc.Render(strings.Join(descList, "\n")),
		))
	}
	if len(cols) == 0 {
		return ""
	}

	// Blank line between row-bands for readability.
	return strings.Join(packHelpRows(cols, separator, sepWidth, width), "\n\n")
}

// packHelpRows greedily packs rendered help columns into rows that fit within
// width, joining columns with separator and wrapping overflow onto new rows. A
// column wider than width on its own is placed alone on its row rather than
// dropped. width <= 0 means "unbounded" → a single row.
func packHelpRows(cols []string, separator string, sepWidth, width int) []string {
	var rows []string
	var row []string
	rowWidth := 0
	for _, col := range cols {
		w := lipgloss.Width(col)
		need := w
		if len(row) > 0 {
			need += sepWidth
		}
		if len(row) > 0 && width > 0 && rowWidth+need > width {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, row...))
			row = nil
			rowWidth = 0
		}
		if len(row) > 0 {
			row = append(row, separator)
			rowWidth += sepWidth
		}
		row = append(row, col)
		rowWidth += w
	}
	if len(row) > 0 {
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, row...))
	}
	return rows
}

func (m *Model) SetShowConfirmQuit(val bool) {
	m.ShowConfirmQuit = val
}

func (m *Model) SetWidth(width int) {
	m.help.SetWidth(width)
}

func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) {
	m.ctx = ctx
	m.help.Styles = ctx.Styles.Help.BubbleStyles
}

func (m *Model) renderViewButton(view config.ViewType) string {
	isActive := m.ctx.View == view

	// Define icons and labels for each view
	var icon, label string
	// Define icons - notifications has solid/outline variants
	solidBell := ""
	outlineBell := ""

	switch view {
	case config.NotificationsView:
		if m.ctx.View == config.NotificationsView {
			icon = solidBell
		} else {
			icon = outlineBell
		}
		label = ""
	case config.PRsView:
		icon = ""
		label = " PRs"
	case config.IssuesView:
		icon = ""
		label = " Issues"
	}

	if isActive {
		// Active: colored icon + prominent background
		// Use gold for notifications bell, green for others
		iconColor := m.ctx.Theme.SuccessText
		if view == config.NotificationsView {
			iconColor = compat.AdaptiveColor{
				Light: lipgloss.Color("#B8860B"),
				Dark:  lipgloss.Color("#FFD700"),
			} // Gold
		}
		activeStyle := lipgloss.NewStyle().
			Foreground(iconColor).
			Background(m.ctx.Styles.ViewSwitcher.ActiveView.GetBackground()).
			Bold(true)
		if label != "" {
			return activeStyle.Render(icon) + activeStyle.Render(label)
		}
		return activeStyle.Render(icon)
	}

	// Inactive: faint styling
	return m.ctx.Styles.ViewSwitcher.InactiveView.Render(icon + label)
}

func (m *Model) renderViewSwitcher(ctx *context.ProgramContext) string {
	var repo string
	if m.ctx.RepoPath != "" {
		name := path.Base(m.ctx.RepoPath)
		if m.ctx.RepoUrl != "" {
			name = git.GetRepoShortName(m.ctx.RepoUrl)
		}
		repo = ctx.Styles.Common.FooterStyle.Render(fmt.Sprintf(" %s", name))
	}

	var user string
	if ctx.User != "" {
		user = ctx.Styles.Common.FooterStyle.Render("@" + ctx.User)
	}

	view := lipgloss.JoinHorizontal(
		lipgloss.Top,
		ctx.Styles.ViewSwitcher.ViewsSeparator.PaddingLeft(1).
			Render(m.renderViewButton(config.NotificationsView)),
		ctx.Styles.ViewSwitcher.ViewsSeparator.Render(viewSeparator),
		m.renderViewButton(config.PRsView),
		ctx.Styles.ViewSwitcher.ViewsSeparator.Render(viewSeparator),
		m.renderViewButton(config.IssuesView),
		lipgloss.NewStyle().Background(ctx.Styles.Common.FooterStyle.GetBackground()).Foreground(
			ctx.Styles.ViewSwitcher.ViewsSeparator.GetBackground()).Render(" "),
		repo,
		ctx.Styles.Common.FooterStyle.Foreground(m.ctx.Theme.FaintText).Render(" • "),
		user,
		ctx.Styles.Common.FooterStyle.Foreground(m.ctx.Theme.FaintBorder).Render(" │"),
	)

	return ctx.Styles.ViewSwitcher.Root.Render(view)
}

func (m *Model) SetLeftSection(leftSection string) {
	*m.leftSection = leftSection
}

