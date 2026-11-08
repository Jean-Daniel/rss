-- name: GetPasswordHash :one
SELECT password_hash FROM settings WHERE id;

-- name: SetPasswordHash :exec
UPDATE settings SET password_hash = $1, updated_at = now() WHERE id;

-- name: CreateSession :exec
INSERT INTO sessions (token_hash, expires_at, user_agent)
VALUES ($1, $2, $3);

-- name: GetSession :one
SELECT token_hash, last_seen_at, expires_at
FROM sessions
WHERE token_hash = $1 AND expires_at > now();

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = now(), expires_at = $2
WHERE token_hash = $1;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = $1;

-- name: DeleteAllSessions :exec
DELETE FROM sessions;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= now();

-- name: DeviceExists :one
SELECT EXISTS (SELECT 1 FROM devices WHERE token_hash = $1);

-- name: CreateDevice :exec
INSERT INTO devices (token_hash) VALUES ($1);

-- name: TouchDevice :exec
UPDATE devices SET last_seen_at = now() WHERE token_hash = $1;

-- name: DeleteAllDevices :exec
DELETE FROM devices;

-- name: DeleteStaleDevices :execrows
DELETE FROM devices WHERE last_seen_at < $1;
