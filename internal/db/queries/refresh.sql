-- DueFeeds returns the next feeds to fetch. skip lists the feeds a run has
-- already taken: one whose outcome could not be saved stays due, and must
-- neither be fetched again right away nor keep the feeds after it waiting.
-- name: DueFeeds :many
SELECT id, url, site_url, favicon_id, favicon_checked_at, auth_username, auth_password,
       etag, last_modified, error_count
FROM feeds
WHERE next_fetch_at <= now() AND NOT (id = ANY(@skip::bigint[]))
ORDER BY next_fetch_at, id
LIMIT @max_rows;

-- KnownGUIDs returns which of the guids a feed lists right now it has
-- already seen: stored items, and expired ones it still lists.
--
-- It also keeps track of the expired entries the feed stopped listing. They
-- are forgotten only once they have stayed away since before forget_before:
-- a feed that serves an empty or partial document for a while, then lists
-- its old entries again, does not bring them back as unread. Rows are written
-- only when an entry disappears or comes back, not on every fetch. (The three
-- changes touch different rows: listed, newly missing, missing for long.)
-- name: KnownGUIDs :many
WITH listed_again AS (
    UPDATE expired_items AS back SET missing_since = NULL
    WHERE back.feed_id = @feed_id AND back.missing_since IS NOT NULL
      AND back.guid = ANY(@guids::text[])
), missing AS (
    UPDATE expired_items AS away SET missing_since = @seen_at::timestamptz
    WHERE away.feed_id = @feed_id AND away.missing_since IS NULL
      AND NOT (away.guid = ANY(@guids::text[]))
), forgotten AS (
    DELETE FROM expired_items AS gone
    WHERE gone.feed_id = @feed_id AND gone.missing_since < @forget_before::timestamptz
      AND NOT (gone.guid = ANY(@guids::text[]))
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

-- MarkGroupDue makes the feeds of a group due, and reports whether the
-- group exists (it may have no feeds).
-- name: MarkGroupDue :one
WITH due AS (
    UPDATE feeds f SET next_fetch_at = now()
    WHERE f.id IN (SELECT fg.feed_id FROM feed_groups fg WHERE fg.group_id = @group_id)
    RETURNING f.id
)
SELECT EXISTS (SELECT 1 FROM groups g WHERE g.id = @group_id)::boolean AS found;

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
