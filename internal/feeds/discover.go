package feeds

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"strings"

	"golang.org/x/net/html"

	"fever/internal/content"
	"fever/internal/store"
)

// Found is a feed Discover found, with the document it downloaded.
type Found struct {
	// URL is the feed's address: the one given, or the one the web page at
	// that address links to.
	URL string

	username, password *string
	res                *fetchResult
}

// Login is the login to store with the feed: the one given, unless the feed
// is on another site than the address given.
func (f *Found) Login() (username, password *string) {
	return f.username, f.password
}

// Choice is a feed a web page links to.
type Choice struct {
	URL   string
	Title string
}

// feedTypes are the link types of feeds in a web page's <head>.
var feedTypes = map[string]bool{
	"application/rss+xml":   true,
	"application/atom+xml":  true,
	"application/rdf+xml":   true,
	"application/feed+json": true,
	// Also used for other alternates, such as WordPress's REST API links:
	// see feedLinks.
	"application/json": true,
	"text/xml":         true,
	"application/xml":  true,
}

// Discover finds the feed at an address: the feed's own address, or a web
// page that links to its feeds, as most sites do. When the page links to
// several feeds, they are returned instead, for the reader to choose: asking
// again with one of their addresses finds it. Errors are messages for the
// reader.
//
// The login, if any, is only sent to the site whose address was given.
func (r *Refresher) Discover(ctx context.Context, address string, username, password *string) (*Found, []Choice, error) {
	// Stored as the importer stores addresses, so the same address always
	// compares equal.
	addr := NormalizeURL(withScheme(strings.TrimSpace(address)))
	u := content.ParseHTTP(addr)
	if u == nil {
		return nil, nil, errors.New("Enter the address of a feed or of a web site, such as https://example.com.")
	}
	f := store.DueFeedsRow{Url: addr, AuthUsername: username, AuthPassword: password}
	res, body, err := download(ctx, r.client, f)
	if err != nil {
		return nil, nil, err
	}
	if res.notModified { // never asked for: no validators were sent
		return nil, nil, errors.New("The site gave an unexpected answer (HTTP 304).")
	}
	feed, err := parseFeed(body)
	if err == nil {
		res.feed, res.size = feed, len(body)
		return newFound(f, res), nil, nil
	}
	if !errors.Is(err, errNotAFeed) {
		return nil, nil, err
	}

	links := feedLinks(body, content.ParseHTTP(res.finalURL))
	switch len(links) {
	case 0:
		return nil, nil, errors.New("No feed found: the address is a web page that does not link to a feed.")
	case 1:
	default:
		return nil, links, nil
	}
	link := content.ParseHTTP(links[0].URL)
	if link.Host != u.Host {
		f.AuthUsername, f.AuthPassword = nil, nil
	}
	f.Url = link.String()
	if res, err = fetch(ctx, r.client, f); err != nil {
		return nil, nil, err
	}
	if res.notModified {
		return nil, nil, errors.New("The site gave an unexpected answer (HTTP 304).")
	}
	return newFound(f, res), nil, nil
}

// withScheme adds https:// to an address typed without a scheme, such as
// example.com/feed or example.com:8443/feed, but not to one that has a
// scheme of its own (mailto:…).
func withScheme(address string) string {
	if address == "" || strings.Contains(address, "://") {
		return address
	}
	host := address
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	if name, port, found := strings.Cut(host, ":"); found &&
		(name == "" || port == "" || strings.Trim(port, "0123456789") != "") {
		return address // not a port: a scheme, or nothing usable
	}
	return "https://" + address
}

func newFound(f store.DueFeedsRow, res *fetchResult) *Found {
	return &Found{URL: f.Url, username: f.AuthUsername, password: f.AuthPassword, res: res}
}

// Subscribed stores the items of a feed that was just added, from the
// document Discover downloaded, so they show right away. A problem is
// recorded on the feed, as after a refresh. The favicon is looked for in the
// background, while the refresher runs (otherwise at the next refresh).
func (r *Refresher) Subscribed(ctx context.Context, id int64, found *Found) {
	username, password := found.Login()
	f := store.DueFeedsRow{ID: id, Url: found.URL, AuthUsername: username, AuthPassword: password}
	_, site := r.save(ctx, f, found.res)
	life := r.life.Load()
	if site == nil || life == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(*life, feedTimeout)
		defer cancel()
		r.checkFavicon(ctx, f, site)
	}()
}

// feedLinks returns the feeds a web page declares in its <head>, in order.
func feedLinks(page []byte, base *url.URL) []Choice {
	if base == nil {
		return nil
	}
	var out []Choice
	seen := make(map[string]bool)
	z := html.NewTokenizer(bytes.NewReader(page))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return out
		case html.EndTagToken:
			if name, _ := z.TagName(); string(name) == "head" {
				return out
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			t := z.Token()
			switch t.Data {
			case "body":
				return out
			case "link":
				var rel, typ, href, title string
				for _, a := range t.Attr {
					switch a.Key {
					case "rel":
						rel = strings.ToLower(a.Val)
					case "type":
						typ = strings.ToLower(strings.TrimSpace(a.Val))
					case "href":
						href = a.Val
					case "title":
						title = a.Val
					}
				}
				if !hasToken(rel, "alternate") || !feedTypes[typ] {
					continue
				}
				u := content.ResolveHTTP(base, href)
				if u == nil || seen[u.String()] {
					continue
				}
				// JSON Feeds are named so (feed.json); WordPress pages
				// also link to their REST API (/wp-json/…) this way.
				if typ == "application/json" && !strings.Contains(strings.ToLower(u.Path), "feed") {
					continue
				}
				seen[u.String()] = true
				out = append(out, Choice{URL: u.String(), Title: content.Text(title, 500)})
			}
		}
	}
}
