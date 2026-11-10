-- +goose Up

-- Reading preferences, used where a group or feed sets nothing. The defaults
-- are the settings of the old Fever installation: oldest first, with images,
-- unread items only, marked read on scroll.
ALTER TABLE settings
    ADD COLUMN oldest_first boolean NOT NULL DEFAULT true,
    ADD COLUMN show_images  boolean NOT NULL DEFAULT true,
    ADD COLUMN show_read    boolean NOT NULL DEFAULT false,
    ADD COLUMN auto_read    boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE settings
    DROP COLUMN oldest_first,
    DROP COLUMN show_images,
    DROP COLUMN show_read,
    DROP COLUMN auto_read;
