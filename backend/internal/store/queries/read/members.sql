-- The reads of the administration of members, mappings and project access
-- (docs/adr/0030 D2, D3, docs/adr/0034 D3, D7), inside the tenant's
-- transaction.

-- name: FindPersonsByEmail :many
-- The active persons of the configured issuer whose address — as the issuer
-- asserted it at their last login — is the given one, compared without regard
-- to case. An address is someone's only when the issuer marked it verified, or,
-- with COWORK_OIDC_EMAIL_TRUSTED (email_trusted), when it said nothing about
-- it; one it marked unverified is no one's (docs/adr/0030 D3). A person of
-- another issuer cannot log in (the security review of 2026-10-04, m6). The
-- transaction names the address in app.person_lookup, which the users policy
-- admits those rows for; two rows say the address is not one person's.
SELECT id
FROM users
WHERE lower(email) = lower(sqlc.arg(email)) AND deactivated_at IS NULL
  AND (email_verified OR (sqlc.arg(email_trusted)::boolean AND email_verified IS NULL))
  AND oidc_issuer = sqlc.arg(issuer)::text
ORDER BY id
LIMIT 2;

-- name: FindPersonByUsername :one
-- The active local account of a username (docs/adr/0033 D2), looked up the same
-- way.
SELECT id
FROM users
WHERE username = sqlc.arg(username) AND deactivated_at IS NULL;

-- name: GetGrant :one
-- The person's marked grant in the tenant (docs/adr/0030 D3).
SELECT id, role
FROM memberships
WHERE tenant_id = sqlc.arg(tenant_id) AND user_id = sqlc.arg(user_id) AND source = 'grant';

-- name: IsMember :one
SELECT EXISTS (
    SELECT 1 FROM memberships WHERE tenant_id = sqlc.arg(tenant_id) AND user_id = sqlc.arg(user_id)
) AS member;

-- name: TenantHasAdmin :one
-- Whether a person who can log in holds the admin role in the tenant, mapped
-- or granted: what a change of a grant or a mapping must leave
-- (docs/adr/0034 D1). A deactivated person cannot, nor can a person of the
-- identity provider who is not of the configured issuer, or whom the gate
-- refused at their last login or refresh (gate_checked_at is cleared then).
SELECT EXISTS (
    SELECT 1
    FROM memberships m
    JOIN users u ON u.id = m.user_id
    WHERE m.tenant_id = sqlc.arg(tenant_id) AND m.role = 'admin' AND u.deactivated_at IS NULL
      AND (u.oidc_issuer IS NULL OR (u.oidc_issuer = sqlc.arg(issuer)::text AND u.gate_checked_at IS NOT NULL))
) AS has_admin;

-- name: ListGroupMappings :many
-- The tenant's group mappings by group, a page after the cursor's group.
SELECT id, group_name, role, version, created_at, updated_at
FROM group_mappings
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.narg(after)::text IS NULL OR group_name > sqlc.narg(after)::text)
ORDER BY group_name
LIMIT sqlc.arg(page_size);

-- name: GetGroupMapping :one
SELECT id, group_name, role, version, created_at, updated_at
FROM group_mappings
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id);

-- name: GroupMappingExists :one
SELECT EXISTS (
    SELECT 1 FROM group_mappings WHERE tenant_id = sqlc.arg(tenant_id) AND group_name = sqlc.arg(group_name)
) AS taken;

-- name: ListProjectAccess :many
-- A project's access list by person id (docs/adr/0034 D3), a page after the
-- cursor's person: the entries of the tenant's members, whom the users policy
-- admits to the join.
SELECT a.user_id, u.username, u.display_name, u.email, a.role, a.created_at
FROM project_access a
JOIN users u ON u.id = a.user_id
WHERE a.tenant_id = sqlc.arg(tenant_id) AND a.project_id = sqlc.arg(project_id)
  AND (sqlc.narg(after)::uuid IS NULL OR a.user_id > sqlc.narg(after)::uuid)
ORDER BY a.user_id
LIMIT sqlc.arg(page_size);

-- name: GetProjectAccessEntry :one
SELECT role, created_at
FROM project_access
WHERE tenant_id = sqlc.arg(tenant_id) AND project_id = sqlc.arg(project_id) AND user_id = sqlc.arg(user_id);
