package feeds

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fever/internal/db"
)

// openTestDB returns the scratch PostgreSQL database named by
// TEST_DATABASE_URL, with the v2 tables emptied. Without it the test is
// skipped.
func openTestDB(t *testing.T) (*pgxpool.Pool, *slog.Logger) {
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
	if _, err := pool.Exec(ctx, `TRUNCATE items, expired_items, feed_groups, feeds, groups, favicons`); err != nil {
		t.Fatal(err)
	}
	return pool, log
}

// rejectWrites makes the writes to a table that break check fail, until the
// returned function is called. The constraint never outlives the test: it is
// also removed when the test ends early, and before adding it, in case an
// earlier run was killed.
func rejectWrites(t *testing.T, pool *pgxpool.Pool, table, check string) (allow func()) {
	t.Helper()
	ctx := context.Background()
	allow = func() {
		if _, err := pool.Exec(ctx, `ALTER TABLE `+table+` DROP CONSTRAINT IF EXISTS test_reject`); err != nil {
			t.Error(err)
		}
	}
	allow()
	t.Cleanup(allow)
	if _, err := pool.Exec(ctx, `ALTER TABLE `+table+` ADD CONSTRAINT test_reject CHECK (`+check+`) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	return allow
}

// TestRefresherWithDatabase runs a refresh end to end.
func TestRefresherWithDatabase(t *testing.T) {
	ctx := context.Background()
	pool, log := openTestDB(t)

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

	_, err := pool.Exec(ctx, `INSERT INTO feeds (id, url) VALUES (1, $1), (2, $2), (3, $3)`,
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
	refreshUndated := func() {
		t.Helper()
		if _, err := r.RefreshFeed(ctx, 3); err != nil {
			t.Fatal(err)
		}
		r.refreshDue(ctx)
	}
	refreshUndated()
	if n := count(`SELECT count(*) FROM items WHERE feed_id = 3`); n != 0 {
		t.Errorf("the expired entry came back as new (%d items)", n)
	}
	// A feed may serve an empty document for a while: the entry is only
	// noted as missing, and is still not new when the feed lists it again.
	listUndated.Store(false)
	refreshUndated()
	if n := count(`SELECT count(*) FROM expired_items WHERE feed_id = 3 AND missing_since IS NOT NULL`); n != 1 {
		t.Errorf("an entry the feed just stopped listing: %d noted as missing, want 1", n)
	}
	listUndated.Store(true)
	refreshUndated()
	if n := count(`SELECT count(*) FROM items WHERE feed_id = 3`); n != 0 {
		t.Errorf("the expired entry came back as new after a gap (%d items)", n)
	}
	if n := count(`SELECT count(*) FROM expired_items WHERE feed_id = 3 AND missing_since IS NULL`); n != 1 {
		t.Errorf("an entry the feed lists again: %d remembered as listed, want 1", n)
	}
	// Once the feed has not listed it for long enough, it is forgotten.
	listUndated.Store(false)
	refreshUndated()
	if n := count(`SELECT count(*) FROM expired_items WHERE feed_id = 3`); n != 1 {
		t.Errorf("an entry the feed just stopped listing: %d remembered, want 1", n)
	}
	later = later.Add(forgetAfter + time.Hour)
	r.now = func() time.Time { return later }
	refreshUndated()
	if n := count(`SELECT count(*) FROM expired_items WHERE feed_id = 3`); n != 0 {
		t.Errorf("an entry the feed has not listed for %v is still remembered", forgetAfter+time.Hour)
	}
}

// TestFailedSaveKeepsValidators checks that when new items cannot be stored,
// the feed's ETag is not saved: otherwise the next fetch would get "not
// modified" and the items would never be stored.
func TestFailedSaveKeepsValidators(t *testing.T) {
	ctx := context.Background()
	pool, log := openTestDB(t)

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
	allow := rejectWrites(t, pool, "items", "false")
	r := NewRefresher(pool, log, Options{Interval: 15 * time.Minute, AllowPrivate: true})
	r.refreshDue(ctx)
	allow()

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

// TestUnsavedRefresh checks what happens when a feed was fetched but the
// outcome cannot be saved: it must not be fetched again and again, and must
// not keep the other feeds waiting.
func TestUnsavedRefresh(t *testing.T) {
	ctx := context.Background()
	pool, log := openTestDB(t)

	var mu sync.Mutex
	hits := make(map[string]int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
		w.Header().Set("ETag", `"v1"`)
		w.Write([]byte(sampleRSS))
	}))
	defer srv.Close()
	fetched := func(id int) int {
		mu.Lock()
		defer mu.Unlock()
		return hits[fmt.Sprintf("/%d", id)]
	}
	// More feeds than one batch, all due at the same time: taken by id.
	const feeds = batchSize + 2
	_, err := pool.Exec(ctx, `INSERT INTO feeds (id, url, favicon_checked_at)
		SELECT n, $1 || '/' || n, now() FROM generate_series(1, $2::int) n`, srv.URL, feeds)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRefresher(pool, log, Options{Interval: 15 * time.Minute, AllowPrivate: true})

	// The first batch can be saved neither as fetched nor as failed, so
	// those feeds stay due. Each is fetched once, and the feeds after them
	// are still refreshed.
	allow := rejectWrites(t, pool, "feeds", fmt.Sprintf("id > %d OR last_fetched_at IS NULL", batchSize))
	r.refreshDue(ctx)
	allow()
	for id := 1; id <= feeds; id++ {
		if n := fetched(id); n != 1 {
			t.Errorf("feed %d fetched %d times in one run, want 1", id, n)
		}
	}
	var saved, due int
	err = pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE last_fetched_at IS NOT NULL AND error_count = 0),
	                                 count(*) FILTER (WHERE next_fetch_at <= now()) FROM feeds`).Scan(&saved, &due)
	if err != nil {
		t.Fatal(err)
	}
	if saved != feeds-batchSize || due != batchSize {
		t.Errorf("%d feeds saved and %d still due, want %d and %d", saved, due, feeds-batchSize, batchSize)
	}

	// The feed's new ETag cannot be saved, but a failure can: the refresh is
	// recorded as failed, so the next attempt waits and the problem shows.
	if _, err := pool.Exec(ctx, `DELETE FROM feeds WHERE id > 1`); err != nil {
		t.Fatal(err)
	}
	allow = rejectWrites(t, pool, "feeds", "etag IS NULL")
	r.refreshDue(ctx)
	allow()
	if n := fetched(1); n != 2 {
		t.Errorf("feed 1 fetched %d times, want 2", n)
	}
	var errorCount, items int
	var lastError *string
	var nextFetch time.Time
	err = pool.QueryRow(ctx, `SELECT error_count, last_error, next_fetch_at, (SELECT count(*) FROM items WHERE feed_id = 1)
	                          FROM feeds WHERE id = 1`).Scan(&errorCount, &lastError, &nextFetch, &items)
	if err != nil {
		t.Fatal(err)
	}
	if errorCount != 1 || lastError == nil || !strings.Contains(*lastError, "could not be saved") || time.Until(nextFetch) < 25*time.Minute {
		t.Errorf("unsaved refresh: errors=%d last_error=%v next in %v", errorCount, lastError, time.Until(nextFetch))
	}
	if items != 3 {
		t.Errorf("%d items stored, want 3", items)
	}
}
