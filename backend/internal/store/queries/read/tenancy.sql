-- name: GetTenantForPerson :one
-- The tenant boundary (docs/adr/0023 D5): the tenant by slug with the
-- person's highest role in it, or no row when the slug is unknown or the
-- person has no membership — the caller answers both alike.
SELECT t.id, t.slug, t.name, max(m.role)::tenant_role AS role
FROM tenants t
JOIN memberships m ON m.tenant_id = t.id
WHERE t.slug = sqlc.arg(slug) AND m.user_id = sqlc.arg(user_id)
GROUP BY t.id, t.slug, t.name;

-- name: GetTenant :one
SELECT id, slug, name, version, time_visible_to_members, time_locked_until,
       members_create_projects, created_at, updated_at
FROM tenants
WHERE id = sqlc.arg(tenant_id);

-- name: GetUser :one
SELECT id, username, display_name, deactivated_at, created_at
FROM users
WHERE id = sqlc.arg(user_id);

-- name: ListMembershipsOfUser :many
-- The person's tenants with the highest role in each (GET /api/v1/me).
SELECT t.id AS tenant_id, t.slug, t.name, max(m.role)::tenant_role AS role
FROM memberships m
JOIN tenants t ON t.id = m.tenant_id
WHERE m.user_id = sqlc.arg(user_id)
GROUP BY t.id, t.slug, t.name
ORDER BY t.slug;

-- name: ListMembers :many
-- The tenant's members by person id (docs/adr/0034 D7), a page after the
-- cursor's person.
SELECT u.id, u.username, u.display_name, max(m.role)::tenant_role AS role
FROM memberships m
JOIN users u ON u.id = m.user_id
WHERE m.tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.narg(after)::uuid IS NULL OR u.id > sqlc.narg(after)::uuid)
GROUP BY u.id, u.username, u.display_name
ORDER BY u.id
LIMIT sqlc.arg(page_size);
