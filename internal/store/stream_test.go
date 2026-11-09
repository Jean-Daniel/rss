package store_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"fever/internal/db"
	"fever/internal/store"
)

func TestCursorRoundTrip(t *testing.T) {
	c := store.Cursor{Time: time.UnixMicro(1730000000123456), ID: 42}
	got, err := store.ParseCursor(c.String())
	if err != nil || !got.Time.Equal(c.Time) || got.ID != c.ID {
		t.Fatalf("ParseCursor(%q) = %+v, %v", c.String(), got, err)
	}
	for _, bad := range []string{"", "12", "a.b", "1.x"} {
		if _, err := store.ParseCursor(bad); err == nil {
			t.Errorf("ParseCursor(%q) accepted", bad)
		}
	}
}

// TestItemStream needs a scratch database in TEST_DATABASE_URL (v2 tables
// there are wiped).
func TestItemStream(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, dsn, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `
		TRUNCATE items, expired_items, feed_groups, feeds, groups, favicons;
		INSERT INTO feeds (id, url) VALUES (1, 'https://a.example/feed'), (2, 'https://b.example/feed');
		INSERT INTO groups (id, title) VALUES (1, 'G');
		INSERT INTO feed_groups VALUES (2, 1);
		-- Items 1..5 in feed 1, one per day; items 2 and 4 read. Item 6 in feed 2.
		INSERT INTO items (id, feed_id, guid, published_at, read_at)
		SELECT n, 1, 'g' || n, timestamptz '2026-11-01' + n * interval '1 day',
		       CASE WHEN n IN (2, 4) THEN now() END
		FROM generate_series(1, 5) n;
		INSERT INTO items (id, feed_id, guid, published_at, saved_at)
		VALUES (6, 2, 'g6', '2026-11-03', now());`)
	if err != nil {
		t.Fatal(err)
	}
	q := store.New(pool)
	ids := func(items []store.StreamItem) []int64 {
		out := make([]int64, len(items))
		for i, it := range items {
			out[i] = it.ID
		}
		return out
	}
	page := func(sq store.StreamQuery) []int64 {
		t.Helper()
		items, err := q.ItemStream(ctx, sq)
		if err != nil {
			t.Fatal(err)
		}
		return ids(items)
	}
	feed1 := int64(1)
	group1 := int64(1)

	// Newest first, two per page.
	first, err := q.ItemStream(ctx, store.StreamQuery{FeedID: &feed1, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(first); !equal(got, []int64{5, 4}) {
		t.Fatalf("first page = %v", got)
	}
	last := first[len(first)-1]
	after := store.Cursor{Time: last.PublishedAt, ID: last.ID}
	if got := page(store.StreamQuery{FeedID: &feed1, Limit: 2, After: &after}); !equal(got, []int64{3, 2}) {
		t.Errorf("second page = %v", got)
	}

	// Oldest first, unread only: marking item 1 read after the first page
	// must not shift the next one.
	unread, err := q.ItemStream(ctx, store.StreamQuery{FeedID: &feed1, UnreadOnly: true, OldestFirst: true, Limit: 1})
	if err != nil || !equal(ids(unread), []int64{1}) {
		t.Fatalf("oldest unread = %v (%v)", ids(unread), err)
	}
	if _, err := q.MarkRead(ctx, []int64{1}); err != nil {
		t.Fatal(err)
	}
	after = store.Cursor{Time: unread[0].PublishedAt, ID: unread[0].ID}
	if got := page(store.StreamQuery{FeedID: &feed1, UnreadOnly: true, OldestFirst: true, Limit: 5, After: &after}); !equal(got, []int64{3, 5}) {
		t.Errorf("unread after the cursor = %v, want [3 5]", got)
	}

	if got := page(store.StreamQuery{GroupID: &group1, Limit: 10}); !equal(got, []int64{6}) {
		t.Errorf("group = %v", got)
	}
	if got := page(store.StreamQuery{SavedOnly: true, Limit: 10}); !equal(got, []int64{6}) {
		t.Errorf("saved = %v", got)
	}

	// Mark all read only covers items that had arrived.
	if _, err := pool.Exec(ctx, `UPDATE items SET added_at = now() + interval '1 hour' WHERE id = 5`); err != nil {
		t.Fatal(err)
	}
	if _, err := q.MarkFeedRead(ctx, store.MarkFeedReadParams{FeedID: 1, Before: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if got := page(store.StreamQuery{FeedID: &feed1, UnreadOnly: true, Limit: 10}); !equal(got, []int64{5}) {
		t.Errorf("unread after marking the feed read = %v, want [5] (arrived later)", got)
	}
}

func equal(a, b []int64) bool {
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
