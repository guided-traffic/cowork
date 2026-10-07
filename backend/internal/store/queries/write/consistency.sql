-- The consistency check of the attachments (docs/adr/0059 D4, migration 43):
-- the job's run, tenant by tenant, and an administrator's confirmed removal
-- of the orphans and acceptance of the missing files.

-- name: ListTenantsToCheck :many
-- Every tenant of the installation, read by the job with no tenant set; the
-- policy of migration 43 admits it.
SELECT id, slug FROM tenants ORDER BY id;

-- name: ListTenantAttachmentIDs :many
-- Every attachment of the tenant, a deleted ticket's included: its row names
-- its object until the purge removes both.
SELECT id FROM attachments WHERE tenant_id = sqlc.arg(tenant_id) ORDER BY id;

-- name: ListCheckedAttachments :many
-- What the list of the missing files shows of each: its name, size and type,
-- when it was uploaded, and its ticket's key — for the tenant's
-- administrators, who see every ticket.
-- visibility: exempt (the check names every file of the tenant whose bytes are missing, for its administrators, who see every ticket)
-- deletion: exempt (a deleted ticket's file has its row and its object until the purge removes both)
SELECT a.id, a.file_name, a.size, a.content_type, a.created_at, p.key AS project_key, t.number,
       (t.deleted_at IS NOT NULL)::boolean AS ticket_deleted
FROM attachments a
JOIN tickets t ON t.tenant_id = a.tenant_id AND t.id = a.ticket_id
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
WHERE a.tenant_id = sqlc.arg(tenant_id) AND a.id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY a.id;

-- name: ListAcceptedAttachments :many
-- The attachments whose loss an administrator accepted.
SELECT attachment_id FROM consistency_acceptances WHERE tenant_id = sqlc.arg(tenant_id) ORDER BY attachment_id;

-- name: ForgetWholeAcceptances :execrows
-- The acceptances of the attachments that are no longer missing: a later loss
-- of the same file counts again.
DELETE FROM consistency_acceptances
WHERE tenant_id = sqlc.arg(tenant_id) AND NOT (attachment_id = ANY(sqlc.arg(missing)::uuid[]));

-- name: SaveConsistencyCheck :exec
-- A run's result replaces the tenant's last one under a new id, and with it any
-- removal confirmed for the last one.
INSERT INTO consistency_checks (id, tenant_id, checked_at, dangling, accepted, orphans, orphan_bytes,
                                dangling_items, orphan_items)
VALUES (sqlc.arg(id), sqlc.arg(tenant_id), sqlc.arg(checked_at), sqlc.arg(dangling), sqlc.arg(accepted),
        sqlc.arg(orphans), sqlc.arg(orphan_bytes), sqlc.arg(dangling_items), sqlc.arg(orphan_items))
ON CONFLICT (tenant_id) DO UPDATE
SET id = EXCLUDED.id, checked_at = EXCLUDED.checked_at, dangling = EXCLUDED.dangling, accepted = EXCLUDED.accepted,
    orphans = EXCLUDED.orphans, orphan_bytes = EXCLUDED.orphan_bytes, dangling_items = EXCLUDED.dangling_items,
    orphan_items = EXCLUDED.orphan_items, orphans_removed_at = NULL, orphans_removed_by = NULL,
    orphans_removed = NULL, orphans_kept = NULL;

-- name: GetConsistencyCheckForUpdate :one
-- The tenant's latest result, if it is the one a confirmation names, held for
-- the confirmation's transaction.
SELECT id, dangling, accepted, orphans, dangling_items, orphan_items, orphans_removed_at
FROM consistency_checks
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id)
FOR UPDATE;

-- name: ListAttachmentsAmong :many
-- Which of these ids an attachment of the tenant has now: an orphan that
-- gained metadata since the check is no orphan any more.
SELECT id FROM attachments WHERE tenant_id = sqlc.arg(tenant_id) AND id = ANY(sqlc.arg(ids)::uuid[]) ORDER BY id;

-- name: RecordOrphanRemoval :exec
-- The administrator's confirmed removal of the result's orphans: none is left
-- on the result, and it says how many went and how many were kept.
UPDATE consistency_checks
SET orphans = 0, orphan_bytes = 0, orphan_items = '[]', orphans_removed_at = sqlc.arg(removed_at)::timestamptz,
    orphans_removed_by = sqlc.arg(removed_by)::uuid, orphans_removed = sqlc.arg(removed)::integer,
    orphans_kept = sqlc.arg(kept)::integer
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id);

-- name: AcceptAttachments :execrows
-- The administrator accepts that these attachments' bytes are lost; one whose
-- row is gone, or that is accepted already, is passed over.
INSERT INTO consistency_acceptances (tenant_id, attachment_id, accepted_by)
SELECT a.tenant_id, a.id, sqlc.arg(accepted_by)::uuid
FROM attachments a
WHERE a.tenant_id = sqlc.arg(tenant_id) AND a.id = ANY(sqlc.arg(ids)::uuid[])
ON CONFLICT (tenant_id, attachment_id) DO NOTHING;

-- name: RecordDanglingAcceptance :exec
-- The result after an acceptance: the counts moved and the list marked.
UPDATE consistency_checks
SET dangling = sqlc.arg(dangling), accepted = sqlc.arg(accepted), dangling_items = sqlc.arg(dangling_items)
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id);
