-- name: InsertLink :one
INSERT INTO ticket_links (tenant_id, type, source_id, target_id, created_by)
VALUES (sqlc.arg(tenant_id), sqlc.arg(type), sqlc.arg(source_id), sqlc.arg(target_id), sqlc.arg(created_by))
RETURNING id, created_at;

-- name: DeleteLink :execrows
DELETE FROM ticket_links
WHERE tenant_id = sqlc.arg(tenant_id) AND type = sqlc.arg(type)
  AND source_id = sqlc.arg(source_id) AND target_id = sqlc.arg(target_id);

-- name: RederiveUrgency :exec
-- A new derivation after an input changed, which ends the override
-- (docs/adr/0010 D3). Caused by another entity — a link, another ticket's
-- state — it leaves the ticket's version alone (docs/adr/0050 D1); the
-- ticket's own transitions bump the version themselves.
UPDATE tickets
SET urgency_derived = sqlc.arg(urgency_derived), urgency_rule = sqlc.arg(urgency_rule),
    urgency_override = NULL, urgency_override_reason = NULL, urgency_override_by = NULL,
    urgency_override_at = NULL, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id);
