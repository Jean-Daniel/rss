package store_test

import (
	"context"
	"fmt"
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
		TRUNCATE items, item_search, expired_items, feed_groups, feeds, groups, favicons;
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

// TestSearch needs a scratch database in TEST_DATABASE_URL (v2 tables there
// are wiped).
func TestSearch(t *testing.T) {
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
		TRUNCATE items, item_search, expired_items, feed_groups, feeds, groups, favicons;
		INSERT INTO feeds (id, url) VALUES (1, 'https://a.example/feed'), (2, 'https://b.example/feed');
		INSERT INTO items (id, feed_id, guid, title, author, content, published_at, read_at) VALUES
		(1, 1, 'g1', 'Building with SwiftUI', 'Jane Doe',
		 '<p class="intro">Node.js and <a href="https://hidden.example/secret">C++</a>, self-hosted.</p>', '2026-11-05', now()),
		(2, 1, 'g2', 'Le café du coin', '', '<p>Résumé de l''été</p>', '2026-11-04', NULL),
		(3, 2, 'g3', 'Unrelated', '', '<img alt="picture" src="swift.png"><p>Lorem ipsum</p>', '2026-11-03', NULL),
		(4, 2, 'g4', 'Swift concurrency', '', '<p>Actors and tasks</p>', '2026-11-02', NULL),
		-- Words far into a long article, and past what is indexed of a huge one.
		(5, 2, 'g5', 'Long', '', '<p>' || repeat('a ', 20000) || 'self-hosted Été2026</p>', '2026-11-01', NULL),
		(6, 2, 'g6', 'Huge', '', (SELECT string_agg(md5(n::text), ' ') FROM generate_series(1, 60000) n) || ' zanzibar',
		 '2026-10-31', NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	// Accents are ignored only where the migration could install unaccent;
	// accented words are found as typed either way.
	var unaccent bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'unaccent')`).Scan(&unaccent); err != nil {
		t.Fatal(err)
	}
	ifUnaccent := func(ids ...int64) []int64 {
		if unaccent {
			return ids
		}
		return nil
	}
	q := store.New(pool)
	feed2 := int64(2)
	for _, tt := range []struct {
		search string
		feed   *int64
		want   []int64
	}{
		{"swift", nil, []int64{1, 4}},       // a word, and words starting with it, newest first
		{"SWIFT", &feed2, []int64{4}},       // whatever the case, within a feed
		{"café", nil, []int64{2}},           // as typed
		{"cafe", nil, ifUnaccent(2)},        // whatever the accents
		{"Résumé ÉTÉ", nil, []int64{2}},     // every word
		{"ete2026", nil, ifUnaccent(5)},     // accents in words with digits too
		{"self-hosted", &feed2, []int64{5}}, // after 16383 words (no positions)
		{"zanzibar", nil, nil},              // past the indexed part of a huge article
		{"swift lorem", nil, nil},           // not one item has both
		{"jane", nil, []int64{1}},           // the author
		{"node.js", nil, []int64{1}},        // read as the text is
		{"self-hosted", nil, []int64{1, 5}},
		{"secret", nil, nil},       // not in links (tags and attributes)
		{"picture", nil, nil},      //
		{"it's \\ 'odd", nil, nil}, // quotes and backslashes are just text
		{"!!!", nil, nil},          // no words
		{"sw", nil, nil},           // short words must be whole
	} {
		items, err := q.ItemStream(ctx, store.StreamQuery{Search: tt.search, FeedID: tt.feed, Limit: 10})
		if err != nil {
			t.Fatalf("search %q: %v", tt.search, err)
		}
		var got []int64
		for _, it := range items {
			got = append(got, it.ID)
		}
		if fmt.Sprint(got) != fmt.Sprint(tt.want) {
			t.Errorf("search %q = %v, want %v", tt.search, got, tt.want)
		}
	}

	// Paging through results.
	first, err := q.ItemStream(ctx, store.StreamQuery{Search: "swift", Limit: 1})
	if err != nil || len(first) != 1 || first[0].ID != 1 {
		t.Fatalf("first page: %v %v", first, err)
	}
	after := store.Cursor{Time: first[0].PublishedAt, ID: first[0].ID}
	second, err := q.ItemStream(ctx, store.StreamQuery{Search: "swift", After: &after, Limit: 1})
	if err != nil || len(second) != 1 || second[0].ID != 4 {
		t.Errorf("second page: %v %v", second, err)
	}

	// Items stored later are found too (the index follows the table).
	if _, err := pool.Exec(ctx, `UPDATE items SET title = 'Kotlin' WHERE id = 4`); err != nil {
		t.Fatal(err)
	}
	if items, _ := q.ItemStream(ctx, store.StreamQuery{Search: "kotlin", Limit: 10}); len(items) != 1 || items[0].ID != 4 {
		t.Errorf("after a change: %v", items)
	}
}
