package footer

import "testing"

// Columns are 10 chars wide; separator is 4 spaces (the bubbles default).
const (
	testCol = "0123456789"
	testSep = "    "
)

func TestPackHelpRows(t *testing.T) {
	cols := []string{testCol, testCol, testCol, testCol, testCol, testCol}
	sepWidth := len(testSep)

	tests := []struct {
		name     string
		width    int
		wantRows int
	}{
		// 6 cols * 10 + 5 seps * 4 = 80 fits exactly on one row.
		{"all fit on one row", 80, 1},
		// 3 cols (10+4+10+4+10=38) fit; a 4th (+4+10=52) overflows → 2 rows.
		{"wraps to two rows", 40, 2},
		// Only one column fits per row (10 ok, +14 for a second overflows).
		{"one column per row", 13, 6},
		// Unbounded width keeps everything on a single row.
		{"unbounded single row", 0, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := packHelpRows(cols, testSep, sepWidth, tt.width)
			if len(rows) != tt.wantRows {
				t.Errorf("width %d: got %d rows, want %d", tt.width, len(rows), tt.wantRows)
			}
		})
	}
}

// A column wider than the available width must still appear (alone on its row),
// never be dropped — that was the original truncation bug.
func TestPackHelpRowsOversizeColumnNotDropped(t *testing.T) {
	wide := "this-single-column-is-wider-than-width"
	cols := []string{wide, testCol}

	rows := packHelpRows(cols, testSep, len(testSep), 10)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (oversize column on its own row)", len(rows))
	}
}
