-- name: ListGroups :many
SELECT id, title, show_unread_count
FROM groups
ORDER BY lower(title), id;

-- ListFeeds returns every feed with its unread count and groups, plus the
-- title to show (the site's host when the feed has none), sorted by that
-- title. saved_count, the same on every row, saves the sidebar a query.
-- name: ListFeeds :many
SELECT f.id, f.display_title, f.url, f.site_url, f.favicon_id, f.show_unread_count, f.last_error,
       coalesce(u.unread, 0)::bigint AS unread,
       coalesce(g.group_ids, '{}')::bigint[] AS group_ids,
       (SELECT count(*) FROM items WHERE saved_at IS NOT NULL)::bigint AS saved_count
FROM (
    SELECT id, url, site_url, favicon_id, show_unread_count, last_error,
           coalesce(nullif(title, ''),
                    regexp_replace(url, '^[a-zA-Z][a-zA-Z0-9+.-]*://([^/?#@]*@)?([^/?#]+).*$', '\2'))::text AS display_title
    FROM feeds
) f
LEFT JOIN (
    SELECT feed_id, count(*) AS unread
    FROM items
    WHERE read_at IS NULL
    GROUP BY feed_id
) u ON u.feed_id = f.id
LEFT JOIN (
    SELECT feed_id, array_agg(group_id ORDER BY group_id) AS group_ids
    FROM feed_groups
    GROUP BY feed_id
) g ON g.feed_id = f.id
ORDER BY lower(f.display_title), f.id;

-- name: GetFavicon :one
SELECT mime_type, data, fetched_at FROM favicons WHERE id = $1;
