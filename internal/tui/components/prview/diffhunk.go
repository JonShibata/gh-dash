package prview

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// GitHub light diff palette, matching the code-block colors already used
// in the sidebar. Each line is rendered as a full-width row with a
// BACKGROUND fill so the hunk reads like a real diff (green = added,
// red = removed) rather than relying on faint foreground text.
var (
	diffAddStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#116329")).
			Background(lipgloss.Color("#DAFBE1"))
	diffDelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#82071E")).
			Background(lipgloss.Color("#FFEBE9"))
	diffHdrStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#0550AE")).
			Background(lipgloss.Color("#DDF4FF"))
	diffCtxStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#24292F")).
			Background(lipgloss.Color("#EAEEF2"))
)

// colorizeDiffHunk renders a unified-diff hunk (GitHub's DiffHunk field
// for an inline review thread) as a stack of background-filled rows:
//
//	@@ … @@   → blue row (hunk header)
//	+added    → green row
//	-removed  → red row
//	 context  → neutral row
//
// Each input line becomes exactly one output line, filled to `width` so
// the background spans the row; long lines are truncated rather than
// wrapped so the caller's height/offset bookkeeping is unaffected.
func colorizeDiffHunk(hunk string, width int) string {
	hunk = strings.TrimRight(hunk, "\n")
	if hunk == "" {
		return ""
	}
	if width < 1 {
		width = 1
	}

	lines := strings.Split(hunk, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		var style lipgloss.Style
		switch {
		case strings.HasPrefix(ln, "@@"):
			style = diffHdrStyle
		case strings.HasPrefix(ln, "+"):
			style = diffAddStyle
		case strings.HasPrefix(ln, "-"):
			style = diffDelStyle
		default:
			// Context lines (leading space) and the "\ No newline at end
			// of file" trailer get the neutral row.
			style = diffCtxStyle
		}
		// Truncate to width first (no wrap → one row per input line),
		// then fill the background out to the full width.
		content := lipgloss.NewStyle().MaxWidth(width).Render(ln)
		out = append(out, style.Width(width).Render(content))
	}
	return strings.Join(out, "\n")
}
