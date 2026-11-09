-- Read and saved state changes. Only rows whose state actually changes are
-- touched, so state_changed_at only moves for real changes.

-- name: MarkRead :execrows
UPDATE items SET read_at = now(), state_changed_at = now()
WHERE id = ANY(@ids::bigint[]) AND read_at IS NULL;

-- name: MarkUnread :execrows
UPDATE items SET read_at = NULL, state_changed_at = now()
WHERE id = ANY(@ids::bigint[]) AND read_at IS NOT NULL;

-- name: MarkSaved :execrows
UPDATE items SET saved_at = now(), state_changed_at = now()
WHERE id = ANY(@ids::bigint[]) AND saved_at IS NULL;

-- name: MarkUnsaved :execrows
UPDATE items SET saved_at = NULL, state_changed_at = now()
WHERE id = ANY(@ids::bigint[]) AND saved_at IS NOT NULL;

-- "Mark all as read" only covers items that had arrived when the list was
-- shown (added_at <= before), so newer ones stay unread.

-- name: MarkAllRead :execrows
UPDATE items SET read_at = now(), state_changed_at = now()
WHERE read_at IS NULL AND added_at <= @before;

-- name: MarkFeedRead :execrows
UPDATE items SET read_at = now(), state_changed_at = now()
WHERE read_at IS NULL AND added_at <= @before AND feed_id = @feed_id;

-- name: MarkGroupRead :execrows
UPDATE items SET read_at = now(), state_changed_at = now()
WHERE read_at IS NULL AND added_at <= @before
  AND feed_id IN (SELECT feed_id FROM feed_groups WHERE group_id = @group_id);

-- ItemChanges lists read/saved changes after a position (state_changed_at,
-- id), for other devices. The id breaks ties: a "mark all as read" gives
-- thousands of items the same time.
-- name: ItemChanges :many
SELECT id, (read_at IS NOT NULL)::bool AS read, (saved_at IS NOT NULL)::bool AS saved,
       state_changed_at
FROM items
WHERE (state_changed_at, id) > (@after_time::timestamptz, @after_id::bigint)
ORDER BY state_changed_at, id
LIMIT @max_rows;
