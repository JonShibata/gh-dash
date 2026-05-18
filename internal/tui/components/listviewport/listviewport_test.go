package listviewport

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

func newTestViewport(numItems, height int) Model {
	return NewModel(
		context.ProgramContext{},
		constants.Dimensions{Width: 80, Height: height},
		time.Time{},
		time.Time{},
		"PR",
		numItems,
		1, // listItemHeight
	)
}

func TestSetCurrItemClampsToRange(t *testing.T) {
	m := newTestViewport(10, 20)

	require.Equal(t, 5, m.SetCurrItem(5), "in-range index should be set verbatim")
	require.Equal(t, 9, m.SetCurrItem(100), "above-range index should clamp to last item")
	require.Equal(t, 0, m.SetCurrItem(-3), "negative index should clamp to 0")
}

func TestSetCurrItemScrollsTargetIntoView(t *testing.T) {
	// 3 items per page; target 7 is below the initial visible window and
	// must be scrolled into view.
	m := newTestViewport(10, 3)

	require.Equal(t, 7, m.SetCurrItem(7))
	require.LessOrEqual(t, m.topBoundId, 7, "target should be at/after the top of the viewport")
	require.GreaterOrEqual(t, m.bottomBoundId, 7, "target should be at/before the bottom of the viewport")
}

func TestSetCurrItemClampsAfterShrink(t *testing.T) {
	// Simulates a refetch that shrinks the list: the cursor was deep in the
	// old list, the new list is shorter. SetCurrItem must not leave the
	// cursor out of bounds.
	m := newTestViewport(10, 20)
	m.SetCurrItem(9)

	m.SetNumItems(3)
	require.Equal(t, 2, m.SetCurrItem(9), "should clamp to the new last index")
}
