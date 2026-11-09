package feeds

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"

	"fever/internal/content"
)

const (
	maxPageBytes = 512 << 10 // enough for any <head>
	maxIconBytes = 256 << 10
	maxIconTries = 4
)

type favicon struct {
	url  string
	mime string
	data []byte
}

var errNoFavicon = errors.New("no favicon found")

// findFavicon looks for the icon of the site at siteURL: icons declared in
// its home page first, then /favicon.ico. Only real images are accepted.
func findFavicon(ctx context.Context, client *http.Client, siteURL *url.URL) (*favicon, error) {
	candidates := iconLinks(ctx, client, siteURL)
	root := &url.URL{Scheme: siteURL.Scheme, Host: siteURL.Host, Path: "/favicon.ico"}
	candidates = append(candidates, root.String())

	tried := make(map[string]bool)
	for _, c := range candidates {
		if tried[c] || len(tried) >= maxIconTries {
			continue
		}
		tried[c] = true
		data, err := get(ctx, client, c, maxIconBytes)
		if err != nil {
			continue
		}
		if t := content.SniffImage(data); t != "" {
			return &favicon{url: c, mime: t, data: data}, nil
		}
	}
	return nil, errNoFavicon
}

// iconLinks returns the icon URLs declared by the site's home page, best
// first: rel="icon" (and "shortcut icon"), then apple-touch-icon.
func iconLinks(ctx context.Context, client *http.Client, siteURL *url.URL) []string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, siteURL.String(), nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html")
	res, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if mt, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type")); res.StatusCode != http.StatusOK || mt != "text/html" {
		return nil
	}
	// Icon links are in the <head>: the beginning of a large page is enough.
	page, err := io.ReadAll(io.LimitReader(res.Body, maxPageBytes))
	if err != nil {
		return nil
	}
	return parseIconLinks(string(page), res.Request.URL)
}

func parseIconLinks(page string, base *url.URL) []string {
	var icons, touch []string
	z := html.NewTokenizer(strings.NewReader(page))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return append(icons, touch...)
		case html.StartTagToken, html.SelfClosingTagToken:
			t := z.Token()
			switch t.Data {
			case "body":
				return append(icons, touch...)
			case "link":
				var rel, href string
				for _, a := range t.Attr {
					switch a.Key {
					case "rel":
						rel = strings.ToLower(a.Val)
					case "href":
						href = a.Val
					}
				}
				u := content.ResolveHTTP(base, href)
				if u == nil {
					continue
				}
				switch {
				case hasToken(rel, "icon"):
					icons = append(icons, u.String())
				case hasToken(rel, "apple-touch-icon"), hasToken(rel, "apple-touch-icon-precomposed"):
					touch = append(touch, u.String())
				}
			}
		}
	}
}

func hasToken(list, token string) bool {
	for _, f := range strings.Fields(list) {
		if f == token {
			return true
		}
	}
	return false
}

// get downloads a small resource.
func get(ctx context.Context, client *http.Client, rawURL string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, &httpError{status: res.StatusCode}
	}
	return readLimited(res.Body, max)
}
