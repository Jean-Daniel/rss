package content

import "testing"

func TestTruncateUTF8(t *testing.T) {
	tests := []struct {
		in   string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"abcdé", 5, "abcd"}, // é is 2 bytes and would be split
		{"abcdé", 6, "abcdé"},
		{"a\xffb", 10, "ab"}, // invalid bytes dropped
		{"a\x00b", 10, "ab"}, // NUL dropped
	}
	for _, tt := range tests {
		if got := TruncateUTF8(tt.in, tt.max); got != tt.want {
			t.Errorf("TruncateUTF8(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
		}
	}
}

func TestText(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Hello &amp; welcome", "Hello & welcome"},
		{"<b>Bold</b> title", "Bold title"},
		{"  spread\n over\t lines ", "spread over lines"},
		{"a < b", "a < b"},
		{"&#8230;", "\u2026"},
		{"<script>alert(1)</script>Safe", "Safe"},
		{"Why Array<Element> is fast", "Why Array<Element> is fast"},
		{"Using <iostream> in C++", "Using <iostream> in C++"},
		{"<code>Vec&lt;T&gt;</code> tips", "Vec<T> tips"},
		{"Line one<br/>line two", "Line one line two"},
		{"Hel<em>lo</em>", "Hello"},
		{"<a href=\"/x\">Link</a> text", "Link text"},
		{"nul\x00byte", "nulbyte"},
		{"Before<!-- a comment\nover two lines -->after", "Beforeafter"},
	}
	for _, tt := range tests {
		if got := Text(tt.in, 100); got != tt.want {
			t.Errorf("Text(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSniffImage(t *testing.T) {
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><rect width="1" height="1"/></svg>`
	tests := []struct{ name, in, want string }{
		{"png", "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR", "image/png"},
		{"gif", "GIF89a\x01\x00\x01\x00", "image/gif"},
		{"svg", svg, "image/svg+xml"},
		{"svg after a byte order mark and blank lines", "\xef\xbb\xbf\n\n" + svg, "image/svg+xml"},
		{"svg with a declaration and a comment", "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!-- Generator: a drawing app -->\n" + svg, "image/svg+xml"},
		{"svg with a doctype", "<?xml version=\"1.0\"?>\n<!DOCTYPE svg PUBLIC \"-//W3C//DTD SVG 1.1//EN\" \"http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd\">\n" + svg, "image/svg+xml"},
		{"svg with entities in the doctype", "<!DOCTYPE svg [\n<!ENTITY ns_svg \"http://www.w3.org/2000/svg\">\n]>\n" + svg, "image/svg+xml"},
		{"bare svg element", "<svg>", "image/svg+xml"},
		{"web page with an inline svg", "<!DOCTYPE html>\n<html><body><svg viewBox=\"0 0 1 1\"></svg>Not found</body></html>", ""},
		{"web page without a doctype", "<html><head><title>x</title></head><body>" + svg + "</body></html>", ""},
		{"comment that mentions svg", "<!-- <svg> --><html></html>", ""},
		{"another element starting with svg", "<svgs></svgs>", ""},
		{"unfinished comment", "<!-- " + svg, ""},
		{"text", "not an image", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		if got := SniffImage([]byte(tt.in)); got != tt.want {
			t.Errorf("%s: SniffImage = %q, want %q", tt.name, got, tt.want)
		}
	}
}
