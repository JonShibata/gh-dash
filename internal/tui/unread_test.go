package tui

import (
	"testing"
	"time"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

func TestLatestActivity(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 11, 20, h, 0, 0, 0, time.UTC) }

	var e data.EnrichedPullRequestData
	if !latestActivity(&e).IsZero() {
		t.Fatal("empty PR should have no activity")
	}

	e.Comments.Nodes = []data.Comment{{CreatedAt: at(10)}}
	e.ReviewThreads.Nodes = []data.ReviewThread{
		{Comments: data.ReviewComments{Nodes: []data.ReviewComment{{CreatedAt: at(9)}, {CreatedAt: at(12)}}}},
	}
	e.Reviews.Nodes = []data.Review{{CreatedAt: at(8), UpdatedAt: at(11)}}

	if got := latestActivity(&e); !got.Equal(at(12)) {
		t.Fatalf("latest = %v, want %v (review thread reply)", got, at(12))
	}

	e.Reviews.Nodes = append(e.Reviews.Nodes, data.Review{CreatedAt: at(7), UpdatedAt: at(13)})
	if got := latestActivity(&e); !got.Equal(at(13)) {
		t.Fatalf("latest = %v, want %v (review updated)", got, at(13))
	}
}
