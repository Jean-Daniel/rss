package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"fever/internal/apitypes"
	"fever/internal/auth"
	"fever/internal/config"
	"fever/internal/db"
	"fever/internal/feeds"
	"fever/internal/store"
)

type idleRefresher struct{}

func (idleRefresher) RefreshAll(context.Context) error                  { return nil }
func (idleRefresher) RefreshFeed(context.Context, int64) (bool, error)  { return true, nil }
func (idleRefresher) RefreshGroup(context.Context, int64) (bool, error) { return true, nil }
func (idleRefresher) Wake()                                             {}
func (idleRefresher) Busy() bool                                        { return false }
func (idleRefresher) Discover(context.Context, string, *string, *string) (*feeds.Found, []feeds.Choice, error) {
	return nil, nil, errors.New("not available in tests")
}
func (idleRefresher) Subscribed(context.Context, int64, *feeds.Found) {}

// apiClient talks to a test server with a session cookie.
type apiClient struct {
	t    *testing.T
	base string
	http *http.Client
}

func (c apiClient) do(method, path string, body any) (int, []byte) {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		r = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, c.base+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, out
}

func (c apiClient) items(query string) apitypes.ItemPage {
	c.t.Helper()
	status, body := c.do("GET", "/api/items?"+query, nil)
	if status != http.StatusOK {
		c.t.Fatalf("GET /api/items?%s: %d %s", query, status, body)
	}
	var page apitypes.ItemPage
	if err := json.Unmarshal(body, &page); err != nil {
		c.t.Fatal(err)
	}
	return page
}

func ids(page apitypes.ItemPage) []int64 {
	out := make([]int64, len(page.Items))
	for i, it := range page.Items {
		out[i] = it.ID
	}
	return out
}

func sameIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestItemsAPI needs a scratch database in TEST_DATABASE_URL (v2 tables
// there are wiped).
func TestItemsAPI(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := db.Open(ctx, dsn, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool, log); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `
		TRUNCATE items, item_search, expired_items, feed_groups, feeds, groups, favicons, sessions, devices;
		INSERT INTO feeds (id, url) VALUES (1, 'https://a.example/feed'), (2, 'https://b.example/feed');
		INSERT INTO groups (id, title) VALUES (1, 'G');
		INSERT INTO feed_groups VALUES (2, 1);
		INSERT INTO items (id, feed_id, guid, published_at, added_at)
		SELECT n, CASE WHEN n <= 5 THEN 1 ELSE 2 END, 'g' || n,
		       timestamptz '2026-11-01' + n * interval '1 day', now() - interval '1 hour'
		FROM generate_series(1, 7) n;
		UPDATE items SET read_at = now(), saved_at = now() WHERE id = 5;
		-- Earlier than anything the test changes.
		UPDATE items SET state_changed_at = now() - interval '1 hour';`)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("test password")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.New(pool).SetPasswordHash(ctx, &hash); err != nil {
		t.Fatal(err)
	}

	server := New(config.Config{}, pool, log, idleRefresher{}, nil)
	srv := httptest.NewServer(server.Handler())
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	c := apiClient{t: t, base: srv.URL, http: &http.Client{Jar: jar}}

	if status, _ := c.do("GET", "/api/items", nil); status != http.StatusUnauthorized {
		t.Fatalf("items without a session: %d, want 401", status)
	}
	if status, body := c.do("POST", "/api/session", apitypes.LoginRequest{Password: "test password"}); status != http.StatusOK {
		t.Fatalf("login: %d %s", status, body)
	}

	for _, bad := range []string{"scope=bogus", "scope=feed", "scope=feed&id=1&after=nope", "limit=0", "limit=500",
		"scope=search", "scope=search&q=+", "scope=search&q=" + strings.Repeat("%C3%A9", 201)} {
		if status, _ := c.do("GET", "/api/items?"+bad, nil); status != http.StatusBadRequest {
			t.Errorf("GET /api/items?%s: %d, want 400", bad, status)
		}
	}

	// Paging, newest first, then oldest first.
	first := c.items("scope=feed&id=1&limit=2")
	if !sameIDs(ids(first), []int64{5, 4}) || first.Next == nil {
		t.Fatalf("first page = %v next=%v", ids(first), first.Next)
	}
	second := c.items("scope=feed&id=1&limit=2&after=" + url.QueryEscape(*first.Next))
	if !sameIDs(ids(second), []int64{3, 2}) {
		t.Errorf("second page = %v", ids(second))
	}
	if got := ids(c.items("scope=feed&id=1&order=oldest&unread=1")); !sameIDs(got, []int64{1, 2, 3, 4}) {
		t.Errorf("oldest unread = %v", got)
	}
	// Search results are listed whether read or not (none here: no titles).
	if page := c.items("scope=search&q=anything&unread=1"); len(page.Items) != 0 || page.Next != nil {
		t.Errorf("search = %v", ids(page))
	}
	// 200 characters, whatever their size in bytes.
	c.items("scope=search&q=" + strings.Repeat("%C3%A9", 200))
	// Saved lists ignore the unread filter: item 5 is read and saved.
	if got := ids(c.items("scope=saved&unread=1")); !sameIDs(got, []int64{5}) {
		t.Errorf("saved = %v", got)
	}
	if got := ids(c.items("scope=group&id=1")); !sameIDs(got, []int64{7, 6}) {
		t.Errorf("group = %v", got)
	}

	// State changes.
	since := time.Now().Add(-time.Second)
	if status, _ := c.do("POST", "/api/items/state", map[string]any{"ids": []int64{}, "read": true}); status != http.StatusBadRequest {
		t.Errorf("empty ids: %d, want 400", status)
	}
	if status, body := c.do("POST", "/api/items/state", map[string]any{"ids": []int64{1, 2}, "read": true}); status != http.StatusNoContent {
		t.Fatalf("mark read: %d %s", status, body)
	}
	if status, _ := c.do("POST", "/api/items/state", map[string]any{"ids": []int64{3}, "saved": true}); status != http.StatusNoContent {
		t.Fatalf("save: %d", status)
	}
	if got := ids(c.items("scope=feed&id=1&order=oldest&unread=1")); !sameIDs(got, []int64{3, 4}) {
		t.Errorf("unread after marking 1 and 2 = %v", got)
	}

	// Mark all as read: the scope's id is required, and only that scope changes.
	before := time.Now()
	for _, bad := range []map[string]any{
		{"scope": "group", "before": before},
		{"scope": "feed", "before": before},
		{"scope": "all"},
		{"scope": "nope", "before": before},
	} {
		if status, _ := c.do("POST", "/api/items/read-all", bad); status != http.StatusBadRequest {
			t.Errorf("read-all %v: %d, want 400", bad, status)
		}
	}
	if status, body := c.do("POST", "/api/items/read-all", map[string]any{"scope": "group", "id": 1, "before": before}); status != http.StatusNoContent {
		t.Fatalf("read-all group: %d %s", status, body)
	}
	if got := ids(c.items("scope=all&unread=1&order=oldest")); !sameIDs(got, []int64{3, 4}) {
		t.Errorf("unread after marking group 1 read = %v, want feed 1's [3 4]", got)
	}

	// Changes since `since`, two at a time: 1, 2 (read), 3 (saved), 6, 7 (group).
	maxChanges = 2
	defer func() { maxChanges = 2000 }()
	var changed []int64
	query := "since=" + url.QueryEscape(since.Format(time.RFC3339Nano))
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("changes never end")
		}
		status, body := c.do("GET", "/api/items/changes?"+query, nil)
		if status != http.StatusOK {
			t.Fatalf("changes: %d %s", status, body)
		}
		var res apitypes.ItemChanges
		if err := json.Unmarshal(body, &res); err != nil {
			t.Fatal(err)
		}
		for _, ch := range res.Changes {
			changed = append(changed, ch.ID)
		}
		if res.Next == nil {
			break
		}
		query = "after=" + url.QueryEscape(*res.Next)
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i] < changed[j] })
	if !sameIDs(changed, []int64{1, 2, 3, 6, 7}) {
		t.Errorf("changes = %v, want [1 2 3 6 7]", changed)
	}

	// While a state change is running, answers are dated from its start: the
	// next sync then asks for it again, by which time it is committed.
	loadedAt := func() (page, changes time.Time) {
		t.Helper()
		status, body := c.do("GET", "/api/items/changes?since="+url.QueryEscape(time.Now().Format(time.RFC3339Nano)), nil)
		if status != http.StatusOK {
			t.Fatalf("changes: %d %s", status, body)
		}
		var res apitypes.ItemChanges
		if err := json.Unmarshal(body, &res); err != nil {
			t.Fatal(err)
		}
		return c.items("limit=1").LoadedAt, res.LoadedAt
	}
	done := server.changing.begin()
	running := time.Now()
	if page, changes := loadedAt(); page.After(running) || changes.After(running) {
		t.Errorf("during a state change: loaded at %v and %v, want no later than its start %v", page, changes, running)
	}
	done()
	if page, changes := loadedAt(); page.Before(running) || changes.Before(running) {
		t.Errorf("after the state change: loaded at %v and %v, want after %v", page, changes, running)
	}
}

func TestStateChanges(t *testing.T) {
	var c stateChanges
	before := time.Now()
	if got := c.settledAt(); got.Before(before) {
		t.Errorf("nothing running: settled at %v, want the present (%v or later)", got, before)
	}
	first := c.begin()
	afterFirst := time.Now()
	second := c.begin()
	if got := c.settledAt(); got.Before(before) || got.After(afterFirst) {
		t.Errorf("two running: settled at %v, want the start of the first (%v to %v)", got, before, afterFirst)
	}
	first()
	if got := c.settledAt(); got.Before(afterFirst) {
		t.Errorf("first done: settled at %v, want the start of the second (%v or later)", got, afterFirst)
	}
	second()
	afterBoth := time.Now()
	if got := c.settledAt(); got.Before(afterBoth) {
		t.Errorf("both done: settled at %v, want the present (%v or later)", got, afterBoth)
	}
	if len(c.started) != 0 {
		t.Errorf("%d requests still recorded", len(c.started))
	}
}
