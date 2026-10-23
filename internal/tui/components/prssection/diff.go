package prssection

import (
	tea "charm.land/bubbletea/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
)

func (m Model) diff() tea.Cmd {
	currRowData := m.GetCurrRow()
	if currRowData == nil {
		return nil
	}

	if common.HasPatchViewer() {
		return common.ViewPRPatch(currRowData.GetNumber(), currRowData.GetRepoNameWithOwner())
	}

	return common.DiffPR(
		currRowData.GetNumber(),
		currRowData.GetRepoNameWithOwner(),
		m.Ctx.Config.GetFullScreenDiffPagerEnv(),
	)
}
