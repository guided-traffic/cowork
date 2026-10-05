-- An attachment is visible with its ticket (docs/adr/0016 D2).

-- name: ListAttachments :many
SELECT a.id, a.comment_id, a.file_name, a.size, a.sha256, a.content_type, a.uploaded_by,
       u.username AS uploaded_by_username, u.display_name AS uploaded_by_name, a.agent, a.token_id, a.token_name,
       a.created_at
FROM attachments a
JOIN tickets t ON t.tenant_id = a.tenant_id AND t.id = a.ticket_id
LEFT JOIN users u ON u.id = a.uploaded_by
WHERE a.tenant_id = sqlc.arg(tenant_id) AND a.ticket_id = sqlc.arg(ticket_id)
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
  AND (sqlc.narg(after)::uuid IS NULL OR a.id > sqlc.narg(after)::uuid)
ORDER BY a.id
LIMIT sqlc.arg(page_size);

-- name: GetAttachment :one
SELECT a.id, a.comment_id, a.file_name, a.size, a.sha256, a.content_type, a.uploaded_by,
       u.username AS uploaded_by_username, u.display_name AS uploaded_by_name, a.agent, a.token_id, a.token_name,
       a.created_at
FROM attachments a
JOIN tickets t ON t.tenant_id = a.tenant_id AND t.id = a.ticket_id
LEFT JOIN users u ON u.id = a.uploaded_by
WHERE a.tenant_id = sqlc.arg(tenant_id) AND a.ticket_id = sqlc.arg(ticket_id) AND a.id = sqlc.arg(id)
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id);

-- name: ListTicketImages :many
-- The attachments of the tickets a rendered text may show as images, with
-- their type; the caller keeps the raster ones (docs/adr/0016 D7).
SELECT a.ticket_id, a.id, a.content_type
FROM attachments a
JOIN tickets t ON t.tenant_id = a.tenant_id AND t.id = a.ticket_id
WHERE a.tenant_id = sqlc.arg(tenant_id) AND a.ticket_id = ANY(sqlc.arg(ticket_ids)::uuid[])
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
ORDER BY a.ticket_id, a.id;

-- name: CountAttachments :one
-- The ticket's attachments, against the per-ticket count (docs/adr/0016 D6).
SELECT count(*)::bigint AS attachments
FROM attachments
WHERE tenant_id = sqlc.arg(tenant_id) AND ticket_id = sqlc.arg(ticket_id);

-- name: TenantAttachmentUsage :one
-- The bytes and the count of every attachment of the tenant, against its quota
-- (docs/adr/0016 D6): every ticket's, a confidential ticket's and a restricted
-- project's included, and a deleted ticket's until the purge removes its rows.
-- Row-level security holds it to the tenant; the handlers answer it to the
-- tenant's administrators only, who see every ticket.
-- visibility: exempt (the tenant's stored bytes count whether or not the caller sees the ticket that holds them)
-- deletion: exempt (a deleted ticket's files occupy the bucket until the purge removes them, docs/adr/0024 D2)
SELECT coalesce(sum(size), 0)::bigint AS used_bytes, count(*)::bigint AS attachments
FROM attachments
WHERE tenant_id = sqlc.arg(tenant_id);
