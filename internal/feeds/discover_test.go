package feeds

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

const testRSS = `<?xml version="1.0"?>
<rss version="2.0"><channel><title>Test &amp; feed</title><link>https://site.example/</link>
<item><title>One</title><guid>1</guid><link>https://site.example/1</link></item>
</channel></rss>`

func page(head string) string {
	return "<!doctype html><html><head><title>Site</title>" + head + "</head><body>" +
		`<link rel="alternate" type="application/rss+xml" href="/in-body.xml">` + "</body></html>"
}

func TestDiscover(t *testing.T) {
	var lastAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/feed.xml", func(w http.ResponseWriter, r *http.Request) {
		_, _, ok := r.BasicAuth()
		if ok {
			lastAuth = r.Header.Get("Authorization")
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		io.WriteString(w, testRSS)
	})
	mux.HandleFunc("/one", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, page(`<link rel="alternate" type="application/rss+xml" title="Posts" href="feed.xml">`))
	})
	mux.HandleFunc("/two", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, page(`<link rel="alternate" type="application/rss+xml" title="Posts" href="/feed.xml">
			<link rel="alternate" type="application/atom+xml" title="Comments" href="/comments.atom">
			<link rel="alternate" type="application/rss+xml" href="/feed.xml">`))
	})
	mux.HandleFunc("/none", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, page(`<link rel="stylesheet" href="/s.css">`))
	})
	mux.HandleFunc("/private", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	// Another site (another port: another host) with a feed, and a page
	// here that links to it.
	var otherAuth string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherAuth = r.Header.Get("Authorization")
		io.WriteString(w, testRSS)
	}))
	defer other.Close()
	mux.HandleFunc("/elsewhere", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, page(`<link rel="alternate" type="application/rss+xml" href="`+other.URL+`/feed.xml">`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	r := NewRefresher(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{AllowPrivate: true})
	ctx := context.Background()

	found, choices, err := r.Discover(ctx, srv.URL+"/feed.xml", nil, nil)
	if err != nil || choices != nil {
		t.Fatalf("feed address: %v %v", err, choices)
	}
	if found.URL != srv.URL+"/feed.xml" || found.res.feed == nil || found.res.feed.Title != "Test & feed" {
		t.Errorf("feed address: found %+v", found)
	}

	found, _, err = r.Discover(ctx, "  "+srv.URL+"/one ", nil, nil)
	if err != nil || found.URL != srv.URL+"/feed.xml" {
		t.Errorf("page with one feed: %+v %v", found, err)
	}

	_, choices, err = r.Discover(ctx, srv.URL+"/two", nil, nil)
	want := []Choice{{URL: srv.URL + "/feed.xml", Title: "Posts"}, {URL: srv.URL + "/comments.atom", Title: "Comments"}}
	if err != nil || !reflect.DeepEqual(choices, want) {
		t.Errorf("page with two feeds: %+v %v", choices, err)
	}

	user, pass := "joe", "secret"
	found, _, err = r.Discover(ctx, srv.URL+"/one", &user, &pass)
	if err != nil {
		t.Fatal(err)
	}
	if u, p := found.Login(); lastAuth == "" || u == nil || *u != "joe" || p == nil || *p != "secret" {
		t.Errorf("feed of the same site: login sent %q, kept %v %v", lastAuth, u, p)
	}
	found, _, err = r.Discover(ctx, srv.URL+"/elsewhere", &user, &pass)
	if err != nil || found.URL != other.URL+"/feed.xml" {
		t.Fatalf("page linking to another site's feed: %+v %v", found, err)
	}
	if u, p := found.Login(); otherAuth != "" || u != nil || p != nil {
		t.Errorf("feed of another site: login sent %q, kept %v %v", otherAuth, u, p)
	}

	for address, msg := range map[string]string{
		srv.URL + "/none":    "No feed found",
		srv.URL + "/private": "username and password",
		"":                   "Enter the address",
		"ftp://x.example/":   "Enter the address",
		"mailto:a@b.example": "Enter the address",
	} {
		if _, _, err := r.Discover(ctx, address, nil, nil); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("Discover(%q) error = %v, want one mentioning %q", address, err, msg)
		}
	}
}

func TestFeedLinks(t *testing.T) {
	base, _ := url.Parse("https://site.example/blog/")
	got := feedLinks([]byte(`<html><head>
		<link rel="Alternate" type="application/RSS+XML" href="feed">
		<link rel="alternate" type="application/feed+json" title="JSON" href="https://site.example/feed.json">
		<link rel="alternate" type="application/json" title="JSON" href="https://site.example/wp-json/wp/v2/posts/7">
		<link rel="alternate" type="application/json" href="/blog/feed.json">
		<link rel="alternate" type="text/html" href="/fr/">
		<link rel="alternate" type="application/atom+xml" href="javascript:alert(1)">
		<link rel="alternate" type="application/rss+xml" href="https://site.example/blog/feed">
		</head><link rel="alternate" type="application/rss+xml" href="/after-head"></html>`), base)
	want := []Choice{
		{URL: "https://site.example/blog/feed"},
		{URL: "https://site.example/feed.json", Title: "JSON"},
		{URL: "https://site.example/blog/feed.json"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("feedLinks = %+v, want %+v", got, want)
	}
}

func TestWithScheme(t *testing.T) {
	for in, want := range map[string]string{
		"example.com":                  "https://example.com",
		"example.com/feed?tag=a:b":     "https://example.com/feed?tag=a:b",
		"example.com:8443/feed":        "https://example.com:8443/feed",
		"localhost:8080":               "https://localhost:8080",
		"http://example.com/feed":      "http://example.com/feed",
		"feed://example.com/rss":       "feed://example.com/rss",
		"feed:https://example.com/rss": "feed:https://example.com/rss",
		"mailto:joe@example.com":       "mailto:joe@example.com",
		"javascript:alert(1)":          "javascript:alert(1)",
		"":                             "",
	} {
		if got := withScheme(in); got != want {
			t.Errorf("withScheme(%q) = %q, want %q", in, got, want)
		}
	}
}
