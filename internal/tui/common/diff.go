package common

import (
	"fmt"
	"os/exec"

	tea "charm.land/bubbletea/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
)

// DiffPR opens a diff view for a PR using the gh CLI.
// The env parameter should be the result of Config.GetFullScreenDiffPagerEnv().
func DiffPR(prNumber int, repoName string, env []string) tea.Cmd {
	c := exec.Command(
		"gh",
		"pr",
		"diff",
		fmt.Sprint(prNumber),
		"-R",
		repoName,
	)
	c.Env = env

	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return constants.ErrMsg{Err: err}
		}
		return nil
	})
}

// patchViewerScript downloads a GitHub diff into a temp file and opens it in diff-tui.
const patchViewerScript = `f=$(mktemp) || exit 1
trap 'rm -f "$f"' EXIT
gh api "$1" -H "Accept: application/vnd.github.diff" >"$f" && diff-tui --patch "$f"`

// HasPatchViewer reports whether diff-tui is on PATH.
func HasPatchViewer() bool {
	_, err := exec.LookPath("diff-tui")
	return err == nil
}

// ViewPatch opens the diff served by a GitHub REST endpoint (a pull or a commit) in diff-tui.
func ViewPatch(endpoint string) tea.Cmd {
	c := exec.Command("bash", "-c", patchViewerScript, "gh-dash", endpoint)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return constants.ErrMsg{Err: err}
		}
		return nil
	})
}

// ViewPRPatch opens a whole PR in diff-tui.
func ViewPRPatch(prNumber int, repoName string) tea.Cmd {
	return ViewPatch(fmt.Sprintf("repos/%s/pulls/%d", repoName, prNumber))
}

// ViewCommitPatch opens a single commit of a PR in diff-tui.
func ViewCommitPatch(repoName, oid string) tea.Cmd {
	return ViewPatch(fmt.Sprintf("repos/%s/commits/%s", repoName, oid))
}
