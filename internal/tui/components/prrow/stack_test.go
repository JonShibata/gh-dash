package prrow

import (
	"testing"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

func stackPR(num int, head, base string, state data.MergeStateStatus) Data {
	p := &data.PullRequestData{Number: num, HeadRefName: head, BaseRefName: base, MergeStateStatus: state}
	p.Repository.NameWithOwner = "o/r"
	return Data{Primary: p}
}

func TestMarkStackConflicts(t *testing.T) {
	prs := []Data{
		stackPR(1, "a", "master", "DIRTY"),
		stackPR(2, "b", "a", "CLEAN"),
		stackPR(3, "c", "b", "CLEAN"),
		stackPR(4, "d", "master", "CLEAN"),
	}
	MarkStackConflicts(prs)
	want := []int{0, 1, 1, 0}
	for i, w := range want {
		if prs[i].ConflictParent != w {
			t.Errorf("PR %d: ConflictParent = %d, want %d", prs[i].GetNumber(), prs[i].ConflictParent, w)
		}
	}
}

func TestMergeStatePrefersEnriched(t *testing.T) {
	d := stackPR(1, "a", "master", "BLOCKED")
	d.IsEnriched = true
	d.Enriched.MergeStateStatus = "CLEAN"
	if got := d.MergeState(); got != "CLEAN" {
		t.Errorf("MergeState = %q, want CLEAN", got)
	}
}
