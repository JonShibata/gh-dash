package prrow

import (
	"time"

	ghchecks "github.com/dlvhdr/x/gh-checks"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

type Data struct {
	Primary    *data.PullRequestData
	Enriched   data.EnrichedPullRequestData
	IsEnriched bool
	// ConflictParent is the number of the nearest PR below this one in its stack that
	// conflicts with its base; 0 when none does.
	ConflictParent int
}

// UnresolvedThreads is the number of review threads not yet resolved. Once
// the PR is enriched it reads the per-PR data, which also follows a thread
// resolved from the sidebar before the next list refresh.
func (data Data) UnresolvedThreads() int {
	n := 0
	if data.IsEnriched {
		for _, t := range data.Enriched.ReviewThreads.Nodes {
			if !t.IsResolved {
				n++
			}
		}
		return n
	}
	if data.Primary == nil {
		return 0
	}
	for _, t := range data.Primary.ReviewThreads.Nodes {
		if !t.IsResolved {
			n++
		}
	}
	return n
}

// MergeState prefers the per-PR value over the list value, which GitHub computes loosely.
func (data Data) MergeState() data.MergeStateStatus {
	if data.IsEnriched && data.Enriched.MergeStateStatus != "" {
		return data.Enriched.MergeStateStatus
	}
	return data.Primary.MergeStateStatus
}

// HasConflict reports whether the PR itself conflicts with its base branch.
func (data Data) HasConflict() bool {
	if data.MergeState() == "DIRTY" {
		return true
	}
	if data.IsEnriched && data.Enriched.Mergeable != "" {
		return data.Enriched.Mergeable == "CONFLICTING"
	}
	return data.Primary.Mergeable == "CONFLICTING"
}

// MarkStackConflicts sets ConflictParent on every PR whose base chain, through other PRs in
// the list, reaches a PR that conflicts with its own base.
func MarkStackConflicts(prs []Data) {
	type key struct{ repo, branch string }
	byHead := make(map[key]int, len(prs))
	for i, p := range prs {
		byHead[key{p.Primary.Repository.NameWithOwner, p.Primary.HeadRefName}] = i
	}
	for i, p := range prs {
		prs[i].ConflictParent = 0
		seen := map[int]bool{i: true}
		curr := p
		for {
			parent, ok := byHead[key{curr.Primary.Repository.NameWithOwner, curr.Primary.BaseRefName}]
			if !ok || seen[parent] {
				break
			}
			seen[parent] = true
			curr = prs[parent]
			if curr.HasConflict() {
				prs[i].ConflictParent = curr.GetNumber()
				break
			}
		}
	}
}

func (data Data) GetTitle() string {
	return data.Primary.Title
}

func (data Data) GetRepoNameWithOwner() string {
	return data.Primary.Repository.NameWithOwner
}

func (data Data) GetNumber() int {
	return data.Primary.Number
}

func (data Data) GetUrl() string {
	return data.Primary.Url
}

func (data Data) GetUpdatedAt() time.Time {
	return data.Primary.UpdatedAt
}

func (data Data) GetCreatedAt() time.Time {
	return data.Primary.CreatedAt
}

// GetFirstFailedCheckUrl walks the last commit's check rollup and returns
// the details_url of the first failed CheckRun (or target_url for status
// contexts). Empty string if the PR isn't enriched, has no last commit,
// or has no failures. Matches the failure detection used by the checks
// stats so what the UI calls "failed" lines up with what we open.
func (data Data) GetFirstFailedCheckUrl() string {
	if !data.IsEnriched {
		return ""
	}
	commits := data.Enriched.Commits.Nodes
	if len(commits) == 0 {
		return ""
	}
	for _, n := range commits[0].Commit.StatusCheckRollup.Contexts.Nodes {
		switch string(n.Typename) {
		case "CheckRun":
			if ghchecks.IsConclusionAFailure(string(n.CheckRun.Conclusion)) &&
				n.CheckRun.DetailsUrl != "" {
				return string(n.CheckRun.DetailsUrl)
			}
		case "StatusContext":
			if ghchecks.IsConclusionAFailure(string(n.StatusContext.State)) &&
				n.StatusContext.TargetUrl != "" {
				return string(n.StatusContext.TargetUrl)
			}
		}
	}
	return ""
}
