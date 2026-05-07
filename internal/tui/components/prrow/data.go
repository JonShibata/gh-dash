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
