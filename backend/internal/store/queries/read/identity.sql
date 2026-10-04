-- The identity provider's reads. They run in a transaction that names the
-- provider as its job (app.job = 'identity-provider'), which the policies of
-- these tables admit across tenants (docs/adr/0021 D3): a person's memberships
-- are derived in every tenant at once (docs/adr/0030 D2).

-- name: GetPersonByIdentity :one
-- The person of the identity provider with this issuer and subject
-- (docs/adr/0029 D5); no row before their first login.
SELECT id, display_name, email, email_verified, global_admin, deactivated_at, oidc_groups
FROM users
WHERE oidc_issuer = sqlc.arg(issuer) AND oidc_subject = sqlc.arg(subject);

-- name: ListMappedRoles :many
-- The highest role the mappings of each tenant give these groups
-- (docs/adr/0030 D2).
SELECT tenant_id, max(role)::tenant_role AS role
FROM group_mappings
WHERE group_name = ANY (sqlc.arg(groups)::text[])
GROUP BY tenant_id;

-- name: ListMappedMembershipsOfUser :many
-- The memberships of a person the mappings derived, in every tenant.
SELECT id, tenant_id, role
FROM memberships
WHERE user_id = sqlc.arg(user_id) AND source = 'mapping';

-- name: ListPersonsInGroup :many
-- The persons of the configured issuer whose groups, as of their last login or
-- refresh, include the group: the ones a change of a mapping of it re-derives
-- (docs/adr/0030 D2), in the order their locks are taken.
SELECT id
FROM users
WHERE oidc_groups @> ARRAY[sqlc.arg(group_name)::text] AND oidc_issuer = sqlc.arg(issuer)
ORDER BY id;

-- name: GetPersonForDerivation :one
-- What a derivation reads of a person once it holds the person's lock: their
-- groups, and whether they can act at all.
SELECT oidc_groups, oidc_issuer, deactivated_at, gate_checked_at
FROM users
WHERE id = sqlc.arg(id);

-- name: MappedRoleInTenant :one
-- The highest role the tenant's mappings give these groups; empty when none
-- does.
SELECT COALESCE(max(role)::text, '')::text AS role
FROM group_mappings
WHERE tenant_id = sqlc.arg(tenant_id) AND group_name = ANY (sqlc.arg(groups)::text[]);

-- name: GetMappedMembership :one
SELECT id, role
FROM memberships
WHERE tenant_id = sqlc.arg(tenant_id) AND user_id = sqlc.arg(user_id) AND source = 'mapping';
