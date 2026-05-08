package markdown

import (
	"fmt"
	"regexp"
	"strings"
)

// detailsRe matches a single <details>…</details> block, capturing the
// optional <summary> text and the inner content. Non-greedy across
// newlines so back-to-back blocks don't merge. Nested <details> inside
// the inner content collapse to the outermost match — acceptable for
// v1; nested disclosure widgets in PR bodies are vanishingly rare.
var detailsRe = regexp.MustCompile(`(?is)<details[^>]*>\s*(?:<summary[^>]*>(.*?)</summary>)?\s*(.*?)</details>`)

// rewriteDetails converts each <details> block into a visually
// delineated section that glamour can render. Pattern:
//
//	---
//	▼ **Summary text**
//
//	body content
//
//	---
//
// The horizontal rules above and below produce the dim grey
// `--------` separator from the theme's HorizontalRule style, so the
// section reads as a bracketed disclosure rather than as inline body
// text. The body content stays as-is (markdown, code fences, lists)
// so glamour renders it normally inside the bracket.
func rewriteDetails(body string) string {
	if !strings.Contains(strings.ToLower(body), "<details") {
		return body
	}
	return detailsRe.ReplaceAllStringFunc(body, func(match string) string {
		sub := detailsRe.FindStringSubmatch(match)
		summary := strings.TrimSpace(stripHTMLTags(sub[1]))
		if summary == "" {
			summary = "Details"
		}
		content := strings.TrimSpace(sub[2])
		if content == "" {
			return fmt.Sprintf("\n\n---\n▼ **%s**\n\n---\n\n", summary)
		}
		return fmt.Sprintf("\n\n---\n▼ **%s**\n\n%s\n\n---\n\n", summary, content)
	})
}

var tagRe = regexp.MustCompile(`<[^>]+>`)

// stripHTMLTags removes HTML markup from a fragment so the summary
// text reads cleanly when bolded. <code>X</code> in a summary
// becomes plain X, etc.
func stripHTMLTags(s string) string {
	return tagRe.ReplaceAllString(s, "")
}
