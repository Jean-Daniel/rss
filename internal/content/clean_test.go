package content

import (
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		base     string
		contains []string
		excludes []string
	}{
		{
			name:     "removes scripts and event handlers",
			raw:      `<p onclick="steal()">hi<script>alert(1)</script></p><style>body{}</style>`,
			contains: []string{"<p>hi</p>"},
			excludes: []string{"script", "onclick", "alert", "style"},
		},
		{
			name:     "drops javascript links",
			raw:      `<a href="javascript:alert(1)">x</a>`,
			base:     "https://example.com/",
			contains: []string{"x"},
			excludes: []string{"javascript"},
		},
		{
			name:     "resolves relative links and opens them outside",
			raw:      `<a href="/post?a=1&amp;b=2" target="_self" rel="opener">read</a>`,
			base:     "https://example.com/blog/feed.xml",
			contains: []string{`href="https://example.com/post?a=1&amp;b=2"`, `target="_blank"`, `rel="noopener noreferrer"`},
			excludes: []string{"_self", `rel="opener"`},
		},
		{
			name:     "resolves images and lazy-loads them",
			raw:      `<img src="pic.png" alt="A" width="10" style="x" onerror="y">`,
			base:     "https://example.com/blog/post",
			contains: []string{`src="https://example.com/blog/pic.png"`, `loading="lazy"`, `alt="A"`, `width="10"`},
			excludes: []string{"style", "onerror"},
		},
		{
			name:     "ignores a non-http base",
			raw:      `<a href="foo">x</a>`,
			base:     "javascript:alert(1)//",
			excludes: []string{"javascript", "href"},
		},
		{
			name:     "keeps in-page anchors without a base",
			raw:      `<a href="#note">1</a>`,
			contains: []string{`href="#note"`},
			excludes: []string{"_blank"},
		},
		{
			name:     "removes iframes with their content",
			raw:      `<p>a</p><iframe src="https://evil.example">b</iframe>`,
			contains: []string{"<p>a</p>"},
			excludes: []string{"iframe", "evil"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Clean(tt.raw, tt.base)
			for _, s := range tt.contains {
				if !strings.Contains(got, s) {
					t.Errorf("Clean() = %q, want it to contain %q", got, s)
				}
			}
			for _, s := range tt.excludes {
				if strings.Contains(got, s) {
					t.Errorf("Clean() = %q, should not contain %q", got, s)
				}
			}
		})
	}
}
