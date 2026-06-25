package prview

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

// colorizeDiffHunk renders a unified-diff hunk (GitHub's DiffHunk field
// for an inline review thread) with diff coloring so the code context
// above a comment reads like a real diff instead of flat faint text:
//
//	@@ … @@   → warning (hunk header)
//	+added    → success (green)
//	-removed  → error (red)
//	 context  → faint
//	\ No newline at end of file → faint
//
// Every line keeps the leading "│ " rule (rendered faint) so the block
// still indents as quoted context under the thread location header. The
// output has exactly one line per input line, so the caller's
// height/offset bookkeeping is unaffected.
func colorizeDiffHunk(hunk string, t theme.Theme) string {
	hunk = strings.TrimRight(hunk, "\n")
	if hunk == "" {
		return ""
	}

	rule := lipgloss.NewStyle().Foreground(t.FaintText).Render("│ ")
	context := lipgloss.NewStyle().Foreground(t.FaintText)
	added := lipgloss.NewStyle().Foreground(t.SuccessText)
	removed := lipgloss.NewStyle().Foreground(t.ErrorText)
	header := lipgloss.NewStyle().Foreground(t.WarningText).Bold(true)

	lines := strings.Split(hunk, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		var style lipgloss.Style
		switch {
		case strings.HasPrefix(ln, "@@"):
			style = header
		case strings.HasPrefix(ln, "+"):
			style = added
		case strings.HasPrefix(ln, "-"):
			style = removed
		default:
			// Context lines (leading space) and the "\ No newline at end
			// of file" trailer both read as faint.
			style = context
		}
		out = append(out, rule+style.Render(ln))
	}
	return strings.Join(out, "\n")
}
