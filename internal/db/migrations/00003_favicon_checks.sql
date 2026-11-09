-- +goose Up

-- When the refresher last looked for a favicon for this feed, so feeds
-- without one are not re-checked on every refresh.
ALTER TABLE feeds ADD COLUMN favicon_checked_at timestamptz;

-- +goose Down
ALTER TABLE feeds DROP COLUMN favicon_checked_at;
