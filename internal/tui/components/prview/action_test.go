package prview

import (
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func newTestModelForAction(t *testing.T) Model {
	t.Helper()
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../../../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	thm := theme.ParseTheme(&cfg)
	ctx := &context.ProgramContext{
		Config: &cfg,
		Theme:  thm,
		Styles: context.InitStyles(thm),
	}

	m := NewModel(ctx)
	m.ctx = ctx
	m.pr = &prrow.PullRequest{
		Ctx: ctx,
		Data: &prrow.Data{
			Primary:    &data.PullRequestData{},
			IsEnriched: true,
		},
	}
	return m
}

func TestMsgToActionReturnsCorrectActions(t *testing.T) {
	testCases := []struct {
		name           string
		keyBindingText rune
		expectedAction PRActionType
	}{
		{"approve key", 'v', PRActionApprove},
		{"assign key", 'a', PRActionAssign},
		{"unassign key", 'A', PRActionUnassign},
		{"comment key", 'c', PRActionComment},
		{"diff key", 'd', PRActionDiff},
		{"checkout key C", 'C', PRActionCheckout},
		{"checkout key space", tea.KeySpace, PRActionCheckout},
		{"close key", 'x', PRActionClose},
		{"ready key", 'W', PRActionReady},
		{"reopen key", 'X', PRActionReopen},
		{"merge key", 'm', PRActionMerge},
		{"update key", 'u', PRActionUpdate},
		{"summary view more key", 'e', PRActionSummaryViewMore},
		{"approve workflows key", 'V', PRActionApproveWorkflows},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msg := tea.KeyPressMsg{Code: tc.keyBindingText}

			action := MsgToAction(msg)

			require.NotNil(t, action, "expected action for key %q", tc.keyBindingText)
			require.Equal(
				t,
				tc.expectedAction,
				action.Type,
				"expected action type %v for key %q, got %v",
				tc.expectedAction,
				tc.keyBindingText,
				action.Type,
			)
		})
	}
}

func TestMsgToActionReturnsNilForUnknownKeys(t *testing.T) {
	msg := tea.KeyPressMsg{Text: "z"}

	action := MsgToAction(msg)

	require.Nil(t, action, "expected nil action for unknown key")
}

func TestIsTextInputBoxFocusedWhenCommenting(t *testing.T) {
	m := newTestModelForAction(t)
	cmd := m.SetIsCommenting(true)

	require.NotNil(t, cmd)
	require.True(
		t,
		m.IsTextInputBoxFocused(),
		"expected text input box focused when in commenting mode",
	)
}

func TestIsTextInputBoxFocusedWhenApproving(t *testing.T) {
	m := newTestModelForAction(t)
	cmd := m.SetIsApproving(true)

	require.NotNil(t, cmd)
	require.True(
		t,
		m.IsTextInputBoxFocused(),
		"expected text input box focused when in approving mode",
	)
}

func TestIsTextInputBoxFocusedWhenAssigning(t *testing.T) {
	m := newTestModelForAction(t)
	cmd := m.SetIsAssigning(true)

	require.NotNil(t, cmd)
	require.True(
		t,
		m.IsTextInputBoxFocused(),
		"expected text input box focused when in assigning mode",
	)
}

func TestIsTextInputBoxFocusedWhenUnassigning(t *testing.T) {
	m := newTestModelForAction(t)
	cmd := m.SetIsUnassigning(true)

	require.NotNil(t, cmd)
	require.True(
		t,
		m.IsTextInputBoxFocused(),
		"expected text input box focused when in unassigning mode",
	)
}

func TestUpdateHandlesSidebarTabNavigation(t *testing.T) {
	t.Run("prev sidebar tab", func(t *testing.T) {
		m := newTestModelForAction(t)
		// Move to a non-first tab first
		m.carousel.MoveRight()
		initialTab := m.carousel.SelectedItem()

		msg := tea.KeyPressMsg{Text: "["}
		m, _ = m.Update(msg)

		require.NotEqual(t, initialTab, m.carousel.SelectedItem(),
			"carousel should have moved to previous tab")
	})

	t.Run("next sidebar tab", func(t *testing.T) {
		m := newTestModelForAction(t)
		initialTab := m.carousel.SelectedItem()

		msg := tea.KeyPressMsg{Text: "]"}
		m, _ = m.Update(msg)

		require.NotEqual(t, initialTab, m.carousel.SelectedItem(),
			"carousel should have moved to next tab")
	})
}

func TestPRActionTypes(t *testing.T) {
	// Verify all action types are distinct
	actionTypes := []PRActionType{
		PRActionNone,
		PRActionApprove,
		PRActionAssign,
		PRActionUnassign,
		PRActionLabel,
		PRActionComment,
		PRActionDiff,
		PRActionCheckout,
		PRActionClose,
		PRActionReady,
		PRActionReopen,
		PRActionMerge,
		PRActionUpdate,
		PRActionSummaryViewMore,
		PRActionApproveWorkflows,
	}

	seen := make(map[PRActionType]bool)
	for _, at := range actionTypes {
		require.False(t, seen[at], "duplicate action type value: %v", at)
		seen[at] = true
	}

	// Verify PRActionNone is zero value
	require.Equal(t, PRActionType(0), PRActionNone, "PRActionNone should be zero value")
}

func TestMsgToActionWithReboundKeys(t *testing.T) {
	// Save original key bindings
	originalApproveKeys := keys.PRKeys.Approve.Keys()

	// Rebind approve key to "V" (uppercase)
	keys.PRKeys.Approve.SetKeys("V")
	defer func() {
		// Restore original bindings
		keys.PRKeys.Approve.SetKeys(originalApproveKeys...)
	}()

	msg := tea.KeyPressMsg{Text: "V"}

	action := MsgToAction(msg)

	require.NotNil(t, action, "expected action for rebound key")
	require.Equal(t, PRActionApprove, action.Type, "expected approve action for rebound key")
}

func TestIsTextInputBoxFocusedWhenLabeling(t *testing.T) {
	m := newTestModelForAction(t)
	cmd := m.SetIsLabeling(true)

	require.NotNil(t, cmd)
	require.True(
		t,
		m.IsTextInputBoxFocused(),
		"expected text input box focused when in labeling mode",
	)
}

func TestGetIsLabeling(t *testing.T) {
	t.Run("returns false initially", func(t *testing.T) {
		m := newTestModelForAction(t)
		require.False(t, m.GetIsLabeling(), "expected GetIsLabeling to return false initially")
	})

	t.Run("returns true when labeling", func(t *testing.T) {
		m := newTestModelForAction(t)
		cmd := m.SetIsLabeling(true)
		require.NotNil(t, cmd)
		require.True(t, m.GetIsLabeling(), "expected GetIsLabeling to return true when labeling")
	})
}

func TestSetIsLabelingWithNilPR(t *testing.T) {
	m := newTestModelForAction(t)
	m.pr = nil

	cmd := m.SetIsLabeling(true)

	require.Nil(t, cmd, "expected nil command when PR is nil")
}

// SetIsReplyingToReview must be a no-op when there is no eligible thread,
// so the keypress doesn't open an empty editor with no submit target.
func TestSetIsReplyingToReviewNoThreadsIsNoOp(t *testing.T) {
	m := newTestModelForAction(t)
	cmd := m.SetIsReplyingToReview(true)
	require.Nil(t, cmd, "expected nil cmd when there are no review threads")
	require.False(t, m.GetIsReplyingToReview(), "editor should not enter reply mode without a target")
}

// pickReplyTarget targets whatever thread the activity-tab cursor is
// pointing at. The cursor walks all threads (resolved and unresolved) so
// x can toggle resolve/unresolve. allThreads() orders them oldest-first
// to match the rendered activity body (top-to-bottom = old-to-new), so
// the default cursor (idx 0) lands on the topmost/oldest thread and
// MoveThreadCursor(1) — the "next review thread" (n) action — moves DOWN
// the page to the newer one. The threads are appended in the opposite of
// their chronological order here to prove allThreads() sorts by time, not
// by GraphQL array position.
func TestPickReplyTargetUsesCursor(t *testing.T) {
	m := newTestModelForAction(t)
	enriched := data.EnrichedPullRequestData{}
	mkThread := func(id int, resolved bool, updated time.Time) data.ReviewThread {
		return data.ReviewThread{
			Id:         fmt.Sprintf("thread-%d", id),
			IsResolved: resolved,
			Comments: data.ReviewComments{Nodes: []data.ReviewComment{
				{DatabaseId: id, UpdatedAt: updated},
			}},
		}
	}
	older := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	enriched.ReviewThreads.Nodes = append(enriched.ReviewThreads.Nodes,
		mkThread(222, true, newer),  // newer — rendered lower on the page
		mkThread(111, false, older), // older — rendered at the top
	)
	m.pr.Data.Enriched = enriched
	m.pr.Data.IsEnriched = true
	buildActivity(t, &m)

	// Default cursor (idx 0) targets the oldest/topmost thread.
	require.Equal(t, 111, m.pickReplyTarget())

	// "next review thread" (n / +1) moves DOWN the page to the newer thread.
	m.MoveThreadCursor(1)
	require.Equal(t, 222, m.pickReplyTarget())
}
