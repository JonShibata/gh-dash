package prview

import (
	"testing"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func TestStatusPillColor(t *testing.T) {
	ctx := &context.ProgramContext{Theme: *theme.DefaultTheme}
	ctx.Styles = context.InitStyles(ctx.Theme)

	queued := &data.PullRequestData{State: "OPEN", IsInMergeQueue: true}
	auto := &data.PullRequestData{State: "OPEN"}
	auto.AutoMergeRequest.EnabledBy.Login = "me"

	tests := []struct {
		name string
		pr   *data.PullRequestData
		want any
	}{
		{"queued", queued, ctx.Theme.WarningText.Dark},
		{"auto-merge", auto, ctx.Theme.WarningText.Dark},
		{"queued draft", &data.PullRequestData{State: "OPEN", IsDraft: true, IsInMergeQueue: true}, ctx.Theme.WarningText.Dark},
		{"draft", &data.PullRequestData{State: "OPEN", IsDraft: true}, ctx.Theme.FaintText.Dark},
		{"open", &data.PullRequestData{State: "OPEN"}, ctx.Styles.Colors.OpenPR.Dark},
		{"closed", &data.PullRequestData{State: "CLOSED"}, ctx.Styles.Colors.ClosedPR.Dark},
		{"merged", &data.PullRequestData{State: "MERGED"}, ctx.Styles.Colors.MergedPR.Dark},
	}
	for _, tc := range tests {
		if got := statusPillColor(ctx, tc.pr); got != tc.want {
			t.Errorf("%s: color = %v, want %v", tc.name, got, tc.want)
		}
	}
	if ctx.Theme.WarningText.Dark == ctx.Styles.Colors.OpenPR.Dark {
		t.Fatal("test is meaningless: warning and open colours are equal")
	}
}
