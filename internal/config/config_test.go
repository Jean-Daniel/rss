package config

import "testing"

func TestNormalizeOrigin(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"https://rss.example.com", "https://rss.example.com", true},
		{"https://RSS.Example.com/", "https://rss.example.com", true},
		{"HTTPS://rss.example.com:443", "https://rss.example.com", true},
		{"http://localhost:80", "http://localhost", true},
		{"http://localhost:5173", "http://localhost:5173", true},
		{"https://rss.example.com:8443", "https://rss.example.com:8443", true},
		{"http://[::1]:8080", "http://[::1]:8080", true},
		{"null", "", false},
		{"ftp://example.com", "", false},
		{"https://user:pw@example.com", "", false},
	}
	for _, tt := range tests {
		got, ok := NormalizeOrigin(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("NormalizeOrigin(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestLoadNormalizesPublicOrigin(t *testing.T) {
	t.Setenv("PUBLIC_ORIGIN", "https://RSS.example.com:443/")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicOrigin != "https://rss.example.com" {
		t.Fatalf("PublicOrigin = %q", c.PublicOrigin)
	}
	if !c.SecureCookies() {
		t.Fatal("https origin should give Secure cookies")
	}

	t.Setenv("PUBLIC_ORIGIN", "https://rss.example.com/reader")
	if _, err := Load(); err == nil {
		t.Fatal("an origin with a path should be rejected")
	}
}
