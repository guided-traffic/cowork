-- name: ListInterest :many
-- The stakes in a ticket, visible inside the tenant with the ticket
-- (docs/adr/0013 D2); settled once the ticket is done or dropped (D5).
SELECT i.user_id, u.username, u.display_name, i.weight, i.note, i.since, i.updated_at,
       (t.state IN ('done', 'dropped'))::boolean AS settled
FROM ticket_interest i
JOIN tickets t ON t.tenant_id = i.tenant_id AND t.id = i.ticket_id
LEFT JOIN users u ON u.id = i.user_id
WHERE i.tenant_id = sqlc.arg(tenant_id) AND i.ticket_id = sqlc.arg(ticket_id)
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND (sqlc.narg(after)::uuid IS NULL OR i.user_id > sqlc.narg(after)::uuid)
ORDER BY i.user_id
LIMIT sqlc.arg(page_size);

-- name: GetInterest :one
-- The caller's own stake in a ticket it has read.
SELECT i.user_id, u.username, u.display_name, i.weight, i.note, i.since, i.updated_at
FROM ticket_interest i
LEFT JOIN users u ON u.id = i.user_id
WHERE i.tenant_id = sqlc.arg(tenant_id) AND i.ticket_id = sqlc.arg(ticket_id) AND i.user_id = sqlc.arg(user_id);
