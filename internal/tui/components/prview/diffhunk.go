package prview

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

// diffStyles holds the four row styles for a diff hunk, built from the
// active theme. Text is uniform (DiffText); only the BACKGROUND
// distinguishes added (green) / removed (red) / hunk header (blue) /
// context (neutral). Each line is rendered as a full-width row so the
// background fill reads like a real diff.
type diffStyles struct {
	add, del, hdr, ctx lipgloss.Style
}

func newDiffStyles(th *theme.Theme) diffStyles {
	base := lipgloss.NewStyle().Foreground(th.DiffText)
	return diffStyles{
		add: base.Background(th.DiffAddedBg),
		del: base.Background(th.DiffRemovedBg),
		hdr: base.Background(th.DiffHeaderBg),
		ctx: base.Background(th.DiffContextBg),
	}
}

// colorizeDiffHunk renders a unified-diff hunk (GitHub's DiffHunk field
// for an inline review thread) as a stack of background-filled rows:
//
//	@@ … @@   → blue row (hunk header)
//	+added    → green row
//	-removed  → red row
//	 context  → neutral row
//
// Colors come from the theme (adaptive light/dark, config-overridable).
// Each input line becomes exactly one output line, filled to `width` so
// the background spans the row; long lines are truncated rather than
// wrapped so the caller's height/offset bookkeeping is unaffected.
func colorizeDiffHunk(hunk string, width int, th *theme.Theme) string {
	hunk = strings.TrimRight(hunk, "\n")
	if hunk == "" {
		return ""
	}
	if width < 1 {
		width = 1
	}

	styles := newDiffStyles(th)
	lines := strings.Split(hunk, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		var style lipgloss.Style
		switch {
		case strings.HasPrefix(ln, "@@"):
			style = styles.hdr
		case strings.HasPrefix(ln, "+"):
			style = styles.add
		case strings.HasPrefix(ln, "-"):
			style = styles.del
		default:
			// Context lines (leading space) and the "\ No newline at end
			// of file" trailer get the neutral row.
			style = styles.ctx
		}
		// Truncate to width first (no wrap → one row per input line),
		// then fill the background out to the full width.
		content := lipgloss.NewStyle().MaxWidth(width).Render(ln)
		out = append(out, style.Width(width).Render(content))
	}
	return strings.Join(out, "\n")
}
