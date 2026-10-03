-- name: InsertProject :one
INSERT INTO projects (tenant_id, key, name, description, wip_limits)
VALUES (sqlc.arg(tenant_id), sqlc.arg(key), sqlc.arg(name), sqlc.arg(description), sqlc.arg(wip_limits))
RETURNING id, key, name, description, restricted, wip_limits, archived_at, version, created_at, updated_at;

-- name: InsertTicketCounter :exec
INSERT INTO ticket_counters (tenant_id, project_id) VALUES (sqlc.arg(tenant_id), sqlc.arg(project_id));

-- name: UpdateProject :one
-- A compare-and-set on the version (docs/adr/0050 D1).
UPDATE projects
SET name = sqlc.arg(name), description = sqlc.arg(description), wip_limits = sqlc.arg(wip_limits),
    version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING id, key, name, description, restricted, wip_limits, archived_at, version, created_at, updated_at;

-- name: ArchiveProject :one
UPDATE projects
SET archived_at = now(), version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND archived_at IS NULL
RETURNING id, key, name, description, restricted, wip_limits, archived_at, version, created_at, updated_at;
