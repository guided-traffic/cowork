-- name: InsertSession :exec
-- A session of the identity provider's login holds the groups of the login and
-- the sealed refresh token, if the issuer gave one (docs/adr/0031 D1).
INSERT INTO sessions (user_id, token_hash, user_agent_hash, created_at, last_seen_at, expires_at, method,
                      groups, groups_refreshed_at, refresh_token_sealed)
VALUES (sqlc.arg(user_id), sqlc.arg(token_hash), sqlc.narg(user_agent_hash), sqlc.arg(created_at),
        sqlc.arg(created_at), sqlc.arg(expires_at), sqlc.arg(method), sqlc.narg(groups),
        sqlc.narg(groups_refreshed_at), sqlc.narg(refresh_token_sealed));

-- name: LockLoginPassword :one
-- The password a local login verified, read again in the transaction that makes
-- its session, under a share lock that a change of the password waits for
-- (docs/adr/0033 D4): a change committed meanwhile is seen, and one that comes
-- later finds the session to end.
SELECT password_hash
FROM local_accounts
WHERE user_id = sqlc.arg(user_id)
FOR SHARE;

-- name: ClaimSessionRefresh :one
-- A request claims a session's groups refresh by a lease (docs/adr/0030 D5, the
-- security review of 2026-10-04, M1): the one statement that moves
-- refresh_retry_at ahead wins, and every other request — of this replica or
-- another — finds the refresh not due and is served on the groups the session
-- holds, without waiting. The issuer is asked with no connection held.
UPDATE sessions
SET refresh_retry_at = sqlc.arg(lease_until)
WHERE token_hash = sqlc.arg(token_hash) AND method = 'oidc'
  AND (groups_refreshed_at IS NULL OR groups_refreshed_at <= sqlc.arg(due_before))
  AND (refresh_retry_at IS NULL OR refresh_retry_at <= sqlc.arg(now))
RETURNING refresh_token_sealed;

-- name: LockSessionForRefresh :one
-- The refresh's answer is applied under a lock of the session's row, while the
-- lease is still the claimant's.
SELECT id, groups, refresh_retry_at
FROM sessions
WHERE token_hash = sqlc.arg(token_hash)
FOR UPDATE;

-- name: SetSessionGroups :exec
-- A refresh that read the groups: the snapshot, its time, and the refresh token
-- the issuer rotated, if it did.
UPDATE sessions
SET groups = sqlc.arg(groups), groups_refreshed_at = sqlc.arg(refreshed_at),
    refresh_token_sealed = COALESCE(sqlc.narg(refresh_token_sealed), refresh_token_sealed),
    refresh_retry_at = NULL
WHERE id = sqlc.arg(session_id);

-- name: DeferSessionRefresh :exec
-- The issuer could not be reached: the session is served, and the next attempt
-- waits (docs/adr/0030 D5). A refresh token the issuer rotated before the
-- failure is kept all the same, since the old one is spent.
UPDATE sessions
SET refresh_retry_at = sqlc.arg(retry_at),
    refresh_token_sealed = COALESCE(sqlc.narg(refresh_token_sealed), refresh_token_sealed)
WHERE id = sqlc.arg(session_id);

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
