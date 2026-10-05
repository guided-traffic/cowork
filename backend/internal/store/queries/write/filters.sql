-- Saved filters (docs/adr/0018 D5): written by their owner only, which the
-- policies of migration 36 hold as well.

-- name: InsertSavedFilter :one
INSERT INTO saved_filters (tenant_id, owner_id, name, parameters, shared)
VALUES (sqlc.arg(tenant_id), sqlc.arg(owner_id), sqlc.arg(name), sqlc.arg(parameters), sqlc.arg(shared))
RETURNING id;

-- name: UpdateSavedFilter :one
-- A compare-and-set on the version (docs/adr/0050 D1); another version, or a
-- filter deleted meanwhile, is no row.
UPDATE saved_filters
SET name = sqlc.arg(name), parameters = sqlc.arg(parameters), shared = sqlc.arg(shared),
    version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND owner_id = sqlc.arg(owner_id)
  AND version = sqlc.arg(version)
RETURNING version;

-- name: DeleteSavedFilter :execrows
DELETE FROM saved_filters
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND owner_id = sqlc.arg(owner_id);
