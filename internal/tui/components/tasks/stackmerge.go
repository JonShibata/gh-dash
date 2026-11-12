package tasks

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"charm.land/log/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/stacks"
)

// fetchStacks is stacks.Fetch, replaceable in tests.
var fetchStacks = stacks.Fetch

// liveStackInfo reads the PR's place in its stack from GitHub right now. The
// cached view in the UI can be stale (a PR added, merged or removed since the
// last refresh), and a stack merge takes more than one PR with it, so the
// decision is made on fresh data. ok is false when the PR is not in an open
// stack or the lookup failed; callers then merge the PR alone.
func liveStackInfo(repo string, pr int) (info stacks.Info, ok bool) {
	all, err := fetchStacks(repo)
	if err != nil {
		log.Warn("could not read stacks before merging", "repo", repo, "pr", pr, "err", err)
		return stacks.Info{}, false
	}
	info, ok = stacks.Index(all)[pr]
	return info, ok
}

type stackPlan int

const (
	// planSingle merges the PR alone, through the merge queue or `gh pr merge`.
	planSingle stackPlan = iota
	// planStack merges the PR together with the open PRs below it.
	planStack
)

// planStackMerge decides how to merge prNumber. Open PRs below it in its stack
// are merged with it, all or nothing, but only if they are the PRs the user
// confirmed. Anything else stops with an error, so a merge never takes in PRs
// the user did not see in the prompt.
func planStackMerge(prNumber int, live stacks.Info, liveOK bool, confirmed []int) (stackPlan, error) {
	if !liveOK || len(live.Below) == 0 {
		return planSingle, nil
	}
	if slices.Equal(live.Below, confirmed) {
		return planStack, nil
	}
	if len(confirmed) == 0 {
		return planSingle, fmt.Errorf(
			"#%d is stacked on open %s; merge it from the PR list (m) to merge the stack together",
			prNumber, prList(live.Below))
	}
	return planSingle, fmt.Errorf(
		"stack #%d changed since you confirmed: open PRs below #%d are now %s; press m again",
		live.Stack, prNumber, prList(live.Below))
}

func prList(nums []int) string {
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = fmt.Sprintf("#%d", n)
	}
	if len(parts) == 1 {
		return "PR " + parts[0]
	}
	return "PRs " + strings.Join(parts, ", ")
}

// stackHint explains a merge failure that mentions stacks, with what the user
// can do about it. It returns "" for other failures.
func stackHint(live stacks.Info, liveOK bool, msg string) string {
	if !liveOK || !strings.Contains(strings.ToLower(msg), "stack") {
		return ""
	}
	below := "none"
	if len(live.Below) > 0 {
		below = prList(live.Below)
	}
	return fmt.Sprintf(
		" (in stack #%d, position %d of %d; open below: %s. m merges the stack together; `gh stack unstack %d` removes it)",
		live.Stack, live.Position, live.Size, below, live.Stack)
}

// runStackMerge merges prNumber and everything below it through GitHub's
// atomic stack merge, using the gh-stack extension. methodFlag is --squash,
// --merge or --rebase, or "" for the extension's default.
func runStackMerge(repo string, prNumber int, methodFlag string) (string, error) {
	args := []string{"stack", "merge", fmt.Sprint(prNumber), "--yes"}
	if methodFlag != "" {
		args = append(args, methodFlag)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", args...)
	// gh-stack finds the repository from the environment, not from a checkout.
	cmd.Env = append(os.Environ(), "GH_REPO="+repo)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if strings.Contains(text, "unknown command") {
			return text, fmt.Errorf("merging a stack needs the gh-stack extension: gh extension install github/gh-stack")
		}
		return text, fmt.Errorf("gh stack merge: %s", text)
	}
	return text, nil
}
