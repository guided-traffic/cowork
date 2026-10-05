-- The bin of a tenant (docs/adr/0024 D1): its deleted tickets, the one view in
-- which a deleted ticket exists. Every other query carries deleted_at IS NULL
-- (D3); these two invert it, under the same visibility predicate.

-- name: ListDeletedTickets :many
-- The deleted tickets the caller can see, the last deleted first.
-- deletion: exempt (the bin, which lists the deleted tickets only)
SELECT t.id, p.key AS project_key, t.number, t.type, t.title, t.state, t.confidential, t.deleted_at,
       t.deleted_by, du.username AS deleted_by_username, du.display_name AS deleted_by_name
FROM tickets t
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
LEFT JOIN users du ON du.id = t.deleted_by
WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.deleted_at IS NOT NULL
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND (sqlc.narg(before_at)::timestamptz IS NULL
       OR t.deleted_at < sqlc.narg(before_at)::timestamptz
       OR (t.deleted_at = sqlc.narg(before_at)::timestamptz AND t.id < sqlc.narg(before_id)::uuid))
ORDER BY t.deleted_at DESC, t.id DESC
LIMIT sqlc.arg(page_size);

-- name: GetDeletedTicket :one
-- A deleted ticket the caller can see, by its project and number: what a
-- restoration and a purge act on.
-- deletion: exempt (the bin, which reads a deleted ticket only)
SELECT t.id, t.project_id, p.key AS project_key, t.number, t.parent_id, t.deleted_at
FROM tickets t
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
WHERE t.tenant_id = sqlc.arg(tenant_id) AND p.key = sqlc.arg(project_key) AND t.number = sqlc.arg(number)
  AND t.deleted_at IS NOT NULL
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id);
