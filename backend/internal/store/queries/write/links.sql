-- A link lives in its source's team; its target may be a ticket of any team
-- (docs/adr/0012 D2 as amended 2026-10-10, migration 47).

-- name: InsertLink :one
-- A new link, or no row where an equal one stands by now: the same type
-- between the same tickets in the same direction, or a relates-to of the pair
-- stored from either end (ticket_links_relates_once). A writer that raced
-- another to it waits for that one's commit here and reads the link back
-- instead of failing (docs/adr/0045 D1).
INSERT INTO ticket_links (tenant_id, type, source_id, target_id, created_by)
VALUES (sqlc.arg(tenant_id), sqlc.arg(type), sqlc.arg(source_id), sqlc.arg(target_id), sqlc.arg(created_by))
ON CONFLICT DO NOTHING
RETURNING id, created_at;

-- name: GetLinkByID :one
-- A link of the transaction's team by its id, as its removal by id names it:
-- the row of a link whose source is a ticket of the team.
SELECT id, type, source_id, target_id, created_by, created_at
FROM ticket_links
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id);

-- name: DeleteLinkByID :execrows
DELETE FROM ticket_links
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id);

-- name: LinkEndKey :one
-- The key of a ticket of the transaction's team at the other end of a link the
-- caller removes, whatever they see of it: the removal is an act on both
-- tickets (docs/adr/0012 D3), the other one's shown to its own readers.
-- visibility: exempt (the other end of a link the caller removes, whose act is recorded on it)
-- deletion: exempt (a link to a deleted ticket is removed with its act as well)
SELECT p.key AS project_key, t.number
FROM tickets t
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.id = sqlc.arg(id);
