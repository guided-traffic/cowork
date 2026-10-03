-- name: GetSessionByHash :one
-- The resolver's lookup: the policy admits the row whose hash the transaction
-- set in app.session_hash, and no other (docs/adr/0031 D1, D6).
SELECT id, user_id, created_at, last_seen_at, expires_at
FROM sessions
WHERE token_hash = sqlc.arg(token_hash);

-- name: SessionStillUsable :one
-- Whether an open stream's session may go on: it exists, neither limit has
-- passed, and its person is not deactivated (docs/adr/0031 D3, D4).
SELECT EXISTS (
    SELECT 1
    FROM sessions s
    JOIN users u ON u.id = s.user_id
    WHERE s.token_hash = sqlc.arg(token_hash) AND s.user_id = sqlc.arg(user_id)
      AND s.expires_at > sqlc.arg(now) AND s.last_seen_at > sqlc.arg(idle_before)
      AND u.deactivated_at IS NULL
) AS usable;
