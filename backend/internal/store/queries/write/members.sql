-- The administration's writes: grants, group mappings, a project's restriction
-- and its access list (docs/adr/0030 D2, D3, docs/adr/0034 D3), inside the
-- tenant's transaction of one of its administrators.

-- name: SetGrantRole :exec
UPDATE memberships
SET role = sqlc.arg(role), version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND user_id = sqlc.arg(user_id) AND source = 'grant';

-- name: DeleteGrant :one
DELETE FROM memberships
WHERE tenant_id = sqlc.arg(tenant_id) AND user_id = sqlc.arg(user_id) AND source = 'grant'
RETURNING id, role;

-- name: InsertGroupMapping :exec
-- The id is made by the application, so the act that records the mapping can
-- name it.
INSERT INTO group_mappings (id, tenant_id, group_name, role, created_by)
VALUES (sqlc.arg(id), sqlc.arg(tenant_id), sqlc.arg(group_name), sqlc.arg(role), sqlc.narg(created_by));

-- name: UpdateGroupMappingRole :one
-- A compare-and-set on the version (docs/adr/0050 D1, D3).
UPDATE group_mappings
SET role = sqlc.arg(role), version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING id, group_name, role, version, created_at, updated_at;

-- name: DeleteGroupMapping :one
DELETE FROM group_mappings
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id)
RETURNING group_name, role;

-- name: SetProjectRestricted :one
-- A compare-and-set on the version (docs/adr/0050 D3): the restriction is a
-- setting of the project (docs/adr/0034 D3).
UPDATE projects
SET restricted = sqlc.arg(restricted), version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING id, key, name, description, restricted, wip_limits, archived_at, version, created_at, updated_at;

-- name: InsertProjectAccess :one
INSERT INTO project_access (tenant_id, project_id, user_id, role)
VALUES (sqlc.arg(tenant_id), sqlc.arg(project_id), sqlc.arg(user_id), sqlc.arg(role))
RETURNING id, created_at;

-- name: SetProjectAccessRole :one
UPDATE project_access
SET role = sqlc.arg(role)
WHERE tenant_id = sqlc.arg(tenant_id) AND project_id = sqlc.arg(project_id) AND user_id = sqlc.arg(user_id)
RETURNING id, created_at;

-- name: DeleteProjectAccess :one
DELETE FROM project_access
WHERE tenant_id = sqlc.arg(tenant_id) AND project_id = sqlc.arg(project_id) AND user_id = sqlc.arg(user_id)
RETURNING id, role;
