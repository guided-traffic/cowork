-- Deleting, restoring and purging a ticket (docs/adr/0024 D1–D3, D7). The
-- deletion and the restoration move the marker and the version; the purge
-- removes the ticket and what belongs only to it, inside a transaction that
-- names the purge in app.job, which the restrictive policies of migration 32
-- demand of every such delete.

-- name: MarkTicketDeleted :one
-- A ticket the caller read through the predicate goes into the bin; one a
-- concurrent request deleted first is no row.
UPDATE tickets
SET deleted_at = now(), deleted_by = sqlc.arg(deleted_by)::uuid, version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND deleted_at IS NULL
RETURNING version;

-- name: RestoreTicket :one
-- A deleted ticket comes back as it was; one a concurrent request restored or
-- purged first is no row.
UPDATE tickets
SET deleted_at = NULL, deleted_by = NULL, version = version + 1, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND deleted_at IS NOT NULL
RETURNING version;

-- name: ListPurgeDue :many
-- The deleted tickets of every tenant whose time in the bin has passed, read by
-- the purge job with no tenant set (migration 32, tickets_purge_due).
-- visibility: exempt (the purge job reads the deleted tickets of every tenant)
-- deletion: exempt (the purge reads deleted tickets only)
SELECT tenant_id, id FROM tickets
WHERE deleted_at IS NOT NULL AND deleted_at < sqlc.arg(deleted_before)::timestamptz
ORDER BY tenant_id, deleted_at, id
LIMIT sqlc.arg(batch);

-- name: GetPurgedTicket :one
-- What the purge keeps of a deleted ticket: its key for the act, the facts its
-- publication carries, and the attachment objects to remove after the commit.
-- visibility: exempt (the purge of a deleted ticket an administrator or the job named)
-- deletion: exempt (the purge reads a deleted ticket only)
SELECT t.id, t.project_id, p.key AS project_key, t.number, t.version, t.confidential, t.assignee_id,
       t.reporter_id, t.imported_from_job,
       ARRAY(SELECT a.id FROM attachments a WHERE a.tenant_id = t.tenant_id AND a.ticket_id = t.id
             ORDER BY a.id)::uuid[] AS attachments
FROM tickets t
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.id = sqlc.arg(id) AND t.deleted_at IS NOT NULL
FOR UPDATE OF t;

-- name: ForgetPurgedImportFile :execrows
-- The purge of a ticket an import created takes its file out of the report
-- of the job that created it, which would keep the title, the threat, the
-- note and the questions the purge removes (docs/adr/0024 D2,
-- docs/adr/0051 D3); the report's summary still counts it. The policies of
-- migration 41 admit the purge to the update.
UPDATE import_jobs j
SET report = jsonb_set(j.report, '{files}', coalesce((
        SELECT jsonb_agg(e.f ORDER BY e.n)
        FROM jsonb_array_elements(j.report -> 'files') WITH ORDINALITY AS e(f, n)
        WHERE e.f ->> 'key' IS DISTINCT FROM sqlc.arg(ticket_key)::text), '[]'::jsonb))
WHERE j.tenant_id = sqlc.arg(tenant_id) AND j.id = sqlc.arg(id) AND j.status = 'executed'
  AND j.report -> 'files' @> jsonb_build_array(jsonb_build_object('key', sqlc.arg(ticket_key)::text));

-- name: PurgeTicketAudit :one
-- The audit rows of the ticket keep its key, the actor and the act, their
-- content emptied (docs/adr/0024 D2), through the owner's function of
-- migration 32.
SELECT purge_ticket_audit(sqlc.arg(id))::bigint AS emptied;

-- name: DeleteTicketNotifications :execrows
-- The notifications about the ticket, and those whose act is on it.
DELETE FROM notifications n
WHERE n.tenant_id = sqlc.arg(tenant_id)
  AND (n.ticket_id = sqlc.arg(ticket_id)
       OR n.audit_event_id IN (SELECT a.id FROM audit_events a
                               WHERE a.tenant_id = sqlc.arg(tenant_id) AND a.ticket_id = sqlc.arg(ticket_id)));

-- name: DeleteTicketAttachments :execrows
DELETE FROM attachments WHERE tenant_id = sqlc.arg(tenant_id) AND ticket_id = sqlc.arg(ticket_id);

-- name: DeleteTicketCommentRevisions :execrows
DELETE FROM comment_revisions r
USING comments c
WHERE r.tenant_id = sqlc.arg(tenant_id) AND c.tenant_id = r.tenant_id AND c.id = r.comment_id
  AND c.ticket_id = sqlc.arg(ticket_id);

-- name: DeleteTicketComments :execrows
DELETE FROM comments WHERE tenant_id = sqlc.arg(tenant_id) AND ticket_id = sqlc.arg(ticket_id);

-- name: DeleteTicketQuestions :execrows
DELETE FROM questions WHERE tenant_id = sqlc.arg(tenant_id) AND ticket_id = sqlc.arg(ticket_id);

-- name: DeleteTicketTimeEntryRevisions :execrows
DELETE FROM time_entry_revisions r
USING time_entries e
WHERE r.tenant_id = sqlc.arg(tenant_id) AND e.tenant_id = r.tenant_id AND e.id = r.entry_id
  AND e.ticket_id = sqlc.arg(ticket_id);

-- name: DeleteTicketTimeEntries :execrows
DELETE FROM time_entries WHERE tenant_id = sqlc.arg(tenant_id) AND ticket_id = sqlc.arg(ticket_id);

-- name: DeleteTicketInterest :execrows
DELETE FROM ticket_interest WHERE tenant_id = sqlc.arg(tenant_id) AND ticket_id = sqlc.arg(ticket_id);

-- name: DeleteTicketLinks :execrows
DELETE FROM ticket_links
WHERE tenant_id = sqlc.arg(tenant_id) AND (source_id = sqlc.arg(ticket_id) OR target_id = sqlc.arg(ticket_id));

-- name: DeleteTicketPullRequests :execrows
-- The pull requests and commits GitHub's webhook linked to the ticket
-- (docs/adr/0071 D6), the removed links among them.
DELETE FROM ticket_pull_requests WHERE tenant_id = sqlc.arg(tenant_id) AND ticket_id = sqlc.arg(ticket_id);

-- name: DetachChildren :execrows
-- The children of a purged ticket become roots. Their parent was hidden since
-- the deletion — nobody saw it any more — so nothing they show changes, and
-- their versions stay (docs/adr/0050 D1).
UPDATE tickets
SET parent_id = NULL
WHERE tenant_id = sqlc.arg(tenant_id) AND parent_id = sqlc.arg(ticket_id)::uuid;

-- name: ReleaseBlocksOn :many
-- A ticket whose block names the purged ticket waits on an external
-- reference from then on, the purged ticket's key: the block keeps what it
-- waits on as text, which a purged ticket can no longer be (docs/adr/0009 D2).
-- visibility: exempt (the tickets whose block names the purged ticket, each an act of the purge)
UPDATE tickets t
SET block_kind = 'external', block_ticket_id = NULL, block_external_ref = sqlc.arg(key)::text,
    version = t.version + 1, updated_at = now()
FROM projects p
WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.block_ticket_id = sqlc.arg(ticket_id)::uuid
  AND p.tenant_id = t.tenant_id AND p.id = t.project_id
RETURNING t.id, p.key AS project_key, t.number;

-- name: DeletePurgedTicket :execrows
-- The ticket itself, last; its number stays taken, because the project's
-- counter only grows (docs/adr/0007 D4).
-- visibility: exempt (the purge of a deleted ticket)
-- deletion: exempt (the purge removes a deleted ticket only)
DELETE FROM tickets
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND deleted_at IS NOT NULL;
