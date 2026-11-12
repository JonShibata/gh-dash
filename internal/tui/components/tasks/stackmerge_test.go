package tasks

import (
	"errors"
	"strings"
	"testing"

	"github.com/dlvhdr/gh-dash/v4/internal/stacks"
)

func TestPlanStackMerge(t *testing.T) {
	live := stacks.Info{Stack: 70000, Position: 3, Size: 3, Below: []int{1, 2}}

	tests := []struct {
		name      string
		live      stacks.Info
		liveOK    bool
		confirmed []int
		want      stackPlan
		wantErr   string
	}{
		{"not in a stack", stacks.Info{}, false, nil, planSingle, ""},
		{"nothing open below (bottom or rest merged)", stacks.Info{Stack: 1, Position: 5, Size: 5, Merged: 4}, true, nil, planSingle, ""},
		{"confirmed list still matches", live, true, []int{1, 2}, planStack, ""},
		{"caller showed no stack", live, true, nil, planSingle, "merge it from the PR list"},
		{"stack grew since confirm", live, true, []int{1}, planSingle, "changed since you confirmed"},
		{"stack shrank since confirm", live, true, []int{1, 2, 4}, planSingle, "changed since you confirmed"},
		{"order differs", live, true, []int{2, 1}, planSingle, "changed since you confirmed"},
		{"lookup failed, merge alone", stacks.Info{}, false, []int{1, 2}, planSingle, ""},
	}
	for _, tc := range tests {
		got, err := planStackMerge(3, tc.live, tc.liveOK, tc.confirmed)
		if got != tc.want {
			t.Errorf("%s: plan = %v, want %v", tc.name, got, tc.want)
		}
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("%s: unexpected error %v", tc.name, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("%s: error = %v, want it to contain %q", tc.name, err, tc.wantErr)
		}
	}
}

func TestPlanRefusalNamesThePRs(t *testing.T) {
	_, err := planStackMerge(3, stacks.Info{Stack: 70000, Below: []int{1, 2}}, true, nil)
	if err == nil || !strings.Contains(err.Error(), "PRs #1, #2") {
		t.Fatalf("error = %v, want it to name PRs #1, #2", err)
	}
	_, err = planStackMerge(3, stacks.Info{Stack: 70000, Below: []int{1}}, true, nil)
	if err == nil || !strings.Contains(err.Error(), "PR #1;") {
		t.Fatalf("error = %v, want singular PR #1", err)
	}
}

func TestStackHint(t *testing.T) {
	live := stacks.Info{Stack: 65129, Position: 5, Size: 5, Below: []int{64931}}
	if got := stackHint(live, true, "enqueue failed: Pull request is part of a Stack"); !strings.Contains(got, "stack #65129") ||
		!strings.Contains(got, "PR #64931") || !strings.Contains(got, "gh stack unstack 65129") {
		t.Errorf("hint = %q", got)
	}
	if got := stackHint(live, true, "required status checks are pending"); got != "" {
		t.Errorf("unrelated failure got a hint: %q", got)
	}
	if got := stackHint(stacks.Info{}, false, "part of a stack"); got != "" {
		t.Errorf("no live info should give no hint, got %q", got)
	}
	if got := stackHint(stacks.Info{Stack: 1, Position: 1, Size: 2}, true, "stack"); !strings.Contains(got, "open below: none") {
		t.Errorf("hint with nothing below = %q", got)
	}
}

func TestLiveStackInfo(t *testing.T) {
	orig := fetchStacks
	defer func() { fetchStacks = orig }()

	fetchStacks = func(repo string) ([]stacks.Stack, error) {
		if repo != "o/r" {
			t.Errorf("fetched %q", repo)
		}
		return []stacks.Stack{{Number: 9, Open: true, PRs: []stacks.PR{
			{Number: 1, Merged: true}, {Number: 2}, {Number: 3},
		}}}, nil
	}
	info, ok := liveStackInfo("o/r", 3)
	if !ok || info.Stack != 9 || len(info.Below) != 1 || info.Below[0] != 2 {
		t.Fatalf("info = %+v ok=%v", info, ok)
	}
	if _, ok := liveStackInfo("o/r", 77); ok {
		t.Error("PR outside any stack should not be found")
	}

	fetchStacks = func(string) ([]stacks.Stack, error) { return nil, errors.New("offline") }
	if _, ok := liveStackInfo("o/r", 3); ok {
		t.Error("a failed lookup should report not found, so the PR merges alone")
	}
}
