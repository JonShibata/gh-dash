package prview

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	gh "github.com/cli/go-gh/v2/pkg/api"
)

// Default image viewer template. {path} is substituted with the
// downloaded tempfile. `--hold` makes kitten icat wait for any keypress
// (q works) before exiting, so the user can dismiss the image and
// return to the TUI.
const defaultImageViewer = "kitten icat --hold {path}"

// imageHint pairs a URL with the hint label the user types to view it.
// Alt is captured for future surfacing (currently unused in the
// rendered output beyond the existing markdown text).
type imageHint struct {
	Label string
	URL   string
	Alt   string
}

// ImageReadyMsg is emitted by DownloadImageCmd when a tempfile is ready
// (or when the download failed). Path is empty on error.
type ImageReadyMsg struct {
	URL  string
	Path string
	Err  error
}

var (
	// Standard markdown image. Optional title (`"…"`) is tolerated.
	mdImageRe = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	// HTML <img>. Case-insensitive; src may appear before or after other attrs.
	htmlImageRe = regexp.MustCompile(`(?i)<img[^>]*\bsrc\s*=\s*["']([^"']+)["'][^>]*/?>`)
)

// extractImages walks bodies in order, returning ordered, deduped
// image references. The first occurrence of each URL determines its
// position (and therefore its hint label).
func extractImages(bodies []string) []imageHint {
	seen := map[string]bool{}
	var out []imageHint
	add := func(url, alt string) {
		url = strings.TrimSpace(url)
		if url == "" || seen[url] {
			return
		}
		seen[url] = true
		out = append(out, imageHint{URL: url, Alt: alt})
	}
	for _, body := range bodies {
		type hit struct {
			start    int
			url, alt string
		}
		var hits []hit
		for _, sm := range mdImageRe.FindAllStringSubmatchIndex(body, -1) {
			hits = append(hits, hit{start: sm[0], alt: body[sm[2]:sm[3]], url: body[sm[4]:sm[5]]})
		}
		for _, sm := range htmlImageRe.FindAllStringSubmatchIndex(body, -1) {
			hits = append(hits, hit{start: sm[0], alt: "image", url: body[sm[2]:sm[3]]})
		}
		sort.Slice(hits, func(i, j int) bool { return hits[i].start < hits[j].start })
		for _, h := range hits {
			add(h.url, h.alt)
		}
	}
	labels := assignHintLabels(len(out))
	for i := range out {
		out[i].Label = labels[i]
	}
	return out
}

// assignHintLabels returns n uniform-width labels.
//   - n ≤ 10 → single chars 1 2 … 9 0
//   - n > 10 → 2-char (digit + letter) "1a" "1b" … "0z" (260 slots)
//
// Putting a digit in the prefix position keeps two-char hints from
// colliding with the letter-heavy PR keymap; the dispatcher only enters
// the "waiting for second char" state when the first char is a digit
// that no other binding owns.
func assignHintLabels(n int) []string {
	if n <= 0 {
		return nil
	}
	digits := []rune("1234567890")
	if n <= len(digits) {
		out := make([]string, n)
		for i := 0; i < n; i++ {
			out[i] = string(digits[i])
		}
		return out
	}
	letters := []rune("abcdefghijklmnopqrstuvwxyz")
	out := make([]string, 0, n)
	for _, d := range digits {
		for _, l := range letters {
			out = append(out, string([]rune{d, l}))
			if len(out) == n {
				return out
			}
		}
	}
	return out
}

// rewriteBodyWithHints prepends a bold **[hint]** marker to every
// recognized image so the user can see which key opens which image.
// HTML <img> tags are also converted to markdown so glamour renders
// them as visible image links rather than dropping the raw HTML.
func rewriteBodyWithHints(body string, urlToHint map[string]string) string {
	if len(urlToHint) == 0 {
		return body
	}
	body = mdImageRe.ReplaceAllStringFunc(body, func(match string) string {
		sub := mdImageRe.FindStringSubmatch(match)
		url := strings.TrimSpace(sub[2])
		hint, ok := urlToHint[url]
		if !ok {
			return match
		}
		return fmt.Sprintf("**[%s]** %s", hint, match)
	})
	body = htmlImageRe.ReplaceAllStringFunc(body, func(match string) string {
		sub := htmlImageRe.FindStringSubmatch(match)
		url := strings.TrimSpace(sub[1])
		hint, ok := urlToHint[url]
		if !ok {
			return match
		}
		return fmt.Sprintf("**[%s]** ![image](%s)", hint, url)
	})
	return body
}

// DownloadImageCmd fetches url to a tempfile using gh's authenticated
// HTTP client (so private user-attachments URLs work). The returned
// Cmd's tea.Msg is ImageReadyMsg, consumed by ui.go to launch the
// viewer.
func DownloadImageCmd(url string) tea.Cmd {
	return func() tea.Msg {
		client, err := gh.DefaultHTTPClient()
		if err != nil {
			return ImageReadyMsg{URL: url, Err: err}
		}
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return ImageReadyMsg{URL: url, Err: err}
		}
		resp, err := client.Do(req)
		if err != nil {
			return ImageReadyMsg{URL: url, Err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return ImageReadyMsg{URL: url, Err: fmt.Errorf("http %d", resp.StatusCode)}
		}
		ext := extensionFromContentType(resp.Header.Get("Content-Type"))
		tmp, err := os.CreateTemp("", "ghdash-img-*"+ext)
		if err != nil {
			return ImageReadyMsg{URL: url, Err: err}
		}
		if _, err := io.Copy(tmp, resp.Body); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return ImageReadyMsg{URL: url, Err: err}
		}
		tmp.Close()
		return ImageReadyMsg{URL: url, Path: tmp.Name()}
	}
}

func extensionFromContentType(ct string) string {
	ct = strings.ToLower(ct)
	switch {
	case strings.Contains(ct, "png"):
		return ".png"
	case strings.Contains(ct, "jpeg"), strings.Contains(ct, "jpg"):
		return ".jpg"
	case strings.Contains(ct, "gif"):
		return ".gif"
	case strings.Contains(ct, "webp"):
		return ".webp"
	case strings.Contains(ct, "svg"):
		return ".svg"
	}
	return ""
}

// ViewImageProcess builds the *exec.Cmd that renders the image
// inline. The returned process inherits stdio, so kitty's graphics
// escape sequences land directly in the user's terminal.
func ViewImageProcess(path string) (*exec.Cmd, error) {
	tokens := strings.Fields(defaultImageViewer)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty viewer command")
	}
	for i, tok := range tokens {
		tokens[i] = strings.ReplaceAll(tok, "{path}", path)
	}
	cmd := exec.Command(tokens[0], tokens[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd, nil
}
