package markdown

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"sync"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
)

// dumpRender writes the given rendered output to a per-stage tag file
// when GHD_DUMP_RENDER=1. Used to diagnose code-block bg padding by
// comparing what glamour produces vs. what padCodeBlockLines mutates.
func dumpRender(tag string, width int, rendered string) {
	if os.Getenv("GHD_DUMP_RENDER") != "1" {
		return
	}
	dump := strings.Builder{}
	dump.WriteString(fmt.Sprintf("=== tag=%s width=%d ===\n", tag, width))
	for i, line := range strings.Split(rendered, "\n") {
		visW := lipgloss.Width(line)
		bg := strings.Contains(line, CodeBgMarker)
		literal := strings.ReplaceAll(line, "\x1b", "\\x1b")
		dump.WriteString(fmt.Sprintf("%3d w=%d bg=%v : %s\n", i, visW, bg, literal))
	}
	_ = os.WriteFile("/tmp/gh-dash-render-"+tag+".dump", []byte(dump.String()), 0o644)
}

var markdownStyle *ansi.StyleConfig

func InitializeMarkdownStyle(hasDarkBackground bool) {
	if markdownStyle != nil {
		return
	}
	if hasDarkBackground {
		markdownStyle = &CustomDarkStyleConfig
	} else {
		markdownStyle = &styles.LightStyleConfig
	}
}

func GetMarkdownRenderer(width int) glamour.TermRenderer {
	registerCodeFormatter()
	markdownRenderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(*markdownStyle),
		glamour.WithWordWrap(width),
		glamour.WithChromaFormatter(codeChromaFormatter),
	)
	if err != nil || markdownRenderer == nil {
		// Return a fallback renderer that just returns input unchanged
		fallback, _ := glamour.NewTermRenderer()
		if fallback != nil {
			return *fallback
		}
		// If even fallback fails, panic with helpful message
		panic("failed to create markdown renderer: " + err.Error())
	}

	return *markdownRenderer
}

// Render is a memoizing wrapper around glamour rendering. Activity tab
// re-runs renderActivity on every keystroke, calling glamour for each
// of the PR's comments/reviews/thread-comments. Each glamour render
// can take 100-500ms; a 30-comment PR means seconds per keystroke.
// Caching by (body, width) drops second-and-later renders to a map
// lookup.
//
// The cache is global and lives for the process lifetime. Comment
// bodies are immutable once posted, so the only collisions are
// the same body re-rendered at the same width — desired behavior.
// A comment that gets edited produces a different cache key (new body
// content), so stale renders aren't a concern.
func Render(width int, body string) (string, error) {
	key := cacheKey(width, body)
	cacheMu.RLock()
	if v, ok := cache[key]; ok {
		cacheMu.RUnlock()
		return v, nil
	}
	cacheMu.RUnlock()

	r := GetMarkdownRenderer(width)
	out, err := r.Render(rewriteDetails(body))
	if err != nil {
		return out, err
	}
	dumpRender("pre-pad", width, out)
	out = padCodeBlockLines(out, width)
	dumpRender("post-pad", width, out)

	cacheMu.Lock()
	cache[key] = out
	cacheMu.Unlock()
	return out, nil
}

var (
	cache   = map[uint64]string{}
	cacheMu sync.RWMutex
)

// cacheKey hashes body + width into a single uint64. SHA-256 is overkill
// for cache keys but keeps collisions astronomically rare without
// having to allocate a string-typed key per lookup.
func cacheKey(width int, body string) uint64 {
	h := sha256.New()
	var w [8]byte
	binary.LittleEndian.PutUint64(w[:], uint64(width))
	h.Write(w[:])
	h.Write([]byte(body))
	sum := h.Sum(nil)
	return binary.LittleEndian.Uint64(sum[:8])
}
