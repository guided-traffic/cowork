-- name: InsertSession :exec
INSERT INTO sessions (user_id, token_hash, user_agent_hash, created_at, last_seen_at, expires_at)
VALUES (sqlc.arg(user_id), sqlc.arg(token_hash), sqlc.narg(user_agent_hash), sqlc.arg(created_at),
        sqlc.arg(created_at), sqlc.arg(expires_at));

-- name: TouchSession :exec
-- Bookkeeping, not an act (docs/adr/0031 D3): the idle clock moves at most once
-- per minute and session.
UPDATE sessions
SET last_seen_at = sqlc.arg(now)
WHERE id = sqlc.arg(session_id) AND last_seen_at < sqlc.arg(older_than);

-- name: DeleteSessionByHash :execrows
-- A login replaces the session its request presented, a logout ends it
-- (docs/adr/0031 D4, D5).
DELETE FROM sessions WHERE token_hash = sqlc.arg(token_hash);

-- name: DeleteSessionsOfUser :execrows
DELETE FROM sessions WHERE user_id = sqlc.arg(user_id);

-- name: DeleteOtherSessionsOfUser :execrows
-- A change of password ends every other session of the account
-- (docs/adr/0033 D4).
DELETE FROM sessions WHERE user_id = sqlc.arg(user_id) AND token_hash <> sqlc.arg(keep_hash);

-- name: DeleteExpiredSessions :execrows
-- The expiry job (docs/adr/0031 D3): past the absolute limit, or idle for
-- longer than the idle time.
DELETE FROM sessions WHERE expires_at <= sqlc.arg(now) OR last_seen_at <= sqlc.arg(idle_before);
