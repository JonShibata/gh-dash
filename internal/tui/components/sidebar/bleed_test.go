package sidebar

import (
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/markdown"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

var sgrRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func visWidth(s string) int { return lipgloss.Width(s) }

// lineHasOpenBackground reports whether a line opens a background SGR
// (48;2;… truecolor or 48;5;…) that is NOT closed by a reset before the
// end of the line — the classic "color bleed" signature.
func lineHasOpenBackground(line string) bool {
	lastBg := -1
	if i := strings.LastIndex(line, "48;2;"); i > lastBg {
		lastBg = i
	}
	if i := strings.LastIndex(line, "48;5;"); i > lastBg {
		lastBg = i
	}
	if lastBg < 0 {
		return false
	}
	lastReset := strings.LastIndex(line, "\x1b[0m")
	if i := strings.LastIndex(line, "\x1b[m"); i > lastReset {
		lastReset = i
	}
	return lastBg > lastReset
}

func testCtx(t *testing.T) *context.ProgramContext {
	t.Helper()
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../../../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	thm := theme.ParseTheme(&cfg)
	return &context.ProgramContext{
		Config:              &cfg,
		Theme:               thm,
		Styles:              context.InitStyles(thm),
		ScreenWidth:         120,
		ScreenHeight:        40,
		MainContentWidth:    60,
		MainContentHeight:   38,
		DynamicPreviewWidth: 60,
		PreviewPosition:     "right",
	}
}

// auditFrame logs every offending line and returns counts of (over-wide,
// open-background) lines.
func auditFrame(t *testing.T, label string, frame string, maxWidth int) (overWide, openBg int) {
	t.Helper()
	for i, line := range strings.Split(frame, "\n") {
		w := visWidth(line)
		if w > maxWidth {
			overWide++
			t.Logf("[%s] line %d OVER-WIDE: vis=%d > max=%d : %q", label, i, w, maxWidth, sgrRe.ReplaceAllString(line, ""))
		}
		if lineHasOpenBackground(line) {
			openBg++
			t.Logf("[%s] line %d OPEN-BG (no reset before EOL): %q", label, i, strings.ReplaceAll(line, "\x1b", "\\x1b"))
		}
	}
	return overWide, openBg
}

// TestSidebarRenderBleed drives the real sidebar through a tall→short
// content switch and audits the emitted frame for the color-bleed
// signatures. Diagnostic-first: it logs offenders and asserts the
// invariants the renderer should hold.
func TestSidebarRenderBleed(t *testing.T) {
	ctx := testCtx(t)
	m := NewModel()
	m.UpdateProgramContext(ctx)
	m.IsOpen = true

	contentW := m.GetSidebarContentWidth()
	t.Logf("sidebar content width = %d, viewport w=%d h=%d", contentW, m.viewport.Width(), m.viewport.Height())

	bg := lipgloss.NewStyle().
		Background(lipgloss.Color("#EAEEF2")).
		Foreground(lipgloss.Color("#000000"))

	// Tall PR A: mix of full-width background rows (like code blocks),
	// an over-wide line, and short plain lines.
	var aLines []string
	for i := 0; i < 50; i++ {
		switch i % 3 {
		case 0:
			aLines = append(aLines, bg.Width(contentW).Render("AAAA code line"))
		case 1:
			aLines = append(aLines, strings.Repeat("A", contentW+40)) // deliberately over-wide
		default:
			aLines = append(aLines, "short A line")
		}
	}
	contentA := strings.Join(aLines, "\n")

	m.SetContent(contentA)
	m.ScrollToBottom()
	frameA := m.View()
	owA, bgA := auditFrame(t, "A", frameA, ctx.DynamicPreviewWidth)
	t.Logf("A: overWide=%d openBg=%d", owA, bgA)

	// Short PR B.
	contentB := "short B line 1\nshort B line 2"
	m.SetContent(contentB)
	frameB := m.View()
	owB, bgB := auditFrame(t, "B", frameB, ctx.DynamicPreviewWidth)
	t.Logf("B: overWide=%d openBg=%d", owB, bgB)

	// Content C: an over-wide line that carries a background and a
	// trailing reset (mimics a code-block line longer than the wrap
	// width, which padCodeBlockLines leaves over-wide). Truncating it to
	// the sidebar width must NOT drop its closing reset.
	overWideBg := bg.Render(strings.Repeat("Z", contentW+40))
	t.Logf("C raw over-wide bg line vis=%d, hasOpenBgBeforeTrunc=%v", visWidth(overWideBg), lineHasOpenBackground(overWideBg))
	m.SetContent("plain C\n" + overWideBg + "\nplain C2")
	frameC := m.View()
	owC, bgC := auditFrame(t, "C", frameC, ctx.DynamicPreviewWidth)
	t.Logf("C: overWide=%d openBg=%d", owC, bgC)
	if bgC > 0 {
		t.Errorf("C frame has %d line(s) with an unreset background after truncation — color bleed", bgC)
	}
	if owC > 0 {
		t.Errorf("C frame has %d over-wide line(s) — overflow/bleed", owC)
	}

	// Content D: REAL markdown render of a code block whose line exceeds
	// the wrap width — the exact path that generates background SGR via
	// chroma + padCodeBlockLines.
	markdown.InitializeMarkdownStyle(true)
	longCode := "```go\n" + "x := \"" + strings.Repeat("y", 140) + "\"\n" + "short := 1\n" + "```"
	mdOut, err := markdown.Render(contentW, longCode)
	if err != nil {
		t.Fatalf("markdown.Render: %v", err)
	}
	// Audit the raw markdown output first (pre-sidebar).
	owMD, bgMD := auditFrame(t, "MD-raw", mdOut, contentW)
	t.Logf("MD-raw: overWide=%d openBg=%d", owMD, bgMD)
	m.SetContent("intro\n" + mdOut + "\noutro")
	frameD := m.View()
	owD, bgD := auditFrame(t, "D(sidebar)", frameD, ctx.DynamicPreviewWidth)
	t.Logf("D(sidebar): overWide=%d openBg=%d", owD, bgD)
	if bgD > 0 {
		t.Errorf("D frame has %d line(s) with unreset background — color bleed from real code block", bgD)
	}
	if owD > 0 {
		t.Errorf("D frame has %d over-wide line(s) from real code block", owD)
	}

	// Residual check: B's frame must not contain A's content.
	if strings.Contains(sgrRe.ReplaceAllString(frameB, ""), "AAAA code line") ||
		strings.Contains(sgrRe.ReplaceAllString(frameB, ""), "short A line") {
		t.Errorf("B frame contains leftover content from A (stale viewport)")
	}

	// Invariants the renderer should hold (these are the bleed causes):
	if owB > 0 {
		t.Errorf("B frame has %d line(s) wider than the sidebar (%d) — overflow/bleed", owB, ctx.DynamicPreviewWidth)
	}
	if bgB > 0 {
		t.Errorf("B frame has %d line(s) with an unreset background — color bleed", bgB)
	}
}
