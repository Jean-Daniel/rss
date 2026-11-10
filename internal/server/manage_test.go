package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fever/internal/apitypes"
	"fever/internal/auth"
	"fever/internal/config"
	"fever/internal/db"
	"fever/internal/feeds"
	"fever/internal/store"
)

// startAPI runs the server against the scratch database in
// TEST_DATABASE_URL (its v2 tables are wiped) and logs in.
func startAPI(t *testing.T, refresher Refresher) (apiClient, *pgxpool.Pool) {
	t.Helper()
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
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool, log); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `
		TRUNCATE items, item_search, expired_items, feed_groups, feeds, groups, favicons, sessions, devices;
		UPDATE settings SET oldest_first = true, show_images = true, show_read = false, auto_read = true;`)
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
	srv := httptest.NewServer(New(config.Config{}, pool, log, refresher, nil).Handler())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	c := apiClient{t: t, base: srv.URL, http: &http.Client{Jar: jar}}
	if status, body := c.do("POST", "/api/session", apitypes.LoginRequest{Password: "test password"}); status != http.StatusOK {
		t.Fatalf("login: %d %s", status, body)
	}
	return c, pool
}

// call makes a request that must answer with status, and decodes the body
// into out (if not nil).
func (c apiClient) call(method, path string, body any, status int, out any) {
	c.t.Helper()
	got, data := c.do(method, path, body)
	if got != status {
		c.t.Fatalf("%s %s: %d %s, want %d", method, path, got, data, status)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			c.t.Fatalf("%s %s: %v", method, path, err)
		}
	}
}

func (c apiClient) upload(path, contentType string, body []byte) (int, []byte) {
	c.t.Helper()
	req, _ := http.NewRequest("POST", c.base+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, out
}

func (c apiClient) subscriptions() apitypes.Subscriptions {
	c.t.Helper()
	var subs apitypes.Subscriptions
	c.call("GET", "/api/subscriptions", nil, http.StatusOK, &subs)
	return subs
}

func feedNamed(subs apitypes.Subscriptions, title string) *apitypes.Feed {
	for i := range subs.Feeds {
		if subs.Feeds[i].Title == title {
			return &subs.Feeds[i]
		}
	}
	return nil
}

func ptr[T any](v T) *T { return &v }

// testSite serves feeds and web pages that link to them.
func testSite(t *testing.T) *httptest.Server {
	rss := func(title string, n int) string {
		var items strings.Builder
		for i := 1; i <= n; i++ {
			fmt.Fprintf(&items, "<item><title>%s %d</title><guid>%s-%d</guid><pubDate>%s</pubDate></item>",
				title, i, title, i, time.Now().Add(-time.Duration(i)*time.Hour).Format(time.RFC1123Z))
		}
		return `<?xml version="1.0"?><rss version="2.0"><channel><title>` + title +
			`</title><link>https://site.example/</link>` + items.String() + `</channel></rss>`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/blog.xml", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, rss("Blog", 3)) })
	mux.HandleFunc("/news.xml", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, rss("News", 2)) })
	mux.HandleFunc("/comments.xml", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, rss("Comments", 1)) })
	mux.HandleFunc("/blog", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<html><head><link rel="alternate" type="application/rss+xml" href="/blog.xml"></head></html>`)
	})
	mux.HandleFunc("/news", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<html><head>
			<link rel="alternate" type="application/rss+xml" title="News" href="/news.xml">
			<link rel="alternate" type="application/rss+xml" title="Comments" href="/comments.xml">
			</head></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestManagement(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	site := testSite(t)
	refresher := &lateRefresher{}
	c, pool := startAPI(t, refresher)
	refresher.Refresher = feeds.NewRefresher(pool, log, feeds.Options{Interval: 15 * time.Minute, AllowPrivate: true})

	// Preferences.
	if d := c.subscriptions().Defaults; !d.OldestFirst || !d.ShowImages || d.ShowRead || !d.AutoRead || d.PageSize != 20 {
		t.Errorf("default preferences = %+v", d)
	}
	c.call("PUT", "/api/preferences", apitypes.Preferences{OldestFirst: false, ShowImages: false, ShowRead: true, AutoRead: false}, http.StatusNoContent, nil)
	if d := c.subscriptions().Defaults; d.OldestFirst || d.ShowImages || !d.ShowRead || d.AutoRead {
		t.Errorf("preferences after the change = %+v", d)
	}

	// Groups.
	var tech, empty apitypes.Created
	c.call("POST", "/api/groups", apitypes.GroupUpdate{Title: "  Tech "}, http.StatusCreated, &tech)
	c.call("POST", "/api/groups", apitypes.GroupUpdate{Title: "Empty", FeedIDs: []int64{999}}, http.StatusCreated, &empty)
	c.call("POST", "/api/groups", apitypes.GroupUpdate{Title: " "}, http.StatusBadRequest, nil)

	// Adding feeds: a web page that links to one feed, then to two.
	var added apitypes.AddFeedResponse
	c.call("POST", "/api/feeds", apitypes.AddFeedRequest{URL: site.URL + "/blog", GroupIDs: []int64{tech.ID}}, http.StatusCreated, &added)
	if added.FeedID == nil || added.Existing {
		t.Fatalf("add blog: %+v", added)
	}
	blog := *added.FeedID
	if page := c.items(fmt.Sprintf("scope=feed&id=%d", blog)); len(page.Items) != 3 {
		t.Errorf("blog items stored right away: %d, want 3", len(page.Items))
	}
	subs := c.subscriptions()
	if f := feedNamed(subs, "Blog"); f == nil || f.URL != site.URL+"/blog.xml" || len(f.GroupIDs) != 1 || f.GroupIDs[0] != tech.ID || f.Unread != 3 || f.Error != nil {
		t.Errorf("blog in the subscriptions: %+v", f)
	}
	c.call("POST", "/api/feeds", apitypes.AddFeedRequest{URL: site.URL + "/blog.xml"}, http.StatusOK, &added)
	if added.FeedID == nil || *added.FeedID != blog || !added.Existing {
		t.Errorf("adding the blog again: %+v", added)
	}
	c.call("POST", "/api/feeds", apitypes.AddFeedRequest{URL: site.URL + "/news"}, http.StatusOK, &added)
	if added.FeedID != nil || len(added.Choices) != 2 || added.Choices[1].Title != "Comments" {
		t.Fatalf("page with two feeds: %+v", added)
	}
	c.call("POST", "/api/feeds", apitypes.AddFeedRequest{URL: added.Choices[0].URL}, http.StatusCreated, &added)
	news := *added.FeedID
	if status, body := c.do("POST", "/api/feeds", apitypes.AddFeedRequest{URL: site.URL + "/missing"}); status != http.StatusUnprocessableEntity || !strings.Contains(string(body), "not there") {
		t.Errorf("missing feed: %d %s", status, body)
	}

	// Feed settings.
	var settings apitypes.FeedSettings
	c.call("GET", fmt.Sprintf("/api/feeds/%d", blog), nil, http.StatusOK, &settings)
	if settings.Title != "Blog" || settings.ItemCount != 3 || settings.HasPassword || len(settings.GroupIDs) != 1 {
		t.Errorf("blog settings: %+v", settings)
	}
	update := apitypes.FeedUpdate{
		Title:          "My blog",
		URL:            site.URL + "/blog.xml",
		GroupIDs:       []int64{tech.ID, empty.ID},
		DisplayOptions: apitypes.DisplayOptions{ShowImages: ptr(false), OldestFirst: ptr(true)},
		Username:       ptr("joe"),
		Password:       ptr("secret"),
	}
	c.call("PUT", fmt.Sprintf("/api/feeds/%d", blog), update, http.StatusNoContent, nil)
	c.call("GET", fmt.Sprintf("/api/feeds/%d", blog), nil, http.StatusOK, &settings)
	if settings.Title != "My blog" || len(settings.GroupIDs) != 2 || settings.ShowImages == nil || *settings.ShowImages ||
		settings.ShowUnreadCount != nil || settings.Username == nil || *settings.Username != "joe" || !settings.HasPassword {
		t.Errorf("blog settings after the change: %+v", settings)
	}
	if !refresher.woken {
		t.Error("a new login does not fetch the feed again")
	}
	// The password is kept unless a new one is given.
	update.Password = nil
	c.call("PUT", fmt.Sprintf("/api/feeds/%d", blog), update, http.StatusNoContent, nil)
	c.call("GET", fmt.Sprintf("/api/feeds/%d", blog), nil, http.StatusOK, &settings)
	if !settings.HasPassword {
		t.Error("password lost when not given")
	}
	update.URL = site.URL + "/news.xml"
	c.call("PUT", fmt.Sprintf("/api/feeds/%d", blog), update, http.StatusConflict, nil)
	update.URL = "not an address"
	c.call("PUT", fmt.Sprintf("/api/feeds/%d", blog), update, http.StatusBadRequest, nil)
	c.call("PUT", "/api/feeds/12345", apitypes.FeedUpdate{URL: site.URL + "/x"}, http.StatusNotFound, nil)

	// Group settings: the feeds listed are exactly the group's.
	c.call("PUT", fmt.Sprintf("/api/groups/%d", tech.ID), apitypes.GroupUpdate{
		Title:          "Technology",
		FeedIDs:        []int64{news},
		DisplayOptions: apitypes.DisplayOptions{ShowUnreadCount: ptr(false)},
	}, http.StatusNoContent, nil)
	subs = c.subscriptions()
	for _, g := range subs.Groups {
		switch g.ID {
		case tech.ID:
			if g.Title != "Technology" || len(g.FeedIDs) != 1 || g.FeedIDs[0] != news || g.ShowUnreadCount == nil || *g.ShowUnreadCount {
				t.Errorf("group after the change: %+v", g)
			}
		case empty.ID:
			if len(g.FeedIDs) != 1 || g.FeedIDs[0] != blog {
				t.Errorf("other group: %+v", g)
			}
		}
	}
	c.call("PUT", "/api/groups/12345", apitypes.GroupUpdate{Title: "x"}, http.StatusNotFound, nil)

	// OPML export, then import into an empty reader.
	res, err := c.http.Get(c.base + "/api/opml")
	if err != nil {
		t.Fatal(err)
	}
	exported, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.Contains(res.Header.Get("Content-Disposition"), "attachment") ||
		!strings.Contains(string(exported), `<outline text="Technology" title="Technology">`) ||
		!strings.Contains(string(exported), `xmlUrl="`+site.URL+`/news.xml"`) {
		t.Fatalf("export: %d %s", res.StatusCode, exported)
	}
	c.call("DELETE", fmt.Sprintf("/api/feeds/%d", blog), nil, http.StatusNoContent, nil)
	c.call("DELETE", fmt.Sprintf("/api/feeds/%d", blog), nil, http.StatusNotFound, nil)
	c.call("DELETE", fmt.Sprintf("/api/groups/%d", empty.ID), nil, http.StatusNoContent, nil)
	if subs := c.subscriptions(); len(subs.Feeds) != 1 || len(subs.Groups) != 1 || subs.TotalUnread != 2 {
		t.Errorf("after deleting: %d feeds, %d groups, %d unread", len(subs.Feeds), len(subs.Groups), subs.TotalUnread)
	}
	refresher.woken = false
	status, body := c.upload("/api/opml", "text/x-opml", exported)
	var imported apitypes.OPMLImport
	json.Unmarshal(body, &imported)
	if status != http.StatusOK || imported != (apitypes.OPMLImport{FeedsAdded: 1, FeedsKnown: 1, GroupsAdded: 1}) {
		t.Errorf("import: %d %s", status, body)
	}
	if !refresher.woken {
		t.Error("imported feeds are not fetched")
	}
	subs = c.subscriptions()
	// The blog was in the group "Empty" only, deleted since: it is created again.
	if f := feedNamed(subs, "My blog"); f == nil || len(f.GroupIDs) != 1 || f.Unread != 0 {
		t.Errorf("imported blog: %+v", f)
	}
	if status, _ := c.upload("/api/opml", "text/xml", []byte("<html>no</html>")); status != http.StatusBadRequest {
		t.Errorf("import of a file that is not OPML: %d", status)
	}
	if status, _ := c.upload("/api/opml", "text/xml", bytes.Repeat([]byte(" "), maxOPMLBytes+1)); status != http.StatusRequestEntityTooLarge {
		t.Errorf("import of a large file: %d", status)
	}
}

// lateRefresher is a real refresher, set once the test has its database,
// that records when it is woken.
type lateRefresher struct {
	*feeds.Refresher
	woken bool
}

func (r *lateRefresher) Wake() { r.woken = true }

func TestFeedLogins(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	site := testSite(t)
	// Another site whose page links to a feed of the first one.
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<html><head><link rel="alternate" type="application/rss+xml" href="`+site.URL+`/news.xml"></head></html>`)
	}))
	t.Cleanup(other.Close)
	refresher := &lateRefresher{}
	c, pool := startAPI(t, refresher)
	refresher.Refresher = feeds.NewRefresher(pool, log, feeds.Options{Interval: 15 * time.Minute, AllowPrivate: true})

	settings := func(id int64) apitypes.FeedSettings {
		t.Helper()
		var s apitypes.FeedSettings
		c.call("GET", fmt.Sprintf("/api/feeds/%d", id), nil, http.StatusOK, &s)
		return s
	}
	var added apitypes.AddFeedResponse
	c.call("POST", "/api/feeds", apitypes.AddFeedRequest{URL: site.URL + "/blog", Username: ptr("joe"), Password: ptr("secret")}, http.StatusCreated, &added)
	if s := settings(*added.FeedID); s.Username == nil || *s.Username != "joe" || !s.HasPassword {
		t.Errorf("feed of the site given: login not kept: %+v", s)
	}
	c.call("POST", "/api/feeds", apitypes.AddFeedRequest{URL: other.URL + "/", Username: ptr("joe"), Password: ptr("secret")}, http.StatusCreated, &added)
	news := *added.FeedID
	if s := settings(news); s.Username != nil || s.HasPassword {
		t.Errorf("feed of another site: login kept: %+v", s)
	}

	// A password without a username (a token), kept when not given again.
	update := apitypes.FeedUpdate{URL: site.URL + "/news.xml", Title: "News", Username: ptr(""), Password: ptr("token")}
	c.call("PUT", fmt.Sprintf("/api/feeds/%d", news), update, http.StatusNoContent, nil)
	if s := settings(news); s.Username == nil || *s.Username != "" || !s.HasPassword {
		t.Errorf("password-only login: %+v", s)
	}
	update.Password = nil
	c.call("PUT", fmt.Sprintf("/api/feeds/%d", news), update, http.StatusNoContent, nil)
	if s := settings(news); !s.HasPassword {
		t.Errorf("password-only login lost when the password is not given again: %+v", s)
	}
	update.Username, update.Password = nil, ptr("")
	c.call("PUT", fmt.Sprintf("/api/feeds/%d", news), update, http.StatusNoContent, nil)
	if s := settings(news); s.Username != nil || s.HasPassword {
		t.Errorf("login not removed: %+v", s)
	}
}

// Addresses are compared as the importer stores them.
func TestFeedAddresses(t *testing.T) {
	c, pool := startAPI(t, idleRefresher{})
	ctx := context.Background()
	const addr = "HTTP://Example.test/Fil Rouge.xml"
	_, err := pool.Exec(ctx, `INSERT INTO feeds (id, url, title, error_count, next_fetch_at) VALUES (50, $1, 'Fil', 3, now() + interval '1 day')`, addr)
	if err != nil {
		t.Fatal(err)
	}
	var added apitypes.AddFeedResponse
	c.call("POST", "/api/feeds", apitypes.AddFeedRequest{URL: " " + addr}, http.StatusOK, &added)
	if added.FeedID == nil || *added.FeedID != 50 || !added.Existing {
		t.Errorf("adding a feed by its stored address: %+v", added)
	}
	c.call("PUT", "/api/feeds/50", apitypes.FeedUpdate{Title: "Fil rouge", URL: addr, DisplayOptions: apitypes.DisplayOptions{ShowImages: ptr(false)}}, http.StatusNoContent, nil)
	var url string
	var errors int
	var due bool
	if err := pool.QueryRow(ctx, `SELECT url, error_count, next_fetch_at <= now() FROM feeds WHERE id = 50`).Scan(&url, &errors, &due); err != nil {
		t.Fatal(err)
	}
	if url != addr || errors != 3 || due {
		t.Errorf("unchanged address taken for a new one: url %q, %d failures, due %v", url, errors, due)
	}
}

func TestOPMLImport(t *testing.T) {
	c, pool := startAPI(t, idleRefresher{})
	file := `<?xml version="1.0"?><opml version="2.0"><body>
		<outline text="Kept empty"/>
		<outline text="Sites">
			<outline text="example.test" type="rss" xmlUrl="https://example.test/feed"/>
			<outline text="Named" type="rss" xmlUrl="https://named.example/feed"/>
		</outline></body></opml>`
	status, body := c.upload("/api/opml", "text/x-opml", []byte(file))
	var imported apitypes.OPMLImport
	json.Unmarshal(body, &imported)
	if status != http.StatusOK || imported != (apitypes.OPMLImport{FeedsAdded: 2, GroupsAdded: 2}) {
		t.Fatalf("import: %d %s", status, body)
	}
	subs := c.subscriptions()
	if len(subs.Groups) != 2 || subs.Groups[0].Title != "Kept empty" || len(subs.Groups[0].FeedIDs) != 0 {
		t.Errorf("groups: %+v", subs.Groups)
	}
	rows, err := pool.Query(context.Background(), `SELECT url, title FROM feeds ORDER BY url`)
	if err != nil {
		t.Fatal(err)
	}
	titles, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (string, error) {
		var url, title string
		err := r.Scan(&url, &title)
		return url + " " + title, err
	})
	if err != nil {
		t.Fatal(err)
	}
	// The host is not taken for a title: the feed's own is filled in when fetched.
	if want := []string{"https://example.test/feed ", "https://named.example/feed Named"}; fmt.Sprint(titles) != fmt.Sprint(want) {
		t.Errorf("stored titles = %q, want %q", titles, want)
	}
}

// The endpoints behind the Z and Shift+R keys.
func TestShortcutEndpoints(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	refresher := &lateRefresher{}
	c, pool := startAPI(t, refresher)
	refresher.Refresher = feeds.NewRefresher(pool, log, feeds.Options{Interval: 15 * time.Minute})
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO feeds (id, url, next_fetch_at) VALUES
			(1, 'https://a.example/feed', now() + interval '1 day'),
			(2, 'https://b.example/feed', now() + interval '1 day');
		INSERT INTO groups (id, title) VALUES (1, 'G'), (2, 'Empty');
		INSERT INTO feed_groups VALUES (2, 1);
		INSERT INTO items (id, feed_id, guid, published_at, read_at) VALUES
			(1, 1, 'a', now(), now() - interval '1 hour'),  -- read long ago
			(2, 1, 'b', now(), now() - interval '20 seconds'),
			(3, 1, 'c', now(), now() - interval '10 seconds'),
			(4, 1, 'd', now(), now()),                      -- the last read
			(5, 1, 'e', now(), NULL);`)
	if err != nil {
		t.Fatal(err)
	}

	var undone apitypes.UnreadRecent
	c.call("POST", "/api/items/unread-recent", nil, http.StatusOK, &undone)
	if fmt.Sprint(sortedIDs(undone)) != "[3 4]" || undone.Items[0].FeedID != 1 {
		t.Errorf("unread again: %+v, want the items read within 15 s of the last read", undone.Items)
	}
	var unread int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM items WHERE read_at IS NULL`).Scan(&unread); err != nil || unread != 3 {
		t.Errorf("unread items: %d %v, want 3", unread, err)
	}
	c.call("POST", "/api/items/unread-recent", nil, http.StatusOK, &undone)
	if fmt.Sprint(sortedIDs(undone)) != "[2]" {
		t.Errorf("then: %+v, want the next most recent", undone.Items)
	}

	// A group's feeds only.
	c.call("POST", "/api/refresh", apitypes.RefreshRequest{GroupID: ptr(int64(1))}, http.StatusAccepted, nil)
	rows, err := pool.Query(ctx, `SELECT id FROM feeds WHERE next_fetch_at <= now() ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	due, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil || fmt.Sprint(due) != "[2]" {
		t.Errorf("due after refreshing the group: %v %v", due, err)
	}
	c.call("POST", "/api/refresh", apitypes.RefreshRequest{GroupID: ptr(int64(2))}, http.StatusAccepted, nil)
	c.call("POST", "/api/refresh", apitypes.RefreshRequest{GroupID: ptr(int64(99))}, http.StatusNotFound, nil)
}

func sortedIDs(res apitypes.UnreadRecent) []int64 {
	out := make([]int64, len(res.Items))
	for i, it := range res.Items {
		out[i] = it.ID
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
