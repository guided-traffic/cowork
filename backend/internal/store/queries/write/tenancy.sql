-- name: UpdateTenantSettings :one
-- A compare-and-set on the version (docs/adr/0050 D1, D3): no row comes back
-- when the version moved since the caller read it.
UPDATE tenants
SET name = sqlc.arg(name),
    time_visible_to_members = sqlc.arg(time_visible_to_members),
    time_locked_until = sqlc.narg(time_locked_until),
    members_create_projects = sqlc.arg(members_create_projects),
    version = version + 1,
    updated_at = now()
WHERE id = sqlc.arg(tenant_id) AND version = sqlc.arg(version)
RETURNING version, updated_at;
