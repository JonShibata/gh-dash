package prview

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
)

// TestChangedFilesPrefersEnriched verifies the Files tab reads from the
// enriched per-PR payload (which fetches up to 100 files) once the PR is
// enriched, and falls back to the list query's small preview otherwise.
// Regression guard for the bug where the tab always read Primary.Files and
// was therefore capped at the list query's first:5.
func TestChangedFilesPrefersEnriched(t *testing.T) {
	primaryFiles := data.ChangedFiles{
		TotalCount: 23,
		Nodes: []data.ChangedFile{
			{Path: "a"}, {Path: "b"}, {Path: "c"}, {Path: "d"}, {Path: "e"},
		},
	}

	// 23 files, mirroring an enriched fetch that pulled the full set.
	enrichedNodes := make([]data.ChangedFile, 23)
	for i := range enrichedNodes {
		enrichedNodes[i] = data.ChangedFile{Path: string(rune('a' + i))}
	}
	enrichedFiles := data.ChangedFiles{TotalCount: 23, Nodes: enrichedNodes}

	t.Run("enriched returns full list", func(t *testing.T) {
		m := &Model{pr: &prrow.PullRequest{Data: &prrow.Data{
			Primary:    &data.PullRequestData{Files: primaryFiles},
			Enriched:   data.EnrichedPullRequestData{Files: enrichedFiles},
			IsEnriched: true,
		}}}

		got := m.changedFiles()
		require.Len(t, got.Nodes, 23, "enriched PR should show all files, not the 5-file preview")
		require.Equal(t, 23, got.TotalCount)
	})

	t.Run("not enriched falls back to preview", func(t *testing.T) {
		m := &Model{pr: &prrow.PullRequest{Data: &prrow.Data{
			Primary:    &data.PullRequestData{Files: primaryFiles},
			Enriched:   data.EnrichedPullRequestData{Files: enrichedFiles},
			IsEnriched: false,
		}}}

		got := m.changedFiles()
		require.Len(t, got.Nodes, 5, "un-enriched PR should show the list query preview")
	})
}
