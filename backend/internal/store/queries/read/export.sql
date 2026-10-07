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

-- name: ExportPerson :one
-- The identity the export writes beside a person's display name
-- (docs/adr/0044 D1): the username of a local account (docs/adr/0033 D2), or
-- the issuer and the subject of a person of the identity provider
-- (docs/adr/0029 D5). The person's read policy decides, as it does for the
-- display name the ticket row carries.
SELECT username, oidc_issuer, oidc_subject
FROM users
WHERE id = sqlc.arg(user_id);

-- name: ExportLinks :many
-- The links of a project export's manifest — every link with an end in the
-- project — or, without a project, of the tenant export, each once
-- (docs/adr/0051 D4): only where the caller sees both ends.
SELECT l.type, sp.key AS source_project, s.number AS source_number, tp.key AS target_project, t.number AS target_number
FROM ticket_links l
JOIN tickets s ON s.tenant_id = l.tenant_id AND s.id = l.source_id
JOIN projects sp ON sp.tenant_id = s.tenant_id AND sp.id = s.project_id
JOIN tickets t ON t.tenant_id = l.tenant_id AND t.id = l.target_id
JOIN projects tp ON tp.tenant_id = t.tenant_id AND tp.id = t.project_id
WHERE l.tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.narg(project_id)::uuid IS NULL OR s.project_id = sqlc.narg(project_id)::uuid
       OR t.project_id = sqlc.narg(project_id)::uuid)
  AND s.deleted_at IS NULL AND app_ticket_visible(s.project_id, s.confidential, s.assignee_id, s.reporter_id)
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
ORDER BY sp.key, s.number, l.type, tp.key, t.number;

-- name: ExportAttachments :many
-- The attachments manifest of an export: the metadata of every attachment of
-- the project's — or, without a project, the tenant's — tickets the caller
-- sees, never the bytes (docs/adr/0051 D4, docs/adr/0016 D5).
SELECT p.key AS project_key, t.number, a.id, a.file_name, a.content_type, a.size
FROM attachments a
JOIN tickets t ON t.tenant_id = a.tenant_id AND t.id = a.ticket_id
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
WHERE a.tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.narg(project_id)::uuid IS NULL OR t.project_id = sqlc.narg(project_id)::uuid)
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
ORDER BY p.key, t.number, a.id;

-- name: ExportHiddenConfidential :many
-- How many confidential tickets of each project the caller sees an export
-- leaves out because the caller may not read them — the manifest's "n
-- confidential tickets not included" (docs/adr/0065 D5). A count per project,
-- never a ticket; a restricted project the caller cannot see is not counted,
-- the export leaves it out altogether. The predicate answers NULL, not false,
-- for a ticket without an assignee, so the count asks IS NOT TRUE.
-- visibility: exempt (the count of the confidential tickets an export leaves out, docs/adr/0065 D5)
SELECT t.project_id, count(*)::integer AS hidden
FROM tickets t
WHERE t.tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.narg(project_id)::uuid IS NULL OR t.project_id = sqlc.narg(project_id)::uuid)
  AND t.deleted_at IS NULL AND t.confidential AND app_project_visible(t.project_id)
  AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id) IS NOT TRUE
GROUP BY t.project_id
ORDER BY t.project_id;
