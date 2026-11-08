-- +goose Up

-- Browsers that have logged in successfully before. They are exempt from the
-- global login limit, so a flood of wrong passwords from elsewhere cannot lock
-- the owner out of their own devices. Only a SHA-256 of the cookie is stored.
CREATE TABLE devices (
    token_hash   bytea PRIMARY KEY,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX devices_last_seen_at ON devices (last_seen_at);

-- +goose Down
DROP TABLE devices;
