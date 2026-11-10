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
	Preferences `tstype:",extends"`
	PageSize    int `json:"pageSize"`
}

// RefreshRequest is the optional body of POST /api/refresh. Without a feed
// or a group, every feed is refreshed.
type RefreshRequest struct {
	FeedID  *int64 `json:"feedId"`
	GroupID *int64 `json:"groupId"`
}

// UnreadRecent answers POST /api/items/unread-recent: the items that were
// marked unread again.
type UnreadRecent struct {
	Items []ItemRef `json:"items"`
}

// ItemRef is an item and its feed.
type ItemRef struct {
	ID     int64 `json:"id"`
	FeedID int64 `json:"feedId"`
}

type Group struct {
	ID             int64   `json:"id"`
	Title          string  `json:"title"`
	Unread         int64   `json:"unread"`
	FeedIDs        []int64 `json:"feedIds"`
	DisplayOptions `tstype:",extends"`
}

type Feed struct {
	ID             int64   `json:"id"`
	Title          string  `json:"title"`
	URL            string  `json:"url"`
	SiteURL        string  `json:"siteUrl"`
	FaviconID      *int64  `json:"faviconId"`
	Unread         int64   `json:"unread"`
	GroupIDs       []int64 `json:"groupIds"`
	DisplayOptions `tstype:",extends"`
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

// Preferences is the body of PUT /api/preferences: the reading preferences
// used where a group or feed sets nothing (Subscriptions.Defaults).
type Preferences struct {
	OldestFirst bool `json:"oldestFirst"`
	ShowImages  bool `json:"showImages"`
	// ShowRead lists read items too, not only unread ones, on devices where
	// the reader has not chosen between Unread and All.
	ShowRead bool `json:"showRead"`
	// AutoRead marks items read as they scroll out of view.
	AutoRead bool `json:"autoRead"`
}

// DisplayOptions are a group's or a feed's own reading options; null means
// the next level applies (the group's, then the preferences).
type DisplayOptions struct {
	ShowImages      *bool `json:"showImages"`
	OldestFirst     *bool `json:"oldestFirst"`
	ShowUnreadCount *bool `json:"showUnreadCount"`
}

// AddFeedRequest is the body of POST /api/feeds. URL may be a web page that
// links to its feed.
type AddFeedRequest struct {
	URL      string  `json:"url"`
	GroupIDs []int64 `json:"groupIds"`
	// Username and Password are for feeds behind a login; null otherwise.
	// The username may be empty when the feed only wants a password.
	Username *string `json:"username"`
	Password *string `json:"password"`
}

// AddFeedResponse answers POST /api/feeds. Either FeedID is set (the new
// feed, or the one already subscribed to when Existing), or Choices lists the
// feeds a web page links to: ask again with one of their URLs.
type AddFeedResponse struct {
	FeedID   *int64       `json:"feedId"`
	Existing bool         `json:"existing"`
	Choices  []FeedChoice `json:"choices" tstype:"FeedChoice[] | null"`
}

type FeedChoice struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// FeedSettings answers GET /api/feeds/{id}: what the feed's settings page
// shows besides Subscriptions.
type FeedSettings struct {
	ID int64 `json:"id"`
	// Title is the stored title: the reader's, or the feed's own. Empty
	// until the feed has been fetched.
	Title          string  `json:"title"`
	URL            string  `json:"url"`
	GroupIDs       []int64 `json:"groupIds"`
	DisplayOptions `tstype:",extends"`
	Username       *string `json:"username"`
	HasPassword    bool    `json:"hasPassword"`
	ItemCount      int64   `json:"itemCount"`
	SavedCount     int64   `json:"savedCount"`
}

// FeedUpdate is the body of PUT /api/feeds/{id}.
type FeedUpdate struct {
	// Title is empty to use the feed's own.
	Title          string  `json:"title"`
	URL            string  `json:"url"`
	GroupIDs       []int64 `json:"groupIds"`
	DisplayOptions `tstype:",extends"`
	// Username is null when the feed needs no login. It may be empty: some
	// feeds only want a password (a token).
	Username *string `json:"username"`
	// Password replaces the stored one when not null.
	Password *string `json:"password"`
}

// GroupUpdate is the body of POST /api/groups and PUT /api/groups/{id}.
type GroupUpdate struct {
	Title          string  `json:"title"`
	FeedIDs        []int64 `json:"feedIds"`
	DisplayOptions `tstype:",extends"`
}

// Created answers a POST that created something.
type Created struct {
	ID int64 `json:"id"`
}

// OPMLImport answers POST /api/opml (the body is the OPML file).
type OPMLImport struct {
	FeedsAdded  int `json:"feedsAdded"`
	FeedsKnown  int `json:"feedsKnown"`
	GroupsAdded int `json:"groupsAdded"`
	// Skipped counts entries whose address is not a web address.
	Skipped int `json:"skipped"`
}
