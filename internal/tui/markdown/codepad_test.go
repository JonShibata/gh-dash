package markdown

import (
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

var mdAnsiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// markdown.Render must never emit a line wider than the requested wrap
// width. A code block whose content fills (or exceeds) the width is the
// regression case: padCodeBlockLines adds a 2-col indent stripe on top of
// glamour's already-width-filled core, pushing the line to width+2. In
// the sidebar that overflow bleeds the code background into the right
// padding gutter ("color bleed").
func TestRenderNeverExceedsWidth(t *testing.T) {
	InitializeMarkdownStyle(true)

	widths := []int{40, 55, 59, 80}
	body := "```go\n" +
		"x := \"" + strings.Repeat("y", 200) + "\"\n" +
		"shortLine := 1\n" +
		"```\n"

	for _, w := range widths {
		out, err := Render(w, body)
		if err != nil {
			t.Fatalf("Render(%d): %v", w, err)
		}
		for i, line := range strings.Split(out, "\n") {
			if vis := lipgloss.Width(line); vis > w {
				t.Errorf("width=%d: line %d is %d cols wide (> %d): %q",
					w, i, vis, w, mdAnsiRe.ReplaceAllString(line, ""))
			}
		}
	}
}
