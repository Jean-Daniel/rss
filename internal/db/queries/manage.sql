-- name: GetPreferences :one
SELECT oldest_first, show_images, show_read, auto_read FROM settings;

-- name: SetPreferences :exec
UPDATE settings SET
    oldest_first = @oldest_first,
    show_images  = @show_images,
    show_read    = @show_read,
    auto_read    = @auto_read,
    updated_at   = now();

-- name: GetFeedSettings :one
SELECT f.id, f.title, f.url, f.show_images, f.oldest_first, f.show_unread_count,
       f.auth_username, coalesce(f.auth_password, '') <> '' AS has_password,
       coalesce((SELECT array_agg(group_id ORDER BY group_id) FROM feed_groups WHERE feed_id = f.id), '{}')::bigint[] AS group_ids,
       (SELECT count(*) FROM items WHERE feed_id = f.id)::bigint AS item_count,
       (SELECT count(*) FROM items WHERE feed_id = f.id AND saved_at IS NOT NULL)::bigint AS saved_count
FROM feeds f
WHERE f.id = $1;

-- name: FeedIDByURL :one
SELECT id FROM feeds WHERE url = $1;

-- CreateFeed adds a feed, unless one has this address already. It is
-- fetched by whoever adds it: next_fetch_at keeps the refresher away
-- meanwhile.
-- name: CreateFeed :one
INSERT INTO feeds (url, title, auth_username, auth_password, next_fetch_at)
VALUES (@url, @title, sqlc.narg(auth_username), sqlc.narg(auth_password), @next_fetch_at)
ON CONFLICT (url) DO NOTHING
RETURNING id;

-- name: FeedForUpdate :one
SELECT url, title, auth_username, auth_password FROM feeds WHERE id = $1 FOR UPDATE;

-- name: UpdateFeed :exec
UPDATE feeds SET
    title             = @title,
    url               = @url,
    show_images       = sqlc.narg(show_images),
    oldest_first      = sqlc.narg(oldest_first),
    show_unread_count = sqlc.narg(show_unread_count),
    auth_username     = sqlc.narg(auth_username),
    auth_password     = sqlc.narg(auth_password)
WHERE id = @id;

-- RefetchFeed makes a feed download its document again, in full, right
-- away: after its address, login or title changed (an empty title is filled
-- in from the feed). A new address or login also starts the wait between
-- failed attempts over.
-- name: RefetchFeed :exec
UPDATE feeds SET
    etag          = NULL,
    last_modified = NULL,
    error_count   = CASE WHEN @fresh_start::boolean THEN 0 ELSE error_count END,
    next_fetch_at = now()
WHERE id = @id;

-- SetFeedGroups puts a feed in exactly the groups listed (unknown ids are
-- ignored).
-- name: SetFeedGroups :exec
WITH removed AS (
    DELETE FROM feed_groups
    WHERE feed_id = @feed_id AND NOT (group_id = ANY(@group_ids::bigint[]))
)
INSERT INTO feed_groups (feed_id, group_id)
SELECT @feed_id, id FROM groups WHERE id = ANY(@group_ids::bigint[])
ON CONFLICT DO NOTHING;

-- name: DeleteFeed :execrows
DELETE FROM feeds WHERE id = $1;

-- name: CreateGroup :one
INSERT INTO groups (title, show_images, oldest_first, show_unread_count)
VALUES (@title, sqlc.narg(show_images), sqlc.narg(oldest_first), sqlc.narg(show_unread_count))
RETURNING id;

-- name: UpdateGroup :execrows
UPDATE groups SET
    title             = @title,
    show_images       = sqlc.narg(show_images),
    oldest_first      = sqlc.narg(oldest_first),
    show_unread_count = sqlc.narg(show_unread_count)
WHERE id = @id;

-- SetGroupFeeds puts exactly the feeds listed in a group (unknown ids are
-- ignored).
-- name: SetGroupFeeds :exec
WITH removed AS (
    DELETE FROM feed_groups
    WHERE group_id = @group_id AND NOT (feed_id = ANY(@feed_ids::bigint[]))
)
INSERT INTO feed_groups (feed_id, group_id)
SELECT id, @group_id FROM feeds WHERE id = ANY(@feed_ids::bigint[])
ON CONFLICT DO NOTHING;

-- name: DeleteGroup :execrows
DELETE FROM groups WHERE id = $1;

-- ImportFeed adds a feed from an OPML file, unless one has this address
-- already (no row then: see FeedIDByURL). New feeds are due right away.
-- name: ImportFeed :one
INSERT INTO feeds (url, title, site_url)
VALUES (@url, @title, @site_url)
ON CONFLICT (url) DO NOTHING
RETURNING id;

-- name: AddFeedToGroup :exec
INSERT INTO feed_groups (feed_id, group_id) VALUES (@feed_id, @group_id)
ON CONFLICT DO NOTHING;
