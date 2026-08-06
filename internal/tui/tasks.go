package tui

import (
	"errors"
	"fmt"
	"io"
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
		// Discard the launcher's stdio: os.Stdout is the live alt-screen,
		// so any line the browser (or xdg-open/Chrome) prints there —
		// e.g. "Opening in existing browser session." — lands in the TUI
		// out-of-band, desyncing tea's cell model and leaving a ghost.
		b := browser.New("", io.Discard, io.Discard)
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
		// Discard the launcher's stdio: os.Stdout is the live alt-screen,
		// so any line the browser (or xdg-open/Chrome) prints there —
		// e.g. "Opening in existing browser session." — lands in the TUI
		// out-of-band, desyncing tea's cell model and leaving a ghost.
		b := browser.New("", io.Discard, io.Discard)
		err := b.Browse(failedUrl)
		return constants.TaskFinishedMsg{TaskId: taskId, Err: err}
	}
	return tea.Batch(startCmd, openCmd)
}
