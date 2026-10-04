-- The identity provider's writes (app.job = 'identity-provider'): its persons
-- and the memberships the mappings derive (docs/adr/0029 D5, docs/adr/0030 D2).

-- name: InsertProviderPerson :exec
-- A person's first login through the identity provider. The id is made by the
-- application, as a local account's is.
INSERT INTO users (id, display_name, global_admin, oidc_issuer, oidc_subject, email, email_verified,
                   oidc_groups, oidc_groups_at, gate_checked_at)
VALUES (sqlc.arg(id), sqlc.arg(display_name), sqlc.arg(global_admin), sqlc.arg(issuer), sqlc.arg(subject),
        sqlc.narg(email), sqlc.narg(email_verified), sqlc.arg(groups), sqlc.arg(now), sqlc.arg(now));

-- name: UpdateProviderPerson :exec
-- Every login refreshes what the issuer says of the person: the display
-- attributes, the groups and the administrator flag the administrator group
-- decides (docs/adr/0029 D5, docs/adr/0030 D1).
UPDATE users
SET display_name = sqlc.arg(display_name), email = sqlc.narg(email), email_verified = sqlc.narg(email_verified),
    global_admin = sqlc.arg(global_admin), oidc_groups = sqlc.arg(groups), oidc_groups_at = sqlc.arg(now),
    gate_checked_at = sqlc.arg(now), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: SetPersonGroups :exec
-- A groups refresh, or a refused login: the person's snapshot and their
-- administrator flag. The gate's check is stamped only when the gate admitted
-- the groups; otherwise it is cleared, so the person's next token request meets
-- the gate at once (docs/adr/0035 D8).
UPDATE users
SET oidc_groups = sqlc.arg(groups), oidc_groups_at = sqlc.arg(now), gate_checked_at = sqlc.narg(gate_checked_at),
    global_admin = sqlc.arg(global_admin), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: StampGateCheck :exec
-- The token gate checked the person's snapshot (docs/adr/0035 D8).
UPDATE users
SET gate_checked_at = sqlc.arg(now), global_admin = sqlc.arg(global_admin), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: InsertMappedMembership :exec
INSERT INTO memberships (id, tenant_id, user_id, role, source)
VALUES (sqlc.arg(id), sqlc.arg(tenant_id), sqlc.arg(user_id), sqlc.arg(role), 'mapping');

-- name: SetMembershipRole :exec
UPDATE memberships SET role = sqlc.arg(role), version = version + 1, updated_at = now() WHERE id = sqlc.arg(id);

-- name: DeleteMembership :exec
DELETE FROM memberships WHERE id = sqlc.arg(id);
