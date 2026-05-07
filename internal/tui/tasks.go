package tui

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cli/go-gh/v2/pkg/browser"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

func (m *Model) openBrowser() tea.Cmd {
	taskId := fmt.Sprintf("open_browser_%d", time.Now().Unix())
	task := context.Task{
		Id:           taskId,
		StartText:    "Opening in browser",
		FinishedText: "Opened in browser",
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := m.ctx.StartTask(task)
	openCmd := func() tea.Msg {
		b := browser.New("", os.Stdout, os.Stdin)
		currRow := m.getCurrRowData()
		if currRow == nil || reflect.ValueOf(currRow).IsNil() {
			return constants.TaskFinishedMsg{
				TaskId: taskId,
				Err:    errors.New("current selection doesn't have a URL"),
			}
		}
		err := b.Browse(currRow.GetUrl())
		return constants.TaskFinishedMsg{TaskId: taskId, Err: err}
	}
	return tea.Batch(startCmd, openCmd)
}

// openFirstFailedCheck opens the details URL of the first failed check on
// the current PR. Falls back to a clear error if there are no failures or
// the PR isn't enriched yet — never opens the PR URL as a substitute,
// since "go to the failure" and "go to the PR" are different intents.
func (m *Model) openFirstFailedCheck() tea.Cmd {
	taskId := fmt.Sprintf("open_failed_check_%d", time.Now().Unix())
	task := context.Task{
		Id:           taskId,
		StartText:    "Opening first failed check",
		FinishedText: "Opened failed check",
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := m.ctx.StartTask(task)
	openCmd := func() tea.Msg {
		currRow := m.getCurrRowData()
		if currRow == nil || reflect.ValueOf(currRow).IsNil() {
			return constants.TaskFinishedMsg{
				TaskId: taskId,
				Err:    errors.New("no PR selected"),
			}
		}
		pr, ok := currRow.(*prrow.Data)
		if !ok {
			return constants.TaskFinishedMsg{
				TaskId: taskId,
				Err:    errors.New("current selection is not a PR"),
			}
		}
		failedUrl := pr.GetFirstFailedCheckUrl()
		if failedUrl == "" {
			return constants.TaskFinishedMsg{
				TaskId: taskId,
				Err:    errors.New("no failed checks"),
			}
		}
		b := browser.New("", os.Stdout, os.Stdin)
		err := b.Browse(failedUrl)
		return constants.TaskFinishedMsg{TaskId: taskId, Err: err}
	}
	return tea.Batch(startCmd, openCmd)
}
