package feeds

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mmcdole/gofeed"

	"fever/internal/store"
)

// maxFeedBytes caps a feed document. Real feeds are far smaller; this only
// protects a small server from a runaway response.
const maxFeedBytes = 8 << 20

// maxValidatorBytes caps a stored ETag or Last-Modified value.
const maxValidatorBytes = 1024

type fetchResult struct {
	notModified  bool
	feed         *gofeed.Feed
	size         int // bytes of the feed document
	etag         string
	lastModified string
	finalURL     string
}

// httpError is a response that was neither a feed nor "not modified".
type httpError struct {
	status     int
	retryAfter time.Duration
}

func (e *httpError) Error() string {
	text := http.StatusText(e.status)
	switch e.status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Sprintf("Access denied (HTTP %d). The feed may need a username and password.", e.status)
	case http.StatusNotFound, http.StatusGone:
		return fmt.Sprintf("The feed is not there any more (HTTP %d %s).", e.status, text)
	case http.StatusTooManyRequests:
		return "The site asks to slow down (HTTP 429)."
	}
	return fmt.Sprintf("The site answered HTTP %d %s.", e.status, text)
}

// fetch downloads and parses one feed, using its validators so unchanged
// feeds cost a single short response.
func fetch(ctx context.Context, client *http.Client, f store.DueFeedsRow) (*fetchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, NormalizeURL(f.Url), nil)
	if err != nil {
		return nil, fmt.Errorf("The feed address is not valid: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/feed+json, application/xml;q=0.9, text/xml;q=0.9, */*;q=0.8")
	if f.Etag != nil && *f.Etag != "" {
		req.Header.Set("If-None-Match", *f.Etag)
	}
	if f.LastModified != nil && *f.LastModified != "" {
		req.Header.Set("If-Modified-Since", *f.LastModified)
	}
	if f.AuthUsername != nil {
		password := ""
		if f.AuthPassword != nil {
			password = *f.AuthPassword
		}
		req.SetBasicAuth(*f.AuthUsername, password)
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, describeNetError(err)
	}
	defer res.Body.Close()

	result := &fetchResult{
		etag:         validator(res.Header.Get("ETag")),
		lastModified: validator(res.Header.Get("Last-Modified")),
		finalURL:     res.Request.URL.String(),
	}
	switch {
	case res.StatusCode == http.StatusNotModified:
		result.notModified = true
		return result, nil
	case res.StatusCode != http.StatusOK:
		return nil, &httpError{status: res.StatusCode, retryAfter: parseRetryAfter(res.Header.Get("Retry-After"))}
	}

	body, err := readLimited(res.Body, maxFeedBytes)
	if errors.Is(err, errTooLarge) {
		return nil, fmt.Errorf("The feed is larger than %d MB.", maxFeedBytes>>20)
	}
	if err != nil {
		return nil, describeNetError(err)
	}
	feed, err := gofeed.NewParser().Parse(bytes.NewReader(body))
	if err != nil {
		if errors.Is(err, gofeed.ErrFeedTypeNotDetected) {
			return nil, errors.New("The address does not return a feed (it may be a web page).")
		}
		return nil, fmt.Errorf("The feed could not be read: %v", err)
	}
	result.feed = feed
	result.size = len(body)
	return result, nil
}

// validator returns an ETag or Last-Modified value that can be stored and
// sent back as it is. Anything else is dropped, and the feed is downloaded in
// full next time: the database refuses text that is not UTF-8 or holds a NUL,
// and a request with control characters in a header cannot be sent.
func validator(v string) string {
	if len(v) > maxValidatorBytes || !utf8.ValidString(v) {
		return ""
	}
	for i := 0; i < len(v); i++ {
		if c := v[i]; c < ' ' && c != '\t' || c == 0x7f {
			return ""
		}
	}
	return v
}

func describeNetError(err error) error {
	var dnsErr *net.DNSError
	switch {
	case errors.As(err, &dnsErr) && dnsErr.IsNotFound:
		return fmt.Errorf("The site's name does not exist (%s).", dnsErr.Name)
	case errors.Is(err, context.DeadlineExceeded), isTimeout(err):
		return errors.New("The site did not answer in time.")
	}
	msg := err.Error()
	// Drop Go's `Get "https://…": ` prefix, the feed address is shown anyway.
	if i := strings.Index(msg, `": `); i >= 0 && strings.HasPrefix(msg, "Get ") {
		msg = msg[i+3:]
	}
	return fmt.Errorf("The feed could not be reached: %s", msg)
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// parseRetryAfter reads a Retry-After header (seconds or an HTTP date).
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return time.Until(t)
	}
	return 0
}
