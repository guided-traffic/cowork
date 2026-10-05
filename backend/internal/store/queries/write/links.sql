-- name: InsertLink :one
INSERT INTO ticket_links (tenant_id, type, source_id, target_id, created_by)
VALUES (sqlc.arg(tenant_id), sqlc.arg(type), sqlc.arg(source_id), sqlc.arg(target_id), sqlc.arg(created_by))
RETURNING id, created_at;

-- name: DeleteLink :execrows
DELETE FROM ticket_links
WHERE tenant_id = sqlc.arg(tenant_id) AND type = sqlc.arg(type)
  AND source_id = sqlc.arg(source_id) AND target_id = sqlc.arg(target_id);
