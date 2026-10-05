-- The parts of the Markdown export beyond the ticket row (docs/adr/0044 D1);
-- the caller has read the ticket through the predicate.

-- name: TransitionNote :one
-- The note and the reason of the act that last brought the ticket into a
-- state: where the export's shipped and dropped-reason come from
-- (docs/adr/0009 D5).
SELECT coalesce(note, '')::text AS note, coalesce(reason, '')::text AS reason
FROM audit_events
WHERE tenant_id = sqlc.arg(tenant_id) AND ticket_id = sqlc.arg(ticket_id) AND action = 'transitioned'
  AND after->>'state' = sqlc.arg(state)::text
ORDER BY id DESC
LIMIT 1;

-- name: ExportQuestions :many
SELECT q.number, q.question, q.options, q.recommendation, q.answer, q.status
FROM questions q
JOIN tickets t ON t.tenant_id = q.tenant_id AND t.id = q.ticket_id
WHERE q.tenant_id = sqlc.arg(tenant_id) AND q.ticket_id = sqlc.arg(ticket_id)
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
ORDER BY q.number;

-- name: ExportAttachmentNames :many
SELECT a.file_name
FROM attachments a
JOIN tickets t ON t.tenant_id = a.tenant_id AND t.id = a.ticket_id
WHERE a.tenant_id = sqlc.arg(tenant_id) AND a.ticket_id = sqlc.arg(ticket_id)
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
ORDER BY a.id;
