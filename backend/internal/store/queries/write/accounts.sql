-- name: InsertTenant :exec
-- The id is made by the application: a returned row would have to pass the
-- tenant's read policy, which the creator has not earned yet.
INSERT INTO tenants (id, slug, name) VALUES (sqlc.arg(id), sqlc.arg(slug), sqlc.arg(name));

-- name: InsertUser :exec
-- The id is made by the application, for the same reason as a tenant's.
INSERT INTO users (id, username, display_name, global_admin)
VALUES (sqlc.arg(id), sqlc.arg(username), sqlc.arg(display_name), sqlc.arg(global_admin));

-- name: InsertGrant :exec
-- A marked manual grant (docs/adr/0030 D3). The id is made by the application,
-- so the act that records the grant can name it.
INSERT INTO memberships (id, tenant_id, user_id, role, source)
VALUES (sqlc.arg(id), sqlc.arg(tenant_id), sqlc.arg(user_id), sqlc.arg(role), 'grant');

-- name: InsertLocalAccount :exec
INSERT INTO local_accounts (user_id, password_hash, password_change_required, origin, managing_tenant_id)
VALUES (sqlc.arg(user_id), sqlc.arg(password_hash), sqlc.arg(password_change_required), sqlc.arg(origin),
        sqlc.narg(managing_tenant_id));

-- name: SetAccountPassword :execrows
UPDATE local_accounts
SET password_hash = sqlc.arg(password_hash), password_change_required = sqlc.arg(password_change_required),
    updated_at = now()
WHERE user_id = sqlc.arg(user_id);

-- name: TakeOverAccount :execrows
-- The start-up synchronisation makes the account it keeps its own: the
-- configured password, no tenant manages it any more.
UPDATE local_accounts
SET password_hash = sqlc.arg(password_hash), password_change_required = false, origin = 'config',
    managing_tenant_id = NULL, updated_at = now()
WHERE user_id = sqlc.arg(user_id);

-- name: SetGlobalAdmin :execrows
UPDATE users SET global_admin = true, updated_at = now() WHERE id = sqlc.arg(user_id) AND NOT global_admin;

-- name: DeactivateUser :execrows
-- docs/adr/0024 D5: a person is deactivated, never deleted.
UPDATE users SET deactivated_at = now(), updated_at = now()
WHERE id = sqlc.arg(user_id) AND deactivated_at IS NULL;

-- name: ReactivateUser :execrows
UPDATE users SET deactivated_at = NULL, updated_at = now()
WHERE id = sqlc.arg(user_id) AND deactivated_at IS NOT NULL;

-- name: RevokeTokensOfUser :execrows
-- Deactivating a person revokes every token they hold (docs/adr/0024 D5,
-- docs/adr/0035 D6); a NULL revoked_by is a system act, as the table says.
UPDATE tokens
SET revoked_at = now(), revoked_by = sqlc.narg(revoked_by)
WHERE user_id = sqlc.arg(user_id) AND revoked_at IS NULL;

-- name: InsertToken :one
INSERT INTO tokens (user_id, name, token_hash, scope, restricted_tenant_id, restricted_project_id, agent,
                    capabilities, expires_at)
VALUES (sqlc.arg(user_id), sqlc.arg(name), sqlc.arg(token_hash), sqlc.arg(scope), sqlc.narg(restricted_tenant_id),
        sqlc.narg(restricted_project_id), sqlc.arg(agent), sqlc.arg(capabilities), sqlc.arg(expires_at))
RETURNING id, created_at;
