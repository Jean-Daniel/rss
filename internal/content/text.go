package content

import (
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
	inlineMarkup  = regexp.MustCompile(`(?i)<!--.*?-->|</?(?:a|abbr|acronym|b|big|cite|code|del|dfn|em|font|i|img|ins|kbd|mark|nobr|q|s|samp|small|span|strike|strong|sub|sup|time|tt|u|var|wbr)(?:\s[^>]*)?/?>`)
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
	head := strings.ToLower(string(data[:min(len(data), 1024)]))
	if strings.Contains(head, "<svg") {
		return "image/svg+xml"
	}
	return ""
}
