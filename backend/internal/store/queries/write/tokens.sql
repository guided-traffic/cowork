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

-- name: RevokeTenantToken :one
-- An administrator's revocation of a token that can act in the tenant
-- (docs/adr/0035 D5, D6): immediate, the row kept; the policy of migration 39
-- admits it.
UPDATE tokens
SET revoked_at = now(), revoked_by = sqlc.arg(revoked_by)
WHERE id = sqlc.arg(token_id) AND revoked_at IS NULL
  AND (restricted_tenant_id IS NULL OR restricted_tenant_id = sqlc.arg(tenant_id)::uuid)
RETURNING id;
