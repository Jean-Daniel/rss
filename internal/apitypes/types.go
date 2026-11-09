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
	// Defaults are the reading preferences used where a feed or group does
	// not set its own.
	Defaults Defaults `json:"defaults"`
}

type Defaults struct {
	OldestFirst bool `json:"oldestFirst"`
	ShowImages  bool `json:"showImages"`
	// ShowRead lists read items too, not only unread ones.
	ShowRead bool `json:"showRead"`
	// AutoRead marks items read as they scroll out of view.
	AutoRead bool `json:"autoRead"`
	PageSize int  `json:"pageSize"`
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
	// Display options; null when the global preference applies.
	ShowUnreadCount *bool `json:"showUnreadCount"`
	ShowImages      *bool `json:"showImages"`
	OldestFirst     *bool `json:"oldestFirst"`
}

type Feed struct {
	ID        int64   `json:"id"`
	Title     string  `json:"title"`
	URL       string  `json:"url"`
	SiteURL   string  `json:"siteUrl"`
	FaviconID *int64  `json:"faviconId"`
	Unread    int64   `json:"unread"`
	GroupIDs  []int64 `json:"groupIds"`
	// Display options; null when the group's or the global one applies.
	ShowUnreadCount *bool `json:"showUnreadCount"`
	ShowImages      *bool `json:"showImages"`
	OldestFirst     *bool `json:"oldestFirst"`
	// Error is the last fetch problem, if any.
	Error *string `json:"error"`
	// LastUpdatedAt is when the feed last brought new items (RFC 3339), or
	// null if it never did.
	LastUpdatedAt *time.Time `json:"lastUpdatedAt"`
	// Stale is true when the feed has brought nothing new for the retention
	// period (only once it has been fetched).
	Stale bool `json:"stale"`
}

// Item is an entry in an item list. Content is sanitized HTML.
type Item struct {
	ID          int64     `json:"id"`
	FeedID      int64     `json:"feedId"`
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Author      string    `json:"author"`
	PublishedAt time.Time `json:"publishedAt"`
	Read        bool      `json:"read"`
	Saved       bool      `json:"saved"`
	Content     string    `json:"content"`
}

// ItemPage answers GET /api/items.
type ItemPage struct {
	Items []Item `json:"items"`
	// Next is the cursor of the following page (the `after` parameter), or
	// null at the end of the list.
	Next *string `json:"next"`
	// LoadedAt is the server time of the request: the `before` of "mark all
	// as read", and the `since` for GET /api/items/changes.
	LoadedAt time.Time `json:"loadedAt"`
}

// ItemStateRequest is the body of POST /api/items/state. Read and Saved are
// applied when not null.
type ItemStateRequest struct {
	IDs   []int64 `json:"ids"`
	Read  *bool   `json:"read"`
	Saved *bool   `json:"saved"`
}

// MarkAllReadRequest is the body of POST /api/items/read-all. Scope is
// "all", "group" or "feed" (with ID); only items that had arrived at Before
// are marked.
type MarkAllReadRequest struct {
	Scope  string    `json:"scope"`
	ID     *int64    `json:"id"`
	Before time.Time `json:"before"`
}

// ItemChanges answers GET /api/items/changes?since=…: read and saved states
// changed since then, on any device. When Next is set more changes follow:
// ask again with after=Next. LoadedAt is the `since` of the next sync.
type ItemChanges struct {
	Changes  []ItemChange `json:"changes"`
	Next     *string      `json:"next"`
	LoadedAt time.Time    `json:"loadedAt"`
}

type ItemChange struct {
	ID    int64 `json:"id"`
	Read  bool  `json:"read"`
	Saved bool  `json:"saved"`
}
