package content

import (
	"bytes"
	"html"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Markup that turns up in feed titles. Only real HTML elements are removed,
// so text that merely looks like a tag, such as Array<Element> or <iostream>,
// is kept.
var (
	scriptOrStyle = regexp.MustCompile(`(?is)<(?:script|style)\b[^>]*>.*?</(?:script|style)\s*>`)
	blockMarkup   = regexp.MustCompile(`(?i)</?(?:br|div|h[1-6]|li|p|tr)(?:\s[^>]*)?/?>`)
	inlineMarkup  = regexp.MustCompile(`(?is)<!--.*?-->|</?(?:a|abbr|acronym|b|big|cite|code|del|dfn|em|font|i|img|ins|kbd|mark|nobr|q|s|samp|small|span|strike|strong|sub|sup|time|tt|u|var|wbr)(?:\s[^>]*)?/?>`)
)

// Text turns a title or author field, which feeds sometimes fill with markup
// or entities, into plain text on one line of at most max bytes.
func Text(raw string, max int) string {
	s := scriptOrStyle.ReplaceAllString(raw, "")
	s = blockMarkup.ReplaceAllString(s, " ")
	s = inlineMarkup.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.Join(strings.Fields(s), " ")
	return TruncateUTF8(s, max)
}

// TruncateUTF8 returns valid UTF-8 without NUL characters (PostgreSQL rejects
// both in text columns) of at most max bytes, without splitting a character.
func TruncateUTF8(v string, max int) string {
	v = strings.ToValidUTF8(strings.ReplaceAll(v, "\x00", ""), "")
	if len(v) <= max {
		return v
	}
	for max > 0 && !utf8.RuneStart(v[max]) {
		max--
	}
	return v[:max]
}

// SniffImage returns the image type of data, or "" when it is not an image.
// Stored or declared types are often wrong for favicons, so the bytes decide.
func SniffImage(data []byte) string {
	if t := http.DetectContentType(data); strings.HasPrefix(t, "image/") {
		return t
	}
	if isSVG(data) {
		return "image/svg+xml"
	}
	return ""
}

// isSVG reports whether data is an SVG document: its first element, after an
// optional XML declaration, comments and a doctype, is <svg>. A web page that
// merely contains an <svg> element (a site's "not found" page, say) is not.
func isSVG(data []byte) bool {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // byte order mark
	for {
		data = bytes.TrimLeft(data, " \t\r\n")
		var end string
		switch {
		case bytes.HasPrefix(data, []byte("<?")):
			end = "?>"
		case bytes.HasPrefix(data, []byte("<!--")):
			end = "-->"
		case len(data) >= 9 && bytes.EqualFold(data[:9], []byte("<!DOCTYPE")):
			end = ">"
			// A doctype may declare entities between brackets.
			if i := bytes.IndexAny(data, "[>"); i >= 0 && data[i] == '[' {
				end = "]"
			}
		default:
			rest, found := bytes.CutPrefix(data, []byte("<svg"))
			return found && len(rest) > 0 && strings.IndexByte(" \t\r\n>/", rest[0]) >= 0
		}
		i := bytes.Index(data, []byte(end))
		if i < 0 {
			return false
		}
		data = data[i+len(end):]
		if end == "]" {
			if i = bytes.IndexByte(data, '>'); i < 0 {
				return false
			}
			data = data[i+1:]
		}
	}
}
