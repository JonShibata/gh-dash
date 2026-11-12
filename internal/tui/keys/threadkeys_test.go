package keys

import (
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Lowercase r is refresh in every view, so the review-thread reply must not
// take it. R replies (overriding "refresh all" only in the thread scope) and
// ctrl+r replies and resolves.
func TestReviewThreadKeysLeaveRefreshAlone(t *testing.T) {
	r := tea.KeyPressMsg{Code: 'r', Text: "r"}
	shiftR := tea.KeyPressMsg{Code: 'R', Text: "R", Mod: tea.ModShift}
	ctrlR := tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}

	tests := []struct {
		name    string
		binding key.Binding
		msg     tea.KeyPressMsg
		want    bool
	}{
		{"r refreshes", Keys.Refresh, r, true},
		{"R refreshes all outside the thread scope", Keys.RefreshAll, shiftR, true},
		{"r does not reply", PRKeys.ReviewThreadReply, r, false},
		{"R replies", PRKeys.ReviewThreadReply, shiftR, true},
		{"r does not reply and resolve", PRKeys.ReviewThreadReplyResolve, r, false},
		{"R does not reply and resolve", PRKeys.ReviewThreadReplyResolve, shiftR, false},
		{"ctrl+r replies and resolves", PRKeys.ReviewThreadReplyResolve, ctrlR, true},
		{"ctrl+r does not refresh", Keys.Refresh, ctrlR, false},
		{"ctrl+r does not reply", PRKeys.ReviewThreadReply, ctrlR, false},
	}
	for _, tc := range tests {
		if got := key.Matches(tc.msg, tc.binding); got != tc.want {
			t.Errorf("%s: Matches = %v, want %v", tc.name, got, tc.want)
		}
	}
}
