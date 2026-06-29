package prview

import (
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// hasBackgroundSGR reports whether s contains a background-color SGR
// (48;2;… truecolor or 48;5;… 256-color or [48m), i.e. the line is filled
// with a background rather than just a foreground.
func hasBackgroundSGR(s string) bool {
	return strings.Contains(s, "48;2;") || strings.Contains(s, "48;5;") || strings.Contains(s, "[48m")
}

func TestColorizeDiffHunkEmpty(t *testing.T) {
	require.Equal(t, "", colorizeDiffHunk("", 80, theme.DefaultTheme))
	require.Equal(t, "", colorizeDiffHunk("\n\n", 80, theme.DefaultTheme))
}

// One output line per input line (so the caller's height/offset
// bookkeeping is unaffected), and the original text is preserved at the
// start of each row (the rest is background padding).
func TestColorizeDiffHunkPreservesTextAndLineCount(t *testing.T) {
	hunk := "@@ -1,3 +1,3 @@ func foo() {\n context line\n-removed\n+added\n\\ No newline at end of file"

	out := colorizeDiffHunk(hunk, 80, theme.DefaultTheme)
	gotLines := strings.Split(out, "\n")
	wantStarts := []string{
		"@@ -1,3 +1,3 @@ func foo() {",
		" context line",
		"-removed",
		"+added",
		"\\ No newline at end of file",
	}
	require.Equal(t, len(wantStarts), len(gotLines), "one row per input line")
	for i, want := range wantStarts {
		require.True(t, strings.HasPrefix(strings.TrimRight(stripANSI(gotLines[i]), " "), want),
			"row %d visible text should start with %q, got %q", i, want, stripANSI(gotLines[i]))
	}
}

// Added / removed / context / header lines must each be BACKGROUND-filled
// and route to distinct styles when the environment renders color.
func TestColorizeDiffHunkBackgroundAndDistinctStyling(t *testing.T) {
	add := colorizeDiffHunk("+added", 40, theme.DefaultTheme)
	del := colorizeDiffHunk("-removed", 40, theme.DefaultTheme)
	ctxLine := colorizeDiffHunk(" context", 40, theme.DefaultTheme)
	hdr := colorizeDiffHunk("@@ -1 +1 @@", 40, theme.DefaultTheme)

	if strings.Contains(add+del+ctxLine+hdr, "\x1b") {
		require.True(t, hasBackgroundSGR(add), "added line should be background-filled")
		require.True(t, hasBackgroundSGR(del), "removed line should be background-filled")
		require.True(t, hasBackgroundSGR(hdr), "hunk header should be background-filled")
		require.NotEqual(t, add, del, "added vs removed should differ")
		require.NotEqual(t, add, ctxLine, "added vs context should differ")
		require.NotEqual(t, hdr, ctxLine, "header vs context should differ")
	}
}

// Each row is padded with its background out to the requested width.
func TestColorizeDiffHunkFillsWidth(t *testing.T) {
	out := colorizeDiffHunk("+x", 20, theme.DefaultTheme)
	require.Equal(t, 20, lipgloss.Width(stripANSI(out)))
}
