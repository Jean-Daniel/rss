package content

import (
	"net/url"
	"strings"
)

// ParseHTTP parses an absolute http(s) URL with a host. Anything else, such
// as "javascript:…" or a relative path, gives nil.
func ParseHTTP(raw string) *url.URL {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !isHTTP(u) {
		return nil
	}
	return u
}

// ResolveHTTP makes ref absolute against base when it is relative, and
// returns it only if the result is an http(s) URL.
func ResolveHTTP(base *url.URL, ref string) *url.URL {
	u := resolveRef(base, ref)
	if u == nil || !isHTTP(u) {
		return nil
	}
	return u
}

// resolveRef makes ref absolute against base when it is relative. An empty
// ref, or a relative one without a base, gives nil.
func resolveRef(base *url.URL, ref string) *url.URL {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	u, err := url.Parse(ref)
	if err != nil {
		return nil
	}
	if !u.IsAbs() {
		if base == nil {
			return nil
		}
		u = base.ResolveReference(u)
	}
	return u
}

func isHTTP(u *url.URL) bool {
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
