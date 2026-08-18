// Package overlay renders floating, bordered cards that the main view
// composites as their own lipgloss layers. Two presets share one card
// style so status feedback and confirmations read as one system:
//
//   - Toast: a compact status card (hotkey feedback) that floats near a
//     screen corner. Because it is its own layer it is never truncated by
//     the footer's fixed chrome, which was the root cause of long status
//     messages (URLs, errors) getting clipped.
//   - Modal: a centered confirmation dialog with an accent border and a
//     title, so "the app is asking me something" is unmistakable.
package overlay

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

// toastMaxWidth caps the status card so a long URL wraps inside the card
// instead of running off-screen.
const toastMaxWidth = 56

// modalMinWidth keeps short confirmations from rendering as a cramped box.
const modalMinWidth = 42

// card is the shared bordered surface. accent colors the rounded border;
// the interior is filled opaquely so the card reads as a solid element when
// composited over the UI. background matches the app's selected-row surface
// so it never looks like a hole punched in the screen.
func card(ctx *context.ProgramContext, accent color.Color) lipgloss.Style {
	bg := ctx.Theme.SelectedBackground
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		BorderBackground(bg).
		Background(bg).
		Padding(0, 1)
}

// Toast renders a status card: an accent-colored icon followed by the
// message. The caller pre-colors the icon (a spinner while running, a green
// check on success, a red cross on error); accent colors only the border.
func Toast(ctx *context.ProgramContext, icon string, accent color.Color, message string) string {
	bg := ctx.Theme.SelectedBackground

	// Snug for short messages, wrapped (not truncated) for long ones.
	msgWidth := lipgloss.Width(message)
	cap := ctx.ScreenWidth - 8
	if cap > toastMaxWidth {
		cap = toastMaxWidth
	}
	if cap > 0 && msgWidth > cap {
		msgWidth = cap
	}

	msg := lipgloss.NewStyle().
		Foreground(ctx.Theme.PrimaryText).
		Background(bg).
		Width(msgWidth).
		Render(message)
	ic := lipgloss.NewStyle().
		Background(bg).
		Render(icon + " ")

	body := lipgloss.JoinHorizontal(lipgloss.Top, ic, msg)
	return card(ctx, accent).Render(body)
}

// Modal renders a centered confirmation dialog. title sits at the top in
// the accent color; body is the prompt (which may embed a live text input);
// hint is a faint key-legend line. accent colors the border and title so
// the dialog stands apart from the content behind it.
func Modal(ctx *context.ProgramContext, title string, accent color.Color, body, hint string) string {
	bg := ctx.Theme.SelectedBackground

	titleLine := lipgloss.NewStyle().
		Foreground(accent).
		Background(bg).
		Bold(true).
		Render(title)
	bodyLine := lipgloss.NewStyle().
		Background(bg).
		Foreground(ctx.Theme.PrimaryText).
		Render(body)

	parts := []string{titleLine, "", bodyLine}
	if hint != "" {
		parts = append(parts, "", lipgloss.NewStyle().
			Foreground(ctx.Theme.FaintText).
			Background(bg).
			Render(hint))
	}
	inner := lipgloss.JoinVertical(lipgloss.Left, parts...)

	width := lipgloss.Width(inner)
	if width < modalMinWidth {
		width = modalMinWidth
	}
	if max := ctx.ScreenWidth - 8; max > 0 && width > max {
		width = max
	}

	return card(ctx, accent).Width(width).Render(inner)
}
