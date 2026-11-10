// Package importer copies the data of the old Fever installation (its fever_*
// tables in the same PostgreSQL database) into the v2 schema.
//
// The import replaces all v2 feeds, groups, items and favicons in a single
// transaction, keeping Fever's ids, so it can be run again at any time while
// the old app is still in use. The password and sessions are left alone.
//
// The v2 tables are emptied with TRUNCATE, which locks them until the import
// commits: a running v2 server answers nothing that reads feeds or items
// meanwhile (requests wait, then show the new data). That is the cheapest way
// to replace everything on a small server, and nobody should be reading a
// list that is being replaced.
package importer

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/crc32"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fever/internal/content"
	"fever/internal/feeds"
)

type Options struct {
	// SourceSchema is the schema holding the Fever tables (default "public").
	SourceSchema string
	// Prefix is Fever's table prefix (default "fever_").
	Prefix string
	// Replace allows wiping v2 data that is already there.
	Replace bool
}

type Stats struct {
	Favicons, Groups, Feeds, Memberships, Items int64
	SkippedFavicons                             int64
}

var ErrHasData = errors.New("v2 already contains feeds or items; run again with --replace to overwrite them")

func Run(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, opts Options) (Stats, error) {
	var st Stats
	if opts.SourceSchema == "" {
		opts.SourceSchema = "public"
	}
	if opts.Prefix == "" {
		opts.Prefix = "fever_"
	}
	table := func(name string) string {
		return pgx.Identifier{opts.SourceSchema, opts.Prefix + name}.Sanitize()
	}

	var found bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2)`,
		opts.SourceSchema, opts.Prefix+"items").Scan(&found)
	if err != nil {
		return st, err
	}
	if !found {
		return st, fmt.Errorf("no Fever tables found (looked for %s)", table("items"))
	}

	if !opts.Replace {
		var hasData bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM feeds) OR EXISTS (SELECT 1 FROM groups)`).Scan(&hasData); err != nil {
			return st, err
		}
		if hasData {
			return st, ErrHasData
		}
	}

	// Two connections: one reads the Fever tables, the other writes v2.
	if pool.Config().MaxConns < 2 {
		return st, errors.New("the import needs at least 2 database connections (DB_MAX_CONNS)")
	}

	// All Fever tables are read in one snapshot, so feeds or items the old
	// app adds meanwhile cannot appear in some tables and not in others.
	src, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return st, err
	}
	defer src.Rollback(ctx)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return st, err
	}
	defer tx.Rollback(ctx)

	// Locks the v2 tables until the commit (see the package comment).
	if opts.Replace {
		log.Info("replacing the v2 data: a running v2 server waits until the import is finished")
	}
	if _, err := tx.Exec(ctx, `TRUNCATE items, expired_items, feed_groups, feeds, groups, favicons`); err != nil {
		return st, err
	}

	// Rows are streamed from the snapshot into COPY on the writing
	// transaction, so memory use stays flat whatever the size of the Fever
	// database.

	// Favicons. Fever stores them as "mime/type;base64,DATA". Id 1 is its
	// blank placeholder.
	faviconIDs := make(map[int64]bool)
	err = copyRows(ctx, src, tx, "favicons",
		[]string{"id", "url", "mime_type", "data", "fetched_at"},
		`SELECT id::bigint, url::text, cache::text, last_cached_on_time::bigint FROM `+table("favicons")+` WHERE id::bigint <> 1`,
		func(rows pgx.Rows) ([]any, error) {
			var id, cachedOn int64
			var url, cache string
			if err := rows.Scan(&id, &url, &cache, &cachedOn); err != nil {
				return nil, err
			}
			mime, data, ok := decodeFavicon(cache)
			if !ok {
				st.SkippedFavicons++
				return nil, nil
			}
			faviconIDs[id] = true
			st.Favicons++
			return []any{id, url, mime, data, unixOrNow(cachedOn)}, nil
		})
	if err != nil {
		return st, fmt.Errorf("favicons: %w", err)
	}

	// Groups. Fever uses -1 for "use the global preference".
	err = copyRows(ctx, src, tx, "groups",
		[]string{"id", "title", "show_excerpts", "show_images", "oldest_first", "show_unread_count"},
		`SELECT id::bigint, title::text, item_excerpts::int, item_allows::int, sort_order::int, unread_counts::int FROM `+table("groups"),
		func(rows pgx.Rows) ([]any, error) {
			var id int64
			var title string
			var excerpts, allows, sortOrder, unread int
			if err := rows.Scan(&id, &title, &excerpts, &allows, &sortOrder, &unread); err != nil {
				return nil, err
			}
			st.Groups++
			return []any{id, content.Text(title, 500), flag(excerpts), imagesFlag(allows), flag(sortOrder), flag(unread)}, nil
		})
	if err != nil {
		return st, fmt.Errorf("groups: %w", err)
	}

	// Feeds. HTTP credentials are stored by Fever as base64("user:password").
	err = copyRows(ctx, src, tx, "feeds",
		[]string{"id", "url", "title", "site_url", "favicon_id", "auth_username", "auth_password",
			"show_excerpts", "show_images", "oldest_first", "show_unread_count",
			"last_fetched_at", "last_new_item_at", "last_error"},
		`SELECT id::bigint, url::text, coalesce(title, '')::text, coalesce(site_url, '')::text,
		        coalesce(favicon_id::bigint, 0), coalesce(requires_auth::int, 0), coalesce(auth::text, ''),
		        item_excerpts::int, item_allows::int, sort_order::int, unread_counts::int,
		        last_refreshed_on_time::bigint, last_added_on_time::bigint
		 FROM `+table("feeds"),
		func(rows pgx.Rows) ([]any, error) {
			var id, faviconID, refreshedOn, addedOn int64
			var url, title, siteURL, auth string
			var requiresAuth, excerpts, allows, sortOrder, unread int
			if err := rows.Scan(&id, &url, &title, &siteURL, &faviconID, &requiresAuth, &auth,
				&excerpts, &allows, &sortOrder, &unread, &refreshedOn, &addedOn); err != nil {
				return nil, err
			}
			var favicon *int64
			if faviconIDs[faviconID] {
				favicon = &faviconID
			}
			user, pass := decodeAuth(auth)
			var lastError *string
			if requiresAuth != 0 && user == nil {
				msg := "This feed needs a username and password."
				lastError = &msg
			}
			st.Feeds++
			return []any{id, feeds.NormalizeURL(url), content.Text(title, 500), siteURL, favicon, user, pass,
				flag(excerpts), imagesFlag(allows), flag(sortOrder), flag(unread),
				unixOrNil(refreshedOn), unixOrNil(addedOn), lastError}, nil
		})
	if err != nil {
		return st, fmt.Errorf("feeds: %w", err)
	}

	// Group memberships, ignoring rows that point to deleted feeds or groups.
	err = copyRows(ctx, src, tx, "feed_groups",
		[]string{"feed_id", "group_id"},
		`SELECT DISTINCT fg.feed_id::bigint, fg.group_id::bigint
		 FROM `+table("feeds_groups")+` fg
		 JOIN `+table("feeds")+` f ON f.id = fg.feed_id
		 JOIN `+table("groups")+` g ON g.id = fg.group_id`,
		func(rows pgx.Rows) ([]any, error) {
			var feedID, groupID int64
			if err := rows.Scan(&feedID, &groupID); err != nil {
				return nil, err
			}
			st.Memberships++
			return []any{feedID, groupID}, nil
		})
	if err != nil {
		return st, fmt.Errorf("feed groups: %w", err)
	}

	// Items. Items without a uid fall back to their link; duplicates within a
	// feed (same guid once Fever's checksums are converted) are collapsed.
	// Rows come feed by feed, saved first within each feed, so a saved copy
	// always wins and only one feed's guids are remembered at a time.
	// Fever has no "saved on" date, so the item's arrival time is used.
	started := time.Now()
	var seenFeed int64
	seen := make(map[string]bool) // the guids of seenFeed so far, after feverGUID
	err = copyRows(ctx, src, tx, "items",
		[]string{"id", "feed_id", "guid", "url", "title", "author", "content",
			"published_at", "added_at", "read_at", "saved_at", "state_changed_at"},
		`SELECT i.id::bigint, i.feed_id::bigint,
		        coalesce(nullif(i.uid::text, ''), nullif(i.link::text, ''), 'fever:' || i.id) AS guid,
		        coalesce(i.link, '')::text, coalesce(i.title, '')::text, coalesce(i.author, '')::text,
		        coalesce(i.description, '')::text, coalesce(f.site_url, '')::text,
		        i.created_on_time::bigint, i.added_on_time::bigint, i.read_on_time::bigint, i.is_saved::int
		 FROM `+table("items")+` i
		 JOIN `+table("feeds")+` f ON f.id = i.feed_id
		 ORDER BY i.feed_id, i.is_saved::int DESC, i.id`,
		func(rows pgx.Rows) ([]any, error) {
			var id, feedID, createdOn, addedOn, readOn int64
			var guid, link, title, author, description, siteURL string
			var saved int
			if err := rows.Scan(&id, &feedID, &guid, &link, &title, &author, &description, &siteURL,
				&createdOn, &addedOn, &readOn, &saved); err != nil {
				return nil, err
			}
			guid = feverGUID(guid, link)
			if feedID != seenFeed {
				seenFeed = feedID
				clear(seen)
			}
			if seen[guid] {
				return nil, nil
			}
			seen[guid] = true
			base := link
			if base == "" {
				base = siteURL
			}
			added := unixOrNow(addedOn)
			published := added
			if createdOn > 0 {
				published = time.Unix(createdOn, 0)
			}
			readAt := unixOrNil(readOn)
			var savedAt *time.Time
			if saved != 0 {
				savedAt = &added
			}
			changed := added
			if readAt != nil {
				changed = *readAt
			}
			st.Items++
			if st.Items%5000 == 0 {
				log.Info("importing items", "done", st.Items, "elapsed", time.Since(started).Round(time.Second))
			}
			return []any{id, feedID, guid, link, content.Text(title, 1000), content.Text(author, 200),
				content.Clean(description, base), published, added, readAt, savedAt, changed}, nil
		})
	if err != nil {
		return st, fmt.Errorf("items: %w", err)
	}

	// Ids were copied explicitly: move the identity sequences past them.
	for _, t := range []string{"favicons", "groups", "feeds", "items"} {
		q := fmt.Sprintf(`SELECT setval(pg_get_serial_sequence('%[1]s', 'id'), coalesce(max(id), 0) + 1, false) FROM %[1]s`, t)
		if _, err := tx.Exec(ctx, q); err != nil {
			return st, fmt.Errorf("reset %s ids: %w", t, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return st, err
	}
	return st, nil
}

// copyRows streams the result of query, run in the src snapshot, into target
// through COPY on dst. convert returns nil to skip a row.
func copyRows(ctx context.Context, src, dst pgx.Tx, target string, columns []string,
	query string, convert func(pgx.Rows) ([]any, error)) error {
	rows, err := src.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	_, err = dst.CopyFrom(ctx, pgx.Identifier{target}, columns, &rowSource{rows: rows, convert: convert})
	if err != nil {
		return err
	}
	return rows.Err()
}

type rowSource struct {
	rows    pgx.Rows
	convert func(pgx.Rows) ([]any, error)
	values  []any
	err     error
}

func (s *rowSource) Next() bool {
	for s.rows.Next() {
		s.values, s.err = s.convert(s.rows)
		if s.err != nil {
			return false
		}
		if s.values != nil {
			return true
		}
	}
	return false
}

func (s *rowSource) Values() ([]any, error) { return s.values, nil }

func (s *rowSource) Err() error {
	if s.err != nil {
		return s.err
	}
	return s.rows.Err()
}

// flag maps Fever's per-feed options (-1 inherit, 0 off, 1 on).
func flag(v int) *bool {
	if v < 0 {
		return nil
	}
	b := v != 0
	return &b
}

// imagesFlag maps Fever's item_allows (0 text only, 1 text with images,
// 2 unfiltered). Unfiltered HTML is not supported any more and becomes
// "with images".
func imagesFlag(v int) *bool {
	if v < 0 {
		return nil
	}
	b := v >= 1
	return &b
}

func unixOrNil(sec int64) *time.Time {
	if sec <= 0 {
		return nil
	}
	t := time.Unix(sec, 0)
	return &t
}

func unixOrNow(sec int64) time.Time {
	if sec <= 0 {
		return time.Now()
	}
	return time.Unix(sec, 0)
}

// decodeFavicon reads Fever's "type;base64,DATA" favicon cache. The stored
// type is often wrong (many PNG and SVG icons are labelled text/x-icon), so
// the type is taken from the data itself when it can be recognized.
func decodeFavicon(cache string) (mime string, data []byte, ok bool) {
	cache = strings.TrimPrefix(strings.TrimSpace(cache), "data:")
	declared, b64, found := strings.Cut(cache, ";base64,")
	if !found {
		return "", nil, false
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil || len(data) == 0 {
		return "", nil, false
	}
	mime = content.SniffImage(data)
	if mime == "" && strings.HasPrefix(declared, "image/") && declared != "image/svg+xml" {
		mime = declared // e.g. a format the sniffer does not know
	}
	if mime == "" {
		return "", nil, false
	}
	return mime, data, true
}

// feverGUID maps Fever's item uid to the guid v2 computes for the same entry.
// For entries without a guid, Fever stored a CRC-32 of the link where v2
// uses the link itself.
func feverGUID(uid, link string) string {
	if link != "" && uid == strconv.FormatUint(uint64(crc32.ChecksumIEEE([]byte(link))), 10) {
		return content.TruncateUTF8(link, 255)
	}
	return uid
}

func decodeAuth(auth string) (user, pass *string) {
	if auth == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(auth)
	if err != nil {
		return nil, nil
	}
	u, p, found := strings.Cut(string(raw), ":")
	if !found || u == "" {
		return nil, nil
	}
	return &u, &p
}
