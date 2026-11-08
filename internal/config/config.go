// Package config reads the server configuration from environment variables.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	// DatabaseURL is a PostgreSQL connection string (DATABASE_URL).
	DatabaseURL string
	// ListenAddr is the HTTP listen address (LISTEN_ADDR, default ":8080").
	ListenAddr string
	// PublicOrigin is the URL the app is reached at, such as
	// "https://rss.example.com" (PUBLIC_ORIGIN). When set, it is the only
	// origin allowed to make state-changing requests, and an https origin
	// turns on Secure cookies. When empty (development), the request's own
	// host is used.
	PublicOrigin string
	// TrustProxy makes the server read the client address and scheme from
	// X-Forwarded-For / X-Forwarded-Proto (TRUST_PROXY). Only enable it when
	// the server is reachable exclusively through the reverse proxy.
	TrustProxy bool
	// DBMaxConns caps the PostgreSQL connection pool (DB_MAX_CONNS, default 4).
	DBMaxConns int32
}

func Load() (Config, error) {
	c := Config{
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		ListenAddr:   envOr("LISTEN_ADDR", ":8080"),
		PublicOrigin: strings.TrimRight(os.Getenv("PUBLIC_ORIGIN"), "/"),
		DBMaxConns:   4,
	}

	if c.PublicOrigin != "" {
		u, err := url.Parse(c.PublicOrigin)
		origin, ok := NormalizeOrigin(c.PublicOrigin)
		if err != nil || !ok || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return c, fmt.Errorf("PUBLIC_ORIGIN must look like https://rss.example.com, got %q", c.PublicOrigin)
		}
		c.PublicOrigin = origin
	}

	if v := os.Getenv("TRUST_PROXY"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("TRUST_PROXY: %w", err)
		}
		c.TrustProxy = b
	}

	if v := os.Getenv("DB_MAX_CONNS"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 1 {
			return c, fmt.Errorf("DB_MAX_CONNS must be a positive integer, got %q", v)
		}
		c.DBMaxConns = int32(n)
	}

	return c, nil
}

// NormalizeOrigin returns the origin of an http(s) URL written the way
// browsers send it in the Origin header: lowercase scheme and host, no
// default port, nothing after the host.
func NormalizeOrigin(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Hostname() == "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]" // IPv6 literal
	}
	if port := u.Port(); port != "" && !(scheme == "https" && port == "443") && !(scheme == "http" && port == "80") {
		host += ":" + port
	}
	return scheme + "://" + host, true
}

// SecureCookies reports whether session cookies must carry the Secure flag.
func (c Config) SecureCookies() bool {
	return strings.HasPrefix(c.PublicOrigin, "https://")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
