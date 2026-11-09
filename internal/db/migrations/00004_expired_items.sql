-- +goose Up

-- Items deleted after the retention period, remembered for as long as their
-- feed still lists them, so they are not added again as new. Without this,
-- entries without a date (stored as published "now") would come back as
-- unread every retention period.
CREATE TABLE expired_items (
    feed_id    bigint NOT NULL REFERENCES feeds (id) ON DELETE CASCADE,
    guid       text NOT NULL,
    expired_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (feed_id, guid)
);

-- +goose Down
DROP TABLE expired_items;
