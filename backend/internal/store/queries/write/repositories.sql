-- name: InsertRepository :one
-- Binds a repository to a project; a binding of the same identity and path
-- that a simultaneous request wrote first returns no row.
INSERT INTO project_repositories (tenant_id, project_id, identity, path, remote)
VALUES (sqlc.arg(tenant_id), sqlc.arg(project_id), sqlc.arg(identity), sqlc.arg(path), sqlc.arg(remote))
ON CONFLICT (tenant_id, identity, path) DO NOTHING
RETURNING id, project_id, identity, path, remote, created_at, updated_at;

-- name: UpdateRepositoryRemote :one
-- Keeps the last original form of a bound remote (docs/adr/0066 D1).
UPDATE project_repositories
SET remote = sqlc.arg(remote), updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id)
RETURNING id, project_id, identity, path, remote, created_at, updated_at;

-- name: DeleteRepository :one
-- Unbinds a repository; no row when it was not bound to the project.
DELETE FROM project_repositories
WHERE tenant_id = sqlc.arg(tenant_id) AND project_id = sqlc.arg(project_id) AND id = sqlc.arg(id)
RETURNING id, identity, path, remote;
