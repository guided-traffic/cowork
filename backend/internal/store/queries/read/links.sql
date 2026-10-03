-- name: ListTicketLinks :many
-- A ticket's links in both directions, read from the ticket's side; a link
-- whose other end the caller cannot see is absent (docs/adr/0065 D4).
SELECT l.id, l.type, (l.source_id = sqlc.arg(ticket_id)::uuid)::boolean AS outgoing,
       o.id AS other_id, op.key AS other_project_key, o.number AS other_number, o.title AS other_title,
       o.state AS other_state, l.created_by, u.username AS created_by_username,
       u.display_name AS created_by_name, l.created_at
FROM ticket_links l
JOIN tickets o ON o.tenant_id = l.tenant_id
     AND o.id = CASE WHEN l.source_id = sqlc.arg(ticket_id)::uuid THEN l.target_id ELSE l.source_id END
JOIN projects op ON op.tenant_id = o.tenant_id AND op.id = o.project_id
LEFT JOIN users u ON u.id = l.created_by
WHERE l.tenant_id = sqlc.arg(tenant_id)
  AND (l.source_id = sqlc.arg(ticket_id)::uuid OR l.target_id = sqlc.arg(ticket_id)::uuid)
  AND app_ticket_visible(o.project_id, o.confidential, o.assignee_id, o.reporter_id)
  AND (sqlc.narg(after)::uuid IS NULL OR l.id > sqlc.narg(after)::uuid)
ORDER BY l.id
LIMIT sqlc.arg(page_size);

-- name: GetLink :one
-- A link between two tickets the caller has read through the predicate.
SELECT l.id, l.created_by, u.username AS created_by_username, u.display_name AS created_by_name, l.created_at
FROM ticket_links l
LEFT JOIN users u ON u.id = l.created_by
WHERE l.tenant_id = sqlc.arg(tenant_id) AND l.type = sqlc.arg(type)
  AND l.source_id = sqlc.arg(source_id) AND l.target_id = sqlc.arg(target_id);

-- name: BlocksPathExists :one
-- Whether to_id is reachable from from_id over blocks links (docs/adr/0012 D4).
-- visibility: exempt (an integrity walk returns no ticket)
SELECT blocks_path_exists(sqlc.arg(tenant_id), sqlc.arg(from_id), sqlc.arg(to_id))::boolean AS reachable;

-- name: GetUrgencyInputs :one
-- The facts rule set v1 reads (docs/adr/0010 D3). The derivation belongs to
-- the ticket, not to a reader: an open decision that blocks it counts whether
-- or not the caller can see it, and only the derived value leaves.
-- visibility: exempt (the derivation's inputs of a ticket the caller writes)
SELECT t.state, t.block_kind,
       EXISTS (SELECT 1
               FROM ticket_links l
               JOIN tickets s ON s.tenant_id = l.tenant_id AND s.id = l.source_id
               WHERE l.tenant_id = t.tenant_id AND l.target_id = t.id AND l.type = 'blocks'
                 AND s.type = 'decision' AND s.state NOT IN ('done', 'dropped')) AS open_decision_blocker
FROM tickets t
WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.id = sqlc.arg(id);

-- name: ListOpenPrerequisites :many
-- The open direct blocks sources of a ticket the caller can see: what refuses
-- done (docs/adr/0012 D7). One the caller cannot see neither shows nor
-- refuses.
SELECT s.id, sp.key AS project_key, s.number, s.title, s.state
FROM ticket_links l
JOIN tickets s ON s.tenant_id = l.tenant_id AND s.id = l.source_id
JOIN projects sp ON sp.tenant_id = s.tenant_id AND sp.id = s.project_id
WHERE l.tenant_id = sqlc.arg(tenant_id) AND l.target_id = sqlc.arg(ticket_id) AND l.type = 'blocks'
  AND s.state NOT IN ('done', 'dropped')
  AND app_ticket_visible(s.project_id, s.confidential, s.assignee_id, s.reporter_id)
ORDER BY sp.key, s.number;

-- name: ListBlockedTickets :many
-- The tickets a ticket blocks, whose urgency derivation reads it
-- (docs/adr/0010 D3). Only their derived urgency changes; nothing of them
-- reaches the caller.
-- visibility: exempt (the dependents of a derivation input)
SELECT t.id
FROM ticket_links l
JOIN tickets t ON t.tenant_id = l.tenant_id AND t.id = l.target_id
WHERE l.tenant_id = sqlc.arg(tenant_id) AND l.source_id = sqlc.arg(ticket_id) AND l.type = 'blocks'
ORDER BY t.id;
