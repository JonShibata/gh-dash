package prssection

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/stacks"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tasks"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

func stackPR(repo string, number int) prrow.Data {
	p := &data.PullRequestData{Number: number, Url: repo + "/" + string(rune('a'+number)), State: "OPEN"}
	p.Repository.NameWithOwner = repo
	return prrow.Data{Primary: p}
}

func withStack(t *testing.T, m *Model) {
	t.Helper()
	store := stacks.NewStore()
	require.True(t, store.Begin("o/r"))
	store.Done("o/r", []stacks.Stack{{Number: 70000, Open: true, PRs: []stacks.PR{
		{Number: 1}, {Number: 2}, {Number: 3},
	}}}, nil)
	m.Ctx.Stacks = store
	m.Ctx.View = config.PRsView // the stock prompts only apply in the PRs view
}

func TestMergePromptNamesStackedPRs(t *testing.T) {
	m := newReselectTestModel(t)
	withStack(t, m)
	m.seed([]prrow.Data{stackPR("o/r", 1), stackPR("o/r", 3), stackPR("o/r", 9)}, 1) // on #3

	m.IsPromptConfirmationShown = true
	m.PromptConfirmationAction = "merge"
	got := ansi.Strip(m.GetPromptConfirmation())
	require.Contains(t, got, "Merge #1, #2 and #3 together", "a stacked PR with open PRs below lists them")

	m.Table.SetCurrItem(0) // #1: bottom of the stack, nothing below
	got = ansi.Strip(m.GetPromptConfirmation())
	require.Contains(t, got, "Are you sure you want to merge this PR?")

	m.Table.SetCurrItem(2) // #9: not stacked
	got = ansi.Strip(m.GetPromptConfirmation())
	require.Contains(t, got, "Are you sure you want to merge this PR?")
	require.False(t, strings.Contains(got, "stack"), got)
}

func TestMergePromptOnlyForMerge(t *testing.T) {
	m := newReselectTestModel(t)
	withStack(t, m)
	m.seed([]prrow.Data{stackPR("o/r", 3)}, 0)

	m.IsPromptConfirmationShown = true
	m.PromptConfirmationAction = "close"
	require.Contains(t, ansi.Strip(m.GetPromptConfirmation()), "close this PR")
}

func TestStackMergeUpdatesMembers(t *testing.T) {
	m := newReselectTestModel(t)
	m.seed([]prrow.Data{
		stackPR("o/r", 1), stackPR("o/r", 2), stackPR("o/r", 3),
		stackPR("other/repo", 2), // same number, different repo: untouched
	}, 2)

	m.Update(tasks.UpdatePRMsg{
		PrNumber:     3,
		Repo:         "o/r",
		StackMembers: []int{1, 2},
		IsMerged:     utils.BoolPtr(true),
	})

	require.Equal(t, "MERGED", m.Prs[0].Primary.State, "#1 merged with the stack")
	require.Equal(t, "MERGED", m.Prs[1].Primary.State, "#2 merged with the stack")
	require.Equal(t, "MERGED", m.Prs[2].Primary.State, "#3 is the PR that was merged")
	require.Equal(t, "OPEN", m.Prs[3].Primary.State, "PR #2 of another repo is untouched")
}

func TestStackQueueMarksMembersQueued(t *testing.T) {
	m := newReselectTestModel(t)
	m.seed([]prrow.Data{stackPR("o/r", 1), stackPR("o/r", 3)}, 1)

	m.Update(tasks.UpdatePRMsg{
		PrNumber:       3,
		Repo:           "o/r",
		StackMembers:   []int{1},
		IsInMergeQueue: utils.BoolPtr(true),
	})

	require.True(t, m.Prs[0].Primary.IsInMergeQueue)
	require.Equal(t, "OPEN", m.Prs[0].Primary.State, "queued, not merged")
}
