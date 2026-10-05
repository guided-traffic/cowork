-- An attachment is visible with its ticket (docs/adr/0016 D2).

-- name: ListAttachments :many
SELECT a.id, a.comment_id, a.file_name, a.size, a.sha256, a.content_type, a.uploaded_by,
       u.username AS uploaded_by_username, u.display_name AS uploaded_by_name, a.agent, a.token_id, a.token_name,
       a.created_at
FROM attachments a
JOIN tickets t ON t.tenant_id = a.tenant_id AND t.id = a.ticket_id
LEFT JOIN users u ON u.id = a.uploaded_by
WHERE a.tenant_id = sqlc.arg(tenant_id) AND a.ticket_id = sqlc.arg(ticket_id)
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
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
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id);

-- name: CountAttachments :one
-- The ticket's attachments, against the per-ticket count (docs/adr/0016 D6).
SELECT count(*)::bigint AS attachments
FROM attachments
WHERE tenant_id = sqlc.arg(tenant_id) AND ticket_id = sqlc.arg(ticket_id);

-- name: TenantAttachmentUsage :one
-- The bytes and the count of every attachment of the tenant, against its quota
-- (docs/adr/0016 D6): every ticket's, a confidential ticket's and a restricted
-- project's included. Row-level security holds it to the tenant; the handlers
-- answer it to the tenant's administrators only, who see every ticket.
-- visibility: exempt (the tenant's stored bytes count whether or not the caller sees the ticket that holds them)
SELECT coalesce(sum(size), 0)::bigint AS used_bytes, count(*)::bigint AS attachments
FROM attachments
WHERE tenant_id = sqlc.arg(tenant_id);
