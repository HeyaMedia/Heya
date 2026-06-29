-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, expires_at, kind, name, user_agent, ip)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetSessionByToken :one
SELECT * FROM sessions
WHERE token_hash = $1
  AND (expires_at IS NULL OR expires_at > now());

-- name: TouchSession :exec
-- Bump last_seen_at on the session backing a request. Throttle is in SQL
-- (no-op UPDATE when the row was touched in the last minute) so the
-- middleware can fire-and-forget without holding any in-memory state.
UPDATE sessions
SET last_seen_at = now()
WHERE token_hash = $1
  AND last_seen_at < now() - interval '60 seconds';

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = $1;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = $1;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at IS NOT NULL AND expires_at <= now();

-- name: ListUserSessionsByKind :many
-- For the "My sessions" and "API tokens" pages — most-recent activity first.
SELECT id, user_id, token_hash, expires_at, created_at, kind, name, last_seen_at, user_agent, ip
FROM sessions
WHERE user_id = $1 AND kind = $2
  AND (expires_at IS NULL OR expires_at > now())
ORDER BY last_seen_at DESC;

-- name: DeleteUserSessionByID :exec
-- Single-session revoke scoped to user_id so a user can't tear down someone
-- else's session even if they guess an ID.
DELETE FROM sessions WHERE id = $1 AND user_id = $2;

-- name: DeleteUserOtherSessions :exec
-- "Sign out of every other device" — keep only the session backing the
-- current request, drop the rest. Scope to kind='session' so a user's
-- long-lived API tokens aren't affected.
DELETE FROM sessions
WHERE user_id = $1
  AND kind = 'session'
  AND token_hash <> $2;

-- name: ListAllSessionsForAdmin :many
-- Admin-only roster: every active session across every user, joined to the
-- owning username for display. Sorted by last activity descending.
SELECT s.id, s.user_id, s.kind, s.name, s.expires_at, s.created_at,
       s.last_seen_at, s.user_agent, s.ip,
       u.username, u.is_admin
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.expires_at IS NULL OR s.expires_at > now()
ORDER BY s.last_seen_at DESC;

-- name: DeleteSessionByIDAdmin :exec
-- Admin-only single-session revoke — no user_id scope, so the admin
-- console can boot any device or token by id.
DELETE FROM sessions WHERE id = $1;
