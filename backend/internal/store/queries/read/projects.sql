-- name: ListProjects :many
-- The projects the caller can see, by key.
SELECT id, key, name, description, restricted, wip_limits, archived_at, version, created_at, updated_at
FROM projects
WHERE tenant_id = sqlc.arg(tenant_id)
  AND app_project_visible(id)
  AND (sqlc.arg(include_archived)::boolean OR archived_at IS NULL)
  AND (sqlc.narg(after)::text IS NULL OR key > sqlc.narg(after)::text)
ORDER BY key
LIMIT sqlc.arg(page_size);

-- name: GetProjectByKey :one
SELECT id, key, name, description, restricted, wip_limits, archived_at, version, created_at, updated_at
FROM projects
WHERE tenant_id = sqlc.arg(tenant_id) AND key = sqlc.arg(key) AND app_project_visible(id);

-- name: GetProjectAccessRole :one
-- The person's entry on a restricted project's list (docs/adr/0034 D3).
SELECT role
FROM project_access
WHERE tenant_id = sqlc.arg(tenant_id) AND project_id = sqlc.arg(project_id) AND user_id = sqlc.arg(user_id);

-- name: ProjectKeyTaken :one
-- Keys are unique within the tenant whether or not the caller can see the
-- project that holds one; the answer says only taken or free.
-- visibility: exempt (a key's existence, not a project)
SELECT EXISTS (SELECT 1 FROM projects WHERE tenant_id = sqlc.arg(tenant_id) AND key = sqlc.arg(key));
