package markdown

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
)

// codeBgMarker is the truecolor SGR fragment our chroma formatter
// stamps onto every code token. Used as a substring marker on the
// line — without the trailing 'm' so it still matches when chroma
// follows the bg parameter with foreground/style parameters in the
// same SGR sequence (e.g. "\x1b[48;2;234;238;242;38;2;5;80;174m").
//
// #EAEEF2 is GitHub's "neutral.subtle" — slightly darker than the
// PrettyLights default code-block bg (#F6F8FA, which read as too
// light against the dark gh-dash canvas) but lighter than the
// previous attempt at #D8DEE4 (a touch too dark). Paired chroma
// palette lives in theme.go.
const codeBgMarker = "\x1b[48;2;234;238;242"
const codeBgHex = "#EAEEF2"

// codeChromaFormatter is the registered name we hand to glamour via
// WithChromaFormatter. The custom formatter wraps every chroma token
// with our background color so the bg stays visible across all
// whitespace inside a token. Chroma's stock TTY formatters call
// clearBackground (chroma/v2/formatters/tty_indexed.go:226) which
// drops any global background — that's why setting StyleConfig.Chroma.
// Background alone has no effect.
const codeChromaFormatter = "ghdash-codeblock"

var registerOnce sync.Once

func registerCodeFormatter() {
	registerOnce.Do(func() {
		formatters.Register(codeChromaFormatter, chroma.FormatterFunc(formatCodeBackgrounded))
	})
}

// formatCodeBackgrounded mirrors chroma's terminal truecolor formatter
// but always sets our background SGR before each token, and resets
// before every embedded newline so the bg doesn't bleed into the
// terminal's right-of-text gutter on lines without trailing spaces.
// The post-processor pads each line out to width to fill that gutter
// in a controlled way.
func formatCodeBackgrounded(w io.Writer, style *chroma.Style, it chroma.Iterator) error {
	for token := it(); token != chroma.EOF; token = it() {
		entry := style.Get(token.Type)
		var sgr strings.Builder
		sgr.WriteString("\x1b[48;2;234;238;242") // bg #EAEEF2
		if entry.Bold == chroma.Yes {
			sgr.WriteString(";1")
		}
		if entry.Italic == chroma.Yes {
			sgr.WriteString(";3")
		}
		if entry.Underline == chroma.Yes {
			sgr.WriteString(";4")
		}
		// Always emit a foreground. Some token types resolve to a
		// chroma entry without an explicit Colour; falling through to
		// the terminal's default fg (typically light on a dark theme)
		// would render as light-on-light against our light bg. The
		// fallback is GitHub's default text color #1F2328.
		if entry.Colour.IsSet() {
			fmt.Fprintf(&sgr, ";38;2;%d;%d;%d", entry.Colour.Red(), entry.Colour.Green(), entry.Colour.Blue())
		} else {
			sgr.WriteString(";38;2;31;35;40") // #1F2328
		}
		sgr.WriteByte('m')
		prefix := sgr.String()
		value := token.Value
		for {
			i := strings.IndexByte(value, '\n')
			if i < 0 {
				if value != "" {
					fmt.Fprint(w, prefix, value, "\x1b[0m")
				}
				break
			}
			fmt.Fprint(w, prefix, value[:i], "\x1b[0m\n")
			value = value[i+1:]
		}
	}
	return nil
}

// padCodeBlockLines rebuilds each code-block line so the bg covers the
// full wrap width: a 2-col bg-stripe indent on the left, the chroma
// "core" (the styled tokens emitted by formatCodeBackgrounded), and
// bg-colored padding on the right out to wrap width. Both leading
// and trailing wrap-padding that glamour adds (per-cell styled-fg
// spaces with no bg, e.g. `\x1b[38;5;234m \x1b[m` repeats) are
// discarded — that padding leaves no-bg gutters on either side of the
// chroma content if we naively trimmed only spaces.
//
// "Core" is identified as the substring from the FIRST bg marker to
// the END of the last bg-styled run on the line (i.e. the closing
// reset that follows the last bg token). Anything before/after that
// is glamour's wrap framing.
func padCodeBlockLines(rendered string, width int) string {
	if width <= 0 || !strings.Contains(rendered, codeBgMarker) {
		return rendered
	}
	const indent = "  "
	bg := lipgloss.NewStyle().Background(lipgloss.Color(codeBgHex))
	indentStripe := bg.Render(indent)
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		firstBg := strings.Index(line, codeBgMarker)
		if firstBg < 0 {
			continue
		}
		// End of the last bg-styled run on the line: starting from
		// the LAST bg marker, skip its SGR (up through the next 'm'),
		// then find the FIRST reset that follows. Prefer the explicit
		// `\x1b[0m`; fall back to the bare `\x1b[m` form.
		lastBg := strings.LastIndex(line, codeBgMarker)
		afterMarker := line[lastBg+len(codeBgMarker):]
		sgrEnd := strings.IndexByte(afterMarker, 'm')
		if sgrEnd < 0 {
			continue
		}
		tail := afterMarker[sgrEnd+1:]
		var resetEnd int
		if r := strings.Index(tail, "\x1b[0m"); r >= 0 {
			resetEnd = r + len("\x1b[0m")
		} else if r := strings.Index(tail, "\x1b[m"); r >= 0 {
			resetEnd = r + len("\x1b[m")
		} else {
			resetEnd = len(tail)
		}
		coreEnd := lastBg + len(codeBgMarker) + sgrEnd + 1 + resetEnd
		core := line[firstBg:coreEnd]
		coreVisW := lipgloss.Width(core)
		padW := width - len(indent) - coreVisW
		pad := ""
		if padW > 0 {
			pad = bg.Render(strings.Repeat(" ", padW))
		}
		lines[i] = indentStripe + core + pad
	}
	return strings.Join(lines, "\n")
}
