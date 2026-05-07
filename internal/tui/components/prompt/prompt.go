package prompt

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

type Model struct {
	ctx    *context.ProgramContext
	prompt textinput.Model
}

func NewModel(ctx *context.ProgramContext) Model {
	ti := textinput.New()
	ti.Focus()
	ti.Blur()
	ti.CursorStart()
	applyThemeStyles(&ti, ctx)

	return Model{
		ctx:    ctx,
		prompt: ti,
	}
}

// applyThemeStyles overrides the textinput defaults (Prompt fg = ANSI 7,
// near-invisible on the light selected-bg) with the theme's PrimaryText so
// confirmation prompts stay readable.
func applyThemeStyles(ti *textinput.Model, ctx *context.ProgramContext) {
	if ctx == nil {
		return
	}
	styles := ti.Styles()
	bold := lipgloss.NewStyle().Foreground(ctx.Theme.PrimaryText).Bold(true)
	styles.Focused.Prompt = bold
	styles.Focused.Text = bold
	styles.Blurred.Prompt = bold
	styles.Blurred.Text = bold
	ti.SetStyles(styles)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.prompt, cmd = m.prompt.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	return m.prompt.View()
}

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m *Model) Blur() {
	m.prompt.Blur()
}

func (m *Model) Focus() tea.Cmd {
	return m.prompt.Focus()
}

func (m *Model) SetValue(value string) {
	m.prompt.SetValue(value)
}

func (m *Model) Value() string {
	return m.prompt.Value()
}

func (m *Model) SetPrompt(prompt string) {
	m.prompt.Prompt = prompt
}

func (m *Model) Reset() {
	m.prompt.Reset()
}

func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) {
	m.ctx = ctx
	applyThemeStyles(&m.prompt, ctx)
}
