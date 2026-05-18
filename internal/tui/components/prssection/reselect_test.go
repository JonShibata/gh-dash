package prssection

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func newReselectTestModel(t *testing.T) *Model {
	t.Helper()
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../../../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	require.NoError(t, err)
	thm := theme.ParseTheme(&cfg)
	ctx := &context.ProgramContext{
		Config: &cfg,
		Theme:  thm,
		Styles: context.InitStyles(thm),
	}
	m := NewModel(1, ctx, cfg.PRSections[0], time.Time{}, time.Time{})
	return &m
}

func pr(number int, url string) prrow.Data {
	return prrow.Data{Primary: &data.PullRequestData{Number: number, Url: url}}
}

// fetched builds a SectionPullRequestsFetchedMsg matching the model's
// LastFetchTaskId so the handler processes it as a full-list replace.
func (m *Model) seed(prs []prrow.Data, cursor int) {
	m.Prs = prs
	m.Table.SetRows(m.BuildRows())
	m.Table.SetCurrItem(cursor)
	m.LastFetchTaskId = "task-1"
	m.PageInfo = nil
}

// TestCursorFollowsPRAcrossReorder is the regression guard for the
// "pressing W opens a different PR" bug: an action bumps a PR's updatedAt,
// the sort:updated refetch reorders it to the top, and the fixed-index
// cursor used to land on a different PR. The cursor must instead follow the
// same PR (by URL) to its new position.
func TestCursorFollowsPRAcrossReorder(t *testing.T) {
	m := newReselectTestModel(t)
	m.seed([]prrow.Data{pr(1, "u1"), pr(2, "u2"), pr(3, "u3")}, 2) // viewing #3

	require.Equal(t, "u3", m.GetCurrRow().GetUrl(), "precondition: cursor on #3")

	// Refetch returns #3 bumped to the top (it was just acted on).
	reordered := SectionPullRequestsFetchedMsg{
		Prs:        []prrow.Data{pr(3, "u3"), pr(1, "u1"), pr(2, "u2")},
		TotalCount: 3,
		TaskId:     "task-1",
	}
	updated, _ := m.Update(reordered)

	got := updated.(*Model).GetCurrRow()
	require.NotNil(t, got)
	require.Equal(t, "u3", got.GetUrl(), "cursor should still be on #3 after the reorder")
}

// TestCursorClampsWhenPRLeavesSection covers the filtered-section variant:
// the selected PR no longer matches (e.g. a draft marked ready leaving a
// drafts section). The cursor must land on a valid neighbor, never nil/OOB.
func TestCursorClampsWhenPRLeavesSection(t *testing.T) {
	m := newReselectTestModel(t)
	m.seed([]prrow.Data{pr(1, "u1"), pr(2, "u2"), pr(3, "u3")}, 2) // viewing #3

	// Refetch no longer contains #3, and is shorter.
	shrunk := SectionPullRequestsFetchedMsg{
		Prs:        []prrow.Data{pr(1, "u1"), pr(2, "u2")},
		TotalCount: 2,
		TaskId:     "task-1",
	}
	updated, _ := m.Update(shrunk)

	got := updated.(*Model).GetCurrRow()
	require.NotNil(t, got, "cursor must not fall out of bounds when the PR leaves the section")
	require.Contains(t, []string{"u1", "u2"}, got.GetUrl())
}
