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
