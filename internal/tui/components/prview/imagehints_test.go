package prview

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// GitHub's API-rendered HTML for an attachment echoes BOTH the signed CDN
// src and the unsigned github.com URL (in data-canonical-src). We must lift
// the signed one regardless of attribute order.
func TestParseSignedImageURL(t *testing.T) {
	signed := "https://private-user-images.githubusercontent.com/211365006/602620054-f418a68f.png?jwt=eyJhbGci.payload.sig"

	cases := []struct {
		name string
		html string
		want string
	}{
		{
			name: "signed src before canonical",
			html: `<p><a href="` + signed + `"><img src="` + signed + `" alt="x" data-canonical-src="https://github.com/user-attachments/assets/f418a68f" style="max-width: 100%;"></a></p>`,
			want: signed,
		},
		{
			name: "canonical attribute before src",
			html: `<img data-canonical-src="https://github.com/user-attachments/assets/f418a68f" src="` + signed + `">`,
			want: signed,
		},
		{
			name: "public user-images host",
			html: `<img src="https://user-images.githubusercontent.com/1/abc.png" alt="y">`,
			want: "https://user-images.githubusercontent.com/1/abc.png",
		},
		{
			name: "no githubusercontent url present",
			html: `<p>Sign in to GitHub</p><img src="https://github.com/user-attachments/assets/f418a68f">`,
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, parseSignedImageURL([]byte(tc.html)))
		})
	}
}

// resolveSignedImageURL must short-circuit (no network) and return the URL
// unchanged for anything that isn't a github.com web route, and whenever the
// repo context is missing.
func TestResolveSignedImageURL_ShortCircuits(t *testing.T) {
	cases := []struct {
		name string
		repo string
		url  string
	}{
		{name: "already-direct CDN url", repo: "o/r", url: "https://private-user-images.githubusercontent.com/1/a.png?jwt=x"},
		{name: "external badge", repo: "o/r", url: "https://img.shields.io/badge/build-passing-green"},
		{name: "raw host", repo: "o/r", url: "https://raw.githubusercontent.com/o/r/main/a.png"},
		{name: "empty repo context", repo: "", url: "https://github.com/user-attachments/assets/abc"},
		{name: "unparseable url", repo: "o/r", url: "://nope"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.url, resolveSignedImageURL(tc.repo, tc.url))
		})
	}
}
