-- Saved filters (docs/adr/0018 D5): written by their owner, and another
-- person's shared filter unshared or deleted by an administrator of the
-- tenant; the policies of migrations 33 and 39 hold the same.

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

-- name: UnshareSavedFilter :one
-- An administrator's unshare of another person's filter, a compare-and-set on
-- the version that sets nothing but shared; a filter no longer shared, of
-- another version or deleted meanwhile is no row. Run it through
-- Writer.UnshareAnothersFilter, which names the filter for the read policy.
UPDATE saved_filters
SET shared = false, version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND shared AND version = sqlc.arg(version)
RETURNING version, updated_at;

-- name: DeleteSavedFilter :execrows
DELETE FROM saved_filters
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND owner_id = sqlc.arg(owner_id);

-- name: DeleteSharedSavedFilter :execrows
-- An administrator's deletion of another person's shared filter; one no
-- longer shared is no row.
DELETE FROM saved_filters
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND shared;
