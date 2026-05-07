package sidebar

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
)

type Model struct {
	IsOpen bool
	// Fullscreen renders the sidebar bare (no border, full screen) so the
	// preview can replace the section list entirely. Set by the parent on
	// Enter, cleared on Esc.
	Fullscreen bool
	data       string
	viewport   viewport.Model
	ctx        *context.ProgramContext
	emptyState string
}

func NewModel() Model {
	vp := viewport.New(
		viewport.WithWidth(0),
		viewport.WithHeight(0),
	)

	return Model{
		IsOpen:     false,
		data:       "",
		viewport:   vp,
		ctx:        nil,
		emptyState: "Nothing selected...",
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Keys.PageDown):
			m.viewport.HalfPageDown()

		case key.Matches(msg, keys.Keys.PageUp):
			m.viewport.HalfPageUp()

		// Up/Down/g/G are list-navigation keys in split mode (move the PR
		// cursor). Only treat them as preview-scroll keys when fullscreen,
		// otherwise the unconditional sidebar.Update at ui.go:949 would
		// hijack them whenever the preview pane is open.
		case m.Fullscreen && key.Matches(msg, keys.Keys.Down):
			m.viewport.ScrollDown(1)

		case m.Fullscreen && key.Matches(msg, keys.Keys.Up):
			m.viewport.ScrollUp(1)

		case m.Fullscreen && key.Matches(msg, keys.Keys.FirstLine):
			m.viewport.GotoTop()

		case m.Fullscreen && key.Matches(msg, keys.Keys.LastLine):
			m.viewport.GotoBottom()
		}
	}

	return m, nil
}

func (m Model) View() string {
	if !m.IsOpen {
		return ""
	}

	if m.Fullscreen {
		height := m.ctx.MainContentHeight
		width := m.ctx.ScreenWidth
		style := lipgloss.NewStyle().Height(height).Width(width).MaxWidth(width)
		if m.data == "" {
			return style.Align(lipgloss.Center).Render(
				lipgloss.PlaceVertical(height, lipgloss.Center, m.emptyState),
			)
		}
		return style.Render(lipgloss.JoinVertical(
			lipgloss.Top,
			m.viewport.View(),
			m.ctx.Styles.Sidebar.PagerStyle.
				Render(fmt.Sprintf("%d%%", int(m.viewport.ScrollPercent()*100))),
		))
	}

	if m.ctx.PreviewPosition == "bottom" {
		height := m.ctx.DynamicPreviewHeight
		width := m.ctx.DynamicPreviewWidth
		style := m.ctx.Styles.Sidebar.BottomRoot.
			Height(height).
			Width(width).
			MaxWidth(width)

		if m.data == "" {
			return style.Align(lipgloss.Center).Render(
				lipgloss.PlaceVertical(height, lipgloss.Center, m.emptyState),
			)
		}

		return style.Render(lipgloss.JoinVertical(
			lipgloss.Top,
			m.viewport.View(),
			m.ctx.Styles.Sidebar.PagerStyle.
				Render(fmt.Sprintf("%d%%", int(m.viewport.ScrollPercent()*100))),
		))
	}

	// Right mode
	height := m.ctx.MainContentHeight
	style := m.ctx.Styles.Sidebar.Root.
		Height(height).
		Width(m.ctx.DynamicPreviewWidth).
		MaxWidth(m.ctx.DynamicPreviewWidth)

	if m.data == "" {
		return style.Align(lipgloss.Center).Render(
			lipgloss.PlaceVertical(height, lipgloss.Center, m.emptyState),
		)
	}

	return style.Render(lipgloss.JoinVertical(
		lipgloss.Top,
		m.viewport.View(),
		m.ctx.Styles.Sidebar.PagerStyle.
			Render(fmt.Sprintf("%d%%", int(m.viewport.ScrollPercent()*100))),
	))
}

func (m *Model) SetContent(data string) {
	m.data = data
	m.viewport.SetContent(data)
}

func (m *Model) GetSidebarContentWidth() int {
	if m.ctx == nil || m.ctx.Config == nil {
		return 0
	}
	if m.Fullscreen {
		return max(0, m.ctx.ScreenWidth)
	}
	if m.ctx.PreviewPosition == "bottom" {
		return max(0, m.ctx.DynamicPreviewWidth)
	}
	return max(0, m.ctx.DynamicPreviewWidth-m.ctx.Styles.Sidebar.BorderWidth)
}

func (m *Model) ScrollToTop() {
	m.viewport.GotoTop()
}

func (m *Model) ScrollToBottom() {
	m.viewport.GotoBottom()
}

func (m *Model) YOffset() int {
	return m.viewport.YOffset()
}

func (m *Model) ScrollToPercent(percent float64) {
	totalLines := m.viewport.TotalLineCount()
	targetLine := int(float64(totalLines) * percent)
	m.viewport.SetYOffset(targetLine)
}

func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) {
	if ctx == nil {
		return
	}
	m.ctx = ctx
	if m.Fullscreen {
		m.viewport.SetHeight(m.ctx.MainContentHeight - m.ctx.Styles.Sidebar.PagerHeight)
	} else if m.ctx.PreviewPosition == "bottom" {
		m.viewport.SetHeight(m.ctx.DynamicPreviewHeight - m.ctx.Styles.Sidebar.PagerHeight)
	} else {
		m.viewport.SetHeight(m.ctx.MainContentHeight - m.ctx.Styles.Sidebar.PagerHeight)
	}
	m.viewport.SetWidth(m.GetSidebarContentWidth())
}
