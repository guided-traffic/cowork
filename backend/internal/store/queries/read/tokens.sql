-- name: GetTokenByHash :one
-- The resolver's lookup: the policy admits the row whose hash the lookup
-- transaction set in app.token_hash, and no other.
SELECT id, user_id, name, scope, restricted_tenant_id, restricted_project_id, agent,
       capabilities, created_at, expires_at, last_used_on, revoked_at
FROM tokens
WHERE token_hash = sqlc.arg(token_hash);

-- name: CountRecentRefusals :one
-- docs/adr/0035 D9: a refused use is recorded at most once per token, reason
-- and hour.
SELECT count(*)
FROM audit_events
WHERE tenant_id IS NULL
  AND token_id = sqlc.arg(token_id)
  AND action = 'refused'
  AND reason = sqlc.arg(reason)
  AND created_at > now() - interval '1 hour';

-- name: ListTokensOfUser :many
-- The person's tokens, newest first, revoked and expired ones included
-- (docs/adr/0035 D6); only one of them for a restricted token. By id after a
-- cursor, or a numbered page by offset (docs/adr/0048 D1, D2).
SELECT t.id, t.name, t.scope, t.agent, t.capabilities, t.restricted_tenant_id, t.restricted_project_id,
       t.created_at, t.expires_at, t.last_used_on, t.revoked_at, rt.slug AS restricted_tenant_slug
FROM tokens t
LEFT JOIN tenants rt ON rt.id = t.restricted_tenant_id
WHERE t.user_id = sqlc.arg(user_id)
  AND (sqlc.narg(only_id)::uuid IS NULL OR t.id = sqlc.narg(only_id)::uuid)
  AND (sqlc.narg(before)::uuid IS NULL OR t.id < sqlc.narg(before)::uuid)
ORDER BY t.id DESC
LIMIT sqlc.arg(page_size) OFFSET sqlc.arg(page_offset);

-- name: CountTokensOfUser :one
-- The person's tokens, for a numbered page's total; one for a restricted
-- token.
SELECT count(*)::bigint AS tokens
FROM tokens t
WHERE t.user_id = sqlc.arg(user_id)
  AND (sqlc.narg(only_id)::uuid IS NULL OR t.id = sqlc.narg(only_id)::uuid);

-- name: GetTokenOfUser :one
SELECT id, revoked_at
FROM tokens
WHERE id = sqlc.arg(token_id) AND user_id = sqlc.arg(user_id);

-- name: TokenStillUsable :one
-- Whether a stream's token may go on: not revoked, not expired, its person
-- not deactivated (docs/adr/0035 D6).
SELECT EXISTS (
    SELECT 1
    FROM tokens t
    JOIN users u ON u.id = t.user_id
    WHERE t.id = sqlc.arg(token_id) AND t.user_id = sqlc.arg(user_id) AND t.revoked_at IS NULL
      AND t.expires_at > now() AND u.deactivated_at IS NULL
) AS usable;

-- name: ListTenantTokens :many
-- The tokens that can act in the tenant, for its administrators
-- (docs/adr/0035 D5 as amended 2026-10-05): every token of a member of the
-- tenant that is unrestricted or restricted to it, newest first, revoked and
-- expired ones included. The tokens policy of migration 39 admits exactly
-- these rows to an administrator of the current tenant; the query names them
-- as well (docs/adr/0021 D4). By id after a cursor, or a numbered page by
-- offset (docs/adr/0048 D1, D2).
SELECT t.id, t.user_id, u.username, u.display_name, t.name, t.scope, t.agent, t.capabilities,
       t.restricted_tenant_id, t.restricted_project_id, p.key AS restricted_project_key,
       t.created_at, t.expires_at, t.last_used_on, t.revoked_at
FROM tokens t
JOIN users u ON u.id = t.user_id
LEFT JOIN projects p ON p.tenant_id = t.restricted_tenant_id AND p.id = t.restricted_project_id
     AND app_project_visible(p.id)
WHERE (t.restricted_tenant_id IS NULL OR t.restricted_tenant_id = sqlc.arg(tenant_id)::uuid)
  AND EXISTS (SELECT 1 FROM memberships m WHERE m.tenant_id = sqlc.arg(tenant_id)::uuid AND m.user_id = t.user_id)
  AND (sqlc.narg(before)::uuid IS NULL OR t.id < sqlc.narg(before)::uuid)
ORDER BY t.id DESC
LIMIT sqlc.arg(page_size) OFFSET sqlc.arg(page_offset);

-- name: CountTenantTokens :one
-- The tokens of ListTenantTokens, for a numbered page's total.
SELECT count(*)::bigint AS tokens
FROM tokens t
WHERE (t.restricted_tenant_id IS NULL OR t.restricted_tenant_id = sqlc.arg(tenant_id)::uuid)
  AND EXISTS (SELECT 1 FROM memberships m WHERE m.tenant_id = sqlc.arg(tenant_id)::uuid AND m.user_id = t.user_id);

-- name: GetTenantToken :one
-- One token of ListTenantTokens, for its revocation.
SELECT t.id, t.user_id, t.name, t.restricted_tenant_id, t.revoked_at
FROM tokens t
WHERE t.id = sqlc.arg(token_id)
  AND (t.restricted_tenant_id IS NULL OR t.restricted_tenant_id = sqlc.arg(tenant_id)::uuid)
  AND EXISTS (SELECT 1 FROM memberships m WHERE m.tenant_id = sqlc.arg(tenant_id)::uuid AND m.user_id = t.user_id);
