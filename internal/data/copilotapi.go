package data

import (
	"fmt"
	"strings"
	"time"
)

const (
	timelinePageSize = 100
	timelineMaxPages = 10
)

// CopilotWork is the state of the latest Copilot review cycle on a PR.
type CopilotWork struct {
	// RequestedAt is when Copilot was last asked to review.
	RequestedAt time.Time
	// StartedAt is when Copilot began that review; zero while it is still queued.
	StartedAt time.Time
}

type timelineEvent struct {
	Event             string    `json:"event"`
	CreatedAt         time.Time `json:"created_at"`
	RequestedReviewer struct {
		Login string `json:"login"`
	} `json:"requested_reviewer"`
}

// FetchCopilotWork reads the REST issue timeline, the only API that exposes
// `copilot_work_started` (GraphQL has no matching event type).
func FetchCopilotWork(repoNameWithOwner string, number int) (CopilotWork, error) {
	var work CopilotWork
	client, err := getRESTClient()
	if err != nil {
		return work, err
	}

	for page := 1; page <= timelineMaxPages; page++ {
		var events []timelineEvent
		path := fmt.Sprintf("repos/%s/issues/%d/timeline?per_page=%d&page=%d",
			repoNameWithOwner, number, timelinePageSize, page)
		if err := client.Get(path, &events); err != nil {
			return work, err
		}
		for _, e := range events {
			switch e.Event {
			case "review_requested":
				if strings.HasPrefix(strings.ToLower(e.RequestedReviewer.Login), "copilot") {
					work.RequestedAt = e.CreatedAt
				}
			case "copilot_work_started":
				work.StartedAt = e.CreatedAt
			}
		}
		if len(events) < timelinePageSize {
			break
		}
	}
	return work, nil
}
