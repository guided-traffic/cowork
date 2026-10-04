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
-- The person with what the resolvers and GET /api/v1/me need: whether they are
-- a global administrator, whether they have a local account, and whether its
-- password must be changed before anything else (docs/adr/0033 D4). The
-- account row is the person's own to read, so another person's flags are not
-- this query's to answer. A person of the identity provider comes with their
-- groups as of the last login or refresh and when the token gate last checked
-- them (docs/adr/0035 D8).
SELECT u.id, u.username, u.display_name, u.deactivated_at, u.created_at, u.global_admin,
       (a.user_id IS NOT NULL)::boolean AS local,
       COALESCE(a.password_change_required, false)::boolean AS password_change_required,
       (u.oidc_issuer IS NOT NULL)::boolean AS provider, u.oidc_issuer, u.oidc_subject, u.oidc_groups,
       u.oidc_groups_at, u.gate_checked_at
FROM users u
LEFT JOIN local_accounts a ON a.user_id = u.id
WHERE u.id = sqlc.arg(user_id);

-- name: ListMembershipsOfUser :many
-- The person's tenants with the highest role in each (GET /api/v1/me), and
-- every source of it with its own role, the mapping before the grant
-- (docs/adr/0030 D4).
SELECT t.id AS tenant_id, t.slug, t.name, max(m.role)::tenant_role AS role,
       array_agg(m.source::text ORDER BY m.source)::text[] AS sources,
       array_agg(m.role::text ORDER BY m.source)::text[] AS roles
FROM memberships m
JOIN tenants t ON t.id = m.tenant_id
WHERE m.user_id = sqlc.arg(user_id)
GROUP BY t.id, t.slug, t.name
ORDER BY t.slug;

-- name: ListMembers :many
-- The tenant's members by person id (docs/adr/0034 D7), a page after the
-- cursor's person: the highest role, every source of it with its own role —
-- the mapping before the grant — whether the person is a local account rather
-- than one of the identity provider (docs/adr/0030 D4, docs/adr/0033), and
-- the address, which the handler shows the tenant's administrators only.
SELECT u.id, u.username, u.display_name, u.email, max(m.role)::tenant_role AS role,
       array_agg(m.source::text ORDER BY m.source)::text[] AS sources,
       array_agg(m.role::text ORDER BY m.source)::text[] AS roles,
       (u.username IS NOT NULL)::boolean AS local
FROM memberships m
JOIN users u ON u.id = m.user_id
WHERE m.tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.narg(after)::uuid IS NULL OR u.id > sqlc.narg(after)::uuid)
GROUP BY u.id, u.username, u.display_name, u.email
ORDER BY u.id
LIMIT sqlc.arg(page_size);

-- name: GetMember :one
-- One member of the tenant, as the list shows them; no row when the person is
-- not a member.
SELECT u.id, u.username, u.display_name, u.email, max(m.role)::tenant_role AS role,
       array_agg(m.source::text ORDER BY m.source)::text[] AS sources,
       array_agg(m.role::text ORDER BY m.source)::text[] AS roles,
       (u.username IS NOT NULL)::boolean AS local
FROM memberships m
JOIN users u ON u.id = m.user_id
WHERE m.tenant_id = sqlc.arg(tenant_id) AND m.user_id = sqlc.arg(user_id)
GROUP BY u.id, u.username, u.display_name, u.email;
