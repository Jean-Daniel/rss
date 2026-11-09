package feeds

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fever/internal/content"
	"fever/internal/store"
)

const (
	// concurrency is how many feeds are fetched at the same time.
	concurrency = 4
	// batchSize is how many due feeds are taken from the database at once.
	batchSize = 32
	// feedTimeout bounds one feed's refresh, favicon search included.
	feedTimeout = 2 * time.Minute
	// maxBackoff is the longest wait before retrying a failing feed.
	maxBackoff = 24 * time.Hour
	// faviconRecheck is how long to wait before looking again for the
	// favicon of a site that has none.
	faviconRecheck = 7 * 24 * time.Hour
	// Retention is how long items are kept unless saved (Fever's default).
	// Older entries a feed still lists are not added again.
	Retention = 10 * 7 * 24 * time.Hour
	// cleanupEvery is how often expired items are deleted.
	cleanupEvery = time.Hour
	// freeMemoryAfter is how much feed data a run must parse before the
	// memory it used is handed back to the system.
	freeMemoryAfter = 1 << 20
)

type Options struct {
	// Interval between two fetches of a working feed.
	Interval time.Duration
	// AllowPrivate lets feeds live on loopback or private addresses.
	AllowPrivate bool
}

// Refresher fetches feeds whose next_fetch_at has passed: every minute, and
// right away when asked to.
type Refresher struct {
	q      *store.Queries
	client *http.Client
	log    *slog.Logger
	opts   Options
	now    func() time.Time

	wake      chan struct{}
	requested atomic.Bool // a refresh was asked for and has not started yet
	running   atomic.Bool
}

func NewRefresher(pool *pgxpool.Pool, log *slog.Logger, opts Options) *Refresher {
	return &Refresher{
		q:      store.New(pool),
		client: newClient(opts.AllowPrivate),
		log:    log,
		opts:   opts,
		now:    time.Now,
		wake:   make(chan struct{}, 1),
	}
}

// Run refreshes due feeds, and deletes expired items, until ctx ends.
func (r *Refresher) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	var lastCleanup time.Time
	for {
		r.running.Store(true)
		r.requested.Store(false)
		r.refreshDue(ctx)
		r.running.Store(false)
		if r.now().Sub(lastCleanup) >= cleanupEvery {
			r.deleteExpired(ctx)
			lastCleanup = r.now()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-r.wake:
		}
	}
}

// Busy reports whether a refresh is running or about to start.
func (r *Refresher) Busy() bool {
	return r.requested.Load() || r.running.Load()
}

// RefreshAll makes every feed due and starts refreshing now.
func (r *Refresher) RefreshAll(ctx context.Context) error {
	if err := r.q.MarkAllFeedsDue(ctx); err != nil {
		return err
	}
	r.poke()
	return nil
}

// RefreshFeed makes one feed due and starts refreshing now. It reports false
// when the feed does not exist.
func (r *Refresher) RefreshFeed(ctx context.Context, id int64) (bool, error) {
	n, err := r.q.MarkFeedDue(ctx, id)
	if err != nil || n == 0 {
		return false, err
	}
	r.poke()
	return true, nil
}

// deleteExpired removes unsaved items older than the retention period.
func (r *Refresher) deleteExpired(ctx context.Context) {
	n, err := r.q.DeleteExpiredItems(ctx, r.now().Add(-Retention))
	switch {
	case err != nil:
		r.warn(ctx, "delete expired items", "err", err)
	case n > 0:
		r.log.Info("deleted expired items", "count", n)
	}
}

// warn logs a problem, unless ctx was canceled because the server is
// stopping: then failures are expected and not worth a warning.
func (r *Refresher) warn(ctx context.Context, msg string, args ...any) {
	if ctx.Err() == nil {
		r.log.Warn(msg, args...)
	}
}

func (r *Refresher) poke() {
	r.requested.Store(true)
	select {
	case r.wake <- struct{}{}:
	default: // a wake-up is already pending
	}
}

type runStats struct {
	feeds, notModified, failed, newItems, bytes int
}

func (r *Refresher) refreshDue(ctx context.Context) {
	start := r.now()
	var stats runStats
	var mu sync.Mutex
	done := make(map[int64]bool)

	for ctx.Err() == nil {
		due, err := r.q.DueFeeds(ctx, batchSize)
		if err != nil {
			r.warn(ctx, "list due feeds", "err", err)
			return
		}
		// A feed whose update could not be saved stays due: never take it
		// twice in one run.
		batch := due[:0]
		for _, f := range due {
			if !done[f.ID] {
				done[f.ID] = true
				batch = append(batch, f)
			}
		}
		if len(batch) == 0 {
			break
		}

		var wg sync.WaitGroup
		slots := make(chan struct{}, concurrency)
		for _, f := range batch {
			slots <- struct{}{}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-slots }()
				res := r.refreshFeed(ctx, f)
				mu.Lock()
				stats.feeds++
				stats.newItems += res.newItems
				stats.bytes += res.bytes
				if res.notModified {
					stats.notModified++
				}
				if res.failed {
					stats.failed++
				}
				mu.Unlock()
			}()
		}
		wg.Wait()
	}

	if stats.feeds > 0 {
		r.log.Info("refreshed feeds", "feeds", stats.feeds, "unchanged", stats.notModified,
			"failed", stats.failed, "new_items", stats.newItems, "took", r.now().Sub(start).Round(time.Millisecond))
	}
	// Parsing feeds briefly needs a few tens of MB. After a run that parsed
	// a lot, give that memory back instead of keeping it until the next one;
	// small runs (most of them) are not worth a forced collection.
	if stats.bytes >= freeMemoryAfter {
		debug.FreeOSMemory()
	}
}

type feedResult struct {
	newItems    int
	bytes       int // size of the feed document parsed
	notModified bool
	failed      bool
}

func (r *Refresher) refreshFeed(parent context.Context, f store.DueFeedsRow) feedResult {
	ctx, cancel := context.WithTimeout(parent, feedTimeout)
	defer cancel()

	res, err := fetch(ctx, r.client, f)
	if err != nil {
		return r.failed(parent, f, err)
	}

	feedURL := content.ParseHTTP(res.finalURL)
	if res.notModified {
		if err := r.q.FeedNotModified(parent, store.FeedNotModifiedParams{ID: f.ID, NextFetchAt: r.nextFetch()}); err != nil {
			r.warn(parent, "save feed", "feed", f.ID, "err", err)
		}
		r.checkFavicon(ctx, f, siteOf(f.SiteUrl, "", feedURL))
		return feedResult{notModified: true}
	}

	now := r.now()
	items := toItems(res.feed, res.finalURL, now, now.Add(-Retention))
	added, err := r.storeItems(parent, f.ID, items)
	if err != nil {
		// Recorded as a failure, keeping the old ETag and Last-Modified:
		// the next fetch must download the feed again, not be told that
		// nothing changed.
		r.warn(parent, "save items", "feed", f.ID, "err", err)
		failed := r.failed(parent, f, fmt.Errorf("New items could not be saved: %w", err))
		failed.bytes = res.size
		return failed
	}
	siteURL := ""
	if site := content.ResolveHTTP(feedURL, res.feed.Link); site != nil {
		siteURL = site.String()
	}
	err = r.q.FeedFetched(parent, store.FeedFetchedParams{
		ID:           f.ID,
		Title:        content.Text(res.feed.Title, 500),
		SiteUrl:      siteURL,
		Etag:         optional(res.etag),
		LastModified: optional(res.lastModified),
		NewItems:     int32(added),
		NextFetchAt:  r.nextFetch(),
	})
	if err != nil {
		r.warn(parent, "save feed", "feed", f.ID, "err", err)
	}
	r.checkFavicon(ctx, f, siteOf(f.SiteUrl, siteURL, feedURL))
	return feedResult{newItems: added, bytes: res.size}
}

// failed records a failed refresh and schedules the next attempt.
func (r *Refresher) failed(ctx context.Context, f store.DueFeedsRow, err error) feedResult {
	if ctx.Err() != nil {
		return feedResult{} // shutting down: the feed stays due
	}
	var retryAfter time.Duration
	var he *httpError
	if errors.As(err, &he) {
		retryAfter = he.retryAfter
	}
	msg := content.TruncateUTF8(err.Error(), 500)
	next := r.retryAt(int(f.ErrorCount)+1, retryAfter)
	if err := r.q.FeedFailed(ctx, store.FeedFailedParams{ID: f.ID, NextFetchAt: next, LastError: &msg}); err != nil {
		r.warn(ctx, "save feed error", "feed", f.ID, "err", err)
	}
	r.log.Debug("feed failed", "feed", f.ID, "url", f.Url, "err", err)
	return feedResult{failed: true}
}

// storeItems inserts the items the feed has not brought before, in a single
// round trip, and returns how many were new. Only those are cleaned.
func (r *Refresher) storeItems(ctx context.Context, feedID int64, items []newItem) (int, error) {
	// Run even for an empty feed: it also forgets the expired entries the
	// feed no longer lists.
	guids := make([]string, len(items))
	for i, it := range items {
		guids[i] = it.guid
	}
	existing, err := r.q.KnownGUIDs(ctx, store.KnownGUIDsParams{FeedID: feedID, Guids: guids})
	if err != nil {
		return 0, err
	}
	known := make(map[string]bool, len(existing))
	for _, g := range existing {
		known[g] = true
	}
	var params []store.InsertItemParams
	for _, it := range items {
		if known[it.guid] {
			continue
		}
		params = append(params, store.InsertItemParams{
			FeedID:      feedID,
			Guid:        it.guid,
			Url:         it.url,
			Title:       it.title,
			Author:      it.author,
			Content:     it.html(),
			PublishedAt: it.published,
		})
	}
	if len(params) == 0 {
		return 0, nil
	}
	var firstErr error
	results := r.q.InsertItem(ctx, params)
	results.Exec(func(_ int, err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	})
	if err := results.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	if firstErr != nil {
		return 0, firstErr
	}
	return len(params), nil
}

// checkFavicon looks for a favicon for feeds that have none, at most once a
// week per feed.
func (r *Refresher) checkFavicon(ctx context.Context, f store.DueFeedsRow, site *url.URL) {
	if f.FaviconID != nil || site == nil {
		return
	}
	if f.FaviconCheckedAt != nil && r.now().Sub(*f.FaviconCheckedAt) < faviconRecheck {
		return
	}
	var id *int64
	icon, err := findFavicon(ctx, r.client, site)
	if ctx.Err() != nil {
		return
	}
	if err == nil {
		saved, err := r.q.UpsertFavicon(ctx, store.UpsertFaviconParams{Url: icon.url, MimeType: icon.mime, Data: icon.data})
		if err != nil {
			r.warn(ctx, "save favicon", "feed", f.ID, "err", err)
			return
		}
		id = &saved
	}
	// Recorded even when nothing was found, so the search waits a week.
	if err := r.q.SetFeedFavicon(ctx, store.SetFeedFaviconParams{ID: f.ID, FaviconID: id}); err != nil {
		r.warn(ctx, "save favicon", "feed", f.ID, "err", err)
	}
}

// nextFetch spreads fetches out a little so they do not all line up.
func (r *Refresher) nextFetch() time.Time {
	return r.now().Add(jitter(r.opts.Interval))
}

// retryAt doubles the wait after each consecutive failure, up to a day, and
// respects the site's Retry-After when it asks for longer.
func (r *Refresher) retryAt(failures int, retryAfter time.Duration) time.Time {
	d := r.opts.Interval << min(failures, 6)
	d = max(d, retryAfter)
	d = min(d, maxBackoff)
	return r.now().Add(jitter(d))
}

func jitter(d time.Duration) time.Duration {
	return time.Duration(float64(d) * (0.9 + 0.2*rand.Float64()))
}

func optional(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
