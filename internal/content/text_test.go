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
	}
	for _, tt := range tests {
		if got := Text(tt.in, 100); got != tt.want {
			t.Errorf("Text(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
