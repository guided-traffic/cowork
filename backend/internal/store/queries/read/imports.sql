-- The import job and what its analysis reads of the project (docs/adr/0051,
-- docs/adr/0063, docs/adr/0064 D3). Only the person who made a job and the
-- tenant's administrators reach it: the policies of migration 45 hold every
-- row to them.

-- name: GetImportJob :one
-- A job of the project with its report; a dry run past its day is gone
-- (docs/adr/0051 D7), as if the expiry job had deleted it already.
SELECT j.id, j.status, j.created_by, cu.username AS created_by_username, cu.display_name AS created_by_name,
       j.created_at, j.expires_at, j.executed_by, eu.username AS executed_by_username,
       eu.display_name AS executed_by_name, j.executed_at, j.report
FROM import_jobs j
LEFT JOIN users cu ON cu.id = j.created_by
LEFT JOIN users eu ON eu.id = j.executed_by
WHERE j.tenant_id = sqlc.arg(tenant_id) AND j.project_id = sqlc.arg(project_id) AND j.id = sqlc.arg(id)
  AND (j.status = 'executed' OR j.expires_at > sqlc.arg(now));

-- name: ImportNumbersTaken :many
-- The numbers of the project a ticket holds — a deleted one in the bin
-- included — among those an import brings: each is a conflict, since a
-- repeated import is a duplicate, not an update, and a number is never handed
-- out twice (docs/adr/0064 D3, docs/adr/0007 D4). A number is unique in the
-- project whether or not the caller sees the ticket that holds it.
-- visibility: exempt (a number's existence in the project, as the unique key holds it)
-- deletion: exempt (a deleted ticket keeps its number)
SELECT t.number
FROM tickets t
WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.project_id = sqlc.arg(project_id)
  AND t.number = ANY (sqlc.arg(numbers)::integer[])
ORDER BY t.number;

-- name: ImportPurgedKeys :many
-- The keys among those an import brings whose ticket was purged: an import
-- gives its number back, and the report warns that what named the key names
-- the new ticket (docs/adr/0007 D4). The purge's act keeps the key
-- (docs/adr/0024 D2).
SELECT DISTINCT ticket_key::text AS ticket_key
FROM audit_events
WHERE tenant_id = sqlc.arg(tenant_id) AND action = 'purged' AND entity_type = 'ticket'
  AND ticket_key = ANY (sqlc.arg(keys)::text[]);

-- name: ImportReferencedTickets :many
-- The project's tickets an import's references name by number — a parent, a
-- blocks or found-in link to a ticket the upload does not hold — as the
-- caller sees them (docs/adr/0051 D9, docs/adr/0063 D3).
SELECT t.id, t.number, t.title, t.state
FROM tickets t
WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.project_id = sqlc.arg(project_id)
  AND t.number = ANY (sqlc.arg(numbers)::integer[])
  AND t.deleted_at IS NULL AND app_ticket_visible(t.project_id, t.confidential, t.assignee_id, t.reporter_id)
ORDER BY t.number;

-- name: ImportPersons :many
-- The members an import's assignees name by their identity (docs/adr/0044 D1):
-- a username, or the subject of the configured issuer. The read policy of
-- users admits the members of the tenant; the caller then holds each to the
-- project as an assignee is held.
SELECT u.id, u.username, u.oidc_issuer, u.oidc_subject, u.display_name
FROM users u
WHERE u.deactivated_at IS NULL
  AND EXISTS (SELECT 1 FROM memberships m WHERE m.user_id = u.id AND m.tenant_id = sqlc.arg(tenant_id))
  AND (u.username = ANY (sqlc.arg(usernames)::text[])
       OR (u.oidc_issuer = sqlc.arg(issuer)::text AND u.oidc_subject = ANY (sqlc.arg(subjects)::text[])))
ORDER BY u.id;
