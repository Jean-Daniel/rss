-- +goose Up

-- When the feed stopped listing an expired entry (NULL while it lists it).
-- The entry is only forgotten once it has stayed away for a while: a feed
-- that serves an empty or partial document for a moment, then lists its old
-- entries again, must not bring them back as unread.
ALTER TABLE expired_items ADD COLUMN missing_since timestamptz;

-- +goose Down
ALTER TABLE expired_items DROP COLUMN missing_since;
