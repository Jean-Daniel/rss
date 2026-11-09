package feeds

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fever/internal/db"
)

// TestRefresherWithDatabase runs a refresh end to end. It needs a scratch
// PostgreSQL database in TEST_DATABASE_URL: the v2 tables there are wiped.
func TestRefresherWithDatabase(t *testing.T) {
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
	if _, err := pool.Exec(ctx, `TRUNCATE items, expired_items, feed_groups, feeds, groups, favicons`); err != nil {
		t.Fatal(err)
	}

	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 32)
	var feedHits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/feed.xml", func(w http.ResponseWriter, r *http.Request) {
		feedHits.Add(1)
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Write([]byte(strings.ReplaceAll(sampleRSS, "https://example.com/blog/", "/blog/")))
	})
	mux.HandleFunc("/gone.xml", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusGone) })
	var listUndated atomic.Bool
	listUndated.Store(true)
	mux.HandleFunc("/undated.xml", func(w http.ResponseWriter, r *http.Request) {
		entry := ""
		if listUndated.Load() {
			entry = `<item><title>No date</title><guid>urn:undated</guid></item>`
		}
		w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>Undated</title>` + entry + `</channel></rss>`))
	})
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(png)) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, err = pool.Exec(ctx, `INSERT INTO feeds (id, url) VALUES (1, $1), (2, $2), (3, $3)`,
		srv.URL+"/feed.xml", srv.URL+"/gone.xml", srv.URL+"/undated.xml")
	if err != nil {
		t.Fatal(err)
	}

	r := NewRefresher(pool, log, Options{Interval: 15 * time.Minute, AllowPrivate: true})
	r.refreshDue(ctx)

	var title, siteURL string
	var etag *string
	var faviconID *int64
	var items int
	err = pool.QueryRow(ctx, `SELECT title, site_url, etag, favicon_id, (SELECT count(*) FROM items WHERE feed_id = 1) FROM feeds WHERE id = 1`).
		Scan(&title, &siteURL, &etag, &faviconID, &items)
	if err != nil {
		t.Fatal(err)
	}
	if title != "Example & Co" || siteURL != srv.URL+"/blog/" || etag == nil || *etag != `"v1"` {
		t.Errorf("feed after refresh: title=%q site=%q etag=%v", title, siteURL, etag)
	}
	if items != 3 {
		t.Errorf("%d items stored, want 3", items)
	}
	if faviconID == nil {
		t.Error("favicon not found")
	}

	var errorCount int
	var lastError *string
	var nextFetch time.Time
	if err := pool.QueryRow(ctx, `SELECT error_count, last_error, next_fetch_at FROM feeds WHERE id = 2`).Scan(&errorCount, &lastError, &nextFetch); err != nil {
		t.Fatal(err)
	}
	if errorCount != 1 || lastError == nil || !strings.Contains(*lastError, "410") || time.Until(nextFetch) < 25*time.Minute {
		t.Errorf("failing feed: errors=%d last_error=%v next in %v", errorCount, lastError, time.Until(nextFetch))
	}

	// Nothing is due any more: no request is made.
	hits := feedHits.Load()
	r.refreshDue(ctx)
	if feedHits.Load() != hits {
		t.Error("a feed that is not due was fetched")
	}

	// A requested refresh fetches again, the server answers "not modified".
	if err := r.RefreshAll(ctx); err != nil {
		t.Fatal(err)
	}
	if !r.Busy() {
		t.Error("Busy() should be true right after RefreshAll")
	}
	r.refreshDue(ctx)
	if feedHits.Load() != hits+1 {
		t.Errorf("feed fetched %d times after RefreshAll, want 1", feedHits.Load()-hits)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM items WHERE feed_id = 1`).Scan(&items); err != nil || items != 3 {
		t.Errorf("items after a 304: %d (err %v), want 3", items, err)
	}
	if err := pool.QueryRow(ctx, `SELECT error_count FROM feeds WHERE id = 2`).Scan(&errorCount); err != nil || errorCount != 2 {
		t.Errorf("failing feed error_count = %d, want 2", errorCount)
	}

	// An entry without a date is stored as published "now". Once it expires
	// and is deleted, it must not come back while the feed still lists it.
	count := func(q string) int {
		var n int
		if err := pool.QueryRow(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM items WHERE feed_id = 3`); n != 1 {
		t.Fatalf("undated feed: %d items, want 1", n)
	}
	later := time.Now().Add(Retention + 24*time.Hour)
	r.now = func() time.Time { return later }
	r.deleteExpired(ctx)
	r.now = time.Now
	if n := count(`SELECT count(*) FROM expired_items WHERE feed_id = 3`); n != 1 {
		t.Fatalf("expired undated entry not remembered: %d", n)
	}
	if _, err := r.RefreshFeed(ctx, 3); err != nil {
		t.Fatal(err)
	}
	r.refreshDue(ctx)
	if n := count(`SELECT count(*) FROM items WHERE feed_id = 3`); n != 0 {
		t.Errorf("the expired entry came back as new (%d items)", n)
	}
	// Once the feed stops listing it, it is forgotten.
	listUndated.Store(false)
	if _, err := r.RefreshFeed(ctx, 3); err != nil {
		t.Fatal(err)
	}
	r.refreshDue(ctx)
	if n := count(`SELECT count(*) FROM expired_items WHERE feed_id = 3`); n != 0 {
		t.Errorf("an entry the feed no longer lists is still remembered")
	}
}

// TestFailedSaveKeepsValidators checks that when new items cannot be stored,
// the feed's ETag is not saved: otherwise the next fetch would get "not
// modified" and the items would never be stored.
func TestFailedSaveKeepsValidators(t *testing.T) {
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
	if _, err := pool.Exec(ctx, `TRUNCATE items, expired_items, feed_groups, feeds, groups, favicons`); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Write([]byte(sampleRSS))
	}))
	defer srv.Close()
	if _, err := pool.Exec(ctx, `INSERT INTO feeds (id, url, favicon_checked_at) VALUES (1, $1, now())`, srv.URL); err != nil {
		t.Fatal(err)
	}

	// Make every insert fail.
	if _, err := pool.Exec(ctx, `ALTER TABLE items ADD CONSTRAINT test_reject CHECK (false) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	r := NewRefresher(pool, log, Options{Interval: 15 * time.Minute, AllowPrivate: true})
	r.refreshDue(ctx)
	if _, err := pool.Exec(ctx, `ALTER TABLE items DROP CONSTRAINT test_reject`); err != nil {
		t.Fatal(err)
	}

	var etag, lastError *string
	if err := pool.QueryRow(ctx, `SELECT etag, last_error FROM feeds WHERE id = 1`).Scan(&etag, &lastError); err != nil {
		t.Fatal(err)
	}
	if etag != nil {
		t.Errorf("etag saved after a failed insert: %q", *etag)
	}
	if lastError == nil || !strings.Contains(*lastError, "could not be saved") {
		t.Errorf("last_error = %v, want the save failure", lastError)
	}

	// The next attempt downloads the feed again and stores the items.
	if _, err := r.RefreshFeed(ctx, 1); err != nil {
		t.Fatal(err)
	}
	r.refreshDue(ctx)
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM items WHERE feed_id = 1`).Scan(&n); err != nil || n != 3 {
		t.Errorf("items after retry: %d (err %v), want 3", n, err)
	}
}
