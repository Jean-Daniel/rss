package feeds

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"

	"fever/internal/content"
)

// newItem is a feed entry ready to be stored. Its HTML is only cleaned once
// the entry is known to be new (see html), as that is the costly part.
type newItem struct {
	guid      string
	url       string
	title     string
	author    string
	body      string // raw HTML from the feed
	base      string // address relative links in body are resolved against
	published time.Time
}

// html returns the entry's content, sanitized and with absolute links.
func (it newItem) html() string {
	return content.Clean(it.body, it.base)
}

// toItems turns parsed entries into items. feedURL is where the feed was
// fetched from; relative links are resolved against the feed's site address,
// or the feed address when the feed does not give one. Entries published
// before oldest are left out: they are past the retention period, and may
// have been deleted already.
func toItems(f *gofeed.Feed, feedURL string, now, oldest time.Time) []newItem {
	base := content.ParseHTTP(feedURL)
	if site := content.ResolveHTTP(base, f.Link); site != nil {
		base = site
	}
	seen := make(map[string]bool, len(f.Items))
	items := make([]newItem, 0, len(f.Items))
	for _, e := range f.Items {
		if e == nil {
			continue
		}
		link := ""
		if u := content.ResolveHTTP(base, firstNonEmpty(e.Link, firstOf(e.Links))); u != nil {
			link = u.String()
		}
		body := e.Content
		if strings.TrimSpace(body) == "" {
			body = e.Description
		}
		published := now
		if e.PublishedParsed != nil {
			published = *e.PublishedParsed
		} else if e.UpdatedParsed != nil {
			published = *e.UpdatedParsed
		}
		// A date in the future would pin the item at the top of every list.
		if published.After(now.Add(time.Hour)) {
			published = now
		}
		if published.Before(oldest) {
			continue
		}

		guid := strings.TrimSpace(e.GUID)
		if guid == "" {
			guid = link
		}
		if guid == "" {
			sum := sha256.Sum256([]byte(e.Title + "\x00" + e.Published + "\x00" + body))
			guid = "sha256:" + hex.EncodeToString(sum[:16])
		}
		// Fever kept 255 bytes: imported items must keep matching.
		guid = content.TruncateUTF8(guid, 255)
		if seen[guid] {
			continue // some feeds repeat entries
		}
		seen[guid] = true

		contentBase := link
		if contentBase == "" && base != nil {
			contentBase = base.String()
		}
		items = append(items, newItem{
			guid:      guid,
			url:       content.TruncateUTF8(link, 2000),
			title:     content.Text(e.Title, 1000),
			author:    content.Text(authorOf(e), 200),
			body:      body,
			base:      contentBase,
			published: published,
		})
	}
	return items
}

func authorOf(e *gofeed.Item) string {
	for _, p := range e.Authors {
		if p != nil && p.Name != "" {
			return p.Name
		}
	}
	if e.Author != nil {
		return e.Author.Name
	}
	return ""
}

// NormalizeURL rewrites the feed address forms browsers and old readers used
// (feed://host/…, feed:https://…, and the podcast schemes itpc:// and
// pcast://) into plain http(s) addresses.
func NormalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	lower := strings.ToLower(raw)
	for _, wrapped := range []string{"feed:http://", "feed:https://"} {
		if strings.HasPrefix(lower, wrapped) {
			return raw[len("feed:"):]
		}
	}
	for _, scheme := range []string{"feed://", "itpc://", "pcast://"} {
		if strings.HasPrefix(lower, scheme) {
			return "http://" + raw[len(scheme):]
		}
	}
	return raw
}

// siteOf picks the site to take a favicon from: the stored site address, the
// one the feed just gave, or the feed's own host.
func siteOf(stored, fromFeed string, feedURL *url.URL) *url.URL {
	for _, s := range []string{stored, fromFeed} {
		if u := content.ParseHTTP(s); u != nil {
			return u
		}
	}
	if feedURL != nil {
		return &url.URL{Scheme: feedURL.Scheme, Host: feedURL.Host, Path: "/"}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func firstOf(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
