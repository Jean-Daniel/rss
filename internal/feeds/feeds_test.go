package feeds

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"

	"fever/internal/store"
)

const sampleRSS = `<?xml version="1.0"?>
<rss version="2.0"><channel>
  <title>Example &amp; Co</title>
  <link>https://example.com/blog/</link>
  <item>
    <title>First &lt;b&gt;post&lt;/b&gt;</title>
    <link>/blog/first</link>
    <guid>urn:first</guid>
    <author>ann@example.com (Ann)</author>
    <description><![CDATA[<p>Hello <img src="pic.png"><script>x()</script></p>]]></description>
    <pubDate>Mon, 02 Nov 2026 10:00:00 GMT</pubDate>
  </item>
  <item>
    <title>No guid</title>
    <link>https://example.com/blog/second</link>
    <description>Second</description>
    <pubDate>Tue, 03 Nov 2026 10:00:00 GMT</pubDate>
  </item>
  <item>
    <title>First again</title>
    <guid>urn:first</guid>
  </item>
  <item>
    <title>From the future</title>
    <guid>urn:future</guid>
    <pubDate>Fri, 01 Jan 2100 00:00:00 GMT</pubDate>
  </item>
</channel></rss>`

func TestToItems(t *testing.T) {
	feed, err := gofeed.NewParser().ParseString(sampleRSS)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	items := toItems(feed, "https://example.com/feed.xml", now, now.Add(-Retention))
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3 (duplicate guid dropped)", len(items))
	}
	// With a retention cut after the first item's date, that entry is left
	// out (its undated repeat, "First again", then counts as new).
	cut := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	for _, it := range toItems(feed, "https://example.com/feed.xml", now, cut) {
		if it.title == "First post" {
			t.Error("an entry older than the retention cut was kept")
		}
	}
	first := items[0]
	if first.guid != "urn:first" || first.url != "https://example.com/blog/first" {
		t.Errorf("first item guid/url = %q %q", first.guid, first.url)
	}
	if first.title != "First post" {
		t.Errorf("title = %q, want tags removed", first.title)
	}
	if html := first.html(); !strings.Contains(html, `src="https://example.com/blog/pic.png"`) || strings.Contains(html, "script") {
		t.Errorf("content not cleaned or not resolved: %q", html)
	}
	if items[1].guid != "https://example.com/blog/second" {
		t.Errorf("an item without guid should use its link, got %q", items[1].guid)
	}
	if !items[2].published.Equal(now) {
		t.Errorf("future date not clamped: %v", items[2].published)
	}
}

func TestIsPublic(t *testing.T) {
	for addr, want := range map[string]bool{
		"93.184.216.34":      true,
		"2606:4700::1111":    true,
		"127.0.0.1":          false,
		"10.89.0.5":          false, // a podman network
		"192.168.1.10":       false,
		"169.254.169.254":    false, // cloud metadata
		"100.100.1.1":        false, // CGNAT
		"::1":                false,
		"fd00::1":            false,
		"::ffff:127.0.0.1":   false,
		"0.0.0.0":            false,
		"0.1.2.3":            false,
		"198.18.0.10":        false, // benchmarking
		"192.0.0.8":          false,
		"255.255.255.255":    false,
		"64:ff9b::a00:1":     false, // NAT64 wrapping 10.0.0.1
		"64:ff9b::5db8:d822": true,  // NAT64 wrapping 93.184.216.34
	} {
		if got := isPublic(netip.MustParseAddr(addr)); got != want {
			t.Errorf("isPublic(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestPrivateAddressesAreRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	_, err := fetch(context.Background(), newClient(false), store.DueFeedsRow{Url: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "private address") {
		t.Fatalf("fetching a loopback feed: err = %v, want a private address error", err)
	}
}

func TestFetch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/feed", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if user, pass, ok := r.BasicAuth(); !ok || user != "joe" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Write([]byte(sampleRSS))
	})
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<!doctype html><html><body>Hello</body></html>"))
	})
	mux.HandleFunc("/busy", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7200")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := newClient(true)
	ctx := context.Background()
	str := func(s string) *string { return &s }

	res, err := fetch(ctx, client, store.DueFeedsRow{Url: srv.URL + "/feed", AuthUsername: str("joe"), AuthPassword: str("secret")})
	if err != nil || res.notModified || res.feed == nil || res.etag != `"v1"` {
		t.Fatalf("first fetch: res=%+v err=%v", res, err)
	}
	res, err = fetch(ctx, client, store.DueFeedsRow{Url: srv.URL + "/feed", Etag: str(`"v1"`)})
	if err != nil || !res.notModified {
		t.Fatalf("conditional fetch: res=%+v err=%v", res, err)
	}
	if _, err = fetch(ctx, client, store.DueFeedsRow{Url: srv.URL + "/feed"}); err == nil || !strings.Contains(err.Error(), "username and password") {
		t.Errorf("missing credentials: err = %v", err)
	}
	if _, err = fetch(ctx, client, store.DueFeedsRow{Url: srv.URL + "/page"}); err == nil || !strings.Contains(err.Error(), "does not return a feed") {
		t.Errorf("web page: err = %v", err)
	}
	_, err = fetch(ctx, client, store.DueFeedsRow{Url: srv.URL + "/busy"})
	var he *httpError
	if !errors.As(err, &he) || he.retryAfter != 2*time.Hour {
		t.Errorf("429: err = %v, want a 2h Retry-After", err)
	}
}

// A validator that cannot be stored, or sent back, is dropped: the feed is
// then fetched in full next time, instead of failing to be saved.
func TestValidator(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`"v1"`, `"v1"`},
		{`W/"abc"`, `W/"abc"`},
		{"Mon, 02 Nov 2026 10:00:00 GMT", "Mon, 02 Nov 2026 10:00:00 GMT"},
		{"", ""},
		{strings.Repeat("x", maxValidatorBytes), strings.Repeat("x", maxValidatorBytes)},
		{strings.Repeat("x", maxValidatorBytes+1), ""},
		{"\"caf\xe9\"", ""}, // not UTF-8: PostgreSQL refuses it
		{"\"a\x00b\"", ""},  // PostgreSQL refuses NUL
		{"\"a\r\nb\"", ""},  // not a valid header value
		{"\"a\x7fb\"", ""},
	} {
		if got := validator(tc.in); got != tc.want {
			t.Errorf("validator(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFindFavicon(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 32)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><head>
			<link rel="apple-touch-icon" href="/touch.png">
			<link rel="stylesheet" href="/style.css">
			<link rel="Shortcut Icon" href="/missing.ico">
			<link rel="icon" href="icons/site.png">
			</head><body><link rel="icon" href="/in-body.png"></body></html>`))
	})
	mux.HandleFunc("/icons/site.png", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(png)) })
	mux.HandleFunc("/touch.png", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(png)) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	site, _ := url.Parse(srv.URL + "/")
	icon, err := findFavicon(context.Background(), newClient(true), site)
	if err != nil {
		t.Fatal(err)
	}
	// /missing.ico (404) is skipped, the next rel=icon wins over the touch icon.
	if icon.url != srv.URL+"/icons/site.png" || icon.mime != "image/png" {
		t.Errorf("icon = %s (%s)", icon.url, icon.mime)
	}

	links := parseIconLinks(`<head><link rel="icon" href="javascript:x"><link rel="icon" href="//cdn.example/i.svg"></head>`, site)
	if len(links) != 1 || links[0] != "http://cdn.example/i.svg" {
		t.Errorf("links = %v", links)
	}
}

func TestRetryAt(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	r := &Refresher{opts: Options{Interval: 15 * time.Minute}, now: func() time.Time { return now }}
	within := func(got time.Time, d time.Duration) bool {
		wait := got.Sub(now)
		return wait >= time.Duration(float64(d)*0.9) && wait <= time.Duration(float64(d)*1.1)
	}
	if got := r.retryAt(1, 0); !within(got, 30*time.Minute) {
		t.Errorf("first failure waits %v, want about 30m", got.Sub(now))
	}
	if got := r.retryAt(20, 0); got.Sub(now) > maxBackoff+maxBackoff/10 {
		t.Errorf("waits %v, more than the maximum", got.Sub(now))
	}
	if got := r.retryAt(1, 3*time.Hour); !within(got, 3*time.Hour) {
		t.Errorf("Retry-After ignored: waits %v", got.Sub(now))
	}
}

func TestNormalizeURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://example.com/feed":       "https://example.com/feed",
		"feed://example.com/rss.xml":     "http://example.com/rss.xml",
		"FEED://example.com/rss.xml":     "http://example.com/rss.xml",
		"feed:https://example.com/a.xml": "https://example.com/a.xml",
		"itpc://podcast.example/feed":    "http://podcast.example/feed",
		"  https://example.com/spaced  ": "https://example.com/spaced",
	} {
		if got := NormalizeURL(in); got != want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIconLinksFromLargePage(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 32)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><link rel="icon" href="/big-site.png"></head><body>`))
		w.Write([]byte(strings.Repeat("<p>filler</p>", maxPageBytes/8))) // well over the limit
	})
	mux.HandleFunc("/big-site.png", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(png)) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	site, _ := url.Parse(srv.URL + "/")
	icon, err := findFavicon(context.Background(), newClient(true), site)
	if err != nil || icon.url != srv.URL+"/big-site.png" {
		t.Fatalf("icon from a large page: %+v, %v", icon, err)
	}
}
