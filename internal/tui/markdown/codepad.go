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
// same SGR sequence (e.g. "\x1b[48;2;246;248;250;38;2;5;80;174m").
//
// Match GitHub Web's code-block presentation: light bg (#F6F8FA, the
// `bgColor.muted` from PrettyLights) with dark navy/blue/red fg
// tokens. Earlier attempts at dark-on-dark variants
// (#373737 → #808080) all suffered from either bg blending with the
// terminal canvas or chroma fg colors being too dim against grey;
// the user requested matching github.com directly. Paired chroma fg
// palette lives in theme.go (PrettyLights light syntax colors).
const codeBgMarker = "\x1b[48;2;246;248;250"
const codeBgHex = "#F6F8FA"

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
		sgr.WriteString("\x1b[48;2;246;248;250") // bg #F6F8FA (GitHub light)
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

// padCodeBlockLines extends the code-block background out to the wrap
// width on every code-block line, and prepends a 2-column bg-colored
// indent stripe. The marker for "this line is part of a code block" is
// the presence of codeBgMarker — emitted by formatCodeBackgrounded on
// every chroma token. Glamour's own indent comes from the parent
// block's style (Document/Paragraph) which has no bg, so we suppress
// glamour-side indentation in the theme and reproduce it here.
func padCodeBlockLines(rendered string, width int) string {
	if width <= 0 || !strings.Contains(rendered, codeBgMarker) {
		return rendered
	}
	const indent = "  "
	bg := lipgloss.NewStyle().Background(lipgloss.Color(codeBgHex))
	indentStripe := bg.Render(indent)
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		if !strings.Contains(line, codeBgMarker) {
			continue
		}
		// Glamour already pads code-block lines out to the wrap
		// width with plain (uncolored) spaces. Strip those, then
		// re-pad with bg-colored spaces so the trailing gutter
		// also reads as part of the block.
		trimmed := strings.TrimRight(line, " ")
		w := lipgloss.Width(trimmed) + len(indent)
		pad := ""
		if w < width {
			pad = bg.Render(strings.Repeat(" ", width-w))
		}
		lines[i] = indentStripe + trimmed + pad
	}
	return strings.Join(lines, "\n")
}
