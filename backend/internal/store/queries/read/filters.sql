-- Saved filters (docs/adr/0018 D5): the caller's own and the tenant's shared
-- ones, each with its owner. The policies of migrations 33 and 39 admit no
-- other row — but the one an administrator's unshare names for its own
-- statement —, and every query names the owner's rule as well
-- (docs/adr/0021 D4).

-- name: ListSavedFilters :many
SELECT f.id, f.owner_id, u.username AS owner_username, u.display_name AS owner_name, f.name, f.parameters,
       f.shared, f.version, f.created_at, f.updated_at
FROM saved_filters f
LEFT JOIN users u ON u.id = f.owner_id
WHERE f.tenant_id = sqlc.arg(tenant_id) AND (f.owner_id = sqlc.arg(user_id) OR f.shared)
  AND (sqlc.narg(after)::uuid IS NULL OR f.id > sqlc.narg(after)::uuid)
ORDER BY f.id
LIMIT sqlc.arg(page_size);

-- name: GetSavedFilter :one
SELECT f.id, f.owner_id, u.username AS owner_username, u.display_name AS owner_name, f.name, f.parameters,
       f.shared, f.version, f.created_at, f.updated_at
FROM saved_filters f
LEFT JOIN users u ON u.id = f.owner_id
WHERE f.tenant_id = sqlc.arg(tenant_id) AND f.id = sqlc.arg(id) AND (f.owner_id = sqlc.arg(user_id) OR f.shared);
