// Package content turns untrusted feed HTML into markup that is safe to show.
package content

import (
	"net/url"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
)

// policy is built once; bluemonday policies are safe for concurrent use.
var policy = newPolicy()

func newPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	// http, https and mailto links only; URLs must parse; relative URLs are
	// kept so they can be resolved against the item's address afterwards.
	// finish() sets target and rel on links once they are absolute.
	p.AllowStandardURLs()
	p.RequireNoFollowOnLinks(false)

	p.AllowElements(
		"h1", "h2", "h3", "h4", "h5", "h6",
		"p", "br", "hr", "div", "span",
		"ul", "ol", "li", "dl", "dt", "dd",
		"b", "i", "em", "strong", "u", "s", "strike", "del", "ins", "small", "mark", "sub", "sup",
		"blockquote", "q", "cite", "abbr", "time",
		"code", "pre", "kbd", "samp", "var",
		"table", "caption", "thead", "tbody", "tfoot", "tr", "th", "td",
		"figure", "figcaption",
	)
	p.AllowAttrs("title").Globally()
	p.AllowAttrs("href").OnElements("a")
	p.AllowAttrs("src", "alt").OnElements("img")
	p.AllowAttrs("width", "height").Matching(bluemonday.NumberOrPercent).OnElements("img")
	p.AllowDataURIImages()
	p.AllowAttrs("cite").OnElements("blockquote", "q", "del", "ins")
	p.AllowAttrs("datetime").OnElements("time", "del", "ins")
	p.AllowAttrs("colspan", "rowspan").Matching(bluemonday.Integer).OnElements("td", "th")
	p.AllowAttrs("start").Matching(bluemonday.Integer).OnElements("ol")
	return p
}

// Clean sanitizes feed HTML, then makes relative links and image addresses
// absolute using base (the item's or the feed's URL), and marks images for
// lazy loading so long lists stay light on phones.
func Clean(raw string, base string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	// PostgreSQL text cannot hold NUL characters.
	safe := policy.Sanitize(strings.ReplaceAll(raw, "\x00", ""))
	// Only an http(s) base is used: a feed could otherwise supply a base such
	// as "javascript:..." and turn relative links into scripts.
	return finish(safe, ParseHTTP(base))
}

// finish rewrites the sanitized markup token by token. It only changes the
// values of href/src (to absolute http(s) URLs) and adds loading attributes,
// so it cannot reintroduce anything the sanitizer removed.
func finish(safe string, base *url.URL) string {
	var b strings.Builder
	b.Grow(len(safe) + 64)
	z := html.NewTokenizer(strings.NewReader(safe))
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return b.String()
		case html.StartTagToken, html.SelfClosingTagToken:
			t := z.Token()
			switch t.Data {
			case "a":
				t.Attr = resolveAttr(t.Attr, "href", base)
				t.Attr = withoutAttrs(t.Attr, "target", "rel")
				if href := attr(t.Attr, "href"); strings.HasPrefix(href, "http:") || strings.HasPrefix(href, "https:") {
					// Open outside the reader, without telling the site
					// where the visit came from.
					t.Attr = append(t.Attr,
						html.Attribute{Key: "target", Val: "_blank"},
						html.Attribute{Key: "rel", Val: "noopener noreferrer"})
				}
			case "img":
				t.Attr = resolveAttr(t.Attr, "src", base)
				t.Attr = append(t.Attr,
					html.Attribute{Key: "loading", Val: "lazy"},
					html.Attribute{Key: "decoding", Val: "async"})
			}
			b.WriteString(t.String())
		default:
			b.Write(z.Raw())
		}
	}
}

func resolveAttr(attrs []html.Attribute, key string, base *url.URL) []html.Attribute {
	out := attrs[:0]
	for _, a := range attrs {
		if a.Key != key {
			out = append(out, a)
			continue
		}
		v, ok := resolve(a.Val, base)
		if !ok {
			continue // drop an address we cannot make safe
		}
		a.Val = v
		out = append(out, a)
	}
	return out
}

func withoutAttrs(attrs []html.Attribute, keys ...string) []html.Attribute {
	out := attrs[:0]
next:
	for _, a := range attrs {
		for _, k := range keys {
			if a.Key == k {
				continue next
			}
		}
		out = append(out, a)
	}
	return out
}

func attr(attrs []html.Attribute, key string) string {
	for _, a := range attrs {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func resolve(ref string, base *url.URL) (string, bool) {
	ref = strings.TrimSpace(ref)
	switch {
	case strings.HasPrefix(ref, "data:"):
		return ref, true // already validated by the sanitizer
	case base == nil && strings.HasPrefix(ref, "#"):
		return ref, true // an in-page anchor
	}
	// Without a base, other relative URLs would point at this app instead
	// of the feed's site, so they are dropped.
	u := resolveRef(base, ref)
	if u == nil {
		return "", false
	}
	switch u.Scheme {
	case "http", "https", "mailto":
		return u.String(), true
	}
	return "", false
}
