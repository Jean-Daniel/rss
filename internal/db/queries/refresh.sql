-- name: DueFeeds :many
SELECT id, url, site_url, favicon_id, favicon_checked_at, auth_username, auth_password,
       etag, last_modified, error_count
FROM feeds
WHERE next_fetch_at <= now()
ORDER BY next_fetch_at, id
LIMIT $1;

-- KnownGUIDs returns which of the guids a feed lists right now it has
-- already seen: stored items, and expired ones it still lists. Expired items
-- the feed no longer lists are forgotten, since they cannot come back.
-- name: KnownGUIDs :many
WITH forgotten AS (
    DELETE FROM expired_items AS gone
    WHERE gone.feed_id = @feed_id AND NOT (gone.guid = ANY(@guids::text[]))
)
SELECT i.guid FROM items AS i WHERE i.feed_id = @feed_id AND i.guid = ANY(@guids::text[])
UNION ALL
SELECT e.guid FROM expired_items AS e WHERE e.feed_id = @feed_id AND e.guid = ANY(@guids::text[]);

-- name: InsertItem :batchexec
INSERT INTO items (feed_id, guid, url, title, author, content, published_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (feed_id, guid) DO NOTHING;

-- A fetch that returned the feed. The feed's own title and site address are
-- only used while the stored ones are empty, so renamed feeds keep their name.
-- name: FeedFetched :exec
UPDATE feeds SET
    title            = CASE WHEN title = '' THEN @title ELSE title END,
    site_url         = CASE WHEN site_url = '' THEN @site_url ELSE site_url END,
    etag             = @etag,
    last_modified    = @last_modified,
    last_fetched_at  = now(),
    last_new_item_at = CASE WHEN @new_items::int > 0 THEN now() ELSE last_new_item_at END,
    next_fetch_at    = @next_fetch_at,
    error_count      = 0,
    last_error       = NULL
WHERE id = @id;

-- name: FeedNotModified :exec
UPDATE feeds SET
    last_fetched_at = now(),
    next_fetch_at   = $2,
    error_count     = 0,
    last_error      = NULL
WHERE id = $1;

-- name: FeedFailed :exec
UPDATE feeds SET
    last_fetched_at = now(),
    next_fetch_at   = $2,
    error_count     = error_count + 1,
    last_error      = $3
WHERE id = $1;

-- name: UpsertFavicon :one
INSERT INTO favicons (url, mime_type, data, fetched_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (url) DO UPDATE
    SET mime_type = EXCLUDED.mime_type, data = EXCLUDED.data, fetched_at = now()
RETURNING id;

-- name: SetFeedFavicon :exec
UPDATE feeds SET favicon_id = $2, favicon_checked_at = now() WHERE id = $1;

-- name: MarkAllFeedsDue :exec
UPDATE feeds SET next_fetch_at = now() WHERE next_fetch_at > now();

-- name: MarkFeedDue :execrows
UPDATE feeds SET next_fetch_at = now() WHERE id = $1;

-- DeleteExpiredItems deletes unsaved items published before the cutoff and
-- remembers their guids (see expired_items).
-- name: DeleteExpiredItems :execrows
WITH expired AS (
    DELETE FROM items WHERE published_at < $1 AND saved_at IS NULL
    RETURNING feed_id, guid
)
INSERT INTO expired_items (feed_id, guid)
SELECT feed_id, guid FROM expired
ON CONFLICT (feed_id, guid) DO NOTHING;
