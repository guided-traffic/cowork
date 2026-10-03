-- name: TouchTokenLastUsed :exec
-- Bookkeeping, not an act (docs/adr/0035 D2): at most one write per token and
-- UTC day.
UPDATE tokens
SET last_used_on = sqlc.arg(day)
WHERE id = sqlc.arg(token_id) AND (last_used_on IS NULL OR last_used_on < sqlc.arg(day));

-- name: RevokeToken :one
-- Revocation is immediate and keeps the row (docs/adr/0035 D6).
UPDATE tokens
SET revoked_at = now(), revoked_by = sqlc.arg(revoked_by)
WHERE id = sqlc.arg(token_id) AND user_id = sqlc.arg(user_id) AND revoked_at IS NULL
RETURNING id;
