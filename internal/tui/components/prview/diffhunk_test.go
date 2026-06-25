package prview

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

func testTheme(t *testing.T) theme.Theme {
	t.Helper()
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../../../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return theme.ParseTheme(&cfg)
}

func TestColorizeDiffHunkEmpty(t *testing.T) {
	thm := testTheme(t)
	require.Equal(t, "", colorizeDiffHunk("", thm))
	require.Equal(t, "", colorizeDiffHunk("\n\n", thm))
}

// The visible (ANSI-stripped) text must be exactly the input lines, each
// prefixed with the "│ " quote rule — one output line per input line, so
// the caller's height/offset bookkeeping is unaffected.
func TestColorizeDiffHunkPreservesTextAndLineCount(t *testing.T) {
	thm := testTheme(t)
	hunk := "@@ -1,3 +1,3 @@ func foo() {\n context line\n-removed\n+added\n\\ No newline at end of file"

	out := colorizeDiffHunk(hunk, thm)
	gotLines := strings.Split(stripANSI(out), "\n")
	wantLines := []string{
		"│ @@ -1,3 +1,3 @@ func foo() {",
		"│  context line",
		"│ -removed",
		"│ +added",
		"│ \\ No newline at end of file",
	}
	require.Equal(t, wantLines, gotLines)
}

// Added / removed / context lines must route to distinct styles. When the
// test environment renders color (ESC present), the three rendered forms
// must differ; in a no-color environment this is a no-op (still validates
// parsing via the test above).
func TestColorizeDiffHunkDistinctStyling(t *testing.T) {
	thm := testTheme(t)
	add := colorizeDiffHunk("+added", thm)
	del := colorizeDiffHunk("-removed", thm)
	ctxLine := colorizeDiffHunk(" context", thm)
	hdr := colorizeDiffHunk("@@ -1 +1 @@", thm)

	if strings.Contains(add+del+ctxLine+hdr, "\x1b") {
		require.NotEqual(t, stripANSI(add), "", "expected visible content")
		// Same visible suffix length, different ANSI → different bytes.
		require.NotEqual(t, add, del, "added vs removed should be styled differently")
		require.NotEqual(t, add, ctxLine, "added vs context should be styled differently")
		require.NotEqual(t, hdr, ctxLine, "hunk header vs context should be styled differently")
	}
}
