package prview

import "testing"

func TestMergeHeadline(t *testing.T) {
	tests := []struct {
		name       string
		in         mergeInputs
		title, sub string
		wantKind   mergeKind
	}{
		{"clean", mergeInputs{state: "CLEAN"}, "Ready to merge", "All requirements are met", mergeReady},
		{"conflict beats blocked", mergeInputs{state: "BLOCKED", conflict: true},
			"This branch has conflicts that must be resolved", "", mergeBlocked},
		{"parent conflict beats clean", mergeInputs{state: "CLEAN", conflictParent: 7},
			"A pull request below this one in the stack has conflicts",
			"Resolve the conflicts in #7 first", mergeBlocked},
		{"blocked, checks failing", mergeInputs{state: "BLOCKED", enriched: true,
			checks: checksStats{failed: 2}}, "Merging is blocked", "2 checks failing", mergeBlocked},
		{"blocked, checks pending", mergeInputs{state: "BLOCKED", enriched: true,
			checks: checksStats{inProgress: 1}}, "Merging is blocked", "1 checks pending", mergeBlocked},
		{"blocked, needs approval", mergeInputs{state: "BLOCKED", reviewDecision: "REVIEW_REQUIRED"},
			"Merging is blocked", "Needs approval", mergeBlocked},
		{"blocked, approved, no cause", mergeInputs{state: "BLOCKED", reviewDecision: "APPROVED"},
			"Merging is blocked", "Blocked by repository rules", mergeBlocked},
		{"behind", mergeInputs{state: "BEHIND"},
			"This branch is out of date with the base branch", "", mergeBlocked},
		{"draft", mergeInputs{state: "BLOCKED", isDraft: true},
			"This pull request is still a work in progress", "Draft pull requests cannot be merged", mergeDraft},
		{"unknown", mergeInputs{}, "Checking merge status", "", mergeWaiting},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, sub, kind := mergeHeadline(tt.in)
			if title != tt.title || sub != tt.sub || kind != tt.wantKind {
				t.Errorf("got (%q, %q, %v), want (%q, %q, %v)",
					title, sub, kind, tt.title, tt.sub, tt.wantKind)
			}
		})
	}
}
