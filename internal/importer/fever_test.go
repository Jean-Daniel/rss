package importer

import (
	"encoding/base64"
	"testing"
)

func TestDecodeFavicon(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n" + "rest"
	svg := `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"></svg>`
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

	tests := []struct {
		name, cache, want string
		ok                bool
	}{
		{"png labelled text/x-icon", "text/x-icon;base64," + b64(png), "image/png", true},
		{"svg labelled text/x-icon", "text/x-icon;base64," + b64(svg), "image/svg+xml", true},
		{"data: prefix", "data:image/png;base64," + b64(png), "image/png", true},
		{"unknown bytes, image type kept", "image/webp;base64," + b64("RIFFxxxxWEBP"), "image/webp", true},
		{"html is rejected", "text/html;base64," + b64("<html>not found</html>"), "", false},
		{"not base64", "image/png;base64,***", "", false},
		{"no data", "not a favicon", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mime, _, ok := decodeFavicon(tt.cache)
			if ok != tt.ok || mime != tt.want {
				t.Errorf("decodeFavicon() = %q, %v; want %q, %v", mime, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestFeverGUID(t *testing.T) {
	link := "https://example.com/post"
	// PHP: sprintf('%u', crc32('https://example.com/post'))
	if got := feverGUID("624656918", link); got != link {
		t.Errorf("checksum uid: got %q, want the link", got)
	}
	if got := feverGUID("urn:uuid:1", link); got != "urn:uuid:1" {
		t.Errorf("real guid changed: %q", got)
	}
	if got := feverGUID("12345", link); got != "12345" {
		t.Errorf("a numeric guid that is not the checksum changed: %q", got)
	}
}

func TestDecodeAuth(t *testing.T) {
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	for _, tt := range []struct{ auth, user, pass string }{
		{b64("joe:secret"), "joe", "secret"},
		{b64(":token"), "", "token"}, // a password only
		{b64("joe:"), "joe", ""},
		{b64(":"), "<nil>", "<nil>"},
		{b64("no colon"), "<nil>", "<nil>"},
		{"", "<nil>", "<nil>"},
		{"not base64!", "<nil>", "<nil>"},
	} {
		u, p := decodeAuth(tt.auth)
		if str(u) != tt.user || str(p) != tt.pass {
			t.Errorf("decodeAuth(%q) = %s, %s; want %s, %s", tt.auth, str(u), str(p), tt.user, tt.pass)
		}
	}
}
