-- +goose Up

-- The Saved list is ordered like every other list, by publication date. The
-- index was keyed by the date items were saved, so it could find the saved
-- items but not return them in order.
DROP INDEX items_saved;
CREATE INDEX items_saved ON items (published_at DESC, id DESC) WHERE saved_at IS NOT NULL;

-- +goose Down
DROP INDEX items_saved;
CREATE INDEX items_saved ON items (saved_at DESC, id DESC) WHERE saved_at IS NOT NULL;
