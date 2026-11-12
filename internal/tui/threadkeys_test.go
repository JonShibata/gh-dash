package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/branchsidebar"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/footer"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issueview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/notificationview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prssection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/sidebar"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tabs"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/markdown"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

// fullscreenOnThread builds a model in the one scope where the review-thread
// keys apply: PR detail fullscreen, Activity tab, a review thread selected.
func fullscreenOnThread(t *testing.T) *Model {
	t.Helper()
	markdown.InitializeMarkdownStyle(true)
	cfg, err := config.ParseConfig(config.Location{ConfigFlag: "../config/testdata/test-config.yml"})
	require.NoError(t, err)

	ctx := &context.ProgramContext{
		Config:    &cfg,
		View:      config.PRsView,
		StartTask: func(task context.Task) tea.Cmd { return nil },
	}
	ctx.Theme = theme.ParseTheme(ctx.Config)
	ctx.Styles = context.InitStyles(ctx.Theme)

	prSection := prssection.NewModel(0, ctx,
		config.PrsSectionConfig{Title: "Test", Filters: "is:open"}, time.Now(), time.Now())

	m := &Model{
		ctx:              ctx,
		keys:             keys.Keys,
		prs:              []section.Section{&prSection},
		sidebar:          sidebar.NewModel(),
		footer:           footer.NewModel(ctx),
		tabs:             tabs.NewModel(ctx),
		prView:           prview.NewModel(ctx),
		issueSidebar:     issueview.NewModel(ctx),
		branchSidebar:    branchsidebar.NewModel(ctx),
		notificationView: notificationview.NewModel(ctx),
	}

	primary := &data.PullRequestData{Number: 7, Title: "t", Url: "https://example.com/o/r/pull/7", State: "OPEN"}
	primary.Repository.NameWithOwner = "o/r"
	row := &prrow.Data{Primary: primary, IsEnriched: true}
	row.Enriched.ReviewThreads.Nodes = []data.ReviewThread{{
		Id: "thread-1", Path: "f.go", Line: 1,
		Comments: data.ReviewComments{Nodes: []data.ReviewComment{{DatabaseId: 11, CreatedAt: time.Now()}}},
	}}

	m.sidebar.IsOpen = true
	m.syncProgramContext()
	m.prView.SetRow(row)
	m.prView.GoToActivityTab()
	m.prView.SetViewportHeight(40)
	m.prView.SyncActivity()
	m.detailFullscreen = true

	_, _, ok := m.prView.FocusedThread()
	require.True(t, ok, "precondition: a review thread is selected")
	require.Equal(t, prview.ActivityTab, m.prView.SelectedTab(), "precondition: on the Activity tab")
	return m
}

func TestThreadScopeKeys(t *testing.T) {
	shiftR := tea.KeyPressMsg{Code: 'R', Text: "R", Mod: tea.ModShift}
	ctrlR := tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}
	r := tea.KeyPressMsg{Code: 'r', Text: "r"}

	// Update has a value receiver, so read the state off the returned model.
	press := func(m *Model, msg tea.KeyPressMsg) Model {
		next, _ := m.Update(msg)
		return next.(Model)
	}
	fetchTaskId := func(m Model) string {
		return m.prs[0].(*prssection.Model).LastFetchTaskId
	}

	t.Run("R replies and does not refresh", func(t *testing.T) {
		m := fullscreenOnThread(t)
		got := press(m, shiftR)
		require.True(t, got.prView.GetIsReplyingToReview())
		require.Empty(t, fetchTaskId(got), "R in the thread scope must not refresh all")
	})

	t.Run("ctrl+r replies and resolves", func(t *testing.T) {
		m := fullscreenOnThread(t)
		got := press(m, ctrlR)
		require.True(t, got.prView.GetIsReplyingToReview())
		require.Empty(t, fetchTaskId(got))
	})

	t.Run("r refreshes and does not reply", func(t *testing.T) {
		m := fullscreenOnThread(t)
		got := press(m, r)
		require.False(t, got.prView.GetIsReplyingToReview(), "lowercase r must not open the reply editor")
		require.NotEmpty(t, fetchTaskId(got), "r must still refresh, even with a thread selected")
	})
}
