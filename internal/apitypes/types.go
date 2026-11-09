// Package apitypes defines the JSON shapes of the HTTP API. The front end's
// TypeScript types are generated from this file (make generate).
package apitypes

import "time"

// ErrorResponse is the body of every non-2xx JSON response.
type ErrorResponse struct {
	Error string `json:"error"`
}

// Session answers GET /api/session.
type Session struct {
	Authenticated bool `json:"authenticated"`
	// PasswordSet is false until `fever set-password` has been run.
	PasswordSet bool `json:"passwordSet"`
}

// LoginRequest is the body of POST /api/session.
type LoginRequest struct {
	Password string `json:"password"`
}

// Subscriptions answers GET /api/subscriptions: everything the sidebar and
// the feed list need, with unread counts.
type Subscriptions struct {
	Groups      []Group `json:"groups"`
	Feeds       []Feed  `json:"feeds"`
	TotalUnread int64   `json:"totalUnread"`
	SavedCount  int64   `json:"savedCount"`
	// LastRefreshedAt is when a feed was last fetched (RFC 3339), or null.
	LastRefreshedAt *time.Time `json:"lastRefreshedAt"`
	// Refreshing is true while feeds are being fetched.
	Refreshing bool `json:"refreshing"`
	// RetentionWeeks is how long items are kept unless saved; a feed with
	// nothing new for that long is marked stale.
	RetentionWeeks int `json:"retentionWeeks"`
}

// RefreshRequest is the optional body of POST /api/refresh. Without a feed,
// every feed is refreshed.
type RefreshRequest struct {
	FeedID *int64 `json:"feedId"`
}

type Group struct {
	ID      int64   `json:"id"`
	Title   string  `json:"title"`
	Unread  int64   `json:"unread"`
	FeedIDs []int64 `json:"feedIds"`
	// ShowUnreadCount is null when the global preference applies.
	ShowUnreadCount *bool `json:"showUnreadCount"`
}

type Feed struct {
	ID              int64   `json:"id"`
	Title           string  `json:"title"`
	URL             string  `json:"url"`
	SiteURL         string  `json:"siteUrl"`
	FaviconID       *int64  `json:"faviconId"`
	Unread          int64   `json:"unread"`
	GroupIDs        []int64 `json:"groupIds"`
	ShowUnreadCount *bool   `json:"showUnreadCount"`
	// Error is the last fetch problem, if any.
	Error *string `json:"error"`
	// LastUpdatedAt is when the feed last brought new items (RFC 3339), or
	// null if it never did.
	LastUpdatedAt *time.Time `json:"lastUpdatedAt"`
	// Stale is true when the feed has brought nothing new for the retention
	// period (only once it has been fetched).
	Stale bool `json:"stale"`
}
